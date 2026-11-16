package migrations

import "gofr.dev/pkg/gofr/migration"

func entityBatches() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			// Scorecard evaluations walk one blueprint's entities in ID order, a batch at a time.
			_, err := d.SQL.Exec(`CREATE INDEX entities_blueprint_batch_idx ON entities (blueprint_id, id)`)
			return err
		},
	}
}
