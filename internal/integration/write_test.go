package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/dvjn/goldenportal/internal/entity"
)

func TestRecordActionCountsTheWrite(t *testing.T) {
	problems := EntityError{[]string{"title is required"}}
	tests := []struct {
		name                             string
		outcome                          string
		err                              error
		created, updated, deleted, fails int
		errors                           string
	}{
		{"created", entity.Created, nil, 1, 0, 0, 0, "[]"},
		{"updated", entity.Updated, nil, 0, 1, 0, 0, "[]"},
		{"deleted", deleted, nil, 0, 0, 1, 0, "[]"},
		{
			"stopped by the entity's problems", "", problems, 0, 0, 0, 1,
			`[{"index":0,"blueprint":"incident","identifier":"inc-1","problems":["title is required"]}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectQuery("INSERT INTO ingestion_runs").
				WithArgs("integration-id", nil, TriggerAction, ModeIncremental, sqlmock.AnyArg(), 1, tt.created, tt.updated,
					0, tt.deleted, tt.fails, []byte(tt.errors), "", "", "run-id").
				WillReturnRows(sqlmock.NewRows([]string{"id", "finished_at"}).AddRow(1, time.Now()))

			got := recordAction(context.Background(), db, "integration-id", "run-id", time.Now(),
				"incident", "inc-1", tt.outcome, tt.err)
			if fmt.Sprint(got) != fmt.Sprint(tt.err) {
				t.Fatalf("recordAction() = %v, want %v", got, tt.err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRecordActionSkipsOtherErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	broken := errors.New("connection reset")

	if got := recordAction(context.Background(), db, "integration-id", "run-id", time.Now(),
		"incident", "inc-1", "", broken); !errors.Is(got, broken) {
		t.Fatalf("recordAction() = %v, want %v", got, broken)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
