package migrations

import "gofr.dev/pkg/gofr/migration"

func writeOnlyChanges() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// Room on each page lets an update stay on its row's page, which
				// spares the indexes a write. It applies to pages written from now.
				`ALTER TABLE entities SET (fillfactor = 90)`,
				// When the source said what a row holds, so an older read of it
				// never overwrites a newer one. NULL guards nothing.
				`ALTER TABLE entities ADD COLUMN source_at timestamptz`,
				// A result is written only when it changes, so its own time says
				// how long it has held its tier, and each scorecard keeps when it
				// last ran.
				`CREATE TABLE scorecard_evaluations (
					scorecard_id      uuid PRIMARY KEY REFERENCES config_documents (id) ON DELETE CASCADE,
					last_evaluated_at timestamptz NOT NULL
				)`,
				`INSERT INTO scorecard_evaluations (scorecard_id, last_evaluated_at)
					SELECT scorecard_id, max(evaluated_at) FROM scorecard_results GROUP BY scorecard_id`,
				// Results were rewritten on every run, so when a tier was reached
				// is lost; the last evaluation is the closest there is.
				`ALTER TABLE scorecard_results ADD COLUMN changed_at timestamptz`,
				`UPDATE scorecard_results SET changed_at = evaluated_at`,
				`ALTER TABLE scorecard_results ALTER COLUMN changed_at SET NOT NULL, ALTER COLUMN changed_at SET DEFAULT now(),
					DROP COLUMN evaluated_at`,
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
