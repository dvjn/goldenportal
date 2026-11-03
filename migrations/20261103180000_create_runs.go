package migrations

import "gofr.dev/pkg/gofr/migration"

func createRuns() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// A run of an action at the version it was published as. It keeps
				// the entity and requester as they were, so it reads the same
				// after either changes or goes. Any portal instance can carry a
				// run on: it holds the run while its lease lasts.
				`CREATE TABLE runs (
					id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
					document_id       uuid NOT NULL REFERENCES config_documents (id) ON DELETE CASCADE,
					version           integer NOT NULL,
					title             text NOT NULL,
					entity_id         uuid REFERENCES entities (id) ON DELETE SET NULL,
					entity_blueprint  text NOT NULL,
					entity_identifier text NOT NULL,
					entity_title      text NOT NULL,
					requested_by      uuid REFERENCES users (id) ON DELETE SET NULL,
					requester         jsonb NOT NULL,
					via               text NOT NULL,
					idempotency_key   text,
					inputs            jsonb NOT NULL,
					status            text NOT NULL,
					summary           text NOT NULL DEFAULT '',
					links             jsonb NOT NULL DEFAULT '[]',
					outputs           jsonb NOT NULL DEFAULT '{}',
					error             text NOT NULL DEFAULT '',
					lease_until       timestamptz,
					created_at        timestamptz NOT NULL DEFAULT now(),
					started_at        timestamptz,
					finished_at       timestamptz,
					resolved_by       uuid REFERENCES users (id) ON DELETE SET NULL,
					resolution        text NOT NULL DEFAULT '',
					-- A run a workflow started, from its step at parent_position.
					parent_run_id     uuid REFERENCES runs (id) ON DELETE CASCADE,
					parent_position   integer
				)`,
				`CREATE INDEX runs_parent_idx ON runs (parent_run_id, parent_position) WHERE parent_run_id IS NOT NULL`,
				`CREATE UNIQUE INDEX runs_idempotency_key_idx ON runs (document_id, requested_by, idempotency_key)`,
				`CREATE INDEX runs_document_id_idx ON runs (document_id, created_at DESC)`,
				`CREATE INDEX runs_requested_by_idx ON runs (requested_by, created_at DESC)`,
				`CREATE INDEX runs_entity_idx ON runs (entity_blueprint, entity_identifier, created_at DESC)`,
				// Finds the runs waiting for an instance to take them on.
				`CREATE INDEX runs_waiting_idx ON runs (created_at) WHERE status IN ('queued', 'running', 'waiting')`,
				`CREATE TABLE run_steps (
					run_id      uuid NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
					position    integer NOT NULL,
					step        text NOT NULL,
					type        text NOT NULL,
					title       text NOT NULL,
					status      text NOT NULL,
					attempts    integer NOT NULL DEFAULT 0,
					output      jsonb,
					error       text NOT NULL DEFAULT '',
					detail      jsonb,
					started_at  timestamptz,
					finished_at timestamptz,
					-- When a waiting step gives up, as an approval nobody gave.
					due_at      timestamptz,
					PRIMARY KEY (run_id, position)
				)`,
				// Who decided an approval step, as they were then.
				`CREATE TABLE run_approvals (
					run_id     uuid NOT NULL,
					position   integer NOT NULL,
					user_id    uuid REFERENCES users (id) ON DELETE SET NULL,
					email      text NOT NULL,
					name       text NOT NULL,
					decision   text NOT NULL,
					comment    text NOT NULL DEFAULT '',
					decided_at timestamptz NOT NULL DEFAULT now(),
					FOREIGN KEY (run_id, position) REFERENCES run_steps (run_id, position) ON DELETE CASCADE,
					UNIQUE (run_id, position, email)
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
