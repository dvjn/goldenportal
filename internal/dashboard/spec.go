// Package dashboard serves Home's dashboards, and checks and loads every
// widget: those on dashboards and those on blueprints' pages.
package dashboard

import (
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/widget"
)

const (
	// Name is the configdoc kind dashboards are stored as.
	Name = "dashboard"
	// DocumentKind is how dashboard documents name their kind.
	DocumentKind = "Dashboard"
)

// Spec is a parsed dashboard: a tab of widgets on Home.
type Spec struct {
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	// Order sorts Home's tabs, lowest first.
	Order   int             `json:"order"`
	Filters []Filter        `json:"filters"`
	Widgets []widget.Widget `json:"widgets"`
}

// Filter is a value Home asks for above the dashboard, which its widgets
// compare with as $filter.<id>.
type Filter struct {
	ID    string `json:"identifier"`
	Title string `json:"title"`
	Type  string `json:"type"`
	// Values are what an enum filter offers.
	Values []string `json:"values,omitempty"`
	// Relation is the blueprint whose entities a relation filter offers.
	Relation string `json:"relation,omitempty"`
	Multiple bool   `json:"multiple,omitempty"`
	// Default is what the filter is set to until someone changes it.
	Default []string `json:"default,omitempty"`
	// Lookup finds the default in the catalog instead, for whoever is looking.
	Lookup *Lookup `json:"lookup,omitempty"`
	line   int
}

// Lookup is a default found when the dashboard loads: the entities of a
// blueprint that match where, which can compare with $me.
type Lookup struct {
	From  string         `json:"from"`
	Where *filter.Filter `json:"where"`
}

// Filter types.
const (
	FilterEnum     = "enum"
	FilterString   = "string"
	FilterRelation = "relation"
)

var filterTypes = []string{FilterEnum, FilterString, FilterRelation}

// reservedFilters can't name a filter, since Home's URL uses them.
var reservedFilters = []string{"d"}

func (s Spec) Key() string { return s.Identifier }

func (s Spec) Meta() configdoc.Meta {
	return configdoc.Meta{Title: s.Title, Description: s.Description, Icon: s.Icon}
}

func (s Spec) Stats() map[string]int { return map[string]int{"widgets": len(s.Widgets)} }

// render fills in the viewer's $me variables in the dashboard's text, for
// the one looking.
func (s Spec) render(me widget.Values) Spec {
	s.Title, s.Description = widget.Render(s.Title, me), widget.Render(s.Description, me)
	s.Widgets = widget.RenderAll(s.Widgets, me)

	return s
}

// Parse reads a dashboard document and checks everything that doesn't
// depend on other documents.
func Parse(source string) (Spec, []configdoc.Issue) {
	r := &configdoc.Reader{}
	doc := configdoc.ReadDocument(r, source, DocumentKind)
	spec := Spec{
		Identifier: doc.Name, Title: doc.Meta.Title, Description: doc.Meta.Description,
		Icon: doc.Meta.Icon, Filters: []Filter{}, Widgets: []widget.Widget{},
	}
	for _, name := range []string{"title", "description"} {
		if n := configdoc.Field(configdoc.Field(doc.Root, "metadata"), name); n != nil {
			if err := widget.CheckText(n.Value); err != nil {
				r.Errorf(n, "metadata."+name, "%v", err)
			}
		}
	}
	if doc.Spec == nil {
		return spec, r.Issues
	}
	fields, ok := r.Fields(doc.Spec, "spec", "order", "filters", "widgets")
	if !ok {
		return spec, r.Issues
	}

	if n := fields["order"]; n != nil {
		if err := n.Decode(&spec.Order); err != nil {
			r.Errorf(n, "spec.order", "order must be a whole number")
		}
	}
	scope := widget.Scope{}
	r.Each(fields["filters"], "spec.filters", func(id string, key, value *yaml.Node) {
		spec.Filters = append(spec.Filters, parseFilter(r, "spec.filters."+id, id, key, value))
		scope.Filters = append(scope.Filters, id)
	})
	spec.Widgets = widget.Parse(r, fields["widgets"], "spec.widgets", scope)
	if len(spec.Widgets) == 0 {
		r.Errorf(configdoc.OrNode(fields["widgets"], doc.Spec), "spec.widgets", "add the widgets, keyed by identifier")
	}

	return spec, r.Issues
}

func parseFilter(r *configdoc.Reader, path, id string, key, node *yaml.Node) Filter {
	f := Filter{ID: id, Title: id, line: key.Line}
	switch {
	case !widget.ValidName(id):
		r.Errorf(key, path, "%q must start with a letter and use only letters, digits, - and _", id)
	case slices.Contains(reservedFilters, id):
		r.Errorf(key, path, "%s picks the open dashboard in Home's URL, so filters can't be named %s", id, id)
	}
	fields, ok := r.Fields(node, path, "title", "type", "values", "relation", "multiple", "default")
	if !ok {
		return f
	}
	if n := fields["title"]; n != nil {
		f.Title = r.Text(n, path+".title")
	}
	f.Type = r.Text(fields["type"], path+".type")
	if !slices.Contains(filterTypes, f.Type) {
		r.Errorf(configdoc.OrNode(fields["type"], key), path+".type", "type must be one of %s", strings.Join(filterTypes, ", "))
		return f
	}
	f.Multiple = r.Bool(fields["multiple"], path+".multiple")

	switch f.Type {
	case FilterEnum:
		f.Values = texts(r, fields["values"], path+".values")
		if len(f.Values) == 0 {
			r.Errorf(configdoc.OrNode(fields["values"], key), path+".values", "list the values an enum filter offers")
		}
		for i, value := range f.Values {
			if slices.Contains(f.Values[:i], value) {
				r.Errorf(fields["values"], path+".values", "values lists %q twice", value)
			}
		}
		if n := fields["relation"]; n != nil {
			r.Errorf(n, path+".relation", "only relation filters name a blueprint")
		}
	case FilterRelation:
		f.Relation = r.Text(fields["relation"], path+".relation")
		if !configdoc.ValidIdentifier(f.Relation) {
			r.Errorf(configdoc.OrNode(fields["relation"], key), path+".relation", "relation must name the blueprint to pick from")
		}
		if n := fields["values"]; n != nil {
			r.Errorf(n, path+".values", "only enum filters list values")
		}
	case FilterString:
		for _, name := range []string{"values", "relation"} {
			if n := fields[name]; n != nil {
				r.Errorf(n, path+"."+name, "string filters take any text, so they have no %s", name)
			}
		}
	}

	if n := fields["default"]; n != nil && n.Kind == yaml.MappingNode {
		f.Lookup = parseLookup(r, n, path+".default", f)
	} else if n != nil {
		f.Default = parseDefault(r, n, path+".default", f)
	}

	return f
}

// parseLookup reads a default written as from and where.
func parseLookup(r *configdoc.Reader, node *yaml.Node, path string, f Filter) *Lookup {
	if f.Type != FilterRelation || !f.Multiple {
		r.Errorf(node, path, "a lookup default picks entities, so only a relation filter with multiple: true can have one")
		return nil
	}
	fields, ok := r.Fields(node, path, "from", "where")
	if !ok {
		return nil
	}
	l := &Lookup{From: r.Text(fields["from"], path+".from")}
	switch {
	case !configdoc.ValidIdentifier(l.From):
		r.Errorf(configdoc.OrNode(fields["from"], node), path+".from", "from must name the blueprint to find the default in")
	case l.From != f.Relation:
		r.Errorf(fields["from"], path+".from", "from must be %s, the blueprint the filter picks from", f.Relation)
	}
	if fields["where"] == nil {
		r.Errorf(node, path+".where", "add where, the condition the default's entities match")
		return l
	}
	l.Where = widget.ReadFilter(r, fields["where"], path+".where")
	if widget.Uses(l.Where, widget.ValueEntity) || len(widget.FilterRefs(l.Where)) > 0 {
		r.Errorf(fields["where"], path+".where", "where can compare with %s and %s.<attribute>, but not with %s or the dashboard's filters", widget.ValueMe, widget.ValueMe, widget.ValueEntity)
	}

	return l
}

// parseDefault reads one value, or for a multiple filter a list of them.
func parseDefault(r *configdoc.Reader, node *yaml.Node, path string, f Filter) []string {
	var values []string
	switch {
	case node.Kind == yaml.ScalarNode:
		values = []string{node.Value}
	case !f.Multiple:
		r.Errorf(node, path, "default is one value; set multiple: true to default to several")
		return nil
	default:
		values = texts(r, node, path)
	}
	for _, value := range values {
		switch {
		case strings.TrimSpace(value) == "":
			r.Errorf(node, path, "default can't be empty; leave it out to start unset")
		case f.Type == FilterEnum && !slices.Contains(f.Values, value):
			r.Errorf(node, path, "default %q is not one of the values", value)
		}
	}

	return values
}

func texts(r *configdoc.Reader, node *yaml.Node, path string) []string {
	var values []string
	r.List(node, path, func(i int, item *yaml.Node) {
		values = append(values, r.Text(item, path+"["+strconv.Itoa(i)+"]"))
	})

	return values
}
