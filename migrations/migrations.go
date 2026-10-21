package migrations

import "gofr.dev/pkg/gofr/migration"

func All() map[int64]migration.Migrate {
	return map[int64]migration.Migrate{
		202610220001: {UP: identitySchema},
		202610230001: {UP: accessSchema},
		202610240001: {UP: catalogSchema},
	}
}

func identitySchema(d migration.Datasource) error {
	return execAll(d, []string{
		`CREATE TABLE users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			external_id TEXT UNIQUE,
			oidc_subject TEXT UNIQUE,
			user_name TEXT NOT NULL UNIQUE,
			email TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX users_active_email ON users (lower(email)) WHERE active`,
		`CREATE TABLE groups (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			external_id TEXT UNIQUE,
			display_name TEXT NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE group_members (
			group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			PRIMARY KEY (group_id, user_id)
		)`,
		`CREATE INDEX group_members_user ON group_members (user_id)`,
		`CREATE TABLE sessions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			token_hash TEXT NOT NULL UNIQUE,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			id_token TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			expires_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE INDEX sessions_user ON sessions (user_id)`,
		`CREATE TABLE api_tokens (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			kind TEXT NOT NULL,
			name TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_used_at TIMESTAMPTZ,
			revoked_at TIMESTAMPTZ
		)`,
	})
}

// accessSchema derives a grant's subject_type and subject_id from the one
// foreign key that is set, because one column cannot reference two tables.
func accessSchema(d migration.Datasource) error {
	return execAll(d, []string{
		`CREATE TABLE role_grants (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			group_id UUID REFERENCES groups(id) ON DELETE CASCADE,
			subject_type TEXT NOT NULL GENERATED ALWAYS AS (CASE WHEN user_id IS NULL THEN 'group' ELSE 'user' END) STORED,
			subject_id UUID NOT NULL GENERATED ALWAYS AS (COALESCE(user_id, group_id)) STORED,
			role TEXT NOT NULL CHECK (role IN ('maintainer', 'admin')),
			granted_by UUID REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CHECK (num_nonnulls(user_id, group_id) = 1),
			UNIQUE (subject_type, subject_id)
		)`,
		`CREATE INDEX role_grants_group ON role_grants (group_id)`,
		`CREATE TABLE audit_log (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
			actor_label TEXT NOT NULL,
			action TEXT NOT NULL,
			target_kind TEXT NOT NULL DEFAULT '',
			target_id TEXT NOT NULL DEFAULT '',
			target_name TEXT NOT NULL DEFAULT '',
			details JSONB NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX audit_log_at ON audit_log (at DESC, id DESC)`,
		`CREATE INDEX audit_log_actor ON audit_log (actor_user_id)`,
		`ALTER TABLE api_tokens
			ADD COLUMN role TEXT CHECK (role IN ('member', 'maintainer')),
			ADD COLUMN created_by UUID REFERENCES users(id) ON DELETE SET NULL,
			ADD COLUMN expires_at TIMESTAMPTZ,
			ADD CHECK (kind <> 'api' OR (role IS NOT NULL AND expires_at IS NOT NULL))`,
		`ALTER TABLE users ADD COLUMN last_sign_in_at TIMESTAMPTZ`,
	})
}

// catalogSchema keeps documents as JSON, not JSONB, because JSONB reorders
// keys and a blueprint's properties keep the order they were written in.
func catalogSchema(d migration.Datasource) error {
	return execAll(d, []string{
		`CREATE TABLE definitions (
			kind TEXT NOT NULL,
			identifier TEXT NOT NULL,
			document JSON NOT NULL,
			PRIMARY KEY (kind, identifier)
		)`,
		`CREATE TABLE definition_revisions (
			number INTEGER PRIMARY KEY CHECK (number > 0),
			author_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
			author_label TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			rollback_of INTEGER,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			snapshot JSON NOT NULL,
			changes JSON NOT NULL,
			impact JSONB NOT NULL DEFAULT '{}'
		)`,
		`CREATE TABLE entities (
			blueprint TEXT NOT NULL,
			identifier TEXT NOT NULL,
			title TEXT NOT NULL,
			properties JSONB NOT NULL DEFAULT '{}',
			integration TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (blueprint, identifier)
		)`,
		`CREATE INDEX entities_integration ON entities (integration)`,
		`CREATE TABLE entity_relations (
			source_blueprint TEXT NOT NULL,
			source_identifier TEXT NOT NULL,
			relation TEXT NOT NULL,
			target_blueprint TEXT NOT NULL,
			target_identifier TEXT NOT NULL,
			PRIMARY KEY (source_blueprint, source_identifier, relation, target_blueprint, target_identifier),
			FOREIGN KEY (source_blueprint, source_identifier) REFERENCES entities (blueprint, identifier) ON DELETE CASCADE,
			FOREIGN KEY (target_blueprint, target_identifier) REFERENCES entities (blueprint, identifier) ON DELETE CASCADE
		)`,
		`CREATE INDEX entity_relations_target ON entity_relations (target_blueprint, target_identifier)`,
	})
}

func execAll(d migration.Datasource, statements []string) error {
	for _, statement := range statements {
		if _, err := d.SQL.Exec(statement); err != nil {
			return err
		}
	}

	return nil
}
