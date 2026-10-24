package migrations

import "gofr.dev/pkg/gofr/migration"

func createEntities() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// Each entity has one writer, the integration that ingested it, and
				// goes when its blueprint or its integration does. Relations hold
				// target identifiers, which may not exist (yet).
				`CREATE TABLE entities (
					id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
					blueprint_id   uuid NOT NULL REFERENCES config_documents (id) ON DELETE CASCADE,
					identifier     text NOT NULL,
					title          text NOT NULL,
					properties     jsonb NOT NULL DEFAULT '{}',
					relations      jsonb NOT NULL DEFAULT '{}',
					computed     jsonb NOT NULL DEFAULT '{}',
					integration_id uuid NOT NULL REFERENCES config_documents (id) ON DELETE CASCADE,
					created_at     timestamptz NOT NULL DEFAULT now(),
					updated_at     timestamptz NOT NULL DEFAULT now(),
					UNIQUE (blueprint_id, identifier)
				)`,
				`CREATE INDEX entities_integration_id_idx ON entities (integration_id)`,
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
