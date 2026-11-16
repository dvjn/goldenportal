//go:build e2e

package index

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx/httpxtest"
	"github.com/dvjn/goldenportal/internal/pgtest"
)

var small = Settings{MinRows: 100, PerBlueprint: 8, Total: 64}

// runsYAML sorts runs by when they started, in the catalog and a table, and
// narrows them to the slow ones in another; nothing else reads status.
const runsYAML = `kind: Blueprint
metadata: {identifier: run, title: Run}
spec:
  properties:
    status: {type: string}
    seconds: {type: number}
    startedAt: {type: string, format: date-time}
  catalog:
    columns: [status, startedAt]
    queries:
      slow: {filter: {property: seconds, op: gt, value: 500}}
    tabs:
      recent:
        widgets:
          latest: {type: table, title: Latest, sort: -startedAt}
          slowest: {type: table, title: Slowest, query: slow}
`

var (
	startedIndex = "(gp_time((properties -> 'startedAt' #>> '{}'))) DESC NULLS LAST, title, identifier"
	secondsIndex = "((CASE WHEN jsonb_typeof(properties -> 'seconds') = 'number' THEN (properties -> 'seconds')::numeric END))"
	// withoutSlow no longer narrows runs by seconds.
	withoutSlow = strings.Replace(runsYAML, "          slowest: {type: table, title: Slowest, query: slow}\n", "", 1)
)

type fixture struct {
	t      *testing.T
	db     *gofrSQL.DB
	source string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := pgtest.New(t)
	return &fixture{t: t, db: db, source: pgtest.Publish(t, db, "connection", "source", "kind: Connection")}
}

func (f *fixture) blueprint(identifier, source string) string {
	f.t.Helper()
	return pgtest.Publish(f.t, f.db, "blueprint", identifier, source)
}

func (f *fixture) republish(identifier, source string) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.t.Context(), `
		UPDATE config_versions v SET source = $2 FROM config_documents d
		WHERE d.id = v.document_id AND d.identifier = $1 AND v.version = d.published_version`, identifier, source); err != nil {
		f.t.Fatal(err)
	}
}

// runs writes n runs of a blueprint, a minute apart, from first on.
func (f *fixture) runs(blueprintID string, first, n int) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.t.Context(), `
		INSERT INTO entities (blueprint_id, identifier, title, connection_id, properties)
		SELECT $1, 'r' || i, 'Run ' || i, $2, jsonb_build_object(
			'status', (ARRAY['success', 'failed'])[i % 2 + 1], 'seconds', i % 600,
			'startedAt', to_char((timestamptz '2026-11-01' + i * interval '1 minute') AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'))
		FROM generate_series($3::int, $4::int) i`, blueprintID, f.source, first, first+n-1); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) reconcile(s Settings) {
	f.t.Helper()
	if err := reconcile(f.t.Context(), f.db, s); err != nil {
		f.t.Fatal(err)
	}
}

// built lists this package's indexes on entities as Postgres has them, by
// name, with "invalid" for one a build left unfinished.
func (f *fixture) built() map[string]string {
	f.t.Helper()
	rows, err := f.db.QueryContext(f.t.Context(), `
		SELECT c.relname, CASE WHEN i.indisvalid THEN pg_get_indexdef(i.indexrelid) ELSE 'invalid' END
		FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
		WHERE i.indrelid = 'entities'::regclass AND c.relname LIKE 'gpx\_%'`)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			f.t.Fatal(err)
		}
		out[name] = def
	}

	return out
}

type recorded struct {
	blueprint, state, problem string
	built                     bool
}

func (f *fixture) recorded() map[string]recorded {
	f.t.Helper()
	rows, err := f.db.QueryContext(f.t.Context(), `SELECT name, blueprint, state, coalesce(error, ''), built_at IS NOT NULL FROM entity_indexes`)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]recorded{}
	for rows.Next() {
		var (
			name string
			r    recorded
		)
		if err := rows.Scan(&name, &r.blueprint, &r.state, &r.problem, &r.built); err != nil {
			f.t.Fatal(err)
		}
		out[name] = r
	}

	return out
}

// reader holds a snapshot open, as a long report would, until it's done.
func (f *fixture) reader() (done func()) {
	f.t.Helper()
	conn, err := f.db.Conn(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := conn.ExecContext(f.t.Context(), `BEGIN ISOLATION LEVEL REPEATABLE READ`); err != nil {
		f.t.Fatal(err)
	}
	var n int
	if err := conn.QueryRowContext(f.t.Context(), `SELECT count(*) FROM entities`).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	var once sync.Once

	return func() {
		once.Do(func() {
			_, _ = conn.ExecContext(context.Background(), `COMMIT`)
			_ = conn.Close()
		})
	}
}

// reconcileDuring reconciles while a reader is open, ending it after wait,
// and says whether the reconcile was still waiting when the reader ended,
// and what it returned.
func (f *fixture) reconcileDuring(s Settings, wait time.Duration) (bool, error) {
	f.t.Helper()
	done := f.reader()
	defer done()
	result := make(chan error, 1)
	go func() { result <- reconcile(context.Background(), f.db, s) }()
	select {
	case err := <-result:
		return false, err
	case <-time.After(wait):
	}
	done()

	return true, <-result
}

// statements keeps what the store runs, to explain it.
type statements struct {
	*gofrSQL.DB
	last string
	args []any
}

func (s *statements) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	s.last, s.args = query, args
	return s.DB.QueryContext(ctx, query, args...)
}

// plan is how Postgres runs the page of the catalog sorted by sort.
func (f *fixture) plan(spec blueprint.Spec, sort string) string {
	f.t.Helper()
	recorder := &statements{DB: f.db}
	if _, err := entity.NewStore(recorder).List(f.t.Context(), entity.Query{
		Blueprint: spec, Blueprints: map[string]blueprint.Spec{spec.Identifier: spec}, Sort: sort, PageSize: 25,
	}); err != nil {
		f.t.Fatal(err)
	}
	rows, err := f.db.QueryContext(f.t.Context(), `EXPLAIN (COSTS OFF) `+recorder.last, recorder.args...)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			f.t.Fatal(err)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func spec(t *testing.T, id, source string) blueprint.Spec {
	t.Helper()
	s := mustParse(t, blueprint.Parse, source)
	s.ID = id

	return s
}

func TestReconcileBuildsWhatConfigurationSortsByAndDropsTheRest(t *testing.T) {
	f := newFixture(t)
	id := f.blueprint("run", runsYAML)
	f.runs(id, 1, 3000)
	f.reconcile(small)

	started, seconds := name(id, startedIndex), name(id, secondsIndex)
	built := f.built()
	if len(built) != 2 || built[seconds] == "" {
		t.Fatalf("built = %v, want startedAt's and seconds'", built)
	}
	if def := built[started]; !strings.Contains(def, "gp_time(") || !strings.Contains(def, "DESC NULLS LAST, title, identifier) WHERE (blueprint_id = '"+id+"'::uuid)") {
		t.Errorf("startedAt's index = %q", def)
	}
	if r := f.recorded()[started]; r.blueprint != "run" || r.state != stateBuilt || !r.built {
		t.Errorf("startedAt's record = %+v", r)
	}
	// The catalog's page reads the index in order and stops at its limit.
	plan := f.plan(spec(t, id, runsYAML), "-startedAt")
	if !strings.Contains(plan, "Index Scan using "+started) || strings.Contains(plan, "Sort Key") {
		t.Errorf("the page sorted by -startedAt runs as\n%s", plan)
	}

	f.republish("run", withoutSlow)
	f.reconcile(small)
	if built := f.built(); len(built) != 1 || built[started] == "" {
		t.Errorf("built = %v, want only startedAt's", built)
	}
	if got := f.recorded(); len(got) != 1 {
		t.Errorf("recorded = %v, want only startedAt's", got)
	}
}

func TestReconcileDropsUnfinishedBuilds(t *testing.T) {
	f := newFixture(t)
	id := f.blueprint("run", withoutSlow)
	f.runs(id, 1, 200)
	// A unique index on a value runs share fails after it's in the catalog,
	// as a concurrent build that's interrupted is.
	if _, err := f.db.ExecContext(t.Context(), `CREATE UNIQUE INDEX CONCURRENTLY gpx_unfinished ON entities ((properties -> 'status'))`); err == nil {
		t.Fatal("the unique index was built")
	}
	if built := f.built(); built["gpx_unfinished"] != "invalid" {
		t.Fatalf("built = %v, want gpx_unfinished invalid", built)
	}
	f.reconcile(small)
	if built := f.built(); len(built) != 1 || built["gpx_unfinished"] != "" {
		t.Errorf("built = %v, want startedAt's alone", built)
	}
}

// TestReconcileWaitsOutLongReads builds while a long read holds its
// snapshot: the build waits for it rather than failing, which doesn't hold
// up writes.
func TestReconcileWaitsOutLongReads(t *testing.T) {
	f := newFixture(t)
	id := f.blueprint("run", runsYAML)
	f.runs(id, 1, 200)
	waited, err := f.reconcileDuring(small, time.Second)
	if err != nil || !waited {
		t.Fatalf("reconcile = %v, waited %t; want it to wait for the read and succeed", err, waited)
	}
	built := f.built()
	for _, index := range []string{startedIndex, secondsIndex} {
		if def := built[name(id, index)]; def == "" || def == "invalid" {
			t.Errorf("%s = %q", index, def)
		}
		if r := f.recorded()[name(id, index)]; r.state != stateBuilt {
			t.Errorf("%s's record = %+v", index, r)
		}
	}
}

// TestReconcileRecordsWhatTimesOut builds and drops while a read outlasts
// their bound: a build that fails is still dropped once the read ends, and
// an index whose drop failed stays recorded until it's gone.
func TestReconcileRecordsWhatTimesOut(t *testing.T) {
	f := newFixture(t)
	id := f.blueprint("run", withoutSlow)
	f.runs(id, 1, 200)
	previous := buildTimeout
	buildTimeout = 300 * time.Millisecond
	t.Cleanup(func() { buildTimeout = previous })

	waited, err := f.reconcileDuring(small, 2*time.Second)
	if !errors.Is(err, errIncomplete) || !waited {
		t.Fatalf("reconcile = %v, waited %t; want it incomplete, having waited for the read to drop what it began", err, waited)
	}
	started := name(id, startedIndex)
	if built := f.built(); len(built) != 0 {
		t.Errorf("built = %v, want the failed build dropped", built)
	}
	if r := f.recorded()[started]; r.state != stateFailed || !strings.Contains(r.problem, "statement timeout") || r.built {
		t.Errorf("startedAt's record = %+v, want failed for the timeout", r)
	}
	f.reconcile(small)
	if r := f.recorded()[started]; r.state != stateBuilt || r.problem != "" {
		t.Errorf("startedAt's record = %+v, want built next time", r)
	}

	f.republish("run", strings.Replace(withoutSlow, "-startedAt", "-seconds", 1))
	if _, err := f.reconcileDuring(small, 2*time.Second); !errors.Is(err, errIncomplete) {
		t.Fatalf("reconcile = %v, want it incomplete", err)
	}
	// A concurrent drop marks the index invalid before it waits.
	if def := f.built()[started]; def == "" {
		t.Error("startedAt's index is gone, but its drop failed")
	}
	if r := f.recorded()[started]; r.state != stateDropping || r.problem == "" {
		t.Errorf("startedAt's record = %+v, want its failed drop", r)
	}
	f.reconcile(small)
	if _, ok := f.built()[started]; ok {
		t.Error("startedAt's index wasn't dropped next time")
	}
	if _, ok := f.recorded()[started]; ok {
		t.Error("startedAt's index is still recorded after it was dropped")
	}
}

func TestReconcileKeepsToSizeAndCaps(t *testing.T) {
	f := newFixture(t)
	big := f.blueprint("run", runsYAML)
	little := f.blueprint("task", strings.Replace(runsYAML, "identifier: run", "identifier: task", 1))
	f.runs(big, 1, 100)
	f.runs(little, 1, 99)
	f.reconcile(Settings{MinRows: 100, PerBlueprint: 1, Total: 64})

	built, recorded := f.built(), f.recorded()
	states := map[string]int{}
	for name, r := range recorded {
		states[r.blueprint+" "+r.state]++
		if r.state == stateSkipped && (!strings.Contains(r.problem, "the most a blueprint gets") || built[name] != "") {
			t.Errorf("%s = %+v, built %q", name, r, built[name])
		}
	}
	if len(built) != 1 || states["run built"] != 1 || states["run skipped"] != 1 || len(states) != 2 {
		t.Errorf("built %v, recorded %v", built, states)
	}

	// A blueprint that had indexes keeps them until it's half the size.
	keep := func(n int) {
		t.Helper()
		if _, err := f.db.ExecContext(t.Context(), `DELETE FROM entities WHERE blueprint_id = $1 AND substr(identifier, 2)::int > $2`, big, n); err != nil {
			t.Fatal(err)
		}
	}
	keep(60)
	f.reconcile(small)
	if built := f.built(); len(built) != 2 {
		t.Errorf("built = %v, want run's at 60 runs", built)
	}
	keep(40)
	f.reconcile(small)
	if built := f.built(); len(built) != 0 {
		t.Errorf("built = %v, want none at 40 runs", built)
	}
}

func TestReconcileTurnedOffDropsEveryIndex(t *testing.T) {
	f := newFixture(t)
	id := f.blueprint("run", runsYAML)
	f.runs(id, 1, 100)
	f.reconcile(small)
	if len(f.built()) != 2 {
		t.Fatalf("built = %v", f.built())
	}
	f.reconcile(Settings{MinRows: 100, PerBlueprint: 8, Total: 0})
	if built, recorded := f.built(), f.recorded(); len(built) != 0 || len(recorded) != 0 {
		t.Errorf("built %v and recorded %v with indexes off", built, recorded)
	}
}

func TestReconcileLeavesTheWorkToWhoeverHasIt(t *testing.T) {
	f := newFixture(t)
	id := f.blueprint("run", runsYAML)
	f.runs(id, 1, 100)
	holder, err := f.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if _, err := holder.ExecContext(t.Context(), `SELECT pg_advisory_lock(`+lockKey+`)`); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	f.reconcile(small)
	if built := f.built(); len(built) != 0 || time.Since(started) > 2*time.Second {
		t.Errorf("built %v in %s while another held the lock", built, time.Since(started))
	}
}

var (
	reindex       func(*gofr.Context)
	registerKinds = sync.OnceFunc(func() {
		reindex = Register(httpxtest.New(), small, DefaultSchedule)
		blueprint.Register(httpxtest.New(), nil, func(ctx *gofr.Context, _ string) { reindex(ctx) })
	})
)

// TestPublishDoesNotWaitForIndexes publishes as a builder would: the
// publish is done before indexes are reconciled, which happens after it.
func TestPublishDoesNotWaitForIndexes(t *testing.T) {
	registerKinds()
	f := newFixture(t)
	var queued []func()
	previous := background
	background = func(work func()) { queued = append(queued, work) }
	t.Cleanup(func() { background = previous })

	principal := auth.WithPrincipal(t.Context(), auth.Principal{Role: auth.RoleBuilder})
	logger := logging.NewLogger(logging.FATAL)
	ctx := &gofr.Context{
		Context: principal, Container: &container.Container{Logger: logger, SQL: f.db},
		ContextLogger: logging.ContextLoggerFor(principal, logger),
	}
	documents, _ := configdoc.DocumentsOf("blueprints")
	publish := func(source string) {
		t.Helper()
		if _, err := documents.SaveDraft(ctx, "run", source); err != nil {
			t.Fatal(err)
		}
		if _, err := documents.Publish(ctx, "run", true); err != nil {
			t.Fatal(err)
		}
	}
	publish(withoutSlow)
	var id string
	if err := f.db.QueryRowContext(t.Context(), `SELECT id FROM config_documents WHERE identifier = 'run'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	f.runs(id, 1, 100)
	run := func() {
		work := queued
		queued = nil
		for _, w := range work {
			w()
		}
	}
	run()

	publish(runsYAML)
	if len(queued) != 1 || len(f.built()) != 1 {
		t.Fatalf("%d reconciles queued and %v built when the publish returned, want one queued and one built", len(queued), f.built())
	}
	// Publishing again meanwhile reconciles again after, rather than twice at once.
	publish(strings.Replace(runsYAML, "title: Run}", "title: Runs}", 1))
	if len(queued) != 1 {
		t.Errorf("%d reconciles queued", len(queued))
	}
	run()
	if len(f.built()) != 2 {
		t.Errorf("built = %v", f.built())
	}
}
