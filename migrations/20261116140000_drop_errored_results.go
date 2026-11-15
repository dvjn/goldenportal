package migrations

import "gofr.dev/pkg/gofr/migration"

func dropErroredResults() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			// Checks are filters checked at publish, so none can error.
			_, err := d.SQL.Exec(`ALTER TABLE scorecard_results DROP COLUMN errored`)
			return err
		},
	}
}
