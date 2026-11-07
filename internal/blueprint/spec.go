// Package blueprint defines the shape of catalog entities: their properties,
// including relations to other entities. Builders write blueprints as YAML, save
// them as drafts and publish them as numbered versions.
package blueprint

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/jq"
	"github.com/dvjn/goldenportal/internal/widget"
)

// Spec is a parsed blueprint. Its lists keep the order they were written in,
// which is the order the catalog shows them.
type Spec struct {
	Identifier  string     `json:"identifier"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Icon        string     `json:"icon,omitempty"`
	Color       string     `json:"color,omitempty"`
	Properties  []Property `json:"properties"`
	// Catalog and Entity lay out the blueprint's catalog page and its
	// entities' pages as tabs of widgets, beside the tabs every page has.
	Catalog *Page `json:"catalog,omitempty"`
	Entity  *Page `json:"entity,omitempty"`
}

// Page is the tabs a blueprint adds to one of its pages, and the queries
// their widgets share. Entity pages also take a header and sections for
// their properties.
type Page struct {
	// Columns are the catalog table's columns until someone picks their
	// own: properties, and the dates every entity has.
	Columns  []string       `json:"columns,omitempty"`
	Header   *Header        `json:"header,omitempty"`
	Sections []Section      `json:"sections,omitempty"`
	Queries  []widget.Query `json:"queries,omitempty"`
	Tabs     []widget.Tab   `json:"tabs"`
}

// Tabs every page has. An entity tab named overview replaces the built-in one.
var (
	CatalogTabs = []string{"entities"}
	EntityTabs  = []string{"relations", "yaml"}
)

// Property is a value entities have. Integrations write stored properties,
// including relations, whose values are identifiers of entities of another
// blueprint. The portal derives the others, from a lookup through relations or a jq
// computation.
type Property struct {
	ID          string   `json:"identifier"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Type        string   `json:"type"`
	Format      string   `json:"format,omitempty"`
	Enum        []any    `json:"enum,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	MinLength   *int     `json:"minLength,omitempty"`
	MaxLength   *int     `json:"maxLength,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
	Items       *Items   `json:"items,omitempty"`
	// Relation names the blueprint a relation points at; its type is "relation".
	Relation string `json:"relation,omitempty"`
	Many     bool   `json:"many,omitempty"`
	// Lookup reads a value from a related entity, such as owner.slackChannel.
	Lookup string `json:"lookup,omitempty"`
	// Scorecard names the scorecard whose result the property holds; its
	// type is "scorecard".
	Scorecard string `json:"scorecard,omitempty"`
	// Compute is a jq expression evaluated against the entity.
	Compute string   `json:"compute,omitempty"`
	Display *Display `json:"display,omitempty"`
	line    int
}

// Derived reports whether the portal works the value out, so integrations can't write it.
func (p Property) Derived() bool { return p.Lookup != "" || p.Compute != "" || p.IsScorecard() }

func (p Property) IsRelation() bool { return p.Relation != "" }

func (p Property) IsScorecard() bool { return p.Scorecard != "" }

// Items describes the values of an array property.
type Items struct {
	Type   string `json:"type"`
	Format string `json:"format,omitempty"`
	Enum   []any  `json:"enum,omitempty"`
}

type (
	Issue  = configdoc.Issue
	Change = configdoc.Change
)

const (
	SeverityError   = configdoc.SeverityError
	SeverityWarning = configdoc.SeverityWarning
)

// HasErrors reports whether any issue is an error.
func HasErrors(issues []Issue) bool { return configdoc.HasErrors(issues) }

func (s Spec) Key() string { return s.Identifier }

func (s Spec) Meta() configdoc.Meta {
	return configdoc.Meta{Title: s.Title, Description: s.Description, Icon: s.Icon, Color: s.Color}
}

func (s Spec) Stats() map[string]int {
	relations := 0
	for _, prop := range s.Properties {
		if prop.IsRelation() {
			relations++
		}
	}

	return map[string]int{"properties": len(s.Properties) - relations, "relations": relations}
}

// MaxLookupHops caps how many relations a lookup may follow.
const MaxLookupHops = 3

var (
	fieldPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,62}$`)

	propertyTypes = []string{"string", "number", "integer", "boolean", "array", "object"}
	itemTypes     = []string{"string", "number", "integer", "boolean", "object"}
	stringFormats = []string{"date", "date-time", "uri", "email", "markdown"}

	// BuiltIns are the fields every entity has. Paths and jq read them
	// beside its properties, so no property can be named after one.
	BuiltIns = []string{"identifier", "title", "blueprint", "integration", "createdAt", "updatedAt"}
)

// The keys each mapping of a blueprint takes, exported so the documents'
// JSON Schema can be checked against them.
var (
	SpecFields     = []string{"properties", "catalog", "entity"}
	PropertyFields = []string{
		"title", "description", "type", "format", "enum", "required", "pattern", "minLength", "maxLength",
		"minimum", "maximum", "items", "relation", "many", "lookup", "compute", "scorecard", "display",
	}
	ItemsFields   = []string{"type", "format", "enum"}
	CatalogFields = []string{"columns", "queries", "tabs"}
	EntityFields  = []string{"header", "sections", "queries", "tabs"}
	DisplayFields = []string{"as", "unit", "colors", "titles", "filter", "hidden", "empty"}
	HeaderFields  = []string{"subtitle", "badges", "link"}
	SectionFields = []string{"title", "properties"}
)

// ValidIdentifier reports whether s can name a blueprint.
func ValidIdentifier(s string) bool { return configdoc.ValidIdentifier(s) }

// DocumentKind is how blueprint documents name their kind.
const DocumentKind = "Blueprint"

// Parse reads a blueprint document from YAML (or JSON, which is YAML too) and
// checks everything that doesn't depend on other blueprints.
func Parse(source string) (Spec, []Issue) {
	p := &parser{}
	doc := configdoc.ReadDocument(&p.Reader, source, DocumentKind)
	spec := Spec{
		Identifier: doc.Identifier, Title: doc.Meta.Title, Description: doc.Meta.Description,
		Icon: doc.Meta.Icon, Color: doc.Meta.Color, Properties: []Property{},
	}
	if doc.Spec == nil {
		return spec, p.Issues
	}

	if fields, ok := p.Fields(doc.Spec, "spec", SpecFields...); ok {
		p.Each(fields["properties"], "spec.properties", func(id string, key, value *yaml.Node) {
			spec.Properties = append(spec.Properties, p.property(id, "spec.properties."+id, key, value))
		})
		spec.Catalog = p.page(fields["catalog"], "spec.catalog", widget.Scope{Blueprint: spec.Identifier}, CatalogTabs)
		if spec.Catalog != nil {
			spec.Catalog.Columns = p.columns(fields["catalog"], spec)
		}
		spec.Entity = p.entityPage(fields["entity"], "spec.entity", spec)
	}
	if spec.Entity != nil {
		for _, tab := range spec.Entity.Tabs {
			for _, w := range tab.Widgets {
				for _, name := range widget.EntityRefs(w) {
					if slices.Contains(BuiltIns, name) {
						continue
					}
					if _, ok := spec.Property(name); !ok {
						p.Issues = append(p.Issues, Issue{Severity: SeverityError, Path: w.Path, Line: w.Line, Message: fmt.Sprintf("unknown variable $entity.%s; %s has no property %q", name, spec.Identifier, name)})
					}
				}
			}
		}
	}
	p.checkNames(spec)

	return spec, p.Issues
}

type parser struct{ configdoc.Reader }

func (p *parser) property(id, path string, key, node *yaml.Node) Property {
	prop := Property{ID: id, Title: id, line: key.Line}
	fields, ok := p.Fields(node, path, PropertyFields...)
	if !ok {
		return prop
	}
	prop = p.shape(prop, path, key, fields)
	if n := fields["display"]; n != nil {
		prop.Display = p.display(n, path+".display", prop)
	}

	return prop
}

// shape reads what kind of property it is and the values it takes.
func (p *parser) shape(prop Property, path string, key *yaml.Node, fields map[string]*yaml.Node) Property {
	p.titles(fields, path, &prop.Title, &prop.Description)
	if fields["relation"] != nil {
		return p.relation(prop, path, fields)
	}
	if fields["scorecard"] != nil || (fields["type"] != nil && fields["type"].Value == "scorecard") {
		return p.scorecard(prop, path, key, fields)
	}
	if n := fields["many"]; n != nil {
		p.Errorf(n, path+".many", "only relations can be many; arrays hold many values")
	}
	if fields["lookup"] != nil || fields["compute"] != nil {
		return p.derived(prop, path, key, fields)
	}
	prop.Type = p.Text(fields["type"], path+".type")
	if !slices.Contains(propertyTypes, prop.Type) {
		p.Errorf(configdoc.OrNode(fields["type"], key), path+".type", "type must be one of %s", strings.Join(propertyTypes, ", "))
		return prop
	}
	prop.Required = p.Bool(fields["required"], path+".required")
	prop.Format = p.format(fields["format"], path+".format", prop.Type)
	prop.Enum = p.enum(fields["enum"], path+".enum", prop.Type)

	if n := fields["pattern"]; n != nil {
		prop.Pattern = p.Text(n, path+".pattern")
		if prop.Type != "string" {
			p.Errorf(n, path+".pattern", "only string properties can have a pattern")
		} else if _, err := regexp.Compile(prop.Pattern); err != nil {
			p.Errorf(n, path+".pattern", "pattern is not a valid regular expression: %v", err)
		}
	}
	for _, bound := range []struct {
		name   string
		target **int
	}{{"minLength", &prop.MinLength}, {"maxLength", &prop.MaxLength}} {
		if n := fields[bound.name]; n != nil {
			if prop.Type != "string" {
				p.Errorf(n, path+"."+bound.name, "only string properties can have %s", bound.name)
				continue
			}
			var value int
			if err := n.Decode(&value); err != nil || value < 0 {
				p.Errorf(n, path+"."+bound.name, "%s must be a whole number of 0 or more", bound.name)
				continue
			}
			*bound.target = &value
		}
	}
	for _, bound := range []struct {
		name   string
		target **float64
	}{{"minimum", &prop.Minimum}, {"maximum", &prop.Maximum}} {
		if n := fields[bound.name]; n != nil {
			if prop.Type != "number" && prop.Type != "integer" {
				p.Errorf(n, path+"."+bound.name, "only number and integer properties can have a %s", bound.name)
				continue
			}
			var value float64
			if err := n.Decode(&value); err != nil {
				p.Errorf(n, path+"."+bound.name, "%s must be a number", bound.name)
				continue
			}
			*bound.target = &value
		}
	}
	if prop.MinLength != nil && prop.MaxLength != nil && *prop.MinLength > *prop.MaxLength {
		p.Errorf(fields["minLength"], path+".minLength", "minLength is more than maxLength")
	}
	if prop.Minimum != nil && prop.Maximum != nil && *prop.Minimum > *prop.Maximum {
		p.Errorf(fields["minimum"], path+".minimum", "minimum is more than maximum")
	}

	if n := fields["items"]; n != nil {
		if prop.Type != "array" {
			p.Errorf(n, path+".items", "only array properties have items")
		} else {
			prop.Items = p.items(n, path+".items")
		}
	} else if prop.Type == "array" {
		prop.Items = &Items{Type: "string"}
	}

	return prop
}

func (p *parser) items(node *yaml.Node, path string) *Items {
	items := &Items{}
	fields, ok := p.Fields(node, path, ItemsFields...)
	if !ok {
		return items
	}
	items.Type = p.Text(fields["type"], path+".type")
	if !slices.Contains(itemTypes, items.Type) {
		p.Errorf(configdoc.OrNode(fields["type"], node), path+".type", "items type must be one of %s", strings.Join(itemTypes, ", "))
		return items
	}
	items.Format = p.format(fields["format"], path+".format", items.Type)
	items.Enum = p.enum(fields["enum"], path+".enum", items.Type)

	return items
}

// relation reads a property whose values are identifiers of entities of
// another blueprint.
func (p *parser) relation(prop Property, path string, fields map[string]*yaml.Node) Property {
	prop.Type = "relation"
	prop.Relation = p.Text(fields["relation"], path+".relation")
	if !ValidIdentifier(prop.Relation) {
		p.Errorf(fields["relation"], path+".relation", "relation must name a blueprint by its identifier")
	}
	prop.Many = p.Bool(fields["many"], path+".many")
	prop.Required = p.Bool(fields["required"], path+".required")
	for _, name := range []string{
		"type", "format", "enum", "pattern", "minLength", "maxLength", "minimum", "maximum",
		"items", "lookup", "compute", "scorecard",
	} {
		if n := fields[name]; n != nil {
			p.Errorf(n, path+"."+name, "relations can't have %s", name)
		}
	}

	return prop
}

// scorecard reads a property holding an entity's result on a scorecard.
// Paths follow it like a relation, into its tier, score and checks.
func (p *parser) scorecard(prop Property, path string, key *yaml.Node, fields map[string]*yaml.Node) Property {
	prop.Type = "scorecard"
	if n := fields["type"]; n != nil && n.Value != "scorecard" {
		p.Errorf(n, path+".type", "a property naming a scorecard has type: scorecard")
	}
	prop.Scorecard = p.Text(fields["scorecard"], path+".scorecard")
	if !configdoc.ValidIdentifier(prop.Scorecard) {
		p.Errorf(configdoc.OrNode(fields["scorecard"], key), path+".scorecard", "scorecard must name the scorecard whose result it holds")
	}
	for _, name := range []string{
		"format", "enum", "required", "pattern", "minLength", "maxLength", "minimum", "maximum",
		"items", "many", "lookup", "compute",
	} {
		if n := fields[name]; n != nil {
			p.Errorf(n, path+"."+name, "scorecard properties can't have %s; the scorecard works the value out", name)
		}
	}

	return prop
}

// derived reads a looked-up or computed property. Their values aren't
// written by integrations, so they take no constraints.
func (p *parser) derived(prop Property, path string, key *yaml.Node, fields map[string]*yaml.Node) Property {
	if fields["lookup"] != nil && fields["compute"] != nil {
		p.Errorf(fields["compute"], path, "a property looks up or computes its value, not both")
		return prop
	}
	for _, name := range []string{"enum", "required", "pattern", "minLength", "maxLength", "minimum", "maximum"} {
		if n := fields[name]; n != nil {
			p.Errorf(n, path+"."+name, "%s only applies to stored properties, which integrations write", name)
		}
	}

	if n := fields["lookup"]; n != nil {
		prop.Lookup = p.Text(n, path+".lookup")
		segments := strings.Split(prop.Lookup, ".")
		switch {
		case strings.Contains(prop.Lookup, "$"):
			p.Errorf(n, path+".lookup", "lookups are plain paths, and $ marks variables: write %s", strings.ReplaceAll(prop.Lookup, "$", ""))
		case len(segments) < 2 || len(segments) > MaxLookupHops+1 || slices.Contains(segments, ""):
			p.Errorf(n, path+".lookup",
				"lookup must follow 1 to %d relations and end on a property, such as owner.title", MaxLookupHops)
		}
		// The type comes from the looked-up property, so it's optional.
		if fields["type"] == nil {
			return prop
		}
	}

	if n := fields["compute"]; n != nil {
		prop.Compute = p.Text(n, path+".compute")
		if strings.TrimSpace(prop.Compute) == "" {
			p.Errorf(n, path+".compute", "add a jq expression, such as .stars > 100")
		} else if err := CompileExpression(prop.Compute); err != nil {
			p.Errorf(n, path+".compute", "compute is not valid jq: %v", err)
		}
	}
	prop.Type = p.Text(fields["type"], path+".type")
	if !slices.Contains(propertyTypes, prop.Type) {
		p.Errorf(configdoc.OrNode(fields["type"], key), path+".type", "type must be one of %s", strings.Join(propertyTypes, ", "))
		return prop
	}
	prop.Format = p.format(fields["format"], path+".format", prop.Type)
	if n := fields["items"]; n != nil {
		if prop.Type != "array" {
			p.Errorf(n, path+".items", "only array properties have items")
		} else {
			prop.Items = p.items(n, path+".items")
		}
	}

	return prop
}

func (p *parser) page(node *yaml.Node, path string, scope widget.Scope, reserved []string) *Page {
	if node == nil {
		return nil
	}
	fields, ok := p.Fields(node, path, CatalogFields...)
	if !ok {
		return nil
	}

	return p.tabs(fields, path, scope, reserved)
}

// columnBuiltIns are the built-ins a catalog column can show; the title
// column, always first, already shows the title and identifier.
var columnBuiltIns = []string{"integration", "createdAt", "updatedAt"}

// columns reads the catalog's default columns, once the properties they
// name are known.
func (p *parser) columns(catalog *yaml.Node, spec Spec) []string {
	var node *yaml.Node
	for i := 0; i+1 < len(catalog.Content); i += 2 {
		if catalog.Content[i].Value == "columns" {
			node = catalog.Content[i+1]
		}
	}
	if node == nil {
		return nil
	}
	columns := []string{}
	p.List(node, "spec.catalog.columns", func(i int, item *yaml.Node) {
		path := fmt.Sprintf("spec.catalog.columns[%d]", i)
		name := p.Text(item, path)
		switch {
		case name == "":
		case slices.Contains(columns, name):
			p.Errorf(item, path, "%s is already a column", name)
		case slices.Contains(columnBuiltIns, name):
			columns = append(columns, name)
		case slices.Contains(BuiltIns, name):
			p.Errorf(item, path, "the title column already shows the %s", name)
		default:
			if _, ok := spec.Property(name); !ok {
				p.Errorf(item, path, "%s has no property %q; columns are properties, integration, createdAt or updatedAt", spec.Identifier, name)
				return
			}
			columns = append(columns, name)
		}
	})

	return columns
}

// tabs reads a page's queries, then the tabs whose widgets use them.
func (p *parser) tabs(fields map[string]*yaml.Node, path string, scope widget.Scope, reserved []string) *Page {
	scope.Queries = widget.ParseQueries(&p.Reader, fields["queries"], path+".queries", scope)
	page := &Page{Tabs: widget.ParseTabs(&p.Reader, fields["tabs"], path+".tabs", scope, reserved...)}
	if len(scope.Queries) > 0 {
		page.Queries = scope.Queries
	}

	return page
}

func (p *parser) entityPage(node *yaml.Node, path string, spec Spec) *Page {
	if node == nil {
		return nil
	}
	fields, ok := p.Fields(node, path, EntityFields...)
	if !ok {
		return nil
	}
	page := p.tabs(fields, path, widget.Scope{Blueprint: spec.Identifier, Entity: true}, EntityTabs)
	if n := fields["header"]; n != nil {
		page.Header = p.header(n, path+".header", spec)
	}
	if n := fields["sections"]; n != nil {
		page.Sections = p.sections(n, path+".sections", spec)
	}

	return page
}

// CompileExpression checks a jq expression over an entity compiles. jq
// sees the entity flat, as paths do.
func CompileExpression(expression string) error {
	_, err := jq.Compile(expression)

	return err
}

// checkNames checks every property name is valid.
func (p *parser) checkNames(spec Spec) {
	check := func(id, section string, line int) {
		path := "spec." + section + "." + id
		switch {
		case !fieldPattern.MatchString(id):
			p.Issues = append(p.Issues, Issue{
				Severity: SeverityError, Path: path, Line: line,
				Message: fmt.Sprintf("%q must start with a letter and use only letters, digits and _ (up to 63)", id),
			})
		case slices.Contains(BuiltIns, id):
			p.Issues = append(p.Issues, Issue{
				Severity: SeverityError, Path: path, Line: line,
				Message: fmt.Sprintf("%s is a built-in field every entity has, so a property can't be named %s", id, id),
			})
		}
	}
	for _, prop := range spec.Properties {
		check(prop.ID, "properties", prop.line)
	}
}

func (p *parser) titles(fields map[string]*yaml.Node, path string, title, description *string) {
	if n := fields["title"]; n != nil {
		*title = p.Text(n, path+".title")
	}
	*description = p.Text(fields["description"], path+".description")
}

func (p *parser) format(node *yaml.Node, path, valueType string) string {
	format := p.Text(node, path)
	if node == nil {
		return ""
	}
	if valueType != "string" {
		p.Errorf(node, path, "only string values have a format")
	} else if !slices.Contains(stringFormats, format) {
		p.Errorf(node, path, "format must be one of %s", strings.Join(stringFormats, ", "))
	}

	return format
}

func (p *parser) enum(node *yaml.Node, path, valueType string) []any {
	if node == nil {
		return nil
	}
	if valueType == "array" || valueType == "object" || valueType == "boolean" {
		p.Errorf(node, path, "%s values can't have an enum", valueType)
		return nil
	}
	if node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		p.Errorf(node, path, "enum must be a list of allowed values")
		return nil
	}

	values := make([]any, 0, len(node.Content))
	for _, item := range node.Content {
		value, ok := scalar(item, valueType)
		if !ok {
			p.Errorf(item, path, "enum value %q is not of type %s", item.Value, valueType)
			continue
		}
		if slices.Contains(values, value) {
			p.Errorf(item, path, "enum lists %q twice", item.Value)
			continue
		}
		values = append(values, value)
	}

	return values
}

// scalar reads a node as a value of valueType. Numbers are float64, as they are in JSON.
func scalar(node *yaml.Node, valueType string) (any, bool) {
	if node.Kind != yaml.ScalarNode {
		return nil, false
	}
	switch valueType {
	case "string":
		return node.Value, node.Tag == "!!str"
	case "number", "integer":
		var value float64
		if node.Decode(&value) != nil || node.Tag == "!!str" {
			return nil, false
		}
		if valueType == "integer" && value != float64(int64(value)) {
			return nil, false
		}
		return value, true
	}

	return nil, false
}
