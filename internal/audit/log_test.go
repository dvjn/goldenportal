package audit

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gofr.dev/pkg/gofr/container"
)

func newLog(t *testing.T) (*Log, sqlmock.Sqlmock) {
	t.Helper()
	c, mocks := container.NewMockContainer(t)

	return NewLog(c.SQL), mocks.SQL
}

func TestRecordStoresTheEntry(t *testing.T) {
	log, db := newLog(t)
	db.ExpectExec(queryRecord).
		WithArgs("u-1", "alice@example.test", GrantCreated, "group", "g-1", "platform-team", `{"role":"admin"}`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	db.ExpectExec(queryRecord).
		WithArgs("", "alice@example.test", SignedOut, "", "", "", `{}`).
		WillReturnResult(sqlmock.NewResult(2, 1))

	err := log.Record(context.Background(), Entry{
		Actor:   Actor{UserID: "u-1", Label: "alice@example.test"},
		Action:  GrantCreated,
		Target:  Target{Kind: "group", ID: "g-1", Name: "platform-team"},
		Details: map[string]any{"role": "admin"},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := log.Record(context.Background(), Entry{Actor: Actor{Label: "alice@example.test"}, Action: SignedOut}); err != nil {
		t.Fatalf("Record() without details error = %v", err)
	}
}

func TestListReturnsAPageNewestFirst(t *testing.T) {
	log, db := newLog(t)
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	db.ExpectQuery(queryCount).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	db.ExpectQuery(queryList).WithArgs(1, 1).WillReturnRows(sqlmock.NewRows([]string{
		"id", "at", "actor_user_id", "actor_label", "action", "target_kind", "target_id", "target_name", "details",
	}).AddRow(2, at, "", "ci", APITokenRevoked, "api_token", "t-1", "ci", []byte(`{"role":"member"}`)))

	entries, total, err := log.List(context.Background(), Page{Offset: 1, Limit: 1})
	if err != nil || total != 3 || len(entries) != 1 {
		t.Fatalf("List() = %v, %d, %v", entries, total, err)
	}
	want := Entry{
		ID: 2, At: at, Actor: Actor{Label: "ci"}, Action: APITokenRevoked,
		Target: Target{Kind: "api_token", ID: "t-1", Name: "ci"}, Details: map[string]any{"role": "member"},
	}
	if !reflect.DeepEqual(entries[0], want) {
		t.Fatalf("List()[0] = %+v, want %+v", entries[0], want)
	}
}
