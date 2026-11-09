package migrations

import (
	"bytes"

	"go.yaml.in/yaml/v3"
	"gofr.dev/pkg/gofr/migration"
)

// renameToConnections renames the integration kind to connection: the
// column that names it, the indexes and constraints named for that column,
// the stored kind, the audit trail, and the documents that refer to it.
func renameToConnections() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				`ALTER TABLE entities RENAME COLUMN integration_id TO connection_id`,
				`ALTER TABLE ingestion_runs RENAME COLUMN integration_id TO connection_id`,
				`ALTER TABLE ingestion_tokens RENAME COLUMN integration_id TO connection_id`,
				`ALTER TABLE sync_cursors RENAME COLUMN integration_id TO connection_id`,
				// Constraints first: renaming one that backs an index renames the
				// index too, which leaves the indexes still named for the column.
				`DO $$
				DECLARE r record;
				BEGIN
					FOR r IN
						SELECT c.conrelid::regclass AS tbl, c.conname
						FROM pg_constraint c
						WHERE c.connamespace = current_schema()::regnamespace
							AND c.conname LIKE '%integration_id%'
					LOOP
						EXECUTE format('ALTER TABLE %s RENAME CONSTRAINT %I TO %I',
							r.tbl, r.conname, replace(r.conname, 'integration_id', 'connection_id'));
					END LOOP;
					FOR r IN
						SELECT indexname FROM pg_indexes
						WHERE schemaname = current_schema() AND indexname LIKE '%integration_id%'
					LOOP
						EXECUTE format('ALTER INDEX %I RENAME TO %I',
							r.indexname, replace(r.indexname, 'integration_id', 'connection_id'));
					END LOOP;
				END $$`,
				`UPDATE config_documents SET kind = 'connection' WHERE kind = 'integration'`,
				`UPDATE audit_events SET action = 'connection.' || substr(action, 13) WHERE action LIKE 'integration.%'`,
				`UPDATE audit_events SET target_type = 'connection.' || substr(target_type, 13) WHERE target_type LIKE 'integration.%'`,
				`UPDATE audit_events SET target_type = 'connection' WHERE target_type = 'integration'`,
			}
			for _, statement := range statements {
				if _, err := d.SQL.Exec(statement); err != nil {
					return err
				}
			}

			return rewriteSources(d.SQL, rewriteConnections)
		},
	}
}

// rewriteConnections rewrites one document source: kind Integration becomes
// Connection, an action or workflow step's entity and sync name their
// connection, and a portal connection's mappings read the connections
// resource. Sources that don't parse come back as they are.
func rewriteConnections(text string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil || len(doc.Content) == 0 {
		return text, nil
	}
	root := doc.Content[0]
	kind := field(root, "kind")
	if kind == nil || root.Kind != yaml.MappingNode {
		return text, nil
	}
	spec := field(root, "spec")

	changed := false
	switch kind.Value {
	case "Integration":
		kind.Value = "Connection"
		changed = true
		mappings, connector := field(spec, "mappings"), field(field(spec, "connector"), "type")
		if mappings != nil && mappings.Kind == yaml.SequenceNode && connector != nil && connector.Value == "portal" {
			for _, mapping := range mappings.Content {
				if r := field(mapping, "resource"); r != nil && r.Value == "integrations" {
					r.Value = "connections"
				}
			}
		}
	case "Action", "Workflow":
		if steps := field(spec, "steps"); steps != nil && steps.Kind == yaml.SequenceNode {
			for _, step := range steps.Content {
				for _, name := range []string{"entity", "sync"} {
					changed = renameKey(field(step, name), "integration", "connection") || changed
				}
			}
		}
	}
	if !changed {
		return text, nil
	}

	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return "", err
	}

	return out.String(), nil
}
