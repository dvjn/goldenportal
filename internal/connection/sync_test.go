package connection

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/httpx"
)

const (
	startRunSQL = `INSERT INTO ingestion_runs (connection_id, trigger, mode, started_at, received, created, updated,
	unchanged, deleted, failed, action_run_id, finished_at) VALUES ($1, $2, $3, $4, 0, 0, 0, 0, 0, 0, $5, NULL) RETURNING id`
	progressSQL = `UPDATE ingestion_runs SET received = $2, created = $3, updated = $4, unchanged = $5, deleted = $6, failed = $7
	WHERE id = $1`
	finishRunSQL = `UPDATE ingestion_runs SET received = $2, created = $3, updated = $4, unchanged = $5, deleted = $6, failed = $7,
	errors = $8, error = $9, warning = $10, failed_scopes = $11, read_since = $12, finished_at = now() WHERE id = $1 RETURNING finished_at`
)

// TestSyncRecordsFailedFetch checks a failed fetch keeps the entities and is
// recorded as a run, so the scheduler waits out the interval.
func TestSyncRecordsFailedFetch(t *testing.T) {
	shortenRetries(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	db, mock := mockSyncStart(t, server.URL)
	expectRunStart(mock, TriggerSchedule, ModeComplete, nil)
	// No DELETE is expected: nothing was fetched.
	expectEarlierRuns(mock)
	mock.ExpectQuery(finishRunSQL).
		WithArgs(int64(1), 0, 0, 0, 0, 0, 0, []byte("[]"), sqlmock.AnyArg(), "", []byte("[]"), []byte("{}")).
		WillReturnRows(sqlmock.NewRows([]string{"finished_at"}).AddRow(time.Now()))
	mock.ExpectRollback()

	_, err := Sync(context.Background(), db, "source", TriggerSchedule, "")
	if err == nil || !strings.Contains(err.Error(), "answered 503") {
		t.Fatalf("Sync() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestSyncWithoutIdentifierDeletesNothing checks an item without an
// identifier keeps every entity of its blueprint, since it could be any of
// them.
func TestSyncWithoutIdentifierDeletesNothing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"deployments": [{"id": 1}, {"name": "no id"}]}`))
	}))
	defer server.Close()
	db, mock := mockSyncStart(t, server.URL)
	expectRunStart(mock, TriggerSchedule, ModeComplete, nil)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT ` + recordColumnsSQL + ` WHERE b.kind = $1 ORDER BY b.identifier`).
		WillReturnRows(sqlmock.NewRows(recordColumnNames))
	// The batch's counts are written with it, so a running sync shows them.
	mock.ExpectExec(progressSQL).WithArgs(int64(1), 2, 0, 0, 0, 0, 2).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	// No DELETE is expected.
	var errorsJSON []byte
	expectEarlierRuns(mock)
	mock.ExpectQuery(finishRunSQL).
		WithArgs(int64(1), 2, 0, 0, 0, 0, 2, capture{&errorsJSON}, "", "", []byte("[]"), []byte("{}")).
		WillReturnRows(sqlmock.NewRows([]string{"finished_at"}).AddRow(time.Now()))
	mock.ExpectCommit()

	run, err := Sync(context.Background(), db, "source", TriggerSchedule, "")
	if err != nil {
		t.Fatal(err)
	}
	if run.Deleted != 0 || !strings.Contains(string(errorsJSON), "so this run deleted no deployment entities") {
		t.Fatalf("deleted %d, errors %s", run.Deleted, errorsJSON)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestSyncForAction checks an action's sync runs the mode it asks for and is
// recorded against the action run, and that it refuses a resource the
// connection doesn't pull before fetching anything.
func TestSyncForAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"deployments": []}`))
	}))
	defer server.Close()
	db, mock := mockSyncStart(t, server.URL)
	expectRunStart(mock, TriggerAction, ModeIncremental, "action-run")
	expectEarlierRuns(mock)
	mock.ExpectQuery(finishRunSQL).
		WithArgs(int64(1), 0, 0, 0, 0, 0, 0, []byte("[]"), "", "", []byte("[]"), []byte("{}")).
		WillReturnRows(sqlmock.NewRows([]string{"finished_at"}).AddRow(time.Now()))
	mock.ExpectCommit()

	// The step waits for the outcome, so the run it gets has finished.
	run, err := SyncForAction(context.Background(), db, "source", "action-run", ModeIncremental, []string{"deployments"})
	if err != nil || run.Trigger != TriggerAction || run.ActionRun != "action-run" || run.FinishedAt == nil {
		t.Fatalf("SyncForAction() = %+v, %v", run, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	db, mock = mockSyncStart(t, server.URL)
	mock.ExpectRollback()
	_, err = SyncForAction(context.Background(), db, "source", "action-run", ModeIncremental, []string{"builds"})
	if err == nil || !strings.Contains(err.Error(), "doesn't pull a resource called builds") {
		t.Fatalf("SyncForAction(builds) error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestSyncNowAnswersWhileItRuns checks Sync Now answers with the run as
// running, and the sync carries on after the request ends to record how it
// ended.
func TestSyncNowAnswersWhileItRuns(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, _ = w.Write([]byte(`{"deployments": []}`))
	}))
	defer server.Close()
	var finished sync.WaitGroup
	waitForBackground(t, func(finish func()) { finished.Go(finish) })
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	expectConnectionID(mock)
	expectSyncStart(mock, server.URL, true)
	expectRunStart(mock, TriggerManual, ModeIncremental, nil)
	// An empty error says the sync didn't stop when the request did.
	expectEarlierRuns(mock)
	mock.ExpectQuery(finishRunSQL).
		WithArgs(int64(1), 0, 0, 0, 0, 0, 0, []byte("[]"), "", "", []byte("[]"), []byte("{}")).
		WillReturnRows(sqlmock.NewRows([]string{"finished_at"}).AddRow(time.Now()))
	mock.ExpectCommit()

	ctx, cancel := builderContext(db)
	run, err := SyncNow(ctx, "source", ModeIncremental)
	cancel()
	if err != nil || run.ID != 1 || run.FinishedAt != nil {
		t.Fatalf("SyncNow() = %+v, %v", run, err)
	}
	close(release)
	finished.Wait()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestSyncNowWhileRunning checks a sync started while another holds the lock
// is refused at once, without recording a run.
func TestSyncNowWhileRunning(t *testing.T) {
	waitForBackground(t, func(func()) { t.Error("the refused sync ran") })
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	expectConnectionID(mock)
	expectSyncStart(mock, "https://tracker.example.com", false)
	mock.ExpectRollback()

	ctx, cancel := builderContext(db)
	defer cancel()
	_, err := SyncNow(ctx, "source", "")
	var refused httpx.Error
	if !errors.As(err, &refused) || refused.Code != http.StatusConflict {
		t.Fatalf("SyncNow() error = %v, want 409", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestFinishInterrupted checks the runs a stopped instance left running are
// failed as interrupted, unless a sync still holds its connection's lock.
func TestFinishInterrupted(t *testing.T) {
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mock.ExpectExec(`WITH running AS MATERIALIZED (SELECT id, connection_id FROM ingestion_runs WHERE finished_at IS NULL)
		UPDATE ingestion_runs r SET finished_at = now(), error = $1 FROM running
		WHERE r.id = running.id AND pg_try_advisory_xact_lock(hashtext('connection-sync:' || running.connection_id))`).
		WithArgs("interrupted: the portal stopped before the sync finished").WillReturnResult(sqlmock.NewResult(0, 2))

	if err := NewStore(db).finishInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestRunningRunsDontCount checks a running sync, which has no end yet,
// moves neither the schedule nor the complete interval, and isn't
// counted as an outcome it hasn't had.
func TestRunningRunsDontCount(t *testing.T) {
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mock.ExpectQuery(syncHistorySQL).WithArgs("connection-id").WillReturnRows(sqlmock.NewRows([]string{"complete"}).AddRow(nil))
	mock.ExpectQuery(readChangesSQL).WithArgs("connection-id").WillReturnRows(sqlmock.NewRows([]string{"source", "published_at"}))
	mock.ExpectQuery(sourceChangesSQL).WithArgs("blueprint").WillReturnRows(sqlmock.NewRows([]string{"identifier", "source", "published_at"}))
	if _, err := NewStore(db).syncHistory(context.Background(), "connection-id", "inventory"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	// Only finished runs set lastComplete, for the complete interval and so
	// the two-miss rule's complete runs.
	if n := strings.Count(syncHistorySQL, "AND finished_at IS NOT NULL"); n != 1 {
		t.Errorf("syncHistorySQL checks finished_at %d times, want 1", n)
	}
	if !strings.Contains(runFilter, "WHEN r.finished_at IS NULL THEN 'running' WHEN r.error") {
		t.Error("a running run must be filtered as running before any other outcome")
	}
}

// expectConnectionID expects Sync Now to find "source".
func expectConnectionID(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT id FROM config_documents WHERE kind = $1 AND identifier = $2`).WithArgs(Name, "source").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("connection-id"))
}

// builderContext is a request from a builder, to cancel as the request ends.
func builderContext(db *gofrSQL.DB) (*gofr.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(auth.WithPrincipal(context.Background(), auth.Principal{Role: auth.RoleBuilder}))

	return &gofr.Context{Context: ctx, Container: &container.Container{Logger: logging.NewLogger(logging.FATAL), SQL: db}}, cancel
}

func waitForBackground(t *testing.T, run func(func())) {
	t.Helper()
	previous := background
	background = run
	t.Cleanup(func() { background = previous })
}

func TestSyncableResource(t *testing.T) {
	spec := withSources(t, strings.Replace(httpYAML("https://tracker.example.com"), "  sync:",
		"        builds:\n          url: https://tracker.example.com/builds\n          items: .builds[]\n  sync:", 1), deploymentYAML("tracker"))
	for name, want := range map[string]string{
		"deployments": "",
		"builds":      "no source reads builds from tracker",
		"tests":       "tracker doesn't pull a resource called tests",
	} {
		if got := spec.SyncableResource(name); !strings.Contains(got, want) || (want == "") != (got == "") {
			t.Errorf("SyncableResource(%s) = %q, want %q", name, got, want)
		}
	}
}

func TestReconcilable(t *testing.T) {
	spec := Spec{sources: []Mapping{
		{Resource: "repos", Blueprint: "repository"},
		{Resource: "deployments", Blueprint: "deployment"},
		{Resource: "events", Blueprint: "deployment"},
		{Resource: "teams", Blueprint: "team"},
		{Resource: "pipelines", Blueprint: "pipeline", Mode: blueprint.SourceUpsert},
	}}
	tests := []struct {
		name    string
		covered []string
		want    string
	}{
		{"everything, but what an upsert source writes", []string{"repos", "deployments", "events", "teams", "pipelines"}, "deployment,repository,team"},
		{"a blueprint needs every resource it maps", []string{"repos", "deployments"}, "repository"},
		{"a failed resource keeps its blueprint", []string{"deployments", "events", "teams"}, "deployment,team"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := strings.Join(spec.reconcilable(test.covered), ","); got != test.want {
				t.Fatalf("reconcilable = %s, want %s", got, test.want)
			}
		})
	}
}

const recordColumnsSQL = `b.identifier, coalesce(b.published_version, 0), coalesce(v.source, ''), coalesce(v.published_at, b.created_at),
		coalesce(pu.email, ''), coalesce(b.draft_source, ''), coalesce(b.draft_updated_at, b.created_at), coalesce(du.email, ''), b.created_at
		FROM config_documents b LEFT JOIN config_versions v ON v.document_id = b.id AND v.version = b.published_version
		LEFT JOIN users pu ON pu.id = v.published_by LEFT JOIN users du ON du.id = b.draft_updated_by`

var recordColumnNames = []string{
	"identifier", "published_version", "source", "published_at", "published_by", "draft_source", "draft_updated_at", "draft_updated_by", "created_at",
}

// mockSyncStart expects a sync of the published http connection "source",
// fetching from url, up to choosing its mode and noting when it started.
func mockSyncStart(t *testing.T, url string) (*gofrSQL.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	expectSyncStart(mock, url, true)

	return db, mock
}

// expectSyncStart expects a sync of "source" up to asking for its lock,
// which it gets when locked, and then up to noting when it started.
func expectSyncStart(mock sqlmock.Sqlmock, url string, locked bool) {
	source := strings.Replace(httpYAML(url), "name: tracker", "name: source", 1)
	now := time.Now()
	mock.ExpectQuery(`SELECT `+recordColumnsSQL+` WHERE b.kind = $1 AND b.identifier = $2`).WithArgs(Name, "source").
		WillReturnRows(sqlmock.NewRows(recordColumnNames).AddRow("source", 1, source, now, "", "", now, "", now))
	mock.ExpectQuery(`SELECT ` + recordColumnsSQL + ` WHERE b.kind = $1 ORDER BY b.identifier`).
		WillReturnRows(sqlmock.NewRows(recordColumnNames).AddRow("deployment", 1, deploymentYAML("tracker"), now, "", "", now, "", now))
	mock.ExpectQuery(`SELECT id FROM config_documents WHERE kind = $1 AND identifier = $2 AND published_version IS NOT NULL`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("connection-id"))
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT pg_try_advisory_xact_lock(hashtext('connection-sync:' || $1))`).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(locked))
	if !locked {
		return
	}
	mock.ExpectQuery(syncHistorySQL).WithArgs("connection-id").WillReturnRows(sqlmock.NewRows([]string{"complete"}).AddRow(nil))
	mock.ExpectQuery(readChangesSQL).WithArgs("connection-id").WillReturnRows(sqlmock.NewRows([]string{"source", "published_at"}).AddRow(source, now))
	mock.ExpectQuery(sourceChangesSQL).WithArgs("blueprint").WillReturnRows(sqlmock.NewRows([]string{"identifier", "source", "published_at"}))
	mock.ExpectQuery(`SELECT clock_timestamp()`).WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
}

// expectRunStart expects the sync to find where earlier syncs left off,
// nowhere, and no blueprints published to keep rows longer, then to be
// recorded as running, as run 1.
func expectRunStart(mock sqlmock.Sqlmock, trigger, mode string, actionRun any) {
	mock.ExpectQuery(`SELECT resource, scope, read_at FROM sync_cursors WHERE connection_id = $1`).
		WithArgs("connection-id").WillReturnRows(sqlmock.NewRows([]string{"resource", "scope", "read_at"}))
	mock.ExpectQuery(`SELECT d.identifier, v.source, v.published_at FROM config_versions v JOIN config_documents d ON d.id = v.document_id
		WHERE d.kind = $1 AND d.identifier = ANY($2) ORDER BY d.identifier, v.version`).
		WillReturnRows(sqlmock.NewRows([]string{"identifier", "source", "published_at"}))
	mock.ExpectQuery(startRunSQL).WithArgs("connection-id", trigger, mode, sqlmock.AnyArg(), actionRun).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
}

// expectEarlierRuns expects the sync to read what the syncs before it
// couldn't, of which there are none.
func expectEarlierRuns(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT failed_scopes FROM ingestion_runs
		WHERE connection_id = $1 AND id < $2 AND trigger IN ('schedule', 'manual') AND finished_at IS NOT NULL
		ORDER BY id DESC LIMIT $3`).WithArgs("connection-id", int64(1), lastingRuns-1).
		WillReturnRows(sqlmock.NewRows([]string{"failed_scopes"}))
}

// capture is an argument matcher that keeps the value it was given.
type capture struct{ to *[]byte }

func (c capture) Match(v driver.Value) bool {
	*c.to, _ = v.([]byte)
	return true
}

// TestSyncNowReadingNothing checks a connection no blueprint's sources read
// is refused, since a sync would read nothing.
func TestSyncNowReadingNothing(t *testing.T) {
	waitForBackground(t, func(func()) { t.Error("the refused sync ran") })
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	expectConnectionID(mock)
	now := time.Now()
	source := strings.Replace(sourcedYAML, "identifier: tracker", "identifier: source", 1)
	mock.ExpectQuery(`SELECT `+recordColumnsSQL+` WHERE b.kind = $1 AND b.identifier = $2`).WithArgs(Name, "source").
		WillReturnRows(sqlmock.NewRows(recordColumnNames).AddRow("source", 1, source, now, "", "", now, "", now))
	mock.ExpectQuery(`SELECT ` + recordColumnsSQL + ` WHERE b.kind = $1 ORDER BY b.identifier`).
		WillReturnRows(sqlmock.NewRows(recordColumnNames))

	ctx, cancel := builderContext(db)
	defer cancel()
	_, err := SyncNow(ctx, "source", "")
	var refused httpx.Error
	if !errors.As(err, &refused) || refused.Code != http.StatusConflict ||
		refused.Message != "no blueprint's sources read source, so a sync reads nothing" {
		t.Fatalf("SyncNow() error = %v, want 409", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
