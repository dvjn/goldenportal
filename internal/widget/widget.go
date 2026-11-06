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
	// Query is the entities a bar, donut, table or scorecard shows.
	Query *Query `json:"query,omitempty"`
	// Value is the number a stat shows.
	Value     *Value   `json:"value,omitempty"`
	GroupBy   string   `json:"groupBy,omitempty"`
	Columns   []Column `json:"columns,omitempty"`
	Sort      string   `json:"sort,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Scorecard string   `json:"scorecard,omitempty"`
	Text      string   `json:"text,omitempty"`
	// Properties picks what a properties widget shows, in order.
	Properties []string `json:"properties,omitempty"`
	// Actions are what an actions widget offers to run, in order: on the
	// page's entity, or on one people pick.
	Actions []string `json:"actions,omitempty"`
	// Workflows are what an actions widget offers after its actions.
	Workflows []string `json:"workflows,omitempty"`
	// Icon and Color mark a stat widget's card. Color is one of
	// configdoc.Colors and tints the icon.
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
	// Link is what clicking the widget opens. Without one it isn't a link.
	Link *Link `json:"link,omitempty"`
	// RowLink is what a table's rows open instead of their entity's page.
	RowLink *Link `json:"rowLink,omitempty"`
	// Measures are a matrix's columns, each a share worked out for every
	// group, so groups can be read against each other and measures against
	// each other in one place.
	Measures []Measure `json:"measures,omitempty"`
	// Overall adds a matrix row of each measure over every group, for a
	// page without stats that show them.
	Overall bool `json:"overall,omitempty"`
	// Path and Line say where the widget is written, for issues.
	Path string `json:"-"`
	Line int    `json:"-"`
}

// Query picks entities: those of a blueprint that match a filter. Pages
// name the queries their widgets share, and widgets can also write one in
// place.
type Query struct {
	// Name is the named query it is, or empty for one written in place.
	Name      string         `json:"name,omitempty"`
	Blueprint string         `json:"blueprint"`
	Filter    *filter.Filter `json:"filter,omitempty"`
	// Path and Line say where a named query is written, for issues.
	Path string `json:"-"`
	Line int    `json:"-"`
}

// Value is a number worked out from a query's entities: how many there
// are, unless it names one way to sum them up. The database works out all
// but jq, which runs over the matching entities, loaded and capped.
type Value struct {
	Query Query `json:"query"`
	// Sum, Avg, Min and Max name a number field's path.
	Sum string `json:"sum,omitempty"`
	Avg string `json:"avg,omitempty"`
	Min string `json:"min,omitempty"`
	Max string `json:"max,omitempty"`
	// Share is the percent of the entities matching a condition.
	Share *filter.Filter `json:"share,omitempty"`
	JQ    string         `json:"jq,omitempty"`
	// Unit follows the number. Share is always %, and sum, avg, min and
	// max take their field's unit unless this says otherwise.
	Unit string `json:"unit,omitempty"`
}

// Value ops, beside sum, avg, min and max.
const (
	OpCount = "count"
	OpShare = "share"
	OpJQ    = "jq"
)

// Op is how the value sums up its entities, with the path it works on.
func (v Value) Op() (op, path string) {
	switch {
	case v.Sum != "":
		return "sum", v.Sum
	case v.Avg != "":
		return "avg", v.Avg
	case v.Min != "":
		return "min", v.Min
	case v.Max != "":
		return "max", v.Max
	case v.Share != nil:
		return OpShare, ""
	case v.JQ != "":
		return OpJQ, ""
	}

	return OpCount, ""
}

// Link is what a widget, or each of its groups or rows, opens: the catalog
// showing a query's entities, a URL, or a path in the portal. URLs and
// paths are templates, with jq in {{ }}.
type Link struct {
	Query *Query `json:"query,omitempty"`
	// Filter narrows the query's entities for the link only.
	Filter *filter.Filter `json:"filter,omitempty"`
	// Group links each group of a bar or donut to its own entities.
	Group bool   `json:"group,omitempty"`
	URL   string `json:"url,omitempty"`
	Path  string `json:"path,omitempty"`
}

// Column is a table column: a field's path, which the database reads, or
// jq worked out from each row's entity. A path's title defaults to its
// field's.
type Column struct {
	Path  string `json:"path,omitempty"`
	Title string `json:"title,omitempty"`
	JQ    string `json:"jq,omitempty"`
	Unit  string `json:"unit,omitempty"`
}

// Measure is a share a matrix works out for each of its groups: of the
// query's entities, narrowed by Filter to those it applies to, the percent
// matching Share.
type Measure struct {
	Title  string         `json:"title"`
	Filter *filter.Filter `json:"filter,omitempty"`
	Share  *filter.Filter `json:"share"`
}

// Widget types.
const (
	TypeStat       = "stat"
	TypeBar        = "bar"
	TypeDonut      = "donut"
	TypeSplit      = "split"
	TypeMatrix     = "matrix"
	TypeTable      = "table"
	TypeScorecard  = "scorecard"
	TypeMarkdown   = "markdown"
	TypeProperties = "properties"
	TypeActions    = "actions"
)

// The keys each mapping of a widget takes, beside its type's own, exported
// so the documents' JSON Schemas can be checked against them.
var (
	TabFields    = []string{"title", "icon", "widgets"}
	QueryFields  = []string{"blueprint", "filter"}
	WidgetFields = []string{"type", "title", "description", "width"}
	ValueFields  = slices.Concat([]string{"query", "unit"}, valueOps)
	// RowLinkFields leave out what needs a query, since rows have none.
	RowLinkFields = []string{"url", "path"}
	LinkFields    = slices.Concat(RowLinkFields, []string{"query", "filter", "group"})
	ColumnFields  = []string{"title", "path", "jq", "unit"}
	MeasureFields = []string{"title", "filter", "share"}
)

// TypeFields lists the keys a widget of a type takes, or nil for a type
// there isn't.
func TypeFields(widgetType string) []string {
	kind, ok := types[widgetType]
	if !ok {
		return nil
	}

	return slices.Concat(WidgetFields, kind.fields)
}

// types lists each type's own fields and default width.
var types = map[string]struct {
	fields []string
	width  int
}{
	TypeStat:       {[]string{"value", "link", "icon", "color"}, 3},
	TypeBar:        {[]string{"query", "groupBy", "limit", "link"}, 6},
	TypeDonut:      {[]string{"query", "groupBy", "limit", "link"}, 4},
	TypeSplit:      {[]string{"query", "groupBy", "limit", "link"}, 6},
	TypeMatrix:     {[]string{"query", "groupBy", "limit", "measures", "overall"}, 12},
	TypeTable:      {[]string{"query", "columns", "sort", "limit", "link", "rowLink"}, 12},
	TypeScorecard:  {[]string{"scorecard", "query", "link"}, 6},
	TypeMarkdown:   {[]string{"text"}, 6},
	TypeProperties: {[]string{"properties"}, 6},
	TypeActions:    {[]string{"actions", "workflows"}, 4},
}

// moved are fields widgets used to have, and where what they said goes now.
var moved = map[string]string{
	"blueprint":     "query",
	"filter":        "query",
	"aggregate":     "value, as count, sum, avg, min, max, share or jq",
	"property":      "value, as sum, avg, min or max: <path>",
	"unit":          "value.unit",
	"ignoreFilters": "query, where inputs apply only as $input.<id> says",
}

// Context values a filter can compare with, filled in when the widget loads.
const (
	// ValueEntity is the identifier of the entity whose page it is.
	ValueEntity = "$entity"
	// ValueMe is whoever is looking: alone, their email, and $me.<name>
	// one of their attributes, listed in me.go.
	ValueMe = "$me"
	// ValueInput prefixes a dashboard input's identifier: $input.team is
	// what Home picked for the input team.
	ValueInput = "$input."
	// ValueToday is the date, which dates compare with.
	ValueToday = "$today"
)

const (
	MaxTableRows = 50
	MaxGroups    = 20
	GridColumns  = 12
)

var namePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

// ValidName reports whether id can name a widget, a tab, a query or an input.
func ValidName(id string) bool { return namePattern.MatchString(id) }

// Scope is the page widgets are on, which decides what they can use.
type Scope struct {
	// Blueprint is the page's blueprint, which queries pick from unless
	// they name another. Home has none.
	Blueprint string
	// Entity is set on entity pages, where $entity is the page's entity.
	Entity bool
	// Inputs are the dashboard's inputs, which queries compare with as
	// $input.<id>. Only Home has them.
	Inputs []string
	// Queries are the page's named queries.
	Queries []Query
}

// Find returns the widget with an identifier.
func Find(widgets []Widget, id string) (Widget, bool) {
	i := slices.IndexFunc(widgets, func(w Widget) bool { return w.ID == id })
	if i < 0 {
		return Widget{}, false
	}

	return widgets[i], true
}

// ParseQueries reads a mapping of named queries keyed by name, in order.
func ParseQueries(r *configdoc.Reader, node *yaml.Node, path string, scope Scope) []Query {
	queries := []Query{}
	r.Each(node, path, func(name string, key, value *yaml.Node) {
		queryPath := path + "." + name
		if !namePattern.MatchString(name) {
			r.Errorf(key, queryPath, "%q must start with a letter and use only letters, digits, - and _", name)
		}
		q := readQuery(r, key, value, queryPath, scope, false)
		q.Name, q.Path, q.Line = name, queryPath, key.Line
		queries = append(queries, q)
	})

	return queries
}

// readQuery reads a query written as blueprint and filter. On a
// blueprint's pages the blueprint defaults to the page's. A scorecard
// widget's query may leave it out, since the scorecard decides it.
func readQuery(r *configdoc.Reader, key, node *yaml.Node, path string, scope Scope, optional bool) Query {
	q := Query{}
	if !optional {
		q.Blueprint = scope.Blueprint
	}
	fields, ok := r.Fields(node, path, QueryFields...)
	if !ok {
		return q
	}
	if n := fields["blueprint"]; n != nil {
		q.Blueprint = r.Text(n, path+".blueprint")
		if !configdoc.ValidIdentifier(q.Blueprint) {
			r.Errorf(n, path+".blueprint", "blueprint must name a blueprint")
		}
	}
	if q.Blueprint == "" && !optional {
		r.Errorf(configdoc.OrNode(node, key), path+".blueprint", "add the blueprint whose entities it picks")
	}
	if n := fields["filter"]; n != nil {
		q.Filter = parseFilter(r, n, path+".filter", scope)
	}

	return q
}

// queryRef reads where a widget's entities come from: the name of one of
// the page's queries, or a query written in place. Left out, it's every
// entity of the page's blueprint, and on Home missing, so nil.
func queryRef(r *configdoc.Reader, key, node *yaml.Node, path string, scope Scope, optional bool) *Query {
	switch {
	case node == nil && scope.Blueprint == "":
		r.Errorf(key, path, "add the query whose entities it shows, by name or as blueprint and filter")
		return nil
	case node == nil:
		return &Query{Blueprint: scope.Blueprint}
	case node.Kind == yaml.ScalarNode:
		i := slices.IndexFunc(scope.Queries, func(q Query) bool { return q.Name == node.Value })
		if i < 0 {
			r.Errorf(node, path, "no query %q is declared in queries", node.Value)
			return nil
		}
		q := scope.Queries[i]
		q.Path, q.Line = "", 0
		return &q
	}
	q := readQuery(r, key, node, path, scope, optional)

	return &q
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
		r.Errorf(configdoc.OrNode(typeNode, key), path+".type", "type must be one of %s", strings.Join(Types(), ", "))
		return w
	}
	current := *node
	current.Content = nil
	for i := 0; i+1 < len(node.Content); i += 2 {
		if to, ok := moved[node.Content[i].Value]; ok {
			r.Errorf(node.Content[i], path+"."+node.Content[i].Value, "widgets no longer take %s; it moved to %s", node.Content[i].Value, to)
			continue
		}
		current.Content = append(current.Content, node.Content[i], node.Content[i+1])
	}
	fields, ok := r.Fields(&current, path, TypeFields(w.Type)...)
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
	if slices.Contains(kind.fields, "query") && w.Type != TypeScorecard {
		w.Query = queryRef(r, key, fields["query"], path+".query", scope, false)
	}
	if n := fields["link"]; n != nil {
		w.Link = parseLink(r, n, path+".link", w.Type, scope)
	}

	switch w.Type {
	case TypeStat:
		w.Value = parseValue(r, key, fields["value"], path+".value", scope)
		w.Icon = r.Text(fields["icon"], path+".icon")
		w.Color = r.Text(fields["color"], path+".color")
		switch n := fields["color"]; {
		case n == nil:
		case !slices.Contains(configdoc.Colors, w.Color):
			r.Errorf(n, path+".color", "color must be one of %s", strings.Join(configdoc.Colors, ", "))
		case w.Icon == "":
			r.Errorf(n, path+".color", "color tints the icon, so add an icon too")
		}
	case TypeBar, TypeDonut, TypeSplit:
		w.GroupBy = r.Text(fields["groupBy"], path+".groupBy")
		if w.GroupBy == "" {
			r.Errorf(key, path+".groupBy", "add the property to count entities by, in groupBy")
		}
		w.Limit = limit(r, fields["limit"], path+".limit", 8, MaxGroups)
	case TypeMatrix:
		w.GroupBy = r.Text(fields["groupBy"], path+".groupBy")
		if w.GroupBy == "" {
			r.Errorf(key, path+".groupBy", "add the property whose values make the rows, in groupBy")
		}
		w.Limit = limit(r, fields["limit"], path+".limit", 10, MaxGroups)
		w.Measures = parseMeasures(r, key, fields["measures"], path+".measures", scope)
		if n := fields["overall"]; n != nil {
			if err := n.Decode(&w.Overall); err != nil || n.Tag != "!!bool" {
				r.Errorf(n, path+".overall", "overall must be true or false")
			}
		}
	case TypeTable:
		w.Columns = parseColumns(r, fields["columns"], path+".columns", scope)
		w.Sort = r.Text(fields["sort"], path+".sort")
		w.Limit = limit(r, fields["limit"], path+".limit", 10, MaxTableRows)
		if n := fields["rowLink"]; n != nil {
			w.RowLink = parseLink(r, n, path+".rowLink", "", scope)
		}
	case TypeScorecard:
		w.Scorecard = r.Text(fields["scorecard"], path+".scorecard")
		if !configdoc.ValidIdentifier(w.Scorecard) {
			r.Errorf(configdoc.OrNode(fields["scorecard"], key), path+".scorecard", "scorecard must name the scorecard to show")
		}
		// The scorecard decides the blueprint, so the query only narrows it.
		if n := fields["query"]; n != nil {
			w.Query = queryRef(r, key, n, path+".query", scope, true)
		}
	case TypeMarkdown:
		w.Text = r.Text(fields["text"], path+".text")
		if strings.TrimSpace(w.Text) == "" {
			r.Errorf(configdoc.OrNode(fields["text"], key), path+".text", "add the text to show")
		}
		checkText(r, fields["text"], path+".text", Scope{Entity: scope.Entity})
	case TypeProperties:
		if !scope.Entity {
			r.Errorf(typeNode, path+".type", "properties widgets show one entity, so they go on entity pages")
		}
		w.Properties = texts(r, fields["properties"], path+".properties")
	case TypeActions:
		w.Actions = identifiers(r, fields["actions"], path+".actions", "an action's")
		w.Workflows = identifiers(r, fields["workflows"], path+".workflows", "a workflow's")
		if len(w.Actions) == 0 && len(w.Workflows) == 0 {
			r.Errorf(configdoc.OrNode(fields["actions"], key), path+".actions", "name the actions or workflows to offer, such as actions: [restart]")
		}
	}

	return w
}

// identifiers reads a list of what the widget offers, each named by
// identifier.
func identifiers(r *configdoc.Reader, n *yaml.Node, path, whose string) []string {
	names := texts(r, n, path)
	for _, name := range names {
		if !configdoc.ValidIdentifier(name) {
			r.Errorf(n, path, "%q isn't %s identifier", name, whose)
		}
	}

	return names
}

// valueOps are the ways a value can sum up its entities besides counting
// them, of which it takes at most one.
var valueOps = []string{"sum", "avg", "min", "max", OpShare, OpJQ}

// parseValue reads a stat's value: a query, and at most one way to sum up
// its entities, which are counted otherwise.
func parseValue(r *configdoc.Reader, key, node *yaml.Node, path string, scope Scope) *Value {
	if node == nil {
		r.Errorf(key, path, "add the value to show, as {query: <query>}, which counts its entities")
		return nil
	}
	if node.Kind == yaml.MappingNode {
		if n := configdoc.Field(node, "calc"); n != nil {
			r.Errorf(n, path+".calc", "calc is gone: leave it out to count, or write one of sum, avg, min or max: <path>, share: <condition> or jq: <expression>")
			return nil
		}
	}
	fields, ok := r.Fields(node, path, ValueFields...)
	if !ok {
		return nil
	}
	v := &Value{Unit: r.Text(fields["unit"], path+".unit")}
	if q := queryRef(r, node, fields["query"], path+".query", scope, false); q != nil {
		v.Query = *q
	}
	var set []string
	for _, op := range valueOps {
		if fields[op] != nil {
			set = append(set, op)
		}
	}
	if len(set) > 1 {
		r.Errorf(fields[set[1]], path+"."+set[1], "a value takes one of %s, and this has %s", strings.Join(valueOps, ", "), strings.Join(set, " and "))
		return v
	}
	for _, op := range []struct {
		name   string
		target *string
	}{{"sum", &v.Sum}, {"avg", &v.Avg}, {"min", &v.Min}, {"max", &v.Max}} {
		if n := fields[op.name]; n != nil {
			*op.target = r.Text(n, path+"."+op.name)
			if !validPath(*op.target) {
				r.Errorf(n, path+"."+op.name, "%s takes the path of a number field, such as cost or service.cost", op.name)
			}
		}
	}
	if n := fields[OpShare]; n != nil {
		v.Share = parseFilter(r, n, path+".share", scope)
		if v.Share != nil && v.Unit != "" {
			r.Errorf(fields["unit"], path+".unit", "share is a percent, so it takes no unit")
		}
	}
	if n := fields[OpJQ]; n != nil {
		v.JQ = strings.TrimSpace(r.Text(n, path+".jq"))
		if v.JQ == "" {
			r.Errorf(n, path+".jq", "add jq over the query's entities, as a list, that gives a number")
		} else if _, err := CompileJQ(v.JQ, scope); err != nil {
			r.Errorf(n, path+".jq", "jq doesn't compile: %v", err)
		}
	}

	return v
}

var pathPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*(\.[a-zA-Z][a-zA-Z0-9_-]*)*$`)

// validPath reports whether text is written as a field's path. Whether the
// blueprint has the field is checked against it later.
func validPath(text string) bool { return pathPattern.MatchString(text) }

// parseLink reads a link: a query, which opens the catalog with its
// entities, or a url or path template. Only bars, donuts and splits link each
// group, and rows, which have no query of their own, only take templates.
func parseLink(r *configdoc.Reader, node *yaml.Node, path, widgetType string, scope Scope) *Link {
	if node.Kind != yaml.MappingNode {
		r.Errorf(node, path, "%s is {query}, {url} or {path}", path)
		return nil
	}
	row := widgetType == ""
	allowed := LinkFields
	if row {
		allowed = RowLinkFields
	}
	fields, ok := r.Fields(node, path, allowed...)
	if !ok {
		return nil
	}
	l := &Link{URL: r.Text(fields["url"], path+".url"), Path: r.Text(fields["path"], path+".path")}
	set := 0
	for _, name := range []string{"query", "url", "path"} {
		if fields[name] != nil {
			set++
		}
	}
	if set != 1 {
		choices := "query, url or path"
		if row {
			choices = "url or path"
		}
		r.Errorf(node, path, "a link has one of %s", choices)
		return l
	}
	if n := fields["query"]; n != nil {
		l.Query = queryRef(r, node, n, path+".query", scope, false)
	}
	if n := fields["filter"]; n != nil {
		if l.Query == nil {
			r.Errorf(n, path+".filter", "filter narrows a query, so it goes with query")
		}
		l.Filter = parseFilter(r, n, path+".filter", scope)
	}
	if n := fields["group"]; n != nil {
		l.Group = r.Bool(n, path+".group")
		switch {
		case widgetType != TypeBar && widgetType != TypeDonut && widgetType != TypeSplit:
			r.Errorf(n, path+".group", "only bars, donuts and splits have groups to link")
		case l.Query == nil:
			r.Errorf(n, path+".group", "group adds each group's value to a query, so it goes with query")
		}
	}
	if n := fields["url"]; n != nil {
		if !strings.HasPrefix(l.URL, "https://") && !strings.HasPrefix(l.URL, "http://") {
			r.Errorf(n, path+".url", "url must start with https:// or http://")
		} else if err := CheckTemplate(l.URL, scope); err != nil {
			r.Errorf(n, path+".url", "%v", err)
		}
	}
	if n := fields["path"]; n != nil {
		if !portalPath(l.Path) {
			r.Errorf(n, path+".path", "path must be a path in the portal, starting with /")
		} else if err := CheckTemplate(l.Path, scope); err != nil {
			r.Errorf(n, path+".path", "%v", err)
		}
	}

	return l
}

// portalPath reports whether a path stays in the portal: it starts with
// one slash, as //host would leave it.
func portalPath(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") && !strings.HasPrefix(path, "/\\")
}

// parseColumns reads a table's columns: each a field's path, or a title
// with a path or jq worked out from the row's entity.
func parseColumns(r *configdoc.Reader, node *yaml.Node, path string, scope Scope) []Column {
	var columns []Column
	r.List(node, path, func(i int, item *yaml.Node) {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		if item.Kind == yaml.ScalarNode {
			c := Column{Path: item.Value}
			checkPath(r, item, itemPath, c.Path)
			columns = append(columns, c)
			return
		}
		if n := configdoc.Field(item, "value"); n != nil {
			r.Errorf(n, itemPath+".value", "value is now jq, beside a path column's path")
			return
		}
		fields, ok := r.Fields(item, itemPath, ColumnFields...)
		if !ok {
			return
		}
		c := Column{
			Title: r.Text(fields["title"], itemPath+".title"),
			Path:  r.Text(fields["path"], itemPath+".path"),
			JQ:    r.Text(fields["jq"], itemPath+".jq"),
			Unit:  r.Text(fields["unit"], itemPath+".unit"),
		}
		switch {
		case (fields["path"] == nil) == (fields["jq"] == nil):
			r.Errorf(item, itemPath, "a column has a path or jq, not both")
		case fields["path"] != nil:
			checkPath(r, fields["path"], itemPath+".path", c.Path)
		case c.Title == "":
			r.Errorf(item, itemPath+".title", "add the column's title")
		case strings.TrimSpace(c.JQ) == "":
			r.Errorf(item, itemPath+".jq", "add jq over the row's entity")
		default:
			if _, err := CompileJQ(c.JQ, scope); err != nil {
				r.Errorf(fields["jq"], itemPath+".jq", "jq doesn't compile: %v", err)
			}
		}
		columns = append(columns, c)
	})

	return columns
}

func parseMeasures(r *configdoc.Reader, key, node *yaml.Node, path string, scope Scope) []Measure {
	var measures []Measure
	r.List(node, path, func(i int, item *yaml.Node) {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		fields, ok := r.Fields(item, itemPath, MeasureFields...)
		if !ok {
			return
		}
		m := Measure{Title: r.Text(fields["title"], itemPath+".title")}
		if m.Title == "" {
			r.Errorf(item, itemPath+".title", "add the measure's title")
		}
		if n := fields["filter"]; n != nil {
			m.Filter = parseFilter(r, n, itemPath+".filter", scope)
		}
		if n := fields["share"]; n != nil {
			m.Share = parseFilter(r, n, itemPath+".share", scope)
		} else {
			r.Errorf(item, itemPath+".share", "add the condition the measure is the share of, in share")
		}
		measures = append(measures, m)
	})
	if node == nil || (node.Kind == yaml.SequenceNode && len(node.Content) == 0) {
		r.Errorf(configdoc.OrNode(node, key), path, "add the measures, each a title and a share")
	}

	return measures
}

// checkPath reports a path written the old way, with $ before a built-in
// or a scorecard, or not as a path at all.
func checkPath(r *configdoc.Reader, node *yaml.Node, path, text string) {
	if err := OldPath(text); err != nil {
		r.Errorf(node, path, "%v", err)
	} else if !validPath(text) {
		r.Errorf(node, path, "%q isn't a field's path, such as tier or service.owner.title", text)
	}
}

// OldPath explains a path written with $, which marks variables, not fields.
func OldPath(text string) error {
	if !strings.Contains(text, "$") {
		return nil
	}
	if name, ok := strings.CutPrefix(text, "$scorecard."); ok {
		return fmt.Errorf("%s is gone: declare a scorecard property on the blueprint, such as readiness: {type: scorecard, scorecard: %s}, and write its name", text, name)
	}

	return fmt.Errorf("%s starts with $, which marks variables; built-in fields are written plain, as %s", text, strings.ReplaceAll(text, "$", ""))
}

// Filters lists every filter a widget compares with, in its queries, its
// value's share and its link.
func (w Widget) Filters() []*filter.Filter {
	var filters []*filter.Filter
	for _, q := range []*Query{w.Query, w.valueQuery(), w.linkQuery()} {
		if q != nil && q.Filter != nil {
			filters = append(filters, q.Filter)
		}
	}
	if w.Value != nil && w.Value.Share != nil {
		filters = append(filters, w.Value.Share)
	}
	if w.Link != nil && w.Link.Filter != nil {
		filters = append(filters, w.Link.Filter)
	}
	for _, m := range w.Measures {
		for _, f := range []*filter.Filter{m.Filter, m.Share} {
			if f != nil {
				filters = append(filters, f)
			}
		}
	}

	return filters
}

func (w Widget) valueQuery() *Query {
	if w.Value == nil {
		return nil
	}
	return &w.Value.Query
}

func (w Widget) linkQuery() *Query {
	if w.Link == nil {
		return nil
	}
	return w.Link.Query
}

// MapFilters returns the widget with fn applied to each of its filters,
// leaving the original alone.
func (w Widget) MapFilters(fn func(*filter.Filter) *filter.Filter) Widget {
	mapQuery := func(q *Query) *Query {
		if q == nil {
			return nil
		}
		mapped := *q
		mapped.Filter = fn(q.Filter)
		return &mapped
	}
	w.Query = mapQuery(w.Query)
	if w.Value != nil {
		value := *w.Value
		value.Query = *mapQuery(&value.Query)
		value.Share = fn(value.Share)
		w.Value = &value
	}
	if w.Link != nil {
		link := *w.Link
		link.Query = mapQuery(link.Query)
		link.Filter = fn(link.Filter)
		w.Link = &link
	}
	if w.Measures != nil {
		measures := make([]Measure, len(w.Measures))
		for i, m := range w.Measures {
			measures[i] = Measure{Title: m.Title, Filter: fn(m.Filter), Share: fn(m.Share)}
		}
		w.Measures = measures
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
	if err := CheckTextIn(node.Value, scope); err != nil {
		r.Errorf(node, path, "%v", err)
		return
	}
	if scope.Blueprint != "" && tokenPattern.MatchString(node.Value) {
		r.Errorf(node, path, "{{ }} variables fill in on Home, so a blueprint's pages only have them in markdown text")
	}
}

// Types lists the widget types, sorted.
func Types() []string {
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
	for _, ref := range InputRefs(f) {
		id := strings.TrimPrefix(ref, ValueInput)
		switch {
		case scope.Blueprint != "":
			r.Errorf(node, path, "%s is a dashboard input, so only Home has it", ref)
		case !slices.Contains(scope.Inputs, id):
			r.Errorf(node, path, "no input %q is declared in spec.inputs", id)
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
	eachCondition(f, func(c *filter.Filter) {
		if unknown == nil {
			unknown = checkCondition(c)
		}
	})
	if unknown != nil {
		r.Errorf(node, path, "%v", unknown)
		return nil
	}

	return f
}

// checkCondition reports a condition written the old way: a path with $,
// in with one value, or a variable in values that isn't one.
func checkCondition(c *filter.Filter) error {
	if err := OldPath(c.Property); err != nil {
		return err
	}
	switch {
	case c.Op == filter.OpIn && c.Value != nil:
		return fmt.Errorf("in on %s takes values, a list or a variable such as values: %steam", c.Property, ValueInput)
	case c.ValuesFrom != "" && !strings.HasPrefix(c.ValuesFrom, "$"):
		return fmt.Errorf("values on %s is a list, or a variable such as %steam", c.Property, ValueInput)
	case c.ValuesFrom != "" && c.Op != filter.OpIn:
		return fmt.Errorf("only in takes values; %s compares with one value", c.Op)
	}

	return nil
}

// eachCondition calls fn with every condition in a filter.
func eachCondition(f *filter.Filter, fn func(*filter.Filter)) {
	if f == nil {
		return
	}
	if f.Property != "" || f.Op != "" {
		fn(f)
	}
	eachCondition(f.Not, fn)
	for _, part := range slices.Concat(f.And, f.Or) {
		eachCondition(&part, fn)
	}
}

// Uses reports whether a filter compares with a context value.
func Uses(f *filter.Filter, value string) bool {
	found := false
	eachText(f, func(text string) { found = found || text == value || strings.HasPrefix(text, value+".") })

	return found
}

// InputRefs lists the dashboard inputs a filter compares with, as written.
func InputRefs(f *filter.Filter) []string {
	var refs []string
	eachText(f, func(text string) {
		if strings.HasPrefix(text, ValueInput) && !slices.Contains(refs, text) {
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
	if f.ValuesFrom != "" {
		fn(f.ValuesFrom)
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
	return BindContext(f, values, nil)
}

func BindData(f *filter.Filter, values map[string][]any) *filter.Filter {
	return BindContext(f, nil, values)
}

func BindContext(f *filter.Filter, values Values, data map[string][]any) *filter.Filter {
	return bind(f, func(name string) ([]any, bool) {
		if picked, set := data[name]; set {
			return picked, true
		}
		picked, set := lookup(values, name)
		if picked == nil {
			return nil, set
		}
		out := make([]any, len(picked))
		for i, value := range picked {
			out[i] = value
		}
		return out, set
	})
}

func bind(f *filter.Filter, resolve func(string) ([]any, bool)) *filter.Filter {
	if f == nil {
		return nil
	}
	bound := *f
	switch {
	case len(f.And) > 0:
		if bound.And = bindAll(f.And, resolve); len(bound.And) == 0 {
			return nil
		}
	case len(f.Or) > 0:
		if bound.Or = bindAll(f.Or, resolve); len(bound.Or) == 0 {
			return nil
		}
	case f.Not != nil:
		if bound.Not = bind(f.Not, resolve); bound.Not == nil {
			return nil
		}
	default:
		return bindCondition(f, resolve)
	}

	return &bound
}

func bindAll(parts []filter.Filter, resolve func(string) ([]any, bool)) []filter.Filter {
	out := make([]filter.Filter, 0, len(parts))
	for i := range parts {
		if part := bind(&parts[i], resolve); part != nil {
			out = append(out, *part)
		}
	}

	return out
}

func bindCondition(f *filter.Filter, resolve func(string) ([]any, bool)) *filter.Filter {
	bound := *f
	// values written as a variable is the variable's values, which an
	// unset variable drops and Nothing empties, matching nothing.
	if f.ValuesFrom != "" {
		picked, set := resolve(f.ValuesFrom)
		if !set {
			return &bound
		}
		if picked == nil {
			return nil
		}
		bound.ValuesFrom, bound.Values = "", append([]any{}, picked...)
		return &bound
	}
	if text, ok := f.Value.(string); ok {
		if picked, set := resolve(text); set {
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
			picked, set := resolve(text)
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
func anyOf(condition filter.Filter, picked []any) *filter.Filter {
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
