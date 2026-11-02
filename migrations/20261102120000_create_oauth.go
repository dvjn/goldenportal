package migrations

import "gofr.dev/pkg/gofr/migration"

func createOAuth() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			for _, statement := range []string{
				// Clients register themselves, so they're public: no secret, and
				// only the redirect URIs they registered.
				`CREATE TABLE oauth_clients (
					id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
					client_id     text NOT NULL UNIQUE,
					name          text NOT NULL,
					redirect_uris text[] NOT NULL,
					created_at    timestamptz NOT NULL DEFAULT now()
				)`,
				// A grant is one user's consent for one client, with the scopes
				// they allowed; revoking it ends every token it issued.
				`CREATE TABLE oauth_grants (
					id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
					user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
					client_id    uuid NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
					scopes       text[] NOT NULL,
					created_at   timestamptz NOT NULL DEFAULT now(),
					last_used_at timestamptz,
					UNIQUE (user_id, client_id)
				)`,
				`CREATE TABLE oauth_codes (
					code_hash      bytea PRIMARY KEY,
					grant_id       uuid NOT NULL REFERENCES oauth_grants (id) ON DELETE CASCADE,
					redirect_uri   text NOT NULL,
					code_challenge text NOT NULL,
					resource       text NOT NULL,
					scopes         text[] NOT NULL,
					expires_at     timestamptz NOT NULL
				)`,
				`CREATE TABLE oauth_tokens (
					token_hash bytea PRIMARY KEY,
					grant_id   uuid NOT NULL REFERENCES oauth_grants (id) ON DELETE CASCADE,
					kind       text NOT NULL CHECK (kind IN ('access', 'refresh')),
					resource   text NOT NULL,
					scopes     text[] NOT NULL,
					expires_at timestamptz NOT NULL
				)`,
				`CREATE INDEX oauth_tokens_grant ON oauth_tokens (grant_id)`,
			} {
				if _, err := d.SQL.Exec(statement); err != nil {
					return err
				}
			}

			return nil
		},
	}
}
