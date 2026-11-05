package migrations

import "gofr.dev/pkg/gofr/migration"

func addIngestionRunWarning() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			// What went wrong without failing anything a run read, such as a
			// webhook a sync couldn't set up, shows with the run.
			_, err := d.SQL.Exec(`ALTER TABLE ingestion_runs ADD COLUMN warning text NOT NULL DEFAULT ''`)

			return err
		},
	}
}
