package integration

import (
	"context"
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
	t.Setenv("GP_SECRET_UNSET_TEST", "")
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	source := scmYAML("github", "{repositories: [team/repo], token: {env: GP_SECRET_UNSET_TEST}}", "repositories", ".full_name")
	now := time.Now()
	mock.ExpectQuery(`SELECT b.identifier, coalesce(b.published_version, 0), coalesce(v.source, ''), coalesce(v.published_at, b.created_at),
		coalesce(pu.email, ''), coalesce(b.draft_source, ''), coalesce(b.draft_updated_at, b.created_at), coalesce(du.email, ''), b.created_at
		FROM config_documents b LEFT JOIN config_versions v ON v.document_id = b.id AND v.version = b.published_version
		LEFT JOIN users pu ON pu.id = v.published_by LEFT JOIN users du ON du.id = b.draft_updated_by
		WHERE b.kind = $1 AND b.identifier = $2`).WithArgs(Name, "source").WillReturnRows(sqlmock.NewRows([]string{
		"identifier", "published_version", "source", "published_at", "published_by", "draft_source", "draft_updated_at", "draft_updated_by", "created_at",
	}).AddRow("source", 1, source, now, "", "", now, "", now))
	mock.ExpectQuery(`SELECT id FROM config_documents WHERE kind = $1 AND identifier = $2 AND published_version IS NOT NULL`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("integration-id"))
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT pg_try_advisory_xact_lock(hashtext('integration-sync:' || $1))`).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	mock.ExpectRollback()
	// No DELETE is expected: the rollback leaves the previous snapshot in place.
	mock.ExpectQuery(`INSERT INTO ingestion_runs (integration_id, token_id, trigger, started_at, received, created, updated,
		unchanged, deleted, failed, errors, error) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id, finished_at`).
		WithArgs("integration-id", nil, TriggerSchedule, sqlmock.AnyArg(), 0, 0, 0, 0, 0, 0, []byte("[]"), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "finished_at"}).AddRow(1, now))

	_, err := Sync(context.Background(), db, "source", TriggerSchedule)
	if err == nil || !strings.Contains(err.Error(), "token environment reference is not set") {
		t.Fatalf("Sync() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
