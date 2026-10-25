// Package widget is the building block of every customizable page: Home's
// dashboards, and the tabs a blueprint gives its catalog and entity pages.
// It reads widgets; the dashboard package checks them and loads their data.
package widget

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/filter"
)

// Widget is one tile on a page. Which fields apply depends on its type.
type Widget struct {
	ID          string `json:"identifier"`
	Type        string `json:"type"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	// Width is how many of the 12 grid columns it spans.
	Width int `json:"width"`
	// Blueprint is the entities it shows; on a blueprint's pages it
	// defaults to that blueprint.
	Blueprint string         `json:"blueprint,omitempty"`
	Filter    *filter.Filter `json:"filter,omitempty"`
	Aggregate string         `json:"aggregate,omitempty"`
	Property  string         `json:"property,omitempty"`
	Unit      string         `json:"unit,omitempty"`
	GroupBy   string         `json:"groupBy,omitempty"`
	Columns   []string       `json:"columns,omitempty"`
	Sort      string         `json:"sort,omitempty"`
	Limit     int            `json:"limit,omitempty"`
	Scorecard string         `json:"scorecard,omitempty"`
	Text      string         `json:"text,omitempty"`
	// Properties picks what a properties widget shows, in order.
	Properties []string `json:"properties,omitempty"`
	// Path and Line say where the widget is written, for issues.
	Path string `json:"-"`
	Line int    `json:"-"`
}

// Widget types.
const (
	TypeStat       = "stat"
	TypeBar        = "bar"
	TypeDonut      = "donut"
	TypeTable      = "table"
	TypeScorecard  = "scorecard"
	TypeMarkdown   = "markdown"
	TypeProperties = "properties"
)

// types lists each type's own fields and default width.
var types = map[string]struct {
	fields []string
	width  int
}{
	TypeStat:       {[]string{"blueprint", "filter", "aggregate", "property", "unit"}, 3},
	TypeBar:        {[]string{"blueprint", "filter", "groupBy", "limit"}, 6},
	TypeDonut:      {[]string{"blueprint", "filter", "groupBy", "limit"}, 4},
	TypeTable:      {[]string{"blueprint", "filter", "columns", "sort", "limit"}, 12},
	TypeScorecard:  {[]string{"scorecard", "filter"}, 6},
	TypeMarkdown:   {[]string{"text"}, 6},
	TypeProperties: {[]string{"properties"}, 6},
}

// Context values a filter can compare with, filled in when the widget loads.
const (
	// ValueEntity is the identifier of the entity whose page it is.
	ValueEntity = "$entity"
	// ValueMe is whoever is looking: alone, their email, and $me.<name>
	// one of their attributes, listed in me.go.
	ValueMe = "$me"
	// ValueFilter prefixes a dashboard filter's identifier: $filter.team is
	// what Home picked for the filter team.
	ValueFilter = "$filter."
)

const (
	MaxTableRows = 50
	MaxGroups    = 20
	GridColumns  = 12
)

var namePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

// ValidName reports whether id can name a widget, a tab or a filter.
func ValidName(id string) bool { return namePattern.MatchString(id) }

// Scope is the page widgets are on, which decides what they can use.
type Scope struct {
	// Blueprint is the page's blueprint, which widgets show unless they
	// name another. Home has none.
	Blueprint string
	// Entity is set on entity pages, where $entity is the page's entity.
	Entity bool
	// Filters are the dashboard's filters, which widgets compare with as
	// $filter.<id>. Only Home has them.
	Filters []string
}

// Find returns the widget with an identifier.
func Find(widgets []Widget, id string) (Widget, bool) {
	i := slices.IndexFunc(widgets, func(w Widget) bool { return w.ID == id })
	if i < 0 {
		return Widget{}, false
	}

	return widgets[i], true
}

// Parse reads a mapping of widgets keyed by identifier, in order.
func Parse(r *configdoc.Reader, node *yaml.Node, path string, scope Scope) []Widget {
	widgets := []Widget{}
	r.Each(node, path, func(id string, key, value *yaml.Node) {
		widgetPath := path + "." + id
		if !namePattern.MatchString(id) {
			r.Errorf(key, widgetPath, "%q must start with a letter and use only letters, digits, - and _", id)
		}
		widgets = append(widgets, parseOne(r, widgetPath, id, key, value, scope))
	})

	return widgets
}

func parseOne(r *configdoc.Reader, path, id string, key, node *yaml.Node, scope Scope) Widget {
	w := Widget{ID: id, Path: path, Line: key.Line}
	if node.Kind != yaml.MappingNode {
		r.Errorf(node, path, "%s must be a mapping of fields", path)
		return w
	}
	typeNode := configdoc.Field(node, "type")
	w.Type = r.Text(typeNode, path+".type")
	kind, known := types[w.Type]
	if !known {
		r.Errorf(configdoc.OrNode(typeNode, key), path+".type", "type must be one of %s", strings.Join(typeNames(), ", "))
		return w
	}
	fields, ok := r.Fields(node, path, append([]string{"type", "title", "description", "width"}, kind.fields...)...)
	if !ok {
		return w
	}

	w.Title = r.Text(fields["title"], path+".title")
	if w.Title == "" && w.Type != TypeMarkdown {
		r.Errorf(key, path+".title", "add a title")
	}
	w.Description = r.Text(fields["description"], path+".description")
	for _, name := range []string{"title", "description"} {
		if n := fields[name]; n != nil {
			checkText(r, n, path+"."+name, scope)
		}
	}
	w.Width = kind.width
	if n := fields["width"]; n != nil {
		if err := n.Decode(&w.Width); err != nil || w.Width < 1 || w.Width > GridColumns {
			r.Errorf(n, path+".width", "width must be 1 to %d columns", GridColumns)
		}
	}

	w.Blueprint = r.Text(fields["blueprint"], path+".blueprint")
	if fields["blueprint"] != nil && !configdoc.ValidIdentifier(w.Blueprint) {
		r.Errorf(fields["blueprint"], path+".blueprint", "blueprint must name a blueprint")
	}
	if w.Blueprint == "" && w.Type != TypeScorecard && w.Type != TypeMarkdown {
		w.Blueprint = scope.Blueprint
		if w.Blueprint == "" && w.Type != TypeProperties {
			r.Errorf(key, path+".blueprint", "widgets on Home name the blueprint they show")
		}
	}
	if n := fields["filter"]; n != nil {
		w.Filter = parseFilter(r, n, path+".filter", scope)
	}

	switch w.Type {
	case TypeStat:
		w.Aggregate = "count"
		if n := fields["aggregate"]; n != nil {
			w.Aggregate = r.Text(n, path+".aggregate")
		}
		w.Property = r.Text(fields["property"], path+".property")
		w.Unit = r.Text(fields["unit"], path+".unit")
	case TypeBar, TypeDonut:
		w.GroupBy = r.Text(fields["groupBy"], path+".groupBy")
		if w.GroupBy == "" {
			r.Errorf(key, path+".groupBy", "add the property to count entities by, in groupBy")
		}
		w.Limit = limit(r, fields["limit"], path+".limit", 8, MaxGroups)
	case TypeTable:
		w.Columns = texts(r, fields["columns"], path+".columns")
		w.Sort = r.Text(fields["sort"], path+".sort")
		w.Limit = limit(r, fields["limit"], path+".limit", 10, MaxTableRows)
	case TypeScorecard:
		w.Scorecard = r.Text(fields["scorecard"], path+".scorecard")
		if !configdoc.ValidIdentifier(w.Scorecard) {
			r.Errorf(configdoc.OrNode(fields["scorecard"], key), path+".scorecard", "scorecard must name the scorecard to show")
		}
	case TypeMarkdown:
		w.Text = r.Text(fields["text"], path+".text")
		if strings.TrimSpace(w.Text) == "" {
			r.Errorf(configdoc.OrNode(fields["text"], key), path+".text", "add the text to show")
		}
		checkText(r, fields["text"], path+".text", Scope{})
	case TypeProperties:
		if !scope.Entity {
			r.Errorf(typeNode, path+".type", "properties widgets show one entity, so they go on entity pages")
		}
		w.Properties = texts(r, fields["properties"], path+".properties")
	}

	return w
}

// checkText checks the {{ }} variables in a text field. Titles and
// descriptions render on Home only, where the dashboard comes to the viewer
// already filled in; a blueprint's pages come as published, so there they
// can't have any. Markdown text loads through its own endpoint everywhere.
func checkText(r *configdoc.Reader, node *yaml.Node, path string, scope Scope) {
	if node == nil {
		return
	}
	if err := CheckText(node.Value); err != nil {
		r.Errorf(node, path, "%v", err)
		return
	}
	if scope.Blueprint != "" && tokenPattern.MatchString(node.Value) {
		r.Errorf(node, path, "{{ }} variables fill in on Home, so a blueprint's pages only have them in markdown text")
	}
}

func typeNames() []string {
	names := make([]string, 0, len(types))
	for t := range types {
		names = append(names, t)
	}
	slices.Sort(names)

	return names
}

func limit(r *configdoc.Reader, node *yaml.Node, path string, fallback, most int) int {
	if node == nil {
		return fallback
	}
	var value int
	if err := node.Decode(&value); err != nil || value < 1 || value > most {
		r.Errorf(node, path, "limit must be 1 to %d", most)
		return fallback
	}

	return value
}

func texts(r *configdoc.Reader, node *yaml.Node, path string) []string {
	var values []string
	r.List(node, path, func(i int, item *yaml.Node) {
		values = append(values, r.Text(item, fmt.Sprintf("%s[%d]", path, i)))
	})

	return values
}

// parseFilter reads a filter in the catalog's filter language.
func parseFilter(r *configdoc.Reader, node *yaml.Node, path string, scope Scope) *filter.Filter {
	f := ReadFilter(r, node, path)
	if f == nil {
		return nil
	}
	if !scope.Entity && Uses(f, ValueEntity) {
		r.Errorf(node, path, "%s is the entity whose page it is, so only entity pages have it", ValueEntity)
	}
	for _, ref := range FilterRefs(f) {
		id := strings.TrimPrefix(ref, ValueFilter)
		switch {
		case scope.Blueprint != "":
			r.Errorf(node, path, "%s is a dashboard filter, so only Home has it", ref)
		case !slices.Contains(scope.Filters, id):
			r.Errorf(node, path, "no filter %q is declared in spec.filters", id)
		}
	}

	return f
}

// ReadFilter reads a filter in the catalog's filter language, as YAML.
func ReadFilter(r *configdoc.Reader, node *yaml.Node, path string) *filter.Filter {
	var raw any
	if err := node.Decode(&raw); err != nil {
		r.Errorf(node, path, "filter is not valid: %v", err)
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		r.Errorf(node, path, "filter is not valid: %v", err)
		return nil
	}
	f, err := filter.Parse(string(encoded))
	if err != nil {
		r.Errorf(node, path, "%v", err)
		return nil
	}
	var unknown error
	eachText(f, func(text string) {
		if err := checkVariable(text); err != nil && unknown == nil {
			unknown = err
		}
	})
	if unknown != nil {
		r.Errorf(node, path, "%v", unknown)
		return nil
	}

	return f
}

// Uses reports whether a filter compares with a context value.
func Uses(f *filter.Filter, value string) bool {
	found := false
	eachText(f, func(text string) { found = found || text == value })

	return found
}

// FilterRefs lists the dashboard filters a filter compares with, as written.
func FilterRefs(f *filter.Filter) []string {
	var refs []string
	eachText(f, func(text string) {
		if strings.HasPrefix(text, ValueFilter) && !slices.Contains(refs, text) {
			refs = append(refs, text)
		}
	})

	return refs
}

// eachText calls fn with every text a filter compares with.
func eachText(f *filter.Filter, fn func(text string)) {
	if f == nil {
		return
	}
	if text, ok := f.Value.(string); ok {
		fn(text)
	}
	for _, item := range f.Values {
		if text, ok := item.(string); ok {
			fn(text)
		}
	}
	eachText(f.Not, fn)
	for _, part := range slices.Concat(f.And, f.Or) {
		eachText(&part, fn)
	}
}

// Values are what the context values stand for when a widget loads. A nil
// value is unset; Nothing is set, to no values at all.
type Values map[string][]string

// Nothing is what a context value stands for when nothing was found for it,
// as when a lookup default matches no entity. The conditions comparing with
// it stay and match nothing, where an unset value drops them.
var Nothing = []string{}

// Bind fills in a filter's context values. An unset value drops the
// conditions that compare with it, and and, or and not drop once nothing is
// left in them, so the result is nil when the whole filter falls away.
func Bind(f *filter.Filter, values Values) *filter.Filter {
	if f == nil {
		return nil
	}
	bound := *f
	switch {
	case len(f.And) > 0:
		if bound.And = bindAll(f.And, values); len(bound.And) == 0 {
			return nil
		}
	case len(f.Or) > 0:
		if bound.Or = bindAll(f.Or, values); len(bound.Or) == 0 {
			return nil
		}
	case f.Not != nil:
		if bound.Not = Bind(f.Not, values); bound.Not == nil {
			return nil
		}
	default:
		return bindCondition(f, values)
	}

	return &bound
}

func bindAll(parts []filter.Filter, values Values) []filter.Filter {
	out := make([]filter.Filter, 0, len(parts))
	for i := range parts {
		if part := Bind(&parts[i], values); part != nil {
			out = append(out, *part)
		}
	}

	return out
}

func bindCondition(f *filter.Filter, values Values) *filter.Filter {
	bound := *f
	if text, ok := f.Value.(string); ok {
		if picked, set := values[text]; set {
			switch {
			case picked == nil:
				return nil
			case len(picked) == 1:
				bound.Value = picked[0]
			default:
				return anyOf(bound, picked)
			}
		}
	}
	if f.Values != nil {
		// A list only Nothing fed stays, as in with no values, which
		// matches nothing; one an unset value emptied drops.
		unset := false
		bound.Values = make([]any, 0, len(f.Values))
		for _, item := range f.Values {
			text, ok := item.(string)
			picked, set := values[text]
			if !ok || !set {
				bound.Values = append(bound.Values, item)
				continue
			}
			unset = unset || picked == nil
			for _, value := range picked {
				bound.Values = append(bound.Values, value)
			}
		}
		if len(bound.Values) == 0 && len(f.Values) > 0 && unset {
			return nil
		}
	}

	return &bound
}

// anyOf compares with several values at once. Picking several means "any of
// these", so eq becomes in, and ne, its opposite, "none of these". Nothing
// is in with no values, which matches nothing, or for ne everything.
func anyOf(condition filter.Filter, picked []string) *filter.Filter {
	values := make([]any, len(picked))
	for i, value := range picked {
		values[i] = value
	}
	switch {
	case condition.Op == filter.OpNe:
		return &filter.Filter{Not: &filter.Filter{Property: condition.Property, Op: filter.OpIn, Values: values}}
	case condition.Op == filter.OpEq || len(picked) == 0:
		return &filter.Filter{Property: condition.Property, Op: filter.OpIn, Values: values}
	}
	or := make([]filter.Filter, len(picked))
	for i, value := range picked {
		or[i] = condition
		or[i].Value = value
	}

	return &filter.Filter{Or: or}
}
