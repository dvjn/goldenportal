package migrations

import "gofr.dev/pkg/gofr/migration"

func groupParents() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// Parents are kept by the identity provider's id for them, so a
				// parent provisioned after its child counts once it arrives.
				`CREATE TABLE group_parents (
					group_id           uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
					parent_external_id text NOT NULL,
					PRIMARY KEY (group_id, parent_external_id)
				)`,
				`CREATE INDEX group_parents_parent_external_id_idx ON group_parents (parent_external_id)`,
				`CREATE INDEX groups_external_id_idx ON groups (external_id)`,
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
