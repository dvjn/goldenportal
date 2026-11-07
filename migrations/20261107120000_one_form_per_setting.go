package migrations

import (
	"bytes"
	"database/sql"
	"fmt"
	"regexp"
	"time"

	"go.yaml.in/yaml/v3"
	"gofr.dev/pkg/gofr/migration"
)

// oneFormPerSetting moves stored documents, drafts and published versions,
// to the single form each setting now has: metadata.identifier for
// metadata.name, lists for a roles, groups or input default written as one
// value, {path} for a table column written as a bare path, and a named query
// for a widget query written in place. Durations in action steps and an
// integration's sync become a count and one unit.
func oneFormPerSetting() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			return rewriteSources(d.SQL, rewriteOneForm)
		},
	}
}

// rewriteSources rewrites every stored document source with rewrite.
func rewriteSources(db migration.SQL, rewrite func(string) (string, error)) error {
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
		WHERE draft_source IS NOT NULL`,
		`UPDATE config_documents SET draft_source = $%d WHERE id = $1`); err != nil {
		return err
	}
	if err := collect(`SELECT document_id, version, source FROM config_versions`,
		`UPDATE config_versions SET source = $%d WHERE document_id = $1 AND version = $2`); err != nil {
		return err
	}

	for _, s := range sources {
		text, err := rewrite(s.text)
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

// rewriteOneForm rewrites one document source. Sources that don't parse
// come back as they are. A scorecard widget's query written in place without
// a blueprint, on a dashboard, stays: the scorecard picks its blueprint, so
// there's no blueprint to give the named query.
func rewriteOneForm(text string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil || len(doc.Content) == 0 {
		return text, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return text, nil
	}
	changed := renameKey(field(root, "metadata"), "name", "identifier")
	spec := field(root, "spec")

	kind := field(root, "kind")
	if kind == nil {
		return text, nil
	}
	switch kind.Value {
	case "Dashboard":
		for _, name := range []string{"visibility", "defaultFor"} {
			changed = listifyAudience(field(spec, name)) || changed
		}
		for _, input := range values(field(spec, "inputs")) {
			changed = listify(field(input, "default")) || changed
		}
		changed = hoistQueries(spec, field(spec, "widgets"), "widgets", true) || changed
	case "Blueprint":
		for _, name := range []string{"catalog", "entity"} {
			page := field(spec, name)
			for _, tab := range values(field(page, "tabs")) {
				changed = hoistQueries(page, field(tab, "widgets"), "tabs", false) || changed
			}
		}
	case "Action", "Workflow":
		changed = listifyAudience(field(spec, "run")) || changed
		if steps := field(spec, "steps"); steps != nil && steps.Kind == yaml.SequenceNode {
			for _, step := range steps.Content {
				changed = listifyAudience(field(field(step, "approval"), "approvers")) || changed
				changed = oneUnit(field(step, "timeout")) || changed
				changed = oneUnit(field(field(step, "retry"), "backoff")) || changed
				changed = oneUnit(field(field(step, "approval"), "timeout")) || changed
			}
		}
	case "Integration":
		sync := field(spec, "sync")
		changed = oneUnit(field(sync, "interval")) || changed
		changed = oneUnit(field(sync, "complete")) || changed
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

var oneUnitPattern = regexp.MustCompile(`^[1-9][0-9]{0,5}[smhdw]$`)

// oneUnit rewrites a duration written the Go way, such as 1h30m or 1500ms,
// as a count and one unit: the largest unit that divides it exactly. A
// duration that isn't a whole number of seconds rounds up to the next one,
// and a text Go can't read stays for the validator to report.
func oneUnit(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.ScalarNode || oneUnitPattern.MatchString(node.Value) {
		return false
	}
	d, err := time.ParseDuration(node.Value)
	if err != nil || d <= 0 {
		return false
	}
	seconds := int64((d + time.Second - 1) / time.Second)
	for _, u := range []struct {
		suffix  string
		seconds int64
	}{{"w", 604800}, {"d", 86400}, {"h", 3600}, {"m", 60}, {"s", 1}} {
		if seconds%u.seconds == 0 {
			if n := seconds / u.seconds; n <= 999999 {
				node.Value, node.Tag, node.Style = fmt.Sprintf("%d%s", n, u.suffix), "!!str", 0
				return true
			}
		}
	}

	return false
}

// values lists a mapping's values, in order.
func values(node *yaml.Node) []*yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	var out []*yaml.Node
	for i := 1; i < len(node.Content); i += 2 {
		out = append(out, node.Content[i])
	}

	return out
}

func renameKey(node *yaml.Node, from, to string) bool {
	if node == nil || node.Kind != yaml.MappingNode || field(node, to) != nil {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == from {
			node.Content[i].Value = to
			return true
		}
	}

	return false
}

// listify turns a scalar into a list of it.
func listify(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.ScalarNode {
		return false
	}
	item := *node
	*node = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle, Content: []*yaml.Node{&item}}

	return true
}

func listifyAudience(node *yaml.Node) bool {
	changed := listify(field(node, "roles"))

	return listify(field(node, "groups")) || changed
}

// hoistQueries gives a page's table columns the {path} form and moves the
// queries its widgets write in place into the page's queries, under the
// widget's name. before is the key the page's queries go ahead of when it
// has none yet.
func hoistQueries(owner, widgets *yaml.Node, before string, home bool) bool {
	if widgets == nil || widgets.Kind != yaml.MappingNode {
		return false
	}
	changed := false
	for i := 0; i+1 < len(widgets.Content); i += 2 {
		id, w := widgets.Content[i].Value, widgets.Content[i+1]
		if columns := field(w, "columns"); columns != nil && columns.Kind == yaml.SequenceNode {
			for _, column := range columns.Content {
				if column.Kind != yaml.ScalarNode {
					continue
				}
				path := *column
				*column = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle, Content: []*yaml.Node{scalar("path"), &path}}
				changed = true
			}
		}
		scorecard := field(w, "type") != nil && field(w, "type").Value == "scorecard"
		for _, ref := range []struct {
			node   *yaml.Node
			suffix string
		}{
			{field(w, "query"), ""},
			{field(field(w, "value"), "query"), "Value"},
			{field(field(w, "link"), "query"), "Link"},
		} {
			q := ref.node
			if q == nil || q.Kind != yaml.MappingNode || (home && scorecard && field(q, "blueprint") == nil) {
				continue
			}
			queries := field(owner, "queries")
			if queries == nil {
				queries = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				insertBefore(owner, before, "queries", queries)
			}
			name := uniqueName(queries, id+ref.suffix)
			moved := *q
			queries.Content = append(queries.Content, scalar(name), &moved)
			*q = *scalar(name)
			changed = true
		}
	}

	return changed
}

// insertBefore puts a key ahead of another, or last when there's no other.
func insertBefore(node *yaml.Node, other, key string, value *yaml.Node) {
	pair := []*yaml.Node{scalar(key), value}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == other {
			node.Content = append(node.Content[:i], append(pair, node.Content[i:]...)...)
			return
		}
	}
	node.Content = append(node.Content, pair...)
}

func uniqueName(queries *yaml.Node, base string) string {
	name := base
	for i := 2; field(queries, name) != nil; i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}

	return name
}
