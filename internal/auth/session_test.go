package auth

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gofr.dev/pkg/gofr/container"

	"github.com/dvjn/goldport/internal/apitoken"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/directory"
)

func newSessions(t *testing.T) (*Sessions, sqlmock.Sqlmock) {
	t.Helper()
	c, mocks := container.NewMockContainer(t)

	return NewSessions(c.SQL, Config{SessionMaxAge: 10 * time.Hour, SessionIdleTimeout: 45 * time.Minute}), mocks.SQL
}

func sessionRow(stale bool) *sqlmock.Rows {
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{
		"id", "stale", "uid", "external_id", "oidc_subject", "user_name", "email", "name", "active", "created_at", "updated_at",
	}).AddRow("s-1", stale, "u-1", "ext-1", "sub-1", "alice", "alice@example.test", "Alice", true, at, at)
}

type capturedArgument struct {
	value *string
}

func (c capturedArgument) Match(value driver.Value) bool {
	*c.value, _ = value.(string)
	return true
}

func TestCreateStoresOnlyTheTokenHash(t *testing.T) {
	sessions, db := newSessions(t)
	var stored string
	db.ExpectExec(queryCreateSession).
		WithArgs(capturedArgument{&stored}, "u-1", "id-token", float64(36000)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	token, err := sessions.Create(context.Background(), "u-1", "id-token")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(token) != 43 {
		t.Fatalf("token length = %d, want 43 (32 bytes, unpadded base64url)", len(token))
	}
	if stored != hashToken(token) || stored == token {
		t.Fatalf("stored %q, want the sha256 of the token", stored)
	}
}

func TestLookupTouchesOnlyStaleSessions(t *testing.T) {
	for _, stale := range []bool{false, true} {
		sessions, db := newSessions(t)
		db.ExpectQuery(queryLookupSession).WithArgs(hashToken("token"), float64(2700)).WillReturnRows(sessionRow(stale))
		if stale {
			db.ExpectExec(queryTouchSession).WithArgs("s-1").WillReturnResult(sqlmock.NewResult(0, 1))
		}

		principal, err := sessions.Lookup(context.Background(), "token")
		if err != nil {
			t.Fatalf("Lookup(stale %t) error = %v", stale, err)
		}
		if principal.SessionID != "s-1" || principal.User.Email != "alice@example.test" || principal.User.OIDCSubject != "sub-1" {
			t.Fatalf("Lookup(stale %t) = %+v", stale, principal)
		}
	}
}

func TestLookupReportsAMissingSession(t *testing.T) {
	sessions, db := newSessions(t)
	db.ExpectQuery(queryLookupSession).WithArgs(hashToken("token"), float64(2700)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if _, err := sessions.Lookup(context.Background(), "token"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Lookup() error = %v, want ErrNoSession", err)
	}
}

func TestDeleteReturnsTheStoredIDToken(t *testing.T) {
	sessions, db := newSessions(t)
	db.ExpectQuery(queryDeleteSession).WithArgs("s-1").
		WillReturnRows(sqlmock.NewRows([]string{"id_token"}).AddRow("id-token"))
	db.ExpectQuery(queryDeleteSession).WithArgs("s-2").
		WillReturnRows(sqlmock.NewRows([]string{"id_token"}))

	if idToken, err := sessions.Delete(context.Background(), "s-1"); idToken != "id-token" || err != nil {
		t.Fatalf("Delete(s-1) = %q, %v, want id-token", idToken, err)
	}
	if _, err := sessions.Delete(context.Background(), "s-2"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Delete(s-2) error = %v, want ErrNoSession", err)
	}
}

func TestDeleteForUserRemovesEverySession(t *testing.T) {
	sessions, db := newSessions(t)
	db.ExpectExec(queryDeleteSessionsOfUser).WithArgs("u-1").WillReturnResult(sqlmock.NewResult(0, 3))

	if err := sessions.DeleteForUser(context.Background(), "u-1"); err != nil {
		t.Fatalf("DeleteForUser() error = %v", err)
	}
}

func TestPrincipalActorNamesTheTokenOrTheUser(t *testing.T) {
	user := directory.User{ID: "u-1", UserName: "alice", Email: "alice@example.test", Name: "Alice"}
	tests := []struct {
		name      string
		principal Principal
		want      audit.Actor
	}{
		{"user", Principal{User: user}, audit.Actor{Kind: audit.ActorUser, ID: "u-1", Name: "Alice", Email: "alice@example.test"}},
		{
			"API token",
			Principal{User: user, APIToken: apitoken.Token{ID: "t-1", Kind: apitoken.KindAPI, Name: "ci"}},
			audit.Actor{Kind: audit.ActorAPIToken, ID: "t-1", Name: "ci"},
		},
		{
			"ingest token",
			Principal{APIToken: apitoken.Token{ID: "t-2", Kind: apitoken.KindIngest, Name: "gitlab"}},
			audit.Actor{Kind: audit.ActorIngestToken, ID: "t-2", Name: "gitlab"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.principal.Actor(); got != tt.want {
				t.Fatalf("Actor() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
