package migrations

import "gofr.dev/pkg/gofr/migration"

func addEntityValueIndexes() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// Filters compare stored and computed values by containment, as they
				// do relations, so widgets narrowing large catalogs read few rows.
				`CREATE INDEX entities_properties_idx ON entities USING gin (properties jsonb_path_ops)`,
				`CREATE INDEX entities_computed_idx ON entities USING gin (computed jsonb_path_ops)`,
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
