package migrations

import "gofr.dev/pkg/gofr/migration"

func createGroups() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// Groups come from the identity provider's groups claim at sign-in.
				`CREATE TABLE groups (
					id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
					name       text NOT NULL UNIQUE,
					is_team    boolean NOT NULL DEFAULT false,
					created_at timestamptz NOT NULL DEFAULT now()
				)`,
				`CREATE TABLE group_members (
					group_id uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
					user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
					PRIMARY KEY (group_id, user_id)
				)`,
				`CREATE INDEX group_members_user_id_idx ON group_members (user_id)`,
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
