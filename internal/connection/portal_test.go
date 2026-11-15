package connection

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPortalConnections(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	mock.ExpectQuery("count").WillReturnRows(sqlmock.NewRows([]string{"key", "count"}).AddRow("connection:portal", 4))
	mock.ExpectQuery("DISTINCT ON").WillReturnRows(sqlmock.NewRows([]string{"identifier", "finished_at", "outcome"}).
		AddRow("portal", now, OutcomeFailed))
	columns := []string{"id", "identifier", "version", "source", "published_at", "published_by", "draft", "draft_at", "draft_by", "created_at"}
	mock.ExpectQuery("FROM config_documents").WithArgs("blueprint").WillReturnRows(sqlmock.NewRows(columns))
	mock.ExpectQuery("FROM config_documents").WithArgs(Name).WillReturnRows(sqlmock.NewRows(columns).
		AddRow("portal-id", "portal", 1, portalYAML, now, "", "", now, "", now).
		AddRow("draft-id", "draft", 0, "", now, "", "kind: Connection", now, "", now))
	mock.ExpectQuery("FROM config_documents").WithArgs("blueprint").WillReturnRows(sqlmock.NewRows(columns))

	data, err := portalSnapshot(t.Context(), db, []string{"connections"})
	if err != nil {
		t.Fatal(err)
	}
	connections, _ := data["connections"].([]any)
	if len(connections) != 1 {
		t.Fatalf("connections = %+v, want the published one", data)
	}
	got, _ := connections[0].(map[string]any)
	if got["lastRunOutcome"] != OutcomeFailed || got["entities"] != float64(4) {
		t.Fatalf("connection = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
