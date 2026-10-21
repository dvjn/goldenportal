package migrations

import "gofr.dev/pkg/gofr/migration"

func All() map[int64]migration.Migrate {
	return map[int64]migration.Migrate{
		202610220001: {UP: identitySchema},
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

func execAll(d migration.Datasource, statements []string) error {
	for _, statement := range statements {
		if _, err := d.SQL.Exec(statement); err != nil {
			return err
		}
	}

	return nil
}
