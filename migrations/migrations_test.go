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
	if len(all) != 6 || all[202610220001].UP == nil || all[202610230001].UP == nil || all[202610240001].UP == nil ||
		all[202610250001].UP == nil || all[202610260001].UP == nil || all[202610270001].UP == nil {
		t.Fatalf("All() = %v, want the identity, access, catalog, integrations, audit detail and service token schemas", all)
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

func TestIntegrationsSchemaCreatesSyncRunsAndScopesIngestTokens(t *testing.T) {
	db := &recordingSQL{}
	if err := integrationsSchema(migration.Datasource{SQL: db}); err != nil {
		t.Fatalf("integrationsSchema() error = %v", err)
	}

	for _, fragment := range []string{
		"CREATE TABLE sync_runs",
		"CHECK (trigger IN ('schedule', 'manual', 'push'))",
		"CHECK (status IN ('running', 'succeeded', 'partial', 'failed'))",
		"warnings JSONB",
		"ON sync_runs (integration, started_at DESC, id DESC)",
		"ADD COLUMN integration TEXT",
		"kind <> 'ingest' OR (integration IS NOT NULL AND expires_at IS NOT NULL)",
	} {
		if !mentions(db.statements, fragment) {
			t.Errorf("no statement mentions %q", fragment)
		}
	}
}

func TestAuditDetailSplitsActorsResourcesAndClients(t *testing.T) {
	db := &recordingSQL{}
	if err := auditDetail(migration.Datasource{SQL: db}); err != nil {
		t.Fatalf("auditDetail() error = %v", err)
	}

	for _, fragment := range []string{
		"ADD COLUMN actor_kind TEXT NOT NULL DEFAULT 'user'",
		"ADD COLUMN actor_email TEXT NOT NULL DEFAULT ''",
		"ADD COLUMN resource_parent_kind TEXT NOT NULL DEFAULT ''",
		"ADD COLUMN ip_address TEXT NOT NULL DEFAULT ''",
		"ADD COLUMN user_agent TEXT NOT NULL DEFAULT ''",
		"RENAME COLUMN target_kind TO resource_kind",
		"RENAME COLUMN target_id TO resource_id",
		"RENAME COLUMN target_name TO resource_name",
		"SET actor_kind = 'api_token'",
		"resource_parent_kind = 'blueprint'",
		"resource_parent_kind = 'integration'",
		"DROP COLUMN actor_user_id, DROP COLUMN actor_label",
		"CHECK (actor_kind IN ('user', 'api_token', 'ingest_token', 'scim', 'system'))",
		"CREATE INDEX audit_log_actor ON audit_log (actor_kind, actor_id)",
		"CREATE INDEX audit_log_resource ON audit_log (resource_kind, resource_id)",
	} {
		if !mentions(db.statements, fragment) {
			t.Errorf("no statement mentions %q", fragment)
		}
	}
}

func TestServiceTokensRenamesTheTableKindAndAuditEntries(t *testing.T) {
	db := &recordingSQL{}
	if err := serviceTokens(migration.Datasource{SQL: db}); err != nil {
		t.Fatalf("serviceTokens() error = %v", err)
	}

	for _, fragment := range []string{
		"ALTER TABLE api_tokens RENAME TO tokens",
		"RENAME CONSTRAINT api_tokens_pkey TO tokens_pkey",
		"ALTER INDEX api_tokens_integration RENAME TO tokens_integration",
		"SET kind = 'service' WHERE kind = 'api'",
		"kind <> 'service' OR (role IS NOT NULL AND expires_at IS NOT NULL)",
		"kind <> 'ingest' OR (integration IS NOT NULL AND expires_at IS NOT NULL)",
		"SET action = 'service_token.'",
		"SET actor_kind = 'service_token' WHERE actor_kind = 'api_token'",
		"SET resource_kind = 'service_token' WHERE resource_kind = 'api_token'",
		"CHECK (actor_kind IN ('user', 'service_token', 'ingest_token', 'scim', 'system'))",
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
