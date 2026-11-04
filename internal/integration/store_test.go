package integration

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRunsFiltersAndPages(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT count").
		WithArgs("integration-id", `%ci\_bot%`, `{"manual","push"}`, `{"failed"}`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(21))
	now := time.Now()
	mock.ExpectQuery("ORDER BY r.id DESC LIMIT").
		WithArgs("integration-id", `%ci\_bot%`, `{"manual","push"}`, `{"failed"}`, 10, 20).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "started_at", "finished_at", "trigger", "mode", "token", "action_run", "action", "received", "created", "updated",
			"unchanged", "deleted", "failed", "errors", "error",
		}).AddRow(3, now, now, TriggerPush, ModeComplete, "ci_bot", "", "", 0, 0, 0, 0, 0, 0, []byte("[]"), "fetch failed"))

	page, err := NewStore(db).Runs(context.Background(), "integration-id", RunQuery{
		Search: "ci_bot", Triggers: []string{TriggerManual, TriggerPush}, Outcomes: []string{OutcomeFailed},
		Page: 3, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("Runs() error = %v", err)
	}
	if page.Total != 21 || len(page.Runs) != 1 || page.Runs[0].Token != "ci_bot" || page.Runs[0].Mode != ModeComplete {
		t.Fatalf("Runs() = %+v", page)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
