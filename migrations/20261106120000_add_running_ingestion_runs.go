package migrations

import "gofr.dev/pkg/gofr/migration"

func addRunningIngestionRuns() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			// A sync is recorded when it starts, so it shows while under way,
			// and is finished once its end is recorded. Runs recorded whole
			// keep the default.
			_, err := d.SQL.Exec(`ALTER TABLE ingestion_runs ALTER COLUMN finished_at DROP NOT NULL`)

			return err
		},
	}
}
