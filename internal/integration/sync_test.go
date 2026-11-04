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
)

const insertRunSQL = `INSERT INTO ingestion_runs (integration_id, token_id, trigger, mode, started_at, received, created, updated,
	unchanged, deleted, failed, errors, error, action_run_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
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
		WithArgs("integration-id", nil, TriggerSchedule, ModeComplete, sqlmock.AnyArg(), 0, 0, 0, 0, 0, 0, []byte("[]"), sqlmock.AnyArg(), nil).
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
		WithArgs("integration-id", nil, TriggerSchedule, ModeComplete, sqlmock.AnyArg(), 2, 0, 0, 0, 0, 2, capture{&errorsJSON}, "", nil).
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
		WithArgs("integration-id", nil, TriggerAction, ModeIncremental, sqlmock.AnyArg(), 0, 0, 0, 0, 0, 0, []byte("[]"), "", "action-run").
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
