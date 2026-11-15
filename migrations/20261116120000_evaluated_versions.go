package migrations

import "gofr.dev/pkg/gofr/migration"

func evaluatedVersions() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// Which published version a scorecard's results are for, so its
				// views can say when they're behind a publish.
				`ALTER TABLE scorecard_evaluations ADD COLUMN evaluated_version int`,
				// Publishing used to evaluate in the same transaction, so results
				// were always for the published version.
				`UPDATE scorecard_evaluations e SET evaluated_version = d.published_version
					FROM config_documents d WHERE d.id = e.scorecard_id`,
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
