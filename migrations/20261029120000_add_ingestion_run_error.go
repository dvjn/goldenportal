package migrations

import "gofr.dev/pkg/gofr/migration"

func addIngestionRunError() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			// A pull sync that couldn't fetch or map its data records why, so the
			// failure shows with the integration's runs and waits out its interval.
			_, err := d.SQL.Exec(`ALTER TABLE ingestion_runs ADD COLUMN error text NOT NULL DEFAULT ''`)

			return err
		},
	}
}
