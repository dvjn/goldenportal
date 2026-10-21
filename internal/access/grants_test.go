package access

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

func newGrants(t *testing.T) (*Grants, sqlmock.Sqlmock) {
	t.Helper()
	c, mocks := container.NewMockContainer(t)

	return NewGrants(c.SQL), mocks.SQL
}

var platform = Subject{Type: SubjectGroup, ID: "g-1"}

func expectCurrentRole(db sqlmock.Sqlmock, subject Subject, id string, role Role) {
	db.ExpectBegin()
	db.ExpectExec(queryLockGrants).WillReturnResult(sqlmock.NewResult(0, 0))
	rows := sqlmock.NewRows([]string{"id", "role"})
	if role != NoRole {
		rows.AddRow(id, role)
	}
	db.ExpectQuery(querySubjectRole).WithArgs(subject.Type, subject.ID).WillReturnRows(rows)
}

func expectLastAdmin(db sqlmock.Sqlmock, id string, last bool) {
	db.ExpectQuery(queryIsLastAdminGrant).WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"last"}).AddRow(last))
}

func TestSetCreatesAGrant(t *testing.T) {
	grants, db := newGrants(t)
	expectCurrentRole(db, platform, "", NoRole)
	db.ExpectExec(queryUpsertGrant).WithArgs(SubjectGroup, "g-1", Admin, "u-1").WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectCommit()

	previous, err := grants.Set(context.Background(), platform, Admin, "u-1")
	if err != nil || previous != NoRole {
		t.Fatalf("Set() = %q, %v, want no previous role", previous, err)
	}
}

func TestSetChangesARoleWithoutTheGuardWhenAdminStays(t *testing.T) {
	grants, db := newGrants(t)
	expectCurrentRole(db, platform, "r-1", Maintainer)
	db.ExpectExec(queryUpsertGrant).WithArgs(SubjectGroup, "g-1", Admin, "u-1").WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectCommit()

	previous, err := grants.Set(context.Background(), platform, Admin, "u-1")
	if err != nil || previous != Maintainer {
		t.Fatalf("Set() = %q, %v, want Maintainer before", previous, err)
	}
}

func TestSetRefusesToTakeAdminFromTheLastAdminGrant(t *testing.T) {
	for name, role := range map[string]Role{"downgrade": Maintainer, "removal": NoRole} {
		t.Run(name, func(t *testing.T) {
			grants, db := newGrants(t)
			expectCurrentRole(db, platform, "r-1", Admin)
			expectLastAdmin(db, "r-1", true)
			db.ExpectRollback()

			if _, err := grants.Set(context.Background(), platform, role, "u-1"); !errors.Is(err, ErrLastAdmin) {
				t.Fatalf("Set() error = %v, want ErrLastAdmin", err)
			}
		})
	}
}

func TestSetRemovesAnAdminGrantWhenAnotherCarriesAdmin(t *testing.T) {
	grants, db := newGrants(t)
	expectCurrentRole(db, platform, "r-1", Admin)
	expectLastAdmin(db, "r-1", false)
	db.ExpectExec(queryDeleteGrant).WithArgs("r-1").WillReturnResult(sqlmock.NewResult(0, 1))
	db.ExpectCommit()

	previous, err := grants.Set(context.Background(), platform, NoRole, "u-1")
	if err != nil || previous != Admin {
		t.Fatalf("Set() = %q, %v, want Admin before", previous, err)
	}
}

func TestSetReportsAMissingGrantOrSubject(t *testing.T) {
	grants, db := newGrants(t)
	expectCurrentRole(db, platform, "", NoRole)
	db.ExpectRollback()

	if _, err := grants.Set(context.Background(), platform, NoRole, "u-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Set(NoRole) error = %v, want ErrNotFound", err)
	}

	expectCurrentRole(db, platform, "", NoRole)
	db.ExpectExec(queryUpsertGrant).WithArgs(SubjectGroup, "g-1", Admin, "u-1").WillReturnError(stateError(foreignKeyViolation))
	db.ExpectRollback()

	if _, err := grants.Set(context.Background(), platform, Admin, "u-1"); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("Set(Admin) error = %v, want ErrSubjectNotFound", err)
	}
}

func grantColumnNames() []string {
	return []string{
		"id", "subject_type", "subject_id", "subject_name", "subject_email", "member_count",
		"role", "granted_by", "granted_by_email", "created_at", "updated_at",
	}
}

func TestForUserScansTheGrants(t *testing.T) {
	grants, db := newGrants(t)
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	db.ExpectQuery(queryGrantsOfUser).WithArgs("u-2").WillReturnRows(sqlmock.NewRows(grantColumnNames()).
		AddRow("r-1", "group", "g-2", "payments-team", "", 2, "maintainer", "u-1", "alice@example.test", at, at))

	got, err := grants.ForUser(context.Background(), "u-2")
	if err != nil || len(got) != 1 {
		t.Fatalf("ForUser() = %v, %v", got, err)
	}
	want := Grant{
		ID: "r-1", Subject: Subject{Type: SubjectGroup, ID: "g-2"}, SubjectName: "payments-team", MemberCount: 2,
		Role: Maintainer, GrantedBy: "u-1", GrantedByEmail: "alice@example.test", CreatedAt: at, UpdatedAt: at,
	}
	if got[0] != want {
		t.Fatalf("ForUser()[0] = %+v, want %+v", got[0], want)
	}
}

func TestAdminCarryingReturnsTheGrantIDs(t *testing.T) {
	grants, db := newGrants(t)
	db.ExpectQuery(queryAdminCarryingGrants).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("r-1").AddRow("r-2"))

	ids, err := grants.AdminCarrying(context.Background())
	if err != nil || len(ids) != 2 || ids[0] != "r-1" || ids[1] != "r-2" {
		t.Fatalf("AdminCarrying() = %v, %v", ids, err)
	}
}
