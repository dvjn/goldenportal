package index

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/lib/pq"
	"go.opentelemetry.io/otel/attribute"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/config"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/telemetry"
)

// The settings, beside a schedule for reconciling between publishes, as
// blueprints grow past MinRows by ingestion alone.
const (
	MinRowsEnv      = "ENTITY_INDEX_MIN_ROWS"
	PerBlueprintEnv = "ENTITY_INDEX_MAX_PER_BLUEPRINT"
	TotalEnv        = "ENTITY_INDEX_MAX"
	ScheduleEnv     = "ENTITY_INDEX_SCHEDULE"
	DefaultSchedule = "41 * * * *"
	lockKey         = "hashtext('entity-indexes')"
)

// buildTimeout bounds a build or a drop. Each waits for the transactions
// under way to end, however long they take, but doesn't hold up writes
// meanwhile, so it waits without a lock timeout.
var buildTimeout = time.Hour

// What became of an index, as entity_indexes records it. Dropping is one
// that's still there because its drop failed.
const (
	stateBuilt    = "built"
	stateBuilding = "building"
	stateFailed   = "failed"
	stateSkipped  = "skipped"
	stateDropping = "dropping"
)

// errIncomplete is a reconcile that went through but failed to build or
// drop some indexes, which entity_indexes records.
var errIncomplete = errors.New("indexes not all built or dropped")

// SettingsFrom reads the settings: MinRows a whole number above 0, and the
// caps whole numbers, 0 turning indexes off.
func SettingsFrom(cfg config.Config) (Settings, error) {
	s := defaults
	for _, setting := range []struct {
		env    string
		target *int
		least  int
	}{{MinRowsEnv, &s.MinRows, 1}, {PerBlueprintEnv, &s.PerBlueprint, 0}, {TotalEnv, &s.Total, 0}} {
		raw := cfg.Get(setting.env)
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < setting.least {
			return Settings{}, fmt.Errorf("%s=%q isn't a whole number of at least %d", setting.env, raw, setting.least)
		}
		*setting.target = n
	}

	return s, nil
}

// Register reconciles indexes as the server starts and on schedule, a cron
// expression, and gives what reconciles them after a publish, for the
// kinds indexes are derived from to call once one commits. A delete is
// left to the schedule.
func Register(app httpx.Routes, s Settings, schedule string) func(*gofr.Context) {
	soon := func(ctx *gofr.Context) {
		db, ok := ctx.SQL.(database)
		if !ok {
			telemetry.Warn(ctx, "indexes not reconciled: the database can't lend a connection", nil)
			return
		}
		reconcileSoon(ctx, db, s)
	}
	app.OnStart(func(ctx *gofr.Context) error {
		soon(ctx)
		return nil
	})
	app.AddCronJob(schedule, "reconcile-indexes", soon)

	return soon
}

// database can lend one of its connections, which session settings and
// the lock stay on.
type database interface {
	configdoc.DB
	Conn(ctx context.Context) (*sql.Conn, error)
}

// running is whether a reconcile is under way in this process, and again
// whether one was asked for meanwhile, which may see what it didn't.
var running struct {
	sync.Mutex
	busy, again bool
}

// background runs work outside the request or start-up that asked for
// it; tests run it in place.
var background = func(work func()) { go work() }

// reconcileSoon reconciles outside ctx, so a publish never waits for an
// index or fails for one. Failures are logged and left to the next run.
func reconcileSoon(ctx context.Context, db database, s Settings) {
	running.Lock()
	if running.busy {
		running.again = true
		running.Unlock()
		return
	}
	running.busy = true
	running.Unlock()

	detached := telemetry.Resume(context.Background(), ctx)
	background(func() {
		for {
			reconcileSafely(detached, db, s)
			running.Lock()
			if !running.again {
				running.busy = false
				running.Unlock()
				return
			}
			running.again = false
			running.Unlock()
		}
	})
}

func reconcileSafely(ctx context.Context, db database, s Settings) {
	defer func() {
		if r := recover(); r != nil {
			telemetry.Error(ctx, "indexes not reconciled", fmt.Errorf("panic: %v", r))
		}
	}()
	// reconcile logs how it went.
	_ = reconcile(ctx, db, s)
}

// reconcile builds the indexes published configuration asks for and drops
// the rest of this package's, one at a time, on one connection outside any
// transaction, as concurrent builds and drops need. Only one replica does
// at a time; another finds the lock taken and leaves it the work. Replicas
// of two versions deriving different indexes would undo each other's work
// until the older ones stop, so a deploy may build an index twice.
func reconcile(ctx context.Context, db database, s Settings) (err error) {
	ctx, span := telemetry.Start(ctx, "reconcile indexes")
	started := time.Now()
	r := reconciler{}
	defer func() {
		attrs := []attribute.KeyValue{
			telemetry.Created.Int(r.built), telemetry.Deleted.Int(r.dropped), telemetry.Failed.Int(r.failed),
			telemetry.Duration.Int64(time.Since(started).Milliseconds()),
		}
		span.SetAttributes(attrs...)
		switch {
		case errors.Is(err, errIncomplete):
			telemetry.Warn(ctx, "indexes reconciled, but not all built or dropped", nil, attrs...)
		case err != nil:
			telemetry.Error(ctx, "indexes not reconciled", err, attrs...)
		case r.locked:
			telemetry.Info(ctx, "indexes reconciled", attrs...)
		}
		telemetry.End(span, err)
	}()

	if r.conn, err = db.Conn(ctx); err != nil {
		return err
	}
	defer r.conn.Close()
	if err := r.conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(`+lockKey+`)`).Scan(&r.locked); err != nil || !r.locked {
		return err
	}
	defer r.release(context.WithoutCancel(ctx))
	if _, err := r.conn.ExecContext(ctx, `SET lock_timeout = 0`); err != nil {
		return err
	}
	if err := r.bound(ctx, buildTimeout); err != nil {
		return err
	}

	var build, skipped []planned
	if !s.off() {
		cfg, err := loadConfiguration(ctx, db)
		if err != nil {
			return err
		}
		ids, err := r.large(ctx, cfg.Blueprints, s.MinRows)
		if err != nil {
			return err
		}
		build, skipped = plan(derive(cfg), ids, s)
	}
	existing, err := r.existing(ctx)
	if err != nil {
		return err
	}
	if r.previous, err = r.states(ctx); err != nil {
		return err
	}
	wanted, kept := map[string]bool{}, []string{}
	for _, ix := range build {
		wanted[ix.Name] = true
	}
	for _, ix := range slices.Concat(build, skipped) {
		kept = append(kept, ix.Name)
	}
	for _, name := range slices.Sorted(maps.Keys(existing)) {
		if existing[name] && wanted[name] {
			continue
		}
		if err := r.drop(ctx, name); err != nil {
			return err
		}
	}
	for _, ix := range build {
		if existing[ix.Name] {
			err = r.record(ctx, ix, stateBuilt, "")
		} else {
			err = r.build(ctx, ix)
		}
		if err != nil {
			return err
		}
	}
	// One look at the table gives the planner every new key's values.
	if r.built > 0 {
		analyzing, span := telemetry.Start(ctx, "analyze entities")
		_, err := r.conn.ExecContext(analyzing, `ANALYZE entities`)
		telemetry.End(span, err)
		if err != nil {
			return err
		}
	}
	for _, ix := range skipped {
		telemetry.Info(ctx, "index left out: "+ix.Skipped, telemetry.Blueprint.String(ix.Blueprint), telemetry.IndexKey.String(ix.Expression))
		if err := r.record(ctx, ix, stateSkipped, ix.Skipped); err != nil {
			return err
		}
	}
	if _, err := r.conn.ExecContext(ctx, `DELETE FROM entity_indexes WHERE name <> ALL(coalesce($1::text[], '{}'))`, pq.Array(slices.Concat(kept, r.remaining))); err != nil {
		return err
	}
	if r.failed > 0 {
		return fmt.Errorf("%w: %d failed", errIncomplete, r.failed)
	}

	return nil
}

type reconciler struct {
	conn   *sql.Conn
	locked bool
	// previous is each recorded index's state before this run, and
	// remaining the indexes whose drop failed, which are still there.
	previous               map[string]string
	remaining              []string
	built, dropped, failed int
}

// bound sets how long each statement may take, 0 for as long as it needs.
func (r *reconciler) bound(ctx context.Context, d time.Duration) error {
	_, err := r.conn.ExecContext(ctx, `SET statement_timeout = `+strconv.FormatInt(d.Milliseconds(), 10))
	return err
}

// release gives up the lock and the settings, or else the connection
// itself, which mustn't go back to the pool holding either.
func (r *reconciler) release(ctx context.Context) {
	var unlocked bool
	err := r.conn.QueryRowContext(ctx, `SELECT pg_advisory_unlock(`+lockKey+`)`).Scan(&unlocked)
	for _, reset := range []string{`RESET lock_timeout`, `RESET statement_timeout`} {
		if err == nil {
			_, err = r.conn.ExecContext(ctx, reset)
		}
	}
	if err != nil || !unlocked {
		telemetry.Warn(ctx, "the connection that reconciled indexes is closed rather than reused", err)
		_ = r.conn.Raw(func(any) error { return driver.ErrBadConn })
	}
}

// large names the blueprints with at least minRows entities, or half as
// many for one that had indexes, so a blueprint near the line doesn't
// gain and lose them by turns, with their IDs, by identifier.
func (r *reconciler) large(ctx context.Context, specs map[string]blueprint.Spec, minRows int) (map[string]string, error) {
	var identifiers, ids []string
	for _, identifier := range slices.Sorted(maps.Keys(specs)) {
		if id := specs[identifier].ID; id != "" {
			identifiers, ids = append(identifiers, identifier), append(ids, id)
		}
	}
	rows, err := r.conn.QueryContext(ctx, `
		SELECT b.identifier, b.id, n.count, EXISTS (SELECT 1 FROM entity_indexes x WHERE x.blueprint = b.identifier AND x.state = $4)
		FROM unnest($1::text[], $2::uuid[]) b(identifier, id),
			LATERAL (SELECT count(*) FROM (SELECT 1 FROM entities e WHERE e.blueprint_id = b.id LIMIT $3) t) n`,
		pq.Array(identifiers), pq.Array(ids), minRows, stateBuilt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	large := map[string]string{}
	for rows.Next() {
		var (
			identifier, id string
			count          int
			had            bool
		)
		if err := rows.Scan(&identifier, &id, &count, &had); err != nil {
			return nil, err
		}
		if count >= minRows || (had && count >= minRows/2) {
			large[identifier] = id
		}
	}

	return large, rows.Err()
}

// existing are this package's indexes on entities, by name, and whether
// each is valid: one a concurrent build didn't finish isn't, and is left
// behind.
func (r *reconciler) existing(ctx context.Context) (map[string]bool, error) {
	rows, err := r.conn.QueryContext(ctx, `
		SELECT c.relname, i.indisvalid FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		WHERE i.indrelid = 'entities'::regclass AND starts_with(c.relname, $1)`, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	existing := map[string]bool{}
	for rows.Next() {
		var (
			name  string
			valid bool
		)
		if err := rows.Scan(&name, &valid); err != nil {
			return nil, err
		}
		existing[name] = valid
	}

	return existing, rows.Err()
}

func (r *reconciler) states(ctx context.Context) (map[string]string, error) {
	rows, err := r.conn.QueryContext(ctx, `SELECT name, state FROM entity_indexes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := map[string]string{}
	for rows.Next() {
		var name, state string
		if err := rows.Scan(&name, &state); err != nil {
			return nil, err
		}
		states[name] = state
	}

	return states, rows.Err()
}

// failure logs a build or drop that failed, as an error when the last run
// failed on the same index too.
func (r *reconciler) failure(ctx context.Context, msg, name string, err error, attrs ...attribute.KeyValue) {
	r.failed++
	attrs = append(attrs, telemetry.Index.String(name))
	if state := r.previous[name]; state == stateFailed || state == stateDropping {
		telemetry.Error(ctx, msg+", again", err, attrs...)
		return
	}
	telemetry.Warn(ctx, msg, err, attrs...)
}

// build builds an index. A failed build leaves an invalid index behind,
// which writes keep up to date, so it's dropped however long that waits
// for, and recorded, to be tried again next time.
func (r *reconciler) build(ctx context.Context, ix planned) error {
	if err := r.record(ctx, ix, stateBuilding, ""); err != nil {
		return err
	}
	attrs := []attribute.KeyValue{telemetry.Blueprint.String(ix.Blueprint), telemetry.IndexKey.String(ix.Expression)}
	building, span := telemetry.Start(ctx, "build index", append(attrs, telemetry.Index.String(ix.Name))...)
	_, err := r.conn.ExecContext(building, ix.create())
	telemetry.End(span, err)
	if err == nil {
		r.built++
		return r.record(ctx, ix, stateBuilt, "")
	}
	r.failure(ctx, "index not built", ix.Name, err, attrs...)
	problem := err.Error()
	if err := r.bound(ctx, 0); err != nil {
		return err
	}
	if dropErr := r.dropIndex(ctx, ix.Name); dropErr != nil {
		problem += "; its unfinished index wasn't dropped either: " + dropErr.Error()
		r.remaining = append(r.remaining, ix.Name)
	}
	if err := r.bound(ctx, buildTimeout); err != nil {
		return err
	}

	return r.record(ctx, ix, stateFailed, problem)
}

// drop drops an index no longer wanted, or left unfinished. One that can't
// be dropped now stays recorded, and is tried again next time.
func (r *reconciler) drop(ctx context.Context, name string) error {
	problem := r.dropIndex(ctx, name)
	if problem == nil {
		r.dropped++
		return nil
	}
	r.failure(ctx, "index not dropped", name, problem)
	r.remaining = append(r.remaining, name)
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO entity_indexes (name, blueprint, expression, reasons, state, error) VALUES ($1, '', '', '{}', $2, $3)
		ON CONFLICT (name) DO UPDATE SET state = EXCLUDED.state, error = EXCLUDED.error, built_at = NULL`,
		name, stateDropping, problem.Error())

	return err
}

func (r *reconciler) dropIndex(ctx context.Context, name string) error {
	dropping, span := telemetry.Start(ctx, "drop index", telemetry.Index.String(name))
	_, err := r.conn.ExecContext(dropping, `DROP INDEX CONCURRENTLY IF EXISTS `+pq.QuoteIdentifier(name))
	telemetry.End(span, err)

	return err
}

// record keeps what became of an index, for operators. built_at is when
// it was first found built.
func (r *reconciler) record(ctx context.Context, ix planned, state, problem string) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO entity_indexes (name, blueprint, expression, reasons, state, error, built_at)
		VALUES ($1, $2, $3, $4, $5, nullif($6, ''), CASE WHEN $5 = '`+stateBuilt+`' THEN now() END)
		ON CONFLICT (name) DO UPDATE SET blueprint = EXCLUDED.blueprint, expression = EXCLUDED.expression,
			reasons = EXCLUDED.reasons, state = EXCLUDED.state, error = EXCLUDED.error,
			built_at = CASE WHEN EXCLUDED.state = '`+stateBuilt+`' THEN coalesce(entity_indexes.built_at, now()) END`,
		ix.Name, ix.Blueprint, ix.Expression, pq.Array(ix.Reasons), state, problem)

	return err
}
