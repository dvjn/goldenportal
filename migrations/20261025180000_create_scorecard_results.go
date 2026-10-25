package migrations

import "gofr.dev/pkg/gofr/migration"

func createScorecardResults() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// Each entity's latest results on each scorecard. They go with the
				// scorecard or the entity, and are re-evaluated on a schedule.
				`CREATE TABLE scorecard_results (
					scorecard_id uuid NOT NULL REFERENCES config_documents (id) ON DELETE CASCADE,
					entity_id    uuid NOT NULL REFERENCES entities (id) ON DELETE CASCADE,
					tier         text NOT NULL DEFAULT '',
					score        integer,
					passed       integer NOT NULL,
					failed       integer NOT NULL,
					skipped      integer NOT NULL,
					errored      integer NOT NULL,
					criteria     jsonb NOT NULL,
					evaluated_at timestamptz NOT NULL DEFAULT now(),
					PRIMARY KEY (scorecard_id, entity_id)
				)`,
				`CREATE INDEX scorecard_results_entity_id_idx ON scorecard_results (entity_id)`,
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
