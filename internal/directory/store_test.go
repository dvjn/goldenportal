package directory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gofr.dev/pkg/gofr/container"
)

type stateError string

func (e stateError) Error() string    { return "postgres error " + string(e) }
func (e stateError) SQLState() string { return string(e) }

var createdAt = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

func userColumnNames() []string {
	return []string{"id", "external_id", "oidc_subject", "user_name", "email", "name", "active", "created_at", "updated_at"}
}

func aliceRow() *sqlmock.Rows {
	return sqlmock.NewRows(userColumnNames()).
		AddRow("u-1", "ext-1", "", "alice", "alice@example.test", "Alice", true, createdAt, createdAt)
}

func newStore(t *testing.T) (*Store, sqlmock.Sqlmock) {
	t.Helper()
	c, mocks := container.NewMockContainer(t)

	return NewStore(c.SQL), mocks.SQL
}

func TestUserByIDScansTheRow(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryUserByID).WithArgs("u-1").WillReturnRows(aliceRow())

	user, err := store.UserByID(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("UserByID() error = %v", err)
	}
	want := User{
		ID: "u-1", ExternalID: "ext-1", UserName: "alice", Email: "alice@example.test", Name: "Alice",
		Active: true, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	if user != want {
		t.Fatalf("UserByID() = %+v, want %+v", user, want)
	}
}

func TestLookupsReportNotFound(t *testing.T) {
	cases := []struct {
		name   string
		query  string
		lookup func(*Store) (User, error)
		result func(*sqlmock.ExpectedQuery)
	}{
		{
			name:   "no row",
			query:  queryUserByOIDCSubject,
			lookup: func(s *Store) (User, error) { return s.UserByOIDCSubject(context.Background(), "sub") },
			result: func(e *sqlmock.ExpectedQuery) { e.WillReturnRows(sqlmock.NewRows(userColumnNames())) },
		},
		{
			name:   "malformed id",
			query:  queryUserByID,
			lookup: func(s *Store) (User, error) { return s.UserByID(context.Background(), "sub") },
			result: func(e *sqlmock.ExpectedQuery) { e.WillReturnError(stateError(invalidTextRepresentation)) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, db := newStore(t)
			tc.result(db.ExpectQuery(tc.query).WithArgs("sub"))

			if _, err := tc.lookup(store); !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestCreateUserPassesTheAttributesAndReportsConflicts(t *testing.T) {
	store, db := newStore(t)
	user := User{ExternalID: "ext-1", UserName: "alice", Email: "alice@example.test", Name: "Alice", Active: true}
	db.ExpectQuery(queryCreateUser).
		WithArgs("ext-1", "alice", "alice@example.test", "Alice", true).
		WillReturnRows(aliceRow())
	db.ExpectQuery(queryCreateUser).
		WithArgs("ext-1", "alice", "alice@example.test", "Alice", true).
		WillReturnError(stateError(uniqueViolation))

	created, err := store.CreateUser(context.Background(), user)
	if err != nil || created.ID != "u-1" {
		t.Fatalf("CreateUser() = %+v, %v, want u-1", created, err)
	}
	if _, err := store.CreateUser(context.Background(), user); !errors.Is(err, ErrConflict) {
		t.Fatalf("second CreateUser() error = %v, want ErrConflict", err)
	}
}

func TestListUsersCountsAndPages(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryCountUsers).WithArgs("alice", "").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	db.ExpectQuery(queryListUsers).WithArgs("alice", "", 0, 50).WillReturnRows(aliceRow())

	users, total, err := store.ListUsers(context.Background(), UserFilter{UserName: "alice"}, Page{Limit: 50})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if total != 1 || len(users) != 1 || users[0].UserName != "alice" {
		t.Fatalf("ListUsers() = %+v, %d, want alice and 1", users, total)
	}
}

func TestDeleteUserReportsAMissingUser(t *testing.T) {
	store, db := newStore(t)
	db.ExpectExec(queryDeleteUser).WithArgs("u-1").WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectExec(queryDeleteUser).WithArgs("u-2").WillReturnResult(sqlmock.NewResult(0, 0))

	if err := store.DeleteUser(context.Background(), "u-1"); err != nil {
		t.Fatalf("DeleteUser(u-1) error = %v", err)
	}
	if err := store.DeleteUser(context.Background(), "u-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteUser(u-2) error = %v, want ErrNotFound", err)
	}
}

func TestMembershipQueriesSendTheIDsAsAJSONArray(t *testing.T) {
	store, db := newStore(t)
	db.ExpectExec(querySetMembers).WithArgs("g-1", `["u-1","u-2"]`).WillReturnResult(sqlmock.NewResult(0, 2))
	db.ExpectExec(queryAddMembers).WithArgs("g-1", `[]`).WillReturnResult(sqlmock.NewResult(0, 0))
	db.ExpectExec(queryRemoveMembers).WithArgs("g-1", `["u-1"]`).WillReturnResult(sqlmock.NewResult(0, 1))

	ctx := context.Background()
	if err := store.SetMembers(ctx, "g-1", []string{"u-1", "u-2"}); err != nil {
		t.Fatalf("SetMembers() error = %v", err)
	}
	if err := store.AddMembers(ctx, "g-1", nil); err != nil {
		t.Fatalf("AddMembers() error = %v", err)
	}
	if err := store.RemoveMembers(ctx, "g-1", []string{"u-1"}); err != nil {
		t.Fatalf("RemoveMembers() error = %v", err)
	}
}

func TestGroupsOfUserScansEveryGroup(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryGroupsOfUser).WithArgs("u-1").WillReturnRows(
		sqlmock.NewRows([]string{"id", "external_id", "display_name", "created_at", "updated_at"}).
			AddRow("g-1", "", "all-engineers", createdAt, createdAt).
			AddRow("g-2", "ext-2", "platform-team", createdAt, createdAt))

	groups, err := store.GroupsOfUser(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GroupsOfUser() error = %v", err)
	}
	if len(groups) != 2 || groups[0].DisplayName != "all-engineers" || groups[1].ExternalID != "ext-2" {
		t.Fatalf("GroupsOfUser() = %+v", groups)
	}
}

func TestRecordSignInBindsTheSubject(t *testing.T) {
	store, db := newStore(t)
	db.ExpectExec(queryRecordSignIn).WithArgs("u-1", "sub-1").WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.RecordSignIn(context.Background(), "u-1", "sub-1"); err != nil {
		t.Fatalf("RecordSignIn() error = %v", err)
	}
}

func TestLastSignInIsZeroBeforeTheFirstSignIn(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryLastSignIn).WithArgs("u-1").WillReturnRows(sqlmock.NewRows([]string{"last_sign_in_at"}).AddRow(nil))
	db.ExpectQuery(queryLastSignIn).WithArgs("u-1").WillReturnRows(sqlmock.NewRows([]string{"last_sign_in_at"}).AddRow(createdAt))

	if at, err := store.LastSignIn(context.Background(), "u-1"); err != nil || !at.IsZero() {
		t.Fatalf("LastSignIn() = %v, %v, want the zero time", at, err)
	}
	if at, err := store.LastSignIn(context.Background(), "u-1"); err != nil || !at.Equal(createdAt) {
		t.Fatalf("LastSignIn() = %v, %v, want %v", at, err, createdAt)
	}
}

func TestCreateUnprovisionedUserCreatesOnlyAnUnknownEmail(t *testing.T) {
	store, db := newStore(t)
	db.ExpectQuery(queryCreateUnprovisionedUser).WithArgs("alice@example.test", "Alice").WillReturnRows(aliceRow())
	db.ExpectQuery(queryCreateUnprovisionedUser).WithArgs("alice@example.test", "Alice").
		WillReturnRows(sqlmock.NewRows(userColumnNames()))

	if user, err := store.CreateUnprovisionedUser(context.Background(), "alice@example.test", "Alice"); err != nil || user.ID != "u-1" {
		t.Fatalf("CreateUnprovisionedUser() = %+v, %v", user, err)
	}
	if _, err := store.CreateUnprovisionedUser(context.Background(), "alice@example.test", "Alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateUnprovisionedUser() error = %v, want ErrNotFound for a known email", err)
	}
}
