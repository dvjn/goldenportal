package integration

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
	startRunSQL = `INSERT INTO ingestion_runs (integration_id, trigger, mode, started_at, received, created, updated,
	unchanged, deleted, failed, action_run_id, finished_at) VALUES ($1, $2, $3, $4, 0, 0, 0, 0, 0, 0, $5, NULL) RETURNING id`
	progressSQL = `UPDATE ingestion_runs SET received = $2, created = $3, updated = $4, unchanged = $5, deleted = $6, failed = $7
	WHERE id = $1`
	finishRunSQL = `UPDATE ingestion_runs SET received = $2, created = $3, updated = $4, unchanged = $5, deleted = $6, failed = $7,
	errors = $8, error = $9, warning = $10, finished_at = now() WHERE id = $1 RETURNING finished_at`
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
	mock.ExpectQuery(finishRunSQL).
		WithArgs(int64(1), 0, 0, 0, 0, 0, 0, []byte("[]"), sqlmock.AnyArg(), "").
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
	mock.ExpectQuery(finishRunSQL).
		WithArgs(int64(1), 2, 0, 0, 0, 0, 2, capture{&errorsJSON}, "", "").
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
// integration doesn't pull before fetching anything.
func TestSyncForAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"deployments": []}`))
	}))
	defer server.Close()
	db, mock := mockSyncStart(t, server.URL)
	expectRunStart(mock, TriggerAction, ModeIncremental, "action-run")
	mock.ExpectQuery(finishRunSQL).
		WithArgs(int64(1), 0, 0, 0, 0, 0, 0, []byte("[]"), "", "").
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
	expectIntegrationID(mock)
	expectSyncStart(mock, server.URL, true)
	expectRunStart(mock, TriggerManual, ModeIncremental, nil)
	// An empty error says the sync didn't stop when the request did.
	mock.ExpectQuery(finishRunSQL).
		WithArgs(int64(1), 0, 0, 0, 0, 0, 0, []byte("[]"), "", "").
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
	expectIntegrationID(mock)
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
// failed as interrupted, unless a sync still holds its integration's lock.
func TestFinishInterrupted(t *testing.T) {
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mock.ExpectExec(`WITH running AS MATERIALIZED (SELECT id, integration_id FROM ingestion_runs WHERE finished_at IS NULL)
		UPDATE ingestion_runs r SET finished_at = now(), error = $1 FROM running
		WHERE r.id = running.id AND pg_try_advisory_xact_lock(hashtext('integration-sync:' || running.integration_id))`).
		WithArgs("interrupted: the portal stopped before the sync finished").WillReturnResult(sqlmock.NewResult(0, 2))

	if err := NewStore(db).finishInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestRunningRunsDontCount checks a running sync, which has no end yet,
// moves neither the schedule nor the complete interval and since, and isn't
// counted as an outcome it hasn't had.
func TestRunningRunsDontCount(t *testing.T) {
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mock.ExpectQuery(syncHistorySQL).WithArgs("integration-id").
		WillReturnRows(sqlmock.NewRows([]string{"complete", "succeeded", "published"}).AddRow(nil, nil, time.Now()))
	if _, err := NewStore(db).syncHistory(context.Background(), "integration-id"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	// Only finished runs set lastComplete, for the complete interval and so
	// the two-miss rule's complete runs, and lastSucceeded, for since.
	if n := strings.Count(syncHistorySQL, "AND finished_at IS NOT NULL"); n != 2 {
		t.Errorf("syncHistorySQL checks finished_at %d times, want 2", n)
	}
	if !strings.Contains(runFilter, "WHEN r.finished_at IS NULL THEN 'running' WHEN r.error") {
		t.Error("a running run must be filtered as running before any other outcome")
	}
}

// expectIntegrationID expects Sync Now to find "source".
func expectIntegrationID(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT id FROM config_documents WHERE kind = $1 AND identifier = $2`).WithArgs(Name, "source").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("integration-id"))
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
	spec := mustParse(t, strings.Replace(httpYAML("https://tracker.example.com"), "  sync:",
		"        builds:\n          url: https://tracker.example.com/builds\n          items: .builds[]\n  sync:", 1))
	for name, want := range map[string]string{
		"deployments": "",
		"builds":      "no mapping of tracker reads builds",
		"tests":       "tracker doesn't pull a resource called tests",
	} {
		if got := spec.SyncableResource(name); !strings.Contains(got, want) || (want == "") != (got == "") {
			t.Errorf("SyncableResource(%s) = %q, want %q", name, got, want)
		}
	}
}

// TestWriteSeesWhatItCantWrite checks an item that fails validation still
// marks its entity seen, so a complete run doesn't delete it for a bad value.
func TestWriteSeesWhatItCantWrite(t *testing.T) {
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	deployment := "kind: Blueprint\nmetadata: {identifier: deployment, title: Deployment}\nspec:\n  properties:\n    status: {type: string, required: true}\n"
	now := time.Now()
	mock.ExpectQuery(`SELECT ` + recordColumnsSQL + ` WHERE b.kind = $1 ORDER BY b.identifier`).
		WillReturnRows(sqlmock.NewRows(recordColumnNames).AddRow("deployment", 1, deployment, now, "", "", now, "", now))
	mock.ExpectQuery(`SELECT id FROM config_documents WHERE kind = $1 AND identifier = $2 AND published_version IS NOT NULL`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("blueprint-id"))
	mock.ExpectExec(`UPDATE entities SET seen_at = now() WHERE blueprint_id = $1 AND identifier = $2 AND integration_id = $3`).
		WithArgs("blueprint-id", "d1", "integration-id").WillReturnResult(sqlmock.NewResult(0, 1))

	run := &Run{}
	entries := []Entry{{Blueprint: "deployment", Entity: blueprint.Entity{Identifier: "d1", Properties: map[string]any{}}}}
	if _, err := write(context.Background(), db, "integration-id", entries, run); err != nil {
		t.Fatal(err)
	}
	if run.Failed != 1 {
		t.Fatalf("failed = %d, want 1", run.Failed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestReconcileDeletesWhatTwoRunsMissed checks a complete run deletes only
// the entities a complete run that finished before it started missed too,
// with nothing seeing them between, under the published version, and marks
// the rest it missed as of when it marks them.
func TestReconcileDeletesWhatTwoRunsMissed(t *testing.T) {
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	spec := Spec{Mappings: []Mapping{{Resource: "repos", Blueprint: "repository"}}}
	started, published := time.Now(), time.Now().Add(-time.Hour)
	mock.ExpectQuery(`SELECT id FROM config_documents WHERE kind = $1 AND identifier = $2 AND published_version IS NOT NULL`).
		WithArgs("blueprint", "repository").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("blueprint-id"))
	mock.ExpectQuery(`SELECT v.published_at FROM config_versions v JOIN config_documents d
			ON d.id = v.document_id AND v.version = d.published_version WHERE d.id = $1`).
		WithArgs("integration-id").WillReturnRows(sqlmock.NewRows([]string{"published_at"}).AddRow(published))
	mock.ExpectExec(`DELETE FROM entities WHERE integration_id = $1 AND blueprint_id = ANY($2) AND seen_at < $3
			AND missed_at < $3 AND missed_at >= $4 AND seen_at < missed_at`).
		WithArgs("integration-id", sqlmock.AnyArg(), started, published).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`UPDATE entities SET missed_at = clock_timestamp() WHERE integration_id = $1 AND blueprint_id = ANY($2) AND seen_at < $3`).
		WithArgs("integration-id", sqlmock.AnyArg(), started).WillReturnResult(sqlmock.NewResult(0, 3))

	deleted, err := reconcile(context.Background(), db, "integration-id", spec, []string{"repos"}, map[string]bool{}, started)
	if err != nil || deleted != 2 {
		t.Fatalf("reconcile = %d, %v", deleted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestReconcileAfterAPublish checks a complete run that started before the
// integration was last published, so read with the old mappings, neither
// deletes nor marks anything, however late it finishes.
func TestReconcileAfterAPublish(t *testing.T) {
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	spec := Spec{Mappings: []Mapping{{Resource: "repos", Blueprint: "repository"}}}
	started := time.Now()
	mock.ExpectQuery(`SELECT id FROM config_documents WHERE kind = $1 AND identifier = $2 AND published_version IS NOT NULL`).
		WithArgs("blueprint", "repository").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("blueprint-id"))
	mock.ExpectQuery(`SELECT v.published_at FROM config_versions v JOIN config_documents d
			ON d.id = v.document_id AND v.version = d.published_version WHERE d.id = $1`).
		WithArgs("integration-id").WillReturnRows(sqlmock.NewRows([]string{"published_at"}).AddRow(started.Add(time.Minute)))

	deleted, err := reconcile(context.Background(), db, "integration-id", spec, []string{"repos"}, map[string]bool{}, started)
	if err != nil || deleted != 0 {
		t.Fatalf("reconcile = %d, %v", deleted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReconcilable(t *testing.T) {
	spec := Spec{Mappings: []Mapping{
		{Resource: "repos", Blueprint: "repository"},
		{Resource: "deployments", Blueprint: "deployment"},
		{Resource: "events", Blueprint: "deployment"},
		{Resource: "teams", Blueprint: "team"},
	}}
	tests := []struct {
		name    string
		covered []string
		unsafe  map[string]bool
		want    string
	}{
		{"everything", []string{"repos", "deployments", "events", "teams"}, nil, "deployment,repository,team"},
		{"a blueprint needs every resource it maps", []string{"repos", "deployments"}, nil, "repository"},
		{"a failed resource keeps its blueprint", []string{"deployments", "events", "teams"}, nil, "deployment,team"},
		{"an entry without an identifier keeps its blueprint", []string{"repos", "teams"}, map[string]bool{"team": true}, "repository"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := strings.Join(spec.reconcilable(test.covered, test.unsafe), ","); got != test.want {
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

// mockSyncStart expects a sync of the published http integration "source",
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
	mock.ExpectQuery(`SELECT id FROM config_documents WHERE kind = $1 AND identifier = $2 AND published_version IS NOT NULL`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("integration-id"))
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT pg_try_advisory_xact_lock(hashtext('integration-sync:' || $1))`).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(locked))
	if !locked {
		return
	}
	mock.ExpectQuery(syncHistorySQL).WithArgs("integration-id").WillReturnRows(sqlmock.NewRows([]string{"complete", "succeeded", "published"}).AddRow(nil, nil, now))
	mock.ExpectQuery(`SELECT clock_timestamp()`).WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
}

// expectRunStart expects the sync to be recorded as running, as run 1.
func expectRunStart(mock sqlmock.Sqlmock, trigger, mode string, actionRun any) {
	mock.ExpectQuery(startRunSQL).WithArgs("integration-id", trigger, mode, sqlmock.AnyArg(), actionRun).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
}

// capture is an argument matcher that keeps the value it was given.
type capture struct{ to *[]byte }

func (c capture) Match(v driver.Value) bool {
	*c.to, _ = v.([]byte)
	return true
}
