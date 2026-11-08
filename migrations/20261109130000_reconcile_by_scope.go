package migrations

import "gofr.dev/pkg/gofr/migration"

func reconcileByScope() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// The part of its system a row was read from, such as a project,
				// so a part that can't be read holds back only its own deletes.
				// '*' is a row more than one part wrote; NULL is one read from no
				// part in particular, or written before rows had a scope.
				`ALTER TABLE entities ADD COLUMN scope text`,
				// A sync now keeps what it saw in memory rather than writing every
				// row it reads. A row seen after its last miss was never missed.
				`UPDATE entities SET missed_at = NULL WHERE seen_at > missed_at`,
				`ALTER TABLE entities DROP COLUMN seen_at`,
				// Deleting an entity clears the runs that name it, which scanned
				// every run without this.
				`CREATE INDEX IF NOT EXISTS runs_entity_id_idx ON runs (entity_id)`,
				// What each sync couldn't read, by resource and scope, so a scope
				// refused run after run can be told from one that failed once.
				`ALTER TABLE ingestion_runs ADD COLUMN failed_scopes jsonb NOT NULL DEFAULT '[]'`,
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
