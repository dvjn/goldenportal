package migrations

import "gofr.dev/pkg/gofr/migration"

func retentionAndCursors() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// The time a blueprint's retention keeps each row by, copied from
				// the property it names, so the retention job and the catalog's
				// ranges and sorts on that property use an index.
				`ALTER TABLE entities ADD COLUMN occurred_at timestamptz`,
				`CREATE INDEX entities_occurred_idx ON entities (blueprint_id, occurred_at) WHERE occurred_at IS NOT NULL`,
				// Where each sync of an integration left off reading a resource in
				// a scope, such as a project: when the last sync that read it
				// there without failing started. The next sync reads what changed
				// since, less a little overlap.
				`CREATE TABLE sync_cursors (
					integration_id uuid NOT NULL REFERENCES config_documents (id) ON DELETE CASCADE,
					resource       text NOT NULL,
					scope          text NOT NULL,
					read_at        timestamptz NOT NULL,
					PRIMARY KEY (integration_id, resource, scope)
				)`,
				// The earliest time a sync read each resource from, of those it
				// didn't read in full.
				`ALTER TABLE ingestion_runs ADD COLUMN read_since jsonb NOT NULL DEFAULT '{}'`,
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
