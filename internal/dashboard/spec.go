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
	Color       string `json:"color,omitempty"`
	// Order sorts Home's tabs, lowest first.
	Order int `json:"order"`
	// Group puts the dashboard under one tab with the others of the same
	// group title, which picks between them from a menu.
	Group      *Group          `json:"group,omitempty"`
	Visibility *Audience       `json:"visibility,omitempty"`
	DefaultFor *Audience       `json:"defaultFor,omitempty"`
	Inputs     []Input         `json:"inputs"`
	Queries    []widget.Query  `json:"queries"`
	Widgets    []widget.Widget `json:"widgets"`
}

// Group is a tab several dashboards share. The first of them in Home's
// order gives it its icon.
type Group struct {
	Title string `json:"title"`
	Icon  string `json:"icon,omitempty"`
}

// Input is a value Home asks for above the dashboard. It narrows only the
// queries that compare with it as $input.<id>, and while it's unset their
// conditions on it drop.
type Input struct {
	ID    string `json:"identifier"`
	Title string `json:"title"`
	Type  string `json:"type"`
	// Values are what an enum input offers.
	Values []string `json:"values,omitempty"`
	// Relation is the blueprint whose entities a relation input offers.
	Relation string `json:"relation,omitempty"`
	Multiple bool   `json:"multiple,omitempty"`
	// Default is what the input is set to until someone changes it.
	Default []string `json:"default,omitempty"`
	// DefaultQuery finds the default in the catalog instead, for whoever
	// is looking.
	DefaultQuery *DefaultQuery `json:"defaultQuery,omitempty"`
	line         int
}

// DefaultQuery is a default found when the dashboard loads: the entities
// a query picks, whose filter can compare with $me.
type DefaultQuery struct {
	Query widget.Query `json:"query"`
	// Otherwise is what the input starts at when the query finds nothing:
	// nothing, the default, or everything, as if it had no default.
	Otherwise string `json:"otherwise,omitempty"`
}

// What a default query that finds nothing starts the input at.
const (
	OtherwiseNothing    = "nothing"
	OtherwiseEverything = "everything"
)

// Input types.
const (
	InputEnum     = "enum"
	InputString   = "string"
	InputRelation = "relation"
)

var inputTypes = []string{InputEnum, InputString, InputRelation}

// The keys each mapping of a dashboard takes, exported so the documents'
// JSON Schema can be checked against them.
var (
	SpecFields     = []string{"order", "group", "inputs", "queries", "widgets", "visibility", "defaultFor"}
	InputFields    = []string{"title", "type", "values", "relation", "multiple", "default"}
	DefaultFields  = []string{"query", "otherwise"}
	AudienceFields = []string{"roles", "groups"}
	GroupFields    = []string{"title", "icon"}
)

// reservedInputs can't name an input, since Home's URL uses them.
var reservedInputs = []string{"d"}

func (s Spec) Key() string { return s.Identifier }

func (s Spec) Meta() configdoc.Meta {
	return configdoc.Meta{Title: s.Title, Description: s.Description, Icon: s.Icon, Color: s.Color}
}

func (s Spec) Stats() map[string]int { return map[string]int{"widgets": len(s.Widgets)} }

// Facts show where the dashboard sits on Home and who sees it there.
func (s Spec) Facts() map[string]any {
	return map[string]any{"order": s.Order, "group": s.Group, "visibility": s.Visibility, "defaultFor": s.DefaultFor}
}

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
		Identifier: doc.Identifier, Title: doc.Meta.Title, Description: doc.Meta.Description,
		Icon: doc.Meta.Icon, Color: doc.Meta.Color, Inputs: []Input{}, Queries: []widget.Query{}, Widgets: []widget.Widget{},
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
	fields, ok := r.Fields(doc.Spec, "spec", SpecFields...)
	if !ok {
		return spec, r.Issues
	}

	if n := fields["order"]; n != nil {
		if err := n.Decode(&spec.Order); err != nil {
			r.Errorf(n, "spec.order", "order must be a whole number")
		}
	}
	spec.Group = parseGroup(r, fields["group"])
	spec.Visibility = parseAudience(r, fields["visibility"], "spec.visibility")
	spec.DefaultFor = parseAudience(r, fields["defaultFor"], "spec.defaultFor")
	scope := widget.Scope{}
	r.Each(fields["inputs"], "spec.inputs", func(id string, key, value *yaml.Node) {
		spec.Inputs = append(spec.Inputs, parseInput(r, "spec.inputs."+id, id, key, value))
		scope.Inputs = append(scope.Inputs, id)
	})
	spec.Queries = widget.ParseQueries(r, fields["queries"], "spec.queries", scope)
	scope.Queries = spec.Queries
	spec.Widgets = widget.Parse(r, fields["widgets"], "spec.widgets", scope)
	if len(spec.Widgets) == 0 {
		r.Errorf(configdoc.OrNode(fields["widgets"], doc.Spec), "spec.widgets", "add the widgets, keyed by identifier")
	}
	for _, in := range unused(spec) {
		r.Report(configdoc.SeverityWarning, &yaml.Node{Line: in.line}, "spec.inputs."+in.ID,
			"no query compares with %s%s, so the input narrows nothing", widget.ValueInput, in.ID)
	}

	return spec, r.Issues
}

// unused lists the inputs nothing compares with: no query, value, link
// filter or link template.
func unused(spec Spec) []Input {
	used := map[string]bool{}
	mark := func(f *filter.Filter) {
		for _, ref := range widget.InputRefs(f) {
			used[strings.TrimPrefix(ref, widget.ValueInput)] = true
		}
	}
	for _, q := range spec.Queries {
		mark(q.Filter)
	}
	var templates []string
	for _, w := range spec.Widgets {
		for _, f := range w.Filters() {
			mark(f)
		}
		for _, l := range []*widget.Link{w.Link, w.RowLink} {
			if l != nil {
				templates = append(templates, l.URL, l.Path)
			}
		}
	}
	var out []Input
	for _, in := range spec.Inputs {
		mentioned := slices.ContainsFunc(templates, func(t string) bool { return strings.Contains(t, widget.ValueInput+in.ID) })
		if !used[in.ID] && !mentioned {
			out = append(out, in)
		}
	}

	return out
}

func parseInput(r *configdoc.Reader, path, id string, key, node *yaml.Node) Input {
	f := Input{ID: id, Title: id, line: key.Line}
	switch {
	case !widget.ValidName(id):
		r.Errorf(key, path, "%q must start with a letter and use only letters, digits, - and _", id)
	case slices.Contains(reservedInputs, id):
		r.Errorf(key, path, "%s picks the open dashboard in Home's URL, so inputs can't be named %s", id, id)
	}
	fields, ok := r.Fields(node, path, InputFields...)
	if !ok {
		return f
	}
	if n := fields["title"]; n != nil {
		f.Title = r.Text(n, path+".title")
	}
	f.Type = r.Text(fields["type"], path+".type")
	if !slices.Contains(inputTypes, f.Type) {
		r.Errorf(configdoc.OrNode(fields["type"], key), path+".type", "type must be one of %s", strings.Join(inputTypes, ", "))
		return f
	}
	f.Multiple = r.Bool(fields["multiple"], path+".multiple")

	switch f.Type {
	case InputEnum:
		f.Values = texts(r, fields["values"], path+".values")
		if len(f.Values) == 0 {
			r.Errorf(configdoc.OrNode(fields["values"], key), path+".values", "list the values an enum input offers")
		}
		for i, value := range f.Values {
			if slices.Contains(f.Values[:i], value) {
				r.Errorf(fields["values"], path+".values", "values lists %q twice", value)
			}
		}
		if n := fields["relation"]; n != nil {
			r.Errorf(n, path+".relation", "only relation inputs name a blueprint")
		}
	case InputRelation:
		f.Relation = r.Text(fields["relation"], path+".relation")
		if !configdoc.ValidIdentifier(f.Relation) {
			r.Errorf(configdoc.OrNode(fields["relation"], key), path+".relation", "relation must name the blueprint to pick from")
		}
		if n := fields["values"]; n != nil {
			r.Errorf(n, path+".values", "only enum inputs list values")
		}
	case InputString:
		for _, name := range []string{"values", "relation"} {
			if n := fields[name]; n != nil {
				r.Errorf(n, path+"."+name, "string inputs take any text, so they have no %s", name)
			}
		}
	}

	if n := fields["default"]; n != nil && n.Kind == yaml.MappingNode {
		f.DefaultQuery = parseDefaultQuery(r, n, path+".default", f)
	} else if n != nil {
		f.Default = parseDefault(r, n, path+".default", f)
	}

	return f
}

// parseDefaultQuery reads a default written as a query and what to start
// at when it finds nothing.
func parseDefaultQuery(r *configdoc.Reader, node *yaml.Node, path string, f Input) *DefaultQuery {
	if f.Type != InputRelation || !f.Multiple {
		r.Errorf(node, path, "a default query picks entities, so only a relation input with multiple: true can have one")
		return nil
	}
	fields, ok := r.Fields(node, path, DefaultFields...)
	if !ok {
		return nil
	}
	d := &DefaultQuery{}
	if n := fields["otherwise"]; n != nil {
		d.Otherwise = r.Text(n, path+".otherwise")
		if d.Otherwise != OtherwiseNothing && d.Otherwise != OtherwiseEverything {
			r.Errorf(n, path+".otherwise", "otherwise must be %s or %s", OtherwiseNothing, OtherwiseEverything)
		}
	}
	n := fields["query"]
	if n == nil {
		r.Errorf(node, path+".query", "add the query whose entities the input starts at, as blueprint and filter")
		return d
	}
	queryFields, ok := r.Fields(n, path+".query", widget.QueryFields...)
	if !ok {
		return d
	}
	d.Query.Blueprint = r.Text(queryFields["blueprint"], path+".query.blueprint")
	switch {
	case !configdoc.ValidIdentifier(d.Query.Blueprint):
		r.Errorf(configdoc.OrNode(queryFields["blueprint"], n), path+".query.blueprint", "blueprint must name the blueprint to find the default in")
	case d.Query.Blueprint != f.Relation:
		r.Errorf(queryFields["blueprint"], path+".query.blueprint", "blueprint must be %s, the blueprint the input picks from", f.Relation)
	}
	if fn := queryFields["filter"]; fn != nil {
		d.Query.Filter = widget.ReadFilter(r, fn, path+".query.filter")
		if widget.Uses(d.Query.Filter, widget.ValueEntity) || len(widget.InputRefs(d.Query.Filter)) > 0 {
			r.Errorf(fn, path+".query.filter", "a default's filter can compare with %s and %s.<attribute>, but not with %s or the dashboard's inputs", widget.ValueMe, widget.ValueMe, widget.ValueEntity)
		}
	}

	return d
}

// parseDefault reads a default, always a list: one value, or for a
// multiple input several.
func parseDefault(r *configdoc.Reader, node *yaml.Node, path string, f Input) []string {
	values := texts(r, node, path)
	if len(values) > 1 && !f.Multiple {
		r.Errorf(node, path, "default is one value; set multiple: true to default to several")
		return nil
	}
	for _, value := range values {
		switch {
		case strings.TrimSpace(value) == "":
			r.Errorf(node, path, "default can't be empty; leave it out to start unset")
		case f.Type == InputEnum && !slices.Contains(f.Values, value):
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

func parseGroup(r *configdoc.Reader, node *yaml.Node) *Group {
	if node == nil {
		return nil
	}
	fields, ok := r.Fields(node, "spec.group", GroupFields...)
	if !ok {
		return nil
	}
	g := &Group{Title: strings.TrimSpace(r.Text(fields["title"], "spec.group.title")), Icon: r.Text(fields["icon"], "spec.group.icon")}
	if g.Title == "" {
		r.Errorf(node, "spec.group.title", "add the group's title, which its tab shows, such as Deep Dives")
		return nil
	}

	return g
}
