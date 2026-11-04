package migrations

import (
	"bytes"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
	"gofr.dev/pkg/gofr/migration"
)

// connectorItems moves integrations to connectors that deliver items: the
// push connector folds into http, where each pushed mapping becomes a
// resource with a push side, and splitting data into items moves from
// mappings to the http connector's resources. Entities gain when they were
// last seen, so a complete run deletes only what it didn't see, and runs say
// whether they were complete.
func connectorItems() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			statements := []string{
				`ALTER TABLE entities ADD COLUMN seen_at timestamptz NOT NULL DEFAULT now()`,
				`ALTER TABLE ingestion_runs ADD COLUMN mode text NOT NULL DEFAULT 'incremental'
					CHECK (mode IN ('complete', 'incremental'))`,
				// Syncs were always complete.
				`UPDATE ingestion_runs SET mode = 'complete' WHERE trigger IN ('schedule', 'manual')`,
			}
			for _, statement := range statements {
				if _, err := d.SQL.Exec(statement); err != nil {
					return err
				}
			}

			return rewriteIntegrations(d.SQL)
		},
	}
}

// rewriteIntegrations moves every stored integration source, drafts and
// published versions, to the new shape.
func rewriteIntegrations(db migration.SQL) error {
	type source struct {
		query string
		args  []any
		text  string
	}
	var sources []source
	collect := func(query, update string) error {
		rows, err := db.Query(query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				id      string
				version sql.NullInt64
				text    string
			)
			if err := rows.Scan(&id, &version, &text); err != nil {
				return err
			}
			args := []any{id}
			if version.Valid {
				args = append(args, version.Int64)
			}
			sources = append(sources, source{query: update, args: args, text: text})
		}
		return rows.Err()
	}
	if err := collect(`SELECT id, NULL::bigint, draft_source FROM config_documents
		WHERE kind = 'integration' AND draft_source IS NOT NULL`,
		`UPDATE config_documents SET draft_source = $%d WHERE id = $1`); err != nil {
		return err
	}
	if err := collect(`SELECT v.document_id, v.version, v.source FROM config_versions v
		JOIN config_documents d ON d.id = v.document_id WHERE d.kind = 'integration'`,
		`UPDATE config_versions SET source = $%d WHERE document_id = $1 AND version = $2`); err != nil {
		return err
	}

	for _, s := range sources {
		text, err := rewriteIntegration(s.text)
		if err != nil {
			return err
		}
		if text == s.text {
			continue
		}
		args := append(s.args, text)
		if _, err := db.Exec(fmt.Sprintf(s.query, len(args)), args...); err != nil {
			return err
		}
	}

	return nil
}

// simplePath is jq that ".body" can be put in front of as it is.
var simplePath = regexp.MustCompile(`^\.[A-Za-z_\[][A-Za-z0-9_.\[\]"]*$`)

// rewriteIntegration rewrites one integration source. Sources that don't
// parse, or aren't in the old shape, come back as they are.
func rewriteIntegration(text string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil || len(doc.Content) == 0 {
		return text, nil
	}
	spec := field(doc.Content[0], "spec")
	connector := field(spec, "connector")
	mappings := field(spec, "mappings")
	if connector == nil || mappings == nil || mappings.Kind != yaml.SequenceNode {
		return text, nil
	}
	kind := field(connector, "type")
	if kind == nil {
		return text, nil
	}
	changed := false

	switch kind.Value {
	case "push":
		kind.Value = "http"
		resources := &yaml.Node{Kind: yaml.MappingNode}
		used := map[string]bool{}
		for _, m := range mappings.Content {
			items := take(m, "items")
			expression := ".body"
			switch {
			case items == "" || items == ".":
			case simplePath.MatchString(items):
				expression = ".body" + items
			default:
				expression = ".body | (" + items + ")"
			}
			name := resourceName(field(m, "blueprint"), used)
			setFirst(m, "resource", name)
			push := &yaml.Node{Kind: yaml.MappingNode}
			set(push, "items", expression)
			resource := &yaml.Node{Kind: yaml.MappingNode}
			resource.Content = append(resource.Content, scalar("push"), push)
			resources.Content = append(resources.Content, scalar(name), resource)
		}
		config := &yaml.Node{Kind: yaml.MappingNode}
		config.Content = append(config.Content, scalar("resources"), resources)
		connector.Style = 0
		connector.Content = append(connector.Content, scalar("config"), config)
		changed = true
	case "http":
		resources := field(field(connector, "config"), "resources")
		if resources == nil {
			return text, nil
		}
		splits := map[string]string{}
		for _, m := range mappings.Content {
			items := take(m, "items")
			if items == "" {
				continue
			}
			changed = true
			name := field(m, "resource")
			if name == nil || items == "." {
				continue
			}
			if previous, ok := splits[name.Value]; ok && previous != items {
				return "", fmt.Errorf("integration resource %s is split into items two ways; give each its own resource first", name.Value)
			}
			splits[name.Value] = items
			if resource := field(resources, name.Value); resource != nil {
				set(resource, "items", items)
			}
		}
	case "portal":
		for _, m := range mappings.Content {
			items := take(m, "items")
			if items != "" && items != "." {
				return "", fmt.Errorf("a portal integration splits a resource into items with %s, which portal resources can't do any more", items)
			}
			changed = changed || items != ""
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

func field(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}

	return nil
}

// take removes a scalar field and returns its value.
func take(node *yaml.Node, key string) string {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			value := node.Content[i+1].Value
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return value
		}
	}

	return ""
}

func set(node *yaml.Node, key, value string) {
	if existing := field(node, key); existing != nil {
		existing.Value = value
		return
	}
	node.Content = append(node.Content, scalar(key), scalar(value))
}

func setFirst(node *yaml.Node, key, value string) {
	node.Content = append([]*yaml.Node{scalar(key), scalar(value)}, node.Content...)
}

func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// resourceName names a pushed mapping's resource after its blueprint.
func resourceName(blueprint *yaml.Node, used map[string]bool) string {
	base := "items"
	if blueprint != nil && blueprint.Value != "" {
		base = strings.ToLower(blueprint.Value)
	}
	name := base
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	used[name] = true

	return name
}
