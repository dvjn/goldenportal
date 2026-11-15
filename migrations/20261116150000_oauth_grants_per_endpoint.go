package migrations

import "gofr.dev/pkg/gofr/migration"

// oauthGrantsPerEndpoint keeps a consent for each MCP server an app
// connects to, so connecting to one never changes or revokes the other.
// Every grant so far was for the only server there was, at /api/mcp, which
// now takes no portal:author: authoring moved to /api/mcp/builder.
func oauthGrantsPerEndpoint() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			for _, statement := range []string{
				`ALTER TABLE oauth_grants ADD COLUMN endpoint text NOT NULL DEFAULT '/api/mcp'`,
				`ALTER TABLE oauth_grants ALTER COLUMN endpoint DROP DEFAULT`,
				`ALTER TABLE oauth_grants DROP CONSTRAINT oauth_grants_user_id_client_id_key`,
				`ALTER TABLE oauth_grants ADD CONSTRAINT oauth_grants_user_client_endpoint UNIQUE (user_id, client_id, endpoint)`,
				`UPDATE oauth_grants SET scopes = array_remove(scopes, 'portal:author')`,
				`UPDATE oauth_codes SET scopes = array_remove(scopes, 'portal:author')`,
				`UPDATE oauth_tokens SET scopes = array_remove(scopes, 'portal:author')`,
				// Its codes and tokens go with it.
				`DELETE FROM oauth_grants WHERE scopes = '{}'`,
			} {
				if _, err := d.SQL.Exec(statement); err != nil {
					return err
				}
			}

			return nil
		},
	}
}
