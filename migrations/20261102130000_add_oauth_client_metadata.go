package migrations

import "gofr.dev/pkg/gofr/migration"

// addOAuthClientMetadata lets a client be named by its metadata document's
// URL: what the document said, and its logo redrawn as a PNG data URL, is
// kept until it's fetched again. Other clients have no logo.
func addOAuthClientMetadata() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			_, err := d.SQL.Exec(`ALTER TABLE oauth_clients
				ADD COLUMN logo text NOT NULL DEFAULT '',
				ADD COLUMN metadata_expires_at timestamptz`)
			return err
		},
	}
}
