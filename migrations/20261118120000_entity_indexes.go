package migrations

import "gofr.dev/pkg/gofr/migration"

// entityIndexes records the indexes the portal builds on entities for what
// published configuration sorts and narrows them by.
func entityIndexes() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			_, err := d.SQL.Exec(`CREATE TABLE entity_indexes (
				name text PRIMARY KEY,
				blueprint text NOT NULL,
				expression text NOT NULL,
				reasons text[] NOT NULL,
				state text NOT NULL,
				error text,
				built_at timestamptz
			)`)

			return err
		},
	}
}
