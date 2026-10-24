// Package integration defines integrations: connectors that write entities
// into the catalog, with mappings from their data to blueprints.
package integration

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Spec is a parsed integration.
type Spec struct {
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Connector   string `json:"connector"`
	// Mappings turn incoming data into entities. They're also the only
	// blueprints the integration can write.
	Mappings []Mapping `json:"mappings"`
}

// Mapping turns items of incoming data into entities of one blueprint.
// Every expression is jq.
type Mapping struct {
	Blueprint string `json:"blueprint"`
	// Items picks the items from the data. Each result is one item.
	Items string `json:"items"`
	// Filter keeps the items it's true for.
	Filter     string  `json:"filter,omitempty"`
	Identifier string  `json:"identifier"`
	Title      string  `json:"title,omitempty"`
	Properties []Field `json:"properties"`
	line       int
}

// Field maps one property, which may be a relation.
type Field struct {
	ID         string `json:"identifier"`
	Expression string `json:"expression"`
	line       int
}

func (s Spec) Key() string { return s.Identifier }

func (s Spec) Meta() configdoc.Meta {
	return configdoc.Meta{Title: s.Title, Description: s.Description, Icon: s.Icon}
}

func (s Spec) Stats() map[string]int { return map[string]int{"mappings": len(s.Mappings)} }

// Parse reads an integration and checks everything that doesn't depend on blueprints.
func Parse(source string) (Spec, []configdoc.Issue) {
	root, issues := configdoc.ParseYAML(source)
	if root == nil {
		return Spec{}, issues
	}

	r := &configdoc.Reader{}
	spec := Spec{Mappings: []Mapping{}}
	fields, ok := r.Fields(root, "", "identifier", "title", "description", "icon", "connector", "mappings")
	if !ok {
		return spec, r.Issues
	}

	spec.Identifier = r.Text(fields["identifier"], "identifier")
	if !configdoc.ValidIdentifier(spec.Identifier) {
		r.Errorf(configdoc.OrNode(fields["identifier"], root), "identifier",
			"identifier must start with a lowercase letter and use only lowercase letters, digits, - and _ (up to 63)")
	}
	spec.Title = r.Text(fields["title"], "title")
	if strings.TrimSpace(spec.Title) == "" {
		r.Errorf(configdoc.OrNode(fields["title"], root), "title", "add a title")
	}
	spec.Description = r.Text(fields["description"], "description")
	spec.Icon = r.Text(fields["icon"], "icon")
	spec.Connector = r.Text(fields["connector"], "connector")
	if _, ok := connector(spec.Connector); !ok {
		var ids []string
		for _, c := range Connectors {
			ids = append(ids, c.ID)
		}
		r.Errorf(configdoc.OrNode(fields["connector"], root), "connector",
			"connector must be one of %s", strings.Join(ids, ", "))
	}

	r.List(fields["mappings"], "mappings", func(i int, item *yaml.Node) {
		spec.Mappings = append(spec.Mappings, parseMapping(r, fmt.Sprintf("mappings[%d]", i), item))
	})
	if len(spec.Mappings) == 0 {
		r.Errorf(configdoc.OrNode(fields["mappings"], root), "mappings",
			"add a mapping for each blueprint the integration writes")
	}

	return spec, r.Issues
}

func parseMapping(r *configdoc.Reader, path string, node *yaml.Node) Mapping {
	m := Mapping{Items: ".", line: node.Line}
	fields, ok := r.Fields(node, path, "blueprint", "items", "filter", "identifier", "title", "properties")
	if !ok {
		return m
	}

	m.Blueprint = r.Text(fields["blueprint"], path+".blueprint")
	if !configdoc.ValidIdentifier(m.Blueprint) {
		r.Errorf(configdoc.OrNode(fields["blueprint"], node), path+".blueprint", "blueprint must name a blueprint by its identifier")
	}
	expression := func(name string, required bool) string {
		n := fields[name]
		value := r.Text(n, path+"."+name)
		if strings.TrimSpace(value) == "" {
			if required {
				r.Errorf(configdoc.OrNode(n, node), path+"."+name, "add a jq expression for %s, such as .id", name)
			}
			return ""
		}
		if err := blueprint.CompileExpression(value); err != nil {
			r.Errorf(n, path+"."+name, "%s is not valid jq: %v", name, err)
		}
		return value
	}
	if items := expression("items", false); items != "" {
		m.Items = items
	}
	m.Filter = expression("filter", false)
	m.Identifier = expression("identifier", true)
	m.Title = expression("title", false)

	m.Properties = parseFields(r, path+".properties", fields["properties"])

	return m
}

func parseFields(r *configdoc.Reader, path string, node *yaml.Node) []Field {
	out := []Field{}
	r.Each(node, path, func(id string, key, value *yaml.Node) {
		field := Field{ID: id, Expression: r.Text(value, path+"."+id), line: key.Line}
		if err := blueprint.CompileExpression(field.Expression); err != nil {
			r.Errorf(value, path+"."+id, "%s is not valid jq: %v", id, err)
		}
		out = append(out, field)
	})

	return out
}

// Check checks mappings against the published blueprints: unpublished
// blueprints are warnings, properties they can't write are errors.
func Check(spec Spec, published map[string]blueprint.Spec) []configdoc.Issue {
	var issues []configdoc.Issue
	for i, m := range spec.Mappings {
		path := fmt.Sprintf("mappings[%d]", i)
		bp, ok := published[m.Blueprint]
		if !ok {
			if configdoc.ValidIdentifier(m.Blueprint) {
				issues = append(issues, configdoc.Issue{Severity: configdoc.SeverityWarning, Path: path + ".blueprint", Line: m.line,
					Message: fmt.Sprintf("no blueprint %q is published yet, so ingesting into it fails until it is", m.Blueprint)})
			}
			continue
		}
		for _, f := range m.Properties {
			if !bp.HasProperty(f.ID) {
				issues = append(issues, configdoc.Issue{Severity: configdoc.SeverityError, Path: path + ".properties." + f.ID,
					Line: f.line, Message: fmt.Sprintf("%s has no stored property %q to write", m.Blueprint, f.ID)})
			}
		}
	}

	return issues
}
