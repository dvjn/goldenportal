package migrations

import "gofr.dev/pkg/gofr/migration"

func createUsersAndSessions() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				`CREATE TABLE users (
					id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
					oidc_issuer   text NOT NULL,
					oidc_subject  text NOT NULL,
					email         text NOT NULL DEFAULT '',
					display_name  text NOT NULL,
					status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deactivated')),
					created_at    timestamptz NOT NULL DEFAULT now(),
					last_login_at timestamptz,
					UNIQUE (oidc_issuer, oidc_subject)
				)`,
				`CREATE TABLE role_bindings (
					id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
					subject_type text NOT NULL CHECK (subject_type IN ('user', 'group')),
					subject_id   uuid NOT NULL,
					role         text NOT NULL CHECK (role IN ('admin', 'builder', 'member')),
					created_at   timestamptz NOT NULL DEFAULT now(),
					UNIQUE (subject_type, subject_id, role)
				)`,
				`CREATE TABLE sessions (
					id_hash      bytea PRIMARY KEY,
					user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
					created_at   timestamptz NOT NULL DEFAULT now(),
					last_seen_at timestamptz NOT NULL DEFAULT now(),
					expires_at   timestamptz NOT NULL
				)`,
				`CREATE INDEX sessions_user_id_idx ON sessions (user_id)`,
				// A sign-in in progress: what the callback needs to finish it.
				`CREATE TABLE sign_in_requests (
					state_hash    bytea PRIMARY KEY,
					nonce         text NOT NULL,
					code_verifier text NOT NULL,
					redirect      text NOT NULL,
					expires_at    timestamptz NOT NULL
				)`,
			}

			for _, statement := range statements {
				if _, err := d.SQL.Exec(statement); err != nil {
					return err
				}
			}

			return nil
		},
	}
}
