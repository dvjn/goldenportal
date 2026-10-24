package configdoc

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

var yamlLinePattern = regexp.MustCompile(`line (\d+)`)

// ParseYAML reads a source into its root node. YAML is a superset of JSON,
// so JSON sources work too.
func ParseYAML(source string) (*yaml.Node, []Issue) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(source), &doc); err != nil {
		issue := Issue{Severity: SeverityError, Message: strings.TrimPrefix(err.Error(), "yaml: ")}
		if match := yamlLinePattern.FindStringSubmatch(err.Error()); match != nil {
			issue.Line, _ = strconv.Atoi(match[1])
		}
		return nil, []Issue{issue}
	}
	if len(doc.Content) == 0 {
		return nil, []Issue{{Severity: SeverityError, Message: "the document is empty"}}
	}

	return doc.Content[0], nil
}

// Reader reads YAML nodes, collecting issues with their lines as it goes.
type Reader struct{ Issues []Issue }

func (r *Reader) Report(severity string, node *yaml.Node, path, format string, args ...any) {
	issue := Issue{Severity: severity, Path: path, Message: fmt.Sprintf(format, args...)}
	if node != nil {
		issue.Line = node.Line
	}
	r.Issues = append(r.Issues, issue)
}

func (r *Reader) Errorf(node *yaml.Node, path, format string, args ...any) {
	r.Report(SeverityError, node, path, format, args...)
}

// Fields reads a mapping, rejecting keys that aren't allowed so typos don't
// pass silently. Null values are left out.
func (r *Reader) Fields(node *yaml.Node, path string, allowed ...string) (map[string]*yaml.Node, bool) {
	if node.Kind != yaml.MappingNode {
		r.Errorf(node, path, "%s must be a mapping of fields", orString(path, "the document"))
		return nil, false
	}
	fields := map[string]*yaml.Node{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if !slices.Contains(allowed, key.Value) {
			r.Errorf(key, JoinPath(path, key.Value), "unknown field %q; use %s", key.Value, strings.Join(allowed, ", "))
			continue
		}
		if value.Tag == "!!null" {
			continue
		}
		fields[key.Value] = value
	}

	return fields, true
}

// Each calls fn for every entry of a mapping keyed by identifier.
func (r *Reader) Each(node *yaml.Node, path string, fn func(id string, key, value *yaml.Node)) {
	if node == nil {
		return
	}
	if node.Kind != yaml.MappingNode {
		r.Errorf(node, path, "%s must be a mapping keyed by identifier", path)
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		fn(node.Content[i].Value, node.Content[i], node.Content[i+1])
	}
}

// List calls fn for every item of a sequence.
func (r *Reader) List(node *yaml.Node, path string, fn func(i int, item *yaml.Node)) {
	if node == nil {
		return
	}
	if node.Kind != yaml.SequenceNode {
		r.Errorf(node, path, "%s must be a list", path)
		return
	}
	for i, item := range node.Content {
		fn(i, item)
	}
}

func (r *Reader) Text(node *yaml.Node, path string) string {
	if node == nil {
		return ""
	}
	if node.Kind != yaml.ScalarNode {
		r.Errorf(node, path, "%s must be text", path)
		return ""
	}

	return node.Value
}

func (r *Reader) Bool(node *yaml.Node, path string) bool {
	var value bool
	if node != nil && node.Decode(&value) != nil {
		r.Errorf(node, path, "%s must be true or false", path)
	}

	return value
}

// OrNode picks node, or fallback when it's missing, to point issues at.
func OrNode(node, fallback *yaml.Node) *yaml.Node {
	if node != nil {
		return node
	}

	return fallback
}

func JoinPath(path, key string) string {
	if path == "" {
		return key
	}

	return path + "." + key
}

func orString(s, fallback string) string {
	if s != "" {
		return s
	}

	return fallback
}
