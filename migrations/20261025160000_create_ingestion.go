package migrations

import "gofr.dev/pkg/gofr/migration"

func createIngestion() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// An ingestion token writes as its integration, and only that integration.
				`CREATE TABLE ingestion_tokens (
					id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
					integration_id uuid NOT NULL REFERENCES config_documents (id) ON DELETE CASCADE,
					name           text NOT NULL,
					prefix         text NOT NULL,
					secret_hash    bytea NOT NULL UNIQUE,
					created_by     uuid REFERENCES users (id) ON DELETE SET NULL,
					created_at     timestamptz NOT NULL DEFAULT now(),
					last_used_at   timestamptz,
					revoked_at     timestamptz
				)`,
				`CREATE INDEX ingestion_tokens_integration_id_idx ON ingestion_tokens (integration_id)`,
				`CREATE TABLE ingestion_runs (
					id             bigserial PRIMARY KEY,
					integration_id uuid NOT NULL REFERENCES config_documents (id) ON DELETE CASCADE,
					token_id       uuid REFERENCES ingestion_tokens (id) ON DELETE SET NULL,
					trigger        text NOT NULL DEFAULT 'push' CHECK (trigger IN ('push', 'schedule', 'manual')),
					started_at     timestamptz NOT NULL,
					finished_at    timestamptz NOT NULL DEFAULT now(),
					received       integer NOT NULL,
					created        integer NOT NULL,
					updated        integer NOT NULL,
					unchanged      integer NOT NULL,
					deleted        integer NOT NULL,
					failed         integer NOT NULL,
					errors         jsonb NOT NULL DEFAULT '[]'
				)`,
				`CREATE INDEX ingestion_runs_integration_id_idx ON ingestion_runs (integration_id, id DESC)`,
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
