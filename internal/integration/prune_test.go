package integration

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/mock/gomock"
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"
)

func expectPruneLock(mock sqlmock.Sqlmock, got bool) {
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT pg_try_advisory_xact_lock(hashtext('integration-prune-push-runs'))`).
		WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(got))
}

// TestPrunePushRunsInBatches checks old push runs go a batch per
// transaction until a batch comes up short, and that a replica that
// doesn't get the lock leaves them to the one that did.
func TestPrunePushRunsInBatches(t *testing.T) {
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	expectPruneLock(mock, true)
	mock.ExpectExec(prunePushRunsSQL).WithArgs(PushRunDays, pruneBatch).WillReturnResult(sqlmock.NewResult(0, pruneBatch))
	mock.ExpectCommit()
	expectPruneLock(mock, true)
	mock.ExpectExec(prunePushRunsSQL).WithArgs(PushRunDays, pruneBatch).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	deleted, err := prunePushRuns(t.Context(), db)
	if err != nil || deleted != pruneBatch+3 {
		t.Fatalf("prunePushRuns() = %d, %v, want %d", deleted, err, pruneBatch+3)
	}

	expectPruneLock(mock, false)
	mock.ExpectRollback()
	if deleted, err := prunePushRuns(t.Context(), db); err != nil || deleted != 0 {
		t.Fatalf("prunePushRuns() without the lock = %d, %v", deleted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
