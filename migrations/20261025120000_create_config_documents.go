package migrations

import "gofr.dev/pkg/gofr/migration"

func createConfigDocuments() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// Blueprints, integrations and other YAML configuration. The source is
				// the record, so exports keep the author's comments and order.
				`CREATE TABLE config_documents (
					id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
					kind              text NOT NULL,
					identifier        text NOT NULL,
					published_version integer,
					draft_source      text,
					draft_updated_at  timestamptz,
					draft_updated_by  uuid REFERENCES users (id) ON DELETE SET NULL,
					created_at        timestamptz NOT NULL DEFAULT now(),
					created_by        uuid REFERENCES users (id) ON DELETE SET NULL,
					UNIQUE (kind, identifier)
				)`,
				`CREATE TABLE config_versions (
					document_id  uuid NOT NULL REFERENCES config_documents (id) ON DELETE CASCADE,
					version      integer NOT NULL,
					source       text NOT NULL,
					published_at timestamptz NOT NULL DEFAULT now(),
					published_by uuid REFERENCES users (id) ON DELETE SET NULL,
					PRIMARY KEY (document_id, version)
				)`,
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
