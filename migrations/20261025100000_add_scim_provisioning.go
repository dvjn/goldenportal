package migrations

import "gofr.dev/pkg/gofr/migration"

func addSCIMProvisioning() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// SCIM creates people before they first sign in, so their OIDC
				// identity is filled in later, when they sign in.
				`ALTER TABLE users
					ALTER COLUMN oidc_issuer DROP NOT NULL,
					ALTER COLUMN oidc_subject DROP NOT NULL,
					ADD COLUMN username    text,
					ADD COLUMN external_id text,
					ADD COLUMN provisioned boolean NOT NULL DEFAULT false,
					ADD COLUMN updated_at  timestamptz NOT NULL DEFAULT now()`,
				`CREATE UNIQUE INDEX users_provisioned_username_idx ON users (lower(username)) WHERE provisioned`,
				`ALTER TABLE groups
					ADD COLUMN external_id text,
					ADD COLUMN provisioned boolean NOT NULL DEFAULT false,
					ADD COLUMN updated_at  timestamptz NOT NULL DEFAULT now()`,
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
