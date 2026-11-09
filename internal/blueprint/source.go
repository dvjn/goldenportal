package blueprint

import (
	"fmt"
	"reflect"
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
}

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
