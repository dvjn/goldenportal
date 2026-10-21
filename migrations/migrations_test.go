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

func TestAllRegistersEverySchemaInOrder(t *testing.T) {
	all := All()
	if len(all) != 3 || all[202610220001].UP == nil || all[202610230001].UP == nil || all[202610240001].UP == nil {
		t.Fatalf("All() = %v, want the identity, access and catalog schemas", all)
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

func TestAccessSchemaCreatesGrantsAuditAndTokenRoles(t *testing.T) {
	db := &recordingSQL{}
	if err := accessSchema(migration.Datasource{SQL: db}); err != nil {
		t.Fatalf("accessSchema() error = %v", err)
	}

	for _, fragment := range []string{
		"CREATE TABLE role_grants",
		"user_id UUID REFERENCES users(id) ON DELETE CASCADE",
		"group_id UUID REFERENCES groups(id) ON DELETE CASCADE",
		"UNIQUE (subject_type, subject_id)",
		"CHECK (role IN ('maintainer', 'admin'))",
		"CREATE TABLE audit_log",
		"details JSONB",
		"ADD COLUMN expires_at TIMESTAMPTZ",
		"ADD COLUMN last_sign_in_at TIMESTAMPTZ",
	} {
		if !mentions(db.statements, fragment) {
			t.Errorf("no statement mentions %q", fragment)
		}
	}
}

func TestCatalogSchemaCreatesDefinitionsRevisionsAndEntities(t *testing.T) {
	db := &recordingSQL{}
	if err := catalogSchema(migration.Datasource{SQL: db}); err != nil {
		t.Fatalf("catalogSchema() error = %v", err)
	}

	for _, fragment := range []string{
		"CREATE TABLE definitions",
		"document JSON NOT NULL",
		"CREATE TABLE definition_revisions",
		"snapshot JSON NOT NULL",
		"CREATE TABLE entities",
		"PRIMARY KEY (blueprint, identifier)",
		"CREATE TABLE entity_relations",
		"REFERENCES entities (blueprint, identifier) ON DELETE CASCADE",
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
