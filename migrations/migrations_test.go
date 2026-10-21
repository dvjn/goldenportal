package migrations

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"gofr.dev/pkg/gofr/migration"
)

type recordingSQL struct {
	migration.SQL
	statements []string
	failOn     int
}

func (r *recordingSQL) Exec(query string, _ ...any) (sql.Result, error) {
	r.statements = append(r.statements, query)
	if len(r.statements) == r.failOn {
		return nil, errors.New("exec failed")
	}

	return nil, nil
}

func TestAllRegistersTheIdentitySchema(t *testing.T) {
	all := All()
	if len(all) != 1 || all[202610220001].UP == nil {
		t.Fatalf("All() = %v, want the identity schema at 202610220001", all)
	}
}

func TestIdentitySchemaCreatesEveryTable(t *testing.T) {
	db := &recordingSQL{}
	if err := identitySchema(migration.Datasource{SQL: db}); err != nil {
		t.Fatalf("identitySchema() error = %v", err)
	}

	for _, fragment := range []string{
		"CREATE TABLE users",
		"external_id TEXT UNIQUE",
		"oidc_subject TEXT UNIQUE",
		"ON users (lower(email)) WHERE active",
		"CREATE TABLE groups",
		"CREATE TABLE group_members",
		"CREATE TABLE sessions",
		"id_token TEXT NOT NULL",
		"CREATE TABLE api_tokens",
	} {
		if !mentions(db.statements, fragment) {
			t.Errorf("no statement mentions %q", fragment)
		}
	}
}

func TestIdentitySchemaStopsOnTheFirstFailure(t *testing.T) {
	db := &recordingSQL{failOn: 2}
	if err := identitySchema(migration.Datasource{SQL: db}); err == nil {
		t.Fatal("identitySchema() error = nil, want the exec failure")
	}
	if len(db.statements) != 2 {
		t.Fatalf("executed %d statements, want 2", len(db.statements))
	}
}

func mentions(statements []string, fragment string) bool {
	for _, statement := range statements {
		if strings.Contains(statement, fragment) {
			return true
		}
	}

	return false
}
