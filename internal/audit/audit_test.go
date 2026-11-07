package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

type recordingLogger struct{ lines []string }

func (l *recordingLogger) Infof(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func TestRecordStoresAndLogsTheEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs("u1", "ada@example.com", TokenCreate, "token", "t1", "CI pipeline", []byte(`{"readOnly":true}`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	logger := &recordingLogger{}

	err = Record(context.Background(), db, logger, Actor{ID: "u1", Label: "ada@example.com"}, TokenCreate,
		Target{Type: "token", ID: "t1", Label: "CI pipeline"}, map[string]any{"readOnly": true})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	want := `audit: ada@example.com token.create token "CI pipeline" {"readOnly":true}`
	if len(logger.lines) != 1 || logger.lines[0] != want {
		t.Fatalf("logged %q, want %q", logger.lines, want)
	}
}

func TestRecordStoresSystemActorsWithoutAnID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(nil, "GP_BOOTSTRAP_ADMIN_EMAIL", BootstrapAdmin, "user", "u1", "ada@example.com", []byte(`{}`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = Record(context.Background(), db, &recordingLogger{}, Actor{Label: "GP_BOOTSTRAP_ADMIN_EMAIL"},
		BootstrapAdmin, Target{Type: "user", ID: "u1", Label: "ada@example.com"}, nil)
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSearchTreatsWildcardsLiterally(t *testing.T) {
	tests := map[string]string{
		"":           "",
		"  ":         "",
		"ada":        "%ada%",
		"100%":       `%100\%%`,
		"snake_case": `%snake\_case%`,
		`back\slash`: `%back\\slash%`,
	}

	for text, want := range tests {
		if got := search(text); got != want {
			t.Errorf("search(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestEventTimestampIsOccurredAt(t *testing.T) {
	raw, err := json.Marshal(Event{OccurredAt: time.Unix(0, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"occurredAt":"1970-01-01T00:00:00Z"`) || strings.Contains(string(raw), `"at"`) {
		t.Errorf("event = %s", raw)
	}
}
