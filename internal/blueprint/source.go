package blueprint

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Source turns the items of one resource of a connection into entities of
// the blueprint. Every expression is jq over an item.
type Source struct {
	Connection string `json:"connection"`
	// Resource names the items it maps, such as users.
	Resource string `json:"resource"`
	// Filter keeps the items it's true for. Deleted items skip it, since
	// what a system sends for a deletion may be no more than an ID.
	Filter string `json:"filter,omitempty"`
	// Mode is SourceReconcile or SourceUpsert.
	Mode   string       `json:"mode"`
	Entity SourceEntity `json:"entity"`
	line   int
	lines  sourceLines
}

// sourceLines are where a source's filter, identifier and title were written.
type sourceLines struct{ filter, identifier, title int }

// How a source keeps its blueprint. A reconcile source makes it hold what
// its resource holds: a complete sync reads the resource in full and
// deletes the entities it no longer has. An upsert source writes what's
// new or changed since the last sync, and deletes only what an item says
// was deleted, so its rows go by the blueprint's retention.
const (
	SourceReconcile = "reconcile"
	SourceUpsert    = "upsert"
)

// Upsert reports whether the source only writes.
func (s Source) Upsert() bool { return s.Mode == SourceUpsert }

// Line is where the source was written, for issues found outside the
// blueprint.
func (s Source) Line() int { return s.line }

// SourceEntity makes an entity from an item.
type SourceEntity struct {
	Identifier string           `json:"identifier"`
	Title      string           `json:"title,omitempty"`
	Properties []SourceProperty `json:"properties"`
}

// SourceProperty maps one property, which may be a relation.
type SourceProperty struct {
	ID         string `json:"identifier"`
	Expression string `json:"expression"`
	line       int
}

// The keys a source takes, exported so the documents' JSON Schema can be
// checked against them.
var (
	SourceFields       = []string{"connection", "resource", "filter", "mode", "entity"}
	SourceEntityFields = []string{"identifier", "title", "properties"}
)

// SameSource reports whether two sources make the same entities, wherever
// they were written.
func SameSource(a, b Source) bool {
	a.line, b.line = 0, 0
	a.lines, b.lines = sourceLines{}, sourceLines{}
	strip := func(props []SourceProperty) []SourceProperty {
		out := make([]SourceProperty, len(props))
		for i, p := range props {
			out[i] = SourceProperty{ID: p.ID, Expression: p.Expression}
		}
		// Properties are a YAML mapping, whose order means nothing.
		slices.SortFunc(out, func(x, y SourceProperty) int { return strings.Compare(x.ID, y.ID) })
		return out
	}
	a.Entity.Properties, b.Entity.Properties = strip(a.Entity.Properties), strip(b.Entity.Properties)

	return reflect.DeepEqual(a, b)
}

// sources reads the blueprint's sources once its properties and retention
// are known. A source with issues is still kept, since history readers
// parse old versions and ignore their issues.
func (p *parser) sources(node *yaml.Node, spec Spec) []Source {
	sources := []Source{}
	modes := map[string]string{}
	p.List(node, "spec.sources", func(i int, item *yaml.Node) {
		path := fmt.Sprintf("spec.sources[%d]", i)
		s, modeNode := p.source(path, item, spec)
		at := configdoc.OrNode(modeNode, item)
		switch was, ok := modes[s.Connection]; {
		case s.Connection == "":
		case !ok:
			modes[s.Connection] = s.Mode
		case was != s.Mode:
			p.Errorf(at, path+".mode",
				"another source from %s uses mode: %s, and a blueprint's sources from one connection share a mode", s.Connection, was)
		}
		if spec.Retention != nil && !s.Upsert() {
			p.Errorf(at, path+".mode", "%s has retention, so its sources must use mode: upsert", spec.Identifier)
		}
		sources = append(sources, s)
	})

	return sources
}

func (p *parser) source(path string, node *yaml.Node, spec Spec) (Source, *yaml.Node) {
	s := Source{line: node.Line, Mode: SourceReconcile, Entity: SourceEntity{Properties: []SourceProperty{}}}
	fields, ok := p.Fields(node, path, SourceFields...)
	if !ok {
		return s, nil
	}
	if n := fields["mode"]; n != nil {
		s.Mode = p.Text(n, path+".mode")
		if s.Mode != SourceReconcile && s.Mode != SourceUpsert {
			p.Errorf(n, path+".mode", "mode must be reconcile or upsert")
		}
	}
	s.Connection = p.Text(fields["connection"], path+".connection")
	if !configdoc.ValidIdentifier(s.Connection) {
		p.Errorf(configdoc.OrNode(fields["connection"], node), path+".connection", "connection must name a connection")
	}
	s.Resource = p.Text(fields["resource"], path+".resource")
	if fields["resource"] == nil {
		p.Errorf(node, path+".resource", "add the resource to map, such as resource: users")
	}
	s.Filter = p.expression(fields, node, path, "filter", false)
	s.lines.filter = lineOf(fields["filter"])

	entity := fields["entity"]
	if entity == nil {
		p.Errorf(node, path+".entity", "add the entity to make, with at least an identifier")
		return s, fields["mode"]
	}
	entityPath := path + ".entity"
	entityFields, ok := p.Fields(entity, entityPath, SourceEntityFields...)
	if !ok {
		return s, fields["mode"]
	}
	s.Entity.Identifier = p.expression(entityFields, entity, entityPath, "identifier", true)
	s.Entity.Title = p.expression(entityFields, entity, entityPath, "title", false)
	s.lines.identifier, s.lines.title = lineOf(entityFields["identifier"]), lineOf(entityFields["title"])
	p.Each(entityFields["properties"], entityPath+".properties", func(id string, key, value *yaml.Node) {
		propPath := entityPath + ".properties." + id
		prop := SourceProperty{ID: id, Expression: p.jqText(value, propPath), line: key.Line}
		if err := CompileExpression(prop.Expression); err != nil {
			p.Errorf(value, propPath, "%s is not valid jq: %v", id, err)
		}
		if !spec.HasProperty(id) {
			p.Errorf(key, propPath, "%s has no stored property %q to write", spec.Identifier, id)
		}
		s.Entity.Properties = append(s.Entity.Properties, prop)
	})

	return s, fields["mode"]
}

// expression reads a jq field of a source.
func (p *parser) expression(fields map[string]*yaml.Node, parent *yaml.Node, path, name string, required bool) string {
	n := fields[name]
	value := p.jqText(n, path+"."+name)
	if strings.TrimSpace(value) == "" {
		if required {
			p.Errorf(configdoc.OrNode(n, parent), path+"."+name, "add a jq expression for %s, such as .id", name)
		}
		return ""
	}
	if err := CompileExpression(value); err != nil {
		p.Errorf(n, path+"."+name, "%s is not valid jq: %v", name, err)
	}

	return value
}

// jqText reads a jq expression. A common slip is writing one that YAML reads
// as a list or mapping, such as [.a], so that gets a pointed hint.
func (p *parser) jqText(node *yaml.Node, path string) string {
	if node == nil {
		return ""
	}
	if node.Kind != yaml.ScalarNode {
		p.Errorf(node, path, "quote this jq expression, or write it as a block scalar with |-, since YAML reads it as a list or mapping")
		return ""
	}

	return node.Value
}

func lineOf(node *yaml.Node) int {
	if node == nil {
		return 0
	}

	return node.Line
}

var (
	// gpRead is a read of a field connectors used to add under _gp, which
	// they now add to the item itself as _<name>.
	gpRead = regexp.MustCompile(`(?:\._gp|\."_gp"|\.\["_gp"\])(\?)?` +
		`(?:\.([A-Za-z_][A-Za-z0-9_]*)|\."([A-Za-z_][A-Za-z0-9_]*)"|\.?\["([A-Za-z_][A-Za-z0-9_]*)"\])`)
	gpHead = regexp.MustCompile(`\._gp\b|\."_gp"|\.\["_gp"\]`)
)

// gpReads rewrites the reads of _gp that start a path from the item, as
// with ._gp.project, saying where each moved, and reports whether any read
// of _gp is left that it couldn't rewrite, as with ._gp | keys. Paths
// further in, such as .labels["_gp"], and string literals are left alone.
func gpReads(expression string) (rewritten string, moved []string, left bool) {
	code := jqCode(expression)
	var out strings.Builder
	last := 0
	for _, m := range gpRead.FindAllStringSubmatchIndex(expression, -1) {
		if !startsPath(expression, code, m[0]) {
			continue
		}
		group := func(i int) string {
			if m[2*i] < 0 {
				return ""
			}
			return expression[m[2*i]:m[2*i+1]]
		}
		to := "._" + group(2) + group(3) + group(4) + group(1)
		if message := expression[m[0]:m[1]] + " is now " + to; !slices.Contains(moved, message) {
			moved = append(moved, message)
		}
		out.WriteString(expression[last:m[0]] + to)
		last = m[1]
	}
	out.WriteString(expression[last:])
	rewritten = out.String()
	code = jqCode(rewritten)
	for _, m := range gpHead.FindAllStringIndex(rewritten, -1) {
		left = left || startsPath(rewritten, code, m[0])
	}

	return rewritten, moved, left
}

// startsPath reports whether the . at at starts a path in code, rather than
// going on from a value, as in .x._gp or $x._gp.
func startsPath(expression string, code []bool, at int) bool {
	return code[at] && (at == 0 || !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_$.?)]}\"", rune(expression[at-1])))
}

// jqCode marks the bytes of a jq expression that are code, leaving out
// string literals, but for their interpolations, and comments.
func jqCode(expression string) []bool {
	code := make([]bool, len(expression))
	// outer holds the paren depth to return to as each interpolation ends.
	var outer []int
	depth, inString := 0, false
	for i := 0; i < len(expression); i++ {
		c := expression[i]
		if inString {
			switch {
			case c == '\\' && i+1 < len(expression) && expression[i+1] == '(':
				outer, depth, inString = append(outer, depth), 0, false
				i++
			case c == '\\':
				i++
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '#':
			for i < len(expression) && expression[i] != '\n' {
				i++
			}
		case '(':
			depth++
			code[i] = true
		case ')':
			if depth == 0 && len(outer) > 0 {
				depth, outer, inString = outer[len(outer)-1], outer[:len(outer)-1], true
				continue
			}
			depth--
			code[i] = true
		default:
			code[i] = true
		}
	}

	return code
}

// checkSourceReads refuses sources that read _gp, which connectors no longer
// add, naming where each read moved to.
func checkSourceReads(spec Spec) []Issue {
	var issues []Issue
	check := func(path string, line int, expression string) {
		_, messages, left := gpReads(expression)
		if left {
			messages = append(messages, "_gp is gone: connectors add each field to the item itself, so ._gp.<name> is now ._<name>")
		}
		if len(messages) > 0 {
			issues = append(issues, Issue{Severity: SeverityError, Path: path, Line: line, Message: strings.Join(messages, "; ")})
		}
	}
	or := func(line, fallback int) int {
		if line == 0 {
			return fallback
		}
		return line
	}
	for i, s := range spec.Sources {
		path := fmt.Sprintf("spec.sources[%d]", i)
		check(path+".filter", or(s.lines.filter, s.line), s.Filter)
		check(path+".entity.identifier", or(s.lines.identifier, s.line), s.Entity.Identifier)
		check(path+".entity.title", or(s.lines.title, s.line), s.Entity.Title)
		for _, prop := range s.Entity.Properties {
			check(path+".entity.properties."+prop.ID, prop.line, prop.Expression)
		}
	}

	return issues
}
