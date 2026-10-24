package migrations

import "gofr.dev/pkg/gofr/migration"

func createAPITokens() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			_, err := d.SQL.Exec(`CREATE TABLE api_tokens (
				id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
				user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
				name         text NOT NULL,
				prefix       text NOT NULL,
				secret_hash  bytea NOT NULL UNIQUE,
				read_only    boolean NOT NULL,
				expires_at   timestamptz NOT NULL,
				last_used_at timestamptz,
				created_at   timestamptz NOT NULL DEFAULT now(),
				revoked_at   timestamptz
			)`)
			if err != nil {
				return err
			}

			_, err = d.SQL.Exec(`CREATE INDEX api_tokens_user_id_idx ON api_tokens (user_id)`)

			return err
		},
	}
}
