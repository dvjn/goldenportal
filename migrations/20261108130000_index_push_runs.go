package migrations

import "gofr.dev/pkg/gofr/migration"

func indexPushRuns() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			// Finds the push runs old enough to delete without reading syncs.
			// A database may have it already, from before it had a migration
			// of its own.
			_, err := d.SQL.Exec(`CREATE INDEX IF NOT EXISTS ingestion_runs_push_idx ON ingestion_runs (started_at) WHERE trigger = 'push'`)

			return err
		},
	}
}
