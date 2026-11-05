package migrations

import "gofr.dev/pkg/gofr/migration"

func addEntityMissedAt() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			// When a complete run last missed an entity, so the next one that
			// misses it too, and nothing saw it between, deletes it.
			_, err := d.SQL.Exec(`ALTER TABLE entities ADD COLUMN missed_at timestamptz`)

			return err
		},
	}
}
