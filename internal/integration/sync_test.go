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

// TestSyncRecordsFailedFetch checks a failed fetch keeps the entities and is
// recorded as a run, so the scheduler waits out the interval.
func TestSyncRecordsFailedFetch(t *testing.T) {
	shortenRetries(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	db, mock := mockSyncStart(t, server.URL)
	mock.ExpectRollback()
	// No DELETE is expected: the rollback leaves the previous snapshot in place.
	mock.ExpectQuery(`INSERT INTO ingestion_runs (integration_id, token_id, trigger, started_at, received, created, updated,
		unchanged, deleted, failed, errors, error, action_run_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id, finished_at`).
		WithArgs("integration-id", nil, TriggerSchedule, sqlmock.AnyArg(), 0, 0, 0, 0, 0, 0, []byte("[]"), sqlmock.AnyArg(), nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "finished_at"}).AddRow(1, time.Now()))

	_, err := Sync(context.Background(), db, "source", TriggerSchedule)
	if err == nil || !strings.Contains(err.Error(), "answered 503") {
		t.Fatalf("Sync() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestSyncWithoutIdentifierDeletesNothing checks an item without an
// identifier keeps every entity, since it could be any of them.
func TestSyncWithoutIdentifierDeletesNothing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"deployments": [{"id": 1}, {"name": "no id"}]}`))
	}))
	defer server.Close()
	db, mock := mockSyncStart(t, server.URL)
	mock.ExpectQuery(`SELECT ` + recordColumnsSQL + ` WHERE b.kind = $1 ORDER BY b.identifier`).
		WillReturnRows(sqlmock.NewRows(recordColumnNames))
	// No DELETE is expected.
	var errorsJSON []byte
	mock.ExpectQuery(`INSERT INTO ingestion_runs (integration_id, token_id, trigger, started_at, received, created, updated,
		unchanged, deleted, failed, errors, error, action_run_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id, finished_at`).
		WithArgs("integration-id", nil, TriggerSchedule, sqlmock.AnyArg(), 2, 0, 0, 0, 0, 2, capture{&errorsJSON}, "", nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "finished_at"}).AddRow(1, time.Now()))
	mock.ExpectCommit()

	run, err := Sync(context.Background(), db, "source", TriggerSchedule)
	if err != nil {
		t.Fatal(err)
	}
	if run.Deleted != 0 || !strings.Contains(string(errorsJSON), "so this sync deleted nothing") {
		t.Fatalf("deleted %d, errors %s", run.Deleted, errorsJSON)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
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
// fetching from url, up to taking its lock.
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

	return db, mock
}

// capture is an argument matcher that keeps the value it was given.
type capture struct{ to *[]byte }

func (c capture) Match(v driver.Value) bool {
	*c.to, _ = v.([]byte)
	return true
}
