package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/lib/pq"
	"gofr.dev/pkg/gofr"
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// What started a run.
const (
	TriggerPush     = "push"
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

// SyncSchedule is how often the scheduler looks for pull integrations due
// a sync, as a cron expression.
const SyncSchedule = "* * * * *"

// pullTimeout bounds how long one sync may spend fetching from a provider.
const pullTimeout = 10 * time.Minute

var (
	ErrSyncing  = errors.New("a sync of this integration is already running")
	ErrNotPull  = errors.New("only integrations that pull their data can sync")
	ErrUnsynced = errors.New("publish the integration before syncing it")
)

// TxDB is a database that can start transactions.
type TxDB interface {
	configdoc.DB
	Begin() (*gofrSQL.Tx, error)
}

// Sync pulls a published pull integration's data and writes it as a full
// snapshot: entities the integration wrote before that aren't in the data
// any more are deleted. Items that fail validation still count as present,
// so a bad value never deletes an entity.
func Sync(ctx context.Context, db TxDB, identifier, trigger string) (*Run, error) {
	started := time.Now().UTC()
	store := configdoc.NewStore(db, Name)
	record, err := store.Get(ctx, identifier)
	if err != nil {
		return nil, err
	}
	if !record.Published() {
		return nil, ErrUnsynced
	}
	spec, _ := Parse(record.PublishedSource)
	c, _ := connector(spec.Connector.Type)
	pull := c.pull()
	if pull == nil {
		return nil, ErrNotPull
	}
	id, err := store.PublishedID(ctx, identifier)
	if err != nil {
		return nil, err
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Take the lock before fetching so overlapping syncs fail fast instead of
	// each spending a full provider fetch.
	var locked bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('integration-sync:' || $1))`,
		id).Scan(&locked); err != nil {
		return nil, err
	}
	if !locked {
		return nil, ErrSyncing
	}

	var resources []string
	for _, m := range spec.Mappings {
		if !slices.Contains(resources, m.Resource) {
			resources = append(resources, m.Resource)
		}
	}
	// The fetch runs inside the transaction, so bound it to keep a slow
	// provider from holding a connection indefinitely.
	pullCtx, cancel := context.WithTimeout(ctx, pullTimeout)
	pulled, err := pull(pullCtx, tx, spec, resources)
	cancel()
	var items []Item
	if err == nil {
		var data any
		if data, err = toJSONValue(pulled); err == nil {
			items, err = spec.Map(data)
		}
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, recordFailure(ctx, db, id, &Run{StartedAt: started, Trigger: trigger, Errors: []RunError{}},
			fmt.Errorf("pull %s: %w", identifier, err))
	}

	run := &Run{StartedAt: started, Trigger: trigger, Received: len(items), Errors: []RunError{}}
	seen, err := write(ctx, tx, id, items, run)
	if err != nil {
		return nil, err
	}

	var keep []string
	for bp, identifiers := range seen {
		for _, identifier := range identifiers {
			keep = append(keep, bp+"/"+identifier)
		}
	}
	result, err := tx.ExecContext(ctx, `
		DELETE FROM entities e USING config_documents b
		WHERE b.id = e.blueprint_id AND e.integration_id = $1
			AND NOT ((b.identifier || '/' || e.identifier) = ANY($2))`, id, pq.Array(keep))
	if err != nil {
		return nil, err
	}
	deleted, _ := result.RowsAffected()
	run.Deleted = int(deleted)

	if err := NewStore(tx).RecordRun(ctx, id, "", run); err != nil {
		return nil, err
	}

	return run, tx.Commit()
}

// recordFailure records a sync that fetched nothing as a run, so the failure
// shows with the integration's runs and the scheduler waits out the interval
// instead of retrying, and burning provider rate limits, every minute.
func recordFailure(ctx context.Context, db configdoc.DB, integrationID string, run *Run, err error) error {
	run.Error = err.Error()
	if recordErr := NewStore(db).RecordRun(context.WithoutCancel(ctx), integrationID, "", run); recordErr != nil {
		return errors.Join(err, recordErr)
	}

	return err
}

// syncAll syncs the published pull integrations whose interval has passed
// since their last sync. It runs every minute.
func syncAll(ctx *gofr.Context) {
	records, err := configdoc.NewStore(ctx.SQL, Name).List(ctx)
	if err != nil {
		ctx.Errorf("list integrations to sync: %v", err)
		return
	}
	for _, record := range records {
		if !record.Published() {
			continue
		}
		spec, _ := Parse(record.PublishedSource)
		if c, _ := connector(spec.Connector.Type); c.pull() == nil {
			continue
		}
		last, err := NewStore(ctx.SQL).LastSync(ctx, record.Identifier)
		if err != nil {
			ctx.Errorf("find the last sync of %s: %v", record.Identifier, err)
			continue
		}
		// A little slack, so a one-minute interval isn't skipped by jitter in the scheduler.
		if !last.IsZero() && time.Since(last) < spec.SyncEvery()-5*time.Second {
			continue
		}
		if _, err := Sync(ctx, ctx.SQL, record.Identifier, TriggerSchedule); err != nil && !errors.Is(err, ErrSyncing) {
			ctx.Errorf("sync %s: %v", record.Identifier, err)
		}
	}
}

// syncNow syncs one integration straight away.
func syncNow(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleBuilder); err != nil {
		return nil, err
	}
	_, identifier, err := integrationID(ctx)
	if err != nil {
		return nil, err
	}

	run, err := Sync(ctx, ctx.SQL, identifier, TriggerManual)
	switch {
	case errors.Is(err, ErrSyncing), errors.Is(err, ErrNotPull), errors.Is(err, ErrUnsynced):
		return nil, httpx.Error{Code: http.StatusConflict, Message: err.Error()}
	case err != nil:
		return nil, err
	}

	return run, nil
}

// toJSONValue turns a struct into the maps and slices jq works on.
func toJSONValue(v any) (any, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	err = json.Unmarshal(encoded, &out)

	return out, err
}
