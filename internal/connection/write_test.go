package connection

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
		titleCut                         bool
		err                              error
		created, updated, deleted, fails int
		errors, warning                  string
	}{
		{"created", entity.Created, false, nil, 1, 0, 0, 0, "[]", ""},
		{"updated", entity.Updated, false, nil, 0, 1, 0, 0, "[]", ""},
		{"deleted", deleted, false, nil, 0, 0, 1, 0, "[]", ""},
		{"title cut", entity.Updated, true, nil, 0, 1, 0, 0, "[]", "Titles too long to index were cut short: incident/inc-1."},
		{
			"stopped by the entity's problems", "", true, problems, 0, 0, 0, 1,
			`[{"index":0,"blueprint":"incident","identifier":"inc-1","problems":["title is required"]}]`, "",
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
				WithArgs("connection-id", nil, TriggerAction, ModeIncremental, sqlmock.AnyArg(), 1, tt.created, tt.updated,
					0, tt.deleted, tt.fails, []byte(tt.errors), "", tt.warning, "run-id").
				WillReturnRows(sqlmock.NewRows([]string{"id", "finished_at"}).AddRow(1, time.Now()))

			got := recordAction(context.Background(), db, "connection-id", "run-id", time.Now(),
				"incident", "inc-1", written{outcome: tt.outcome, titleCut: tt.titleCut}, tt.err)
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

	if got := recordAction(context.Background(), db, "connection-id", "run-id", time.Now(),
		"incident", "inc-1", written{}, broken); !errors.Is(got, broken) {
		t.Fatalf("recordAction() = %v, want %v", got, broken)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCutTitlesWarningNamesTheFirstFew(t *testing.T) {
	run := &Run{}
	if got := run.cutTitlesWarning(); got != "" {
		t.Errorf("no cuts warn %q", got)
	}
	for i := range maxNamedCuts + 2 {
		run.cutTitle("service", fmt.Sprint("e", i))
	}
	// Written again, as a push may write an entity twice.
	run.cutTitle("service", "e0")
	run.cutTitle("team", "e0")
	want := "Titles too long to index were cut short: service/e0, service/e1, service/e2, service/e3, service/e4, " +
		"service/e5, service/e6, service/e7, service/e8, service/e9, and 3 more."
	if got := run.cutTitlesWarning(); got != want {
		t.Errorf("warning = %q, want %q", got, want)
	}
}
