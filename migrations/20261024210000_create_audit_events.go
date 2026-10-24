package migrations

import "gofr.dev/pkg/gofr/migration"

func createAuditEvents() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			// Append-only: nothing in the app updates or deletes audit events.
			_, err := d.SQL.Exec(`CREATE TABLE audit_events (
				id           bigserial PRIMARY KEY,
				at           timestamptz NOT NULL DEFAULT now(),
				actor_id     uuid,
				actor_label  text NOT NULL,
				action       text NOT NULL,
				target_type  text NOT NULL DEFAULT '',
				target_id    text NOT NULL DEFAULT '',
				target_label text NOT NULL DEFAULT '',
				details      jsonb NOT NULL DEFAULT '{}'
			)`)

			return err
		},
	}
}
