package migrations

import "gofr.dev/pkg/gofr/migration"

func indexableQueryShapes() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				// Only dates, as midnight UTC, and times with an offset pass, so unlike
				// a cast it's immutable and can be indexed. A sql function that isn't
				// inlined runs slower.
				`CREATE FUNCTION gp_time(value text) RETURNS timestamptz
					LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
				BEGIN
					IF value ~ '^\d{4}-\d{2}-\d{2}$' THEN
						value := value || 'T00:00:00Z';
					END IF;
					IF value ~ '^\d{4}-\d{2}-\d{2}[Tt]\d{2}:\d{2}:\d{2}(\.\d+)?([Zz]|[+-]\d{2}:\d{2})$'
						AND pg_input_is_valid(value, 'timestamptz') THEN
						RETURN value::timestamptz;
					END IF;
					RETURN NULL;
				END
				$$`,
				// Empty values sort lowest both ways, which one order serves read either way.
				`DROP INDEX entities_occurred_idx`,
				`CREATE INDEX entities_occurred_idx ON entities (blueprint_id, occurred_at DESC NULLS LAST)`,
				// An index entry can't hold a title much over 2 KB, so longer ones are
				// cut to the 1000 bytes writes now allow, on a character boundary.
				`UPDATE entities SET title = (
					SELECT left(title, n) FROM generate_series(1000, 1, -1) n WHERE octet_length(left(title, n)) <= 1000 LIMIT 1)
				WHERE octet_length(title) > 1000`,
				// Serves the catalog's default order.
				`CREATE INDEX entities_title_idx ON entities (blueprint_id, title, identifier)`,
				// Search finds text anywhere in titles and identifiers.
				`CREATE EXTENSION IF NOT EXISTS pg_trgm`,
				`CREATE INDEX entities_title_trgm_idx ON entities USING gin (lower(title) gin_trgm_ops)`,
				`CREATE INDEX entities_identifier_trgm_idx ON entities USING gin (lower(identifier) gin_trgm_ops)`,
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
