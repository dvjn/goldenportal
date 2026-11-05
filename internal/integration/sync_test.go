package integration

import (
	"context"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/mock/gomock"
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

const insertRunSQL = `INSERT INTO ingestion_runs (integration_id, token_id, trigger, mode, started_at, received, created, updated,
	unchanged, deleted, failed, errors, error, warning, action_run_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	RETURNING id, finished_at`

// TestSyncRecordsFailedFetch checks a failed fetch keeps the entities and is
// recorded as a run, so the scheduler waits out the interval.
func TestSyncRecordsFailedFetch(t *testing.T) {
	shortenRetries(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	db, mock := mockSyncStart(t, server.URL)
	// No DELETE is expected: nothing was fetched.
	mock.ExpectQuery(insertRunSQL).
		WithArgs("integration-id", nil, TriggerSchedule, ModeComplete, sqlmock.AnyArg(), 0, 0, 0, 0, 0, 0, []byte("[]"), sqlmock.AnyArg(), "", nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "finished_at"}).AddRow(1, time.Now()))
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
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT ` + recordColumnsSQL + ` WHERE b.kind = $1 ORDER BY b.identifier`).
		WillReturnRows(sqlmock.NewRows(recordColumnNames))
	mock.ExpectCommit()
	// No DELETE is expected.
	var errorsJSON []byte
	mock.ExpectQuery(insertRunSQL).
		WithArgs("integration-id", nil, TriggerSchedule, ModeComplete, sqlmock.AnyArg(), 2, 0, 0, 0, 0, 2, capture{&errorsJSON}, "", "", nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "finished_at"}).AddRow(1, time.Now()))
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
	mock.ExpectQuery(insertRunSQL).
		WithArgs("integration-id", nil, TriggerAction, ModeIncremental, sqlmock.AnyArg(), 0, 0, 0, 0, 0, 0, []byte("[]"), "", "", "action-run").
		WillReturnRows(sqlmock.NewRows([]string{"id", "finished_at"}).AddRow(1, time.Now()))
	mock.ExpectCommit()

	run, err := SyncForAction(context.Background(), db, "source", "action-run", ModeIncremental, []string{"deployments"})
	if err != nil || run.Trigger != TriggerAction || run.ActionRun != "action-run" {
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
	deployment := "kind: Blueprint\nmetadata: {name: deployment, title: Deployment}\nspec:\n  properties:\n    status: {type: string, required: true}\n"
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
	source := strings.Replace(httpYAML(url), "name: tracker", "name: source", 1)
	now := time.Now()
	mock.ExpectQuery(`SELECT `+recordColumnsSQL+` WHERE b.kind = $1 AND b.identifier = $2`).WithArgs(Name, "source").
		WillReturnRows(sqlmock.NewRows(recordColumnNames).AddRow("source", 1, source, now, "", "", now, "", now))
	mock.ExpectQuery(`SELECT id FROM config_documents WHERE kind = $1 AND identifier = $2 AND published_version IS NOT NULL`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("integration-id"))
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT pg_try_advisory_xact_lock(hashtext('integration-sync:' || $1))`).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	mock.ExpectQuery(syncHistorySQL).WithArgs("integration-id").WillReturnRows(sqlmock.NewRows([]string{"complete", "succeeded", "published"}).AddRow(nil, nil, now))
	mock.ExpectQuery(`SELECT clock_timestamp()`).WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))

	return db, mock
}

// capture is an argument matcher that keeps the value it was given.
type capture struct{ to *[]byte }

func (c capture) Match(v driver.Value) bool {
	*c.to, _ = v.([]byte)
	return true
}
