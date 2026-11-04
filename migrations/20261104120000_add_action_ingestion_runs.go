package migrations

import "gofr.dev/pkg/gofr/migration"

// addActionIngestionRuns lets an action's entity step record a run on the push
// integration it writes through, tied to the action run.
func addActionIngestionRuns() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			_, err := d.SQL.Exec(`ALTER TABLE ingestion_runs
				DROP CONSTRAINT ingestion_runs_trigger_check,
				ADD CONSTRAINT ingestion_runs_trigger_check CHECK (trigger IN ('push', 'schedule', 'manual', 'action')),
				ADD COLUMN action_run_id uuid REFERENCES runs (id) ON DELETE SET NULL`)
			return err
		},
	}
}
