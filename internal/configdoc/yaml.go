package configdoc

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

var yamlLinePattern = regexp.MustCompile(`line (\d+)`)

// ParseYAML reads a source holding one document into its root node. YAML is
// a superset of JSON, so JSON sources work too.
func ParseYAML(source string) (*yaml.Node, []Issue) {
	decoder := yaml.NewDecoder(strings.NewReader(source))
	var doc yaml.Node
	if err := decoder.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, []Issue{{Severity: SeverityError, Message: "the document is empty"}}
		}
		return nil, []Issue{yamlIssue(err)}
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		return nil, []Issue{{Severity: SeverityError, Line: extra.Line,
			Message: "this holds more than one document; apply them together instead"}}
	}
	if len(doc.Content) == 0 {
		return nil, []Issue{{Severity: SeverityError, Message: "the document is empty"}}
	}

	return doc.Content[0], nil
}

func yamlIssue(err error) Issue {
	issue := Issue{Severity: SeverityError, Message: strings.TrimPrefix(err.Error(), "yaml: ")}
	if match := yamlLinePattern.FindStringSubmatch(err.Error()); match != nil {
		issue.Line, _ = strconv.Atoi(match[1])
	}

	return issue
}

// Document is what every configuration document shares: its kind, its
// metadata, and the spec its kind defines.
type Document struct {
	Kind string
	Name string
	Meta Meta
	Spec *yaml.Node
	Root *yaml.Node
}

// ReadDocument reads a document's envelope, kind / metadata / spec,
// expecting kind. Issues go to r; the spec is nil when there's none to read.
func ReadDocument(r *Reader, source, kind string) Document {
	root, issues := ParseYAML(source)
	r.Issues = append(r.Issues, issues...)
	if root == nil {
		return Document{}
	}

	doc := Document{Root: root}
	fields, ok := r.Fields(root, "", "kind", "metadata", "spec")
	if !ok {
		return doc
	}

	doc.Kind = r.Text(fields["kind"], "kind")
	switch {
	case fields["kind"] == nil:
		r.Errorf(root, "kind", "add kind: %s", kind)
	case doc.Kind != kind:
		r.Errorf(fields["kind"], "kind", "this takes kind: %s, not %s", kind, doc.Kind)
	}

	metadata := fields["metadata"]
	if metadata == nil {
		r.Errorf(root, "metadata", "add metadata with a name and a title")
	} else if meta, ok := r.Fields(metadata, "metadata", "name", "title", "description", "icon"); ok {
		doc.Name = r.Text(meta["name"], "metadata.name")
		if !ValidIdentifier(doc.Name) {
			r.Errorf(OrNode(meta["name"], metadata), "metadata.name",
				"name must start with a lowercase letter and use only lowercase letters, digits, - and _ (up to 63)")
		}
		doc.Meta.Title = r.Text(meta["title"], "metadata.title")
		if strings.TrimSpace(doc.Meta.Title) == "" {
			r.Errorf(OrNode(meta["title"], metadata), "metadata.title", "add a title")
		}
		doc.Meta.Description = r.Text(meta["description"], "metadata.description")
		doc.Meta.Icon = r.Text(meta["icon"], "metadata.icon")
	}

	doc.Spec = fields["spec"]
	if doc.Spec == nil {
		r.Errorf(root, "spec", "add a spec")
	}

	return doc
}

// documentSeparator splits multi-document files. Each document's text is
// kept as written, since sources are stored verbatim.
var documentSeparator = regexp.MustCompile(`(?m)^---[ \t]*(?:#.*)?$`)

// SplitDocuments splits a multi-document source into its documents, leaving
// out empty ones, with the line each starts on.
func SplitDocuments(source string) []Chunk {
	var chunks []Chunk
	line, start := 1, 0
	for _, match := range documentSeparator.FindAllStringIndex(source, -1) {
		chunks = appendChunk(chunks, source[start:match[0]], line)
		line += strings.Count(source[start:match[1]], "\n")
		start = match[1]
	}

	return appendChunk(chunks, source[start:], line)
}

// Chunk is one document of a multi-document source.
type Chunk struct {
	Source string
	// Line is where the document starts in the whole source.
	Line int
}

func appendChunk(chunks []Chunk, text string, line int) []Chunk {
	trimmed := strings.TrimLeft(text, "\n")
	line += len(text) - len(trimmed)
	if strings.TrimSpace(stripComments(trimmed)) == "" {
		return chunks
	}

	return append(chunks, Chunk{Source: strings.TrimRight(trimmed, "\n") + "\n", Line: line})
}

func stripComments(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			b.WriteString(line)
		}
	}

	return b.String()
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
