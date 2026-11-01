package integration

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPortalIntegrations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	mock.ExpectQuery("count").WillReturnRows(sqlmock.NewRows([]string{"key", "count"}).AddRow("integration:portal", 4))
	mock.ExpectQuery("DISTINCT ON").WillReturnRows(sqlmock.NewRows([]string{"identifier", "finished_at", "outcome"}).
		AddRow("portal", now, OutcomeFailed))
	columns := []string{"identifier", "version", "source", "published_at", "published_by", "draft", "draft_at", "draft_by", "created_at"}
	mock.ExpectQuery("FROM config_documents").WithArgs("blueprint").WillReturnRows(sqlmock.NewRows(columns))
	mock.ExpectQuery("FROM config_documents").WithArgs(Name).WillReturnRows(sqlmock.NewRows(columns).
		AddRow("portal", 1, portalYAML, now, "", "", now, "", now).
		AddRow("draft", 0, "", now, "", "kind: Integration", now, "", now))

	data, err := portalSnapshot(t.Context(), db, Spec{}, []string{"integrations"})
	if err != nil {
		t.Fatal(err)
	}
	integrations, _ := data["integrations"].([]any)
	if len(integrations) != 1 {
		t.Fatalf("integrations = %+v, want the published one", data)
	}
	got, _ := integrations[0].(map[string]any)
	if got["lastRunOutcome"] != OutcomeFailed || got["entities"] != float64(4) {
		t.Fatalf("integration = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
