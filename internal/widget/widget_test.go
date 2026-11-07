package widget

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/filter"
)

func parse(t *testing.T, source string, scope Scope) ([]Widget, []configdoc.Issue) {
	t.Helper()

	return parseWith(t, "", source, scope)
}

// parseWith reads widgets beside the queries they name, as a page does.
func parseWith(t *testing.T, queries, source string, scope Scope) ([]Widget, []configdoc.Issue) {
	t.Helper()
	r := &configdoc.Reader{}
	if queries != "" {
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(queries), &node); err != nil {
			t.Fatal(err)
		}
		scope.Queries = ParseQueries(r, node.Content[0], "queries", scope)
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(source), &node); err != nil {
		t.Fatal(err)
	}
	widgets := Parse(r, node.Content[0], "widgets", scope)

	return widgets, r.Issues
}

func TestParse(t *testing.T) {
	widgets, issues := parseWith(t, `
teams: {}
ownedServices: {blueprint: service, filter: {property: owner, op: eq, value: $entity}}
deployments: {blueprint: deployment}
`, `
count:
  type: stat
  title: Services
  value: {}
byTier:
  type: donut
  title: By tier
  groupBy: tier
  link: {query: teams, group: true}
services:
  type: table
  title: Services
  query: ownedServices
  columns: [{path: tier}, {title: Owner, path: owner.title}, {title: Age, jq: '.createdAt | fromdate', unit: d}]
  sort: -lastDeployedAt
  rowLink: {path: '/catalog/service/{{ .identifier }}'}
byOwner:
  type: matrix
  title: By owner
  groupBy: owner
  measures:
    - {title: Documented, share: {property: docs, op: exists}}
    - {title: Paged, filter: {property: tier, op: eq, value: critical}, share: {property: oncall, op: exists}}
about:
  type: properties
  title: About
deploys:
  type: stat
  title: Deploys
  icon: rocket
  color: red
  value: {query: deployments, sum: cost, unit: $}
  link: {url: 'https://deploys.example.com/?team={{ $entity.identifier }}'}
`, Scope{Blueprint: "team", Entity: true})
	if configdoc.HasErrors(issues) {
		t.Fatalf("issues = %+v", issues)
	}

	ids := []string{}
	for _, w := range widgets {
		ids = append(ids, w.ID)
	}
	if got := strings.Join(ids, ","); got != "count,byTier,services,byOwner,about,deploys" {
		t.Fatalf("widget order = %s", got)
	}
	count, byTier, services, byOwner, deploys := widgets[0], widgets[1], widgets[2], widgets[3], widgets[5]
	if byOwner.Limit != 10 || byOwner.Width != 12 || byOwner.Query.Blueprint != "team" || len(byOwner.Measures) != 2 ||
		byOwner.Measures[0].Filter != nil || byOwner.Measures[1].Filter.Value != "critical" || byOwner.Measures[1].Share.Property != "oncall" {
		t.Errorf("matrix = %+v", byOwner)
	}
	if op, _ := count.Value.Op(); op != OpCount || count.Value.Query.Blueprint != "team" || count.Width != 3 || count.Path != "widgets.count" || count.Link != nil {
		t.Errorf("stat defaults = %+v", count)
	}
	if byTier.Limit != 8 || byTier.Width != 4 || byTier.Query.Blueprint != "team" || !byTier.Link.Group || byTier.Link.Query.Blueprint != "team" {
		t.Errorf("donut defaults = %+v", byTier)
	}
	if services.Limit != 10 || services.Width != 12 || services.Query.Blueprint != "service" || services.Query.Filter.Value != ValueEntity {
		t.Errorf("table = %+v", services)
	}
	if want := []Column{{Path: "tier"}, {Title: "Owner", Path: "owner.title"}, {Title: "Age", JQ: ".createdAt | fromdate", Unit: "d"}}; !reflect.DeepEqual(services.Columns, want) || services.RowLink.Path == "" {
		t.Errorf("table columns = %+v, row link %+v", services.Columns, services.RowLink)
	}
	if op, path := deploys.Value.Op(); op != "sum" || path != "cost" || deploys.Icon != "rocket" || deploys.Color != "red" || deploys.Value.Unit != "$" || deploys.Link.URL == "" {
		t.Errorf("stat = %+v", deploys)
	}
}

func TestParseQueries(t *testing.T) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(`
services: {blueprint: service, filter: {property: owner, op: in, values: $input.team}}
mine: {filter: {property: owner, op: eq, value: $me}}
`), &node); err != nil {
		t.Fatal(err)
	}
	r := &configdoc.Reader{}
	queries := ParseQueries(r, node.Content[0], "spec.queries", Scope{Inputs: []string{"team"}})
	if len(queries) != 2 || queries[0].Name != "services" || queries[0].Path != "spec.queries.services" || queries[0].Line != 2 {
		t.Fatalf("queries = %+v", queries)
	}
	if len(r.Issues) != 1 || r.Issues[0].Path != "spec.queries.mine.blueprint" {
		t.Fatalf("issues = %+v, want mine's missing blueprint", r.Issues)
	}

	widgets, issues := parse(t, `
w: {type: bar, title: W, query: services, groupBy: tier}
`, Scope{Queries: queries})
	if len(issues) > 0 || widgets[0].Query.Name != "services" || widgets[0].Query.Path != "" || widgets[0].Query.Filter == nil {
		t.Fatalf("widget query = %+v, issues %+v", widgets[0].Query, issues)
	}
}

func TestValueOp(t *testing.T) {
	share := &filter.Filter{Property: "tier", Op: filter.OpEq, Value: "critical"}
	for _, test := range []struct {
		value    Value
		op, path string
	}{
		{Value{}, OpCount, ""},
		{Value{Sum: "cost"}, "sum", "cost"},
		{Value{Avg: "service.cost"}, "avg", "service.cost"},
		{Value{Max: "readiness.score"}, "max", "readiness.score"},
		{Value{Share: share}, OpShare, ""},
		{Value{JQ: "length"}, OpJQ, ""},
	} {
		if op, path := test.value.Op(); op != test.op || path != test.path {
			t.Errorf("Op(%+v) = %s %s, want %s %s", test.value, op, path, test.op, test.path)
		}
	}
}

func TestParseReportsProblems(t *testing.T) {
	home, catalog := Scope{}, Scope{Blueprint: "service"}
	tests := []struct {
		name, source  string
		scope         Scope
		path, message string
	}{
		{"bad type", "w: {type: pie}", home, "widgets.w.type", "type must be one of"},
		{"wrong field", "w: {type: markdown, text: x, groupBy: a}", home, "widgets.w.groupBy", "unknown field"},
		{"unknown widget field", "w: {type: stat, title: W, blueprint: s, aggregate: count}", home, "widgets.w.blueprint", "unknown field"},
		{"home needs a query", "w: {type: bar, title: W, groupBy: a}", home, "widgets.w.query", "add the name of the query"},
		{"stat needs a value", "w: {type: stat, title: W}", catalog, "widgets.w.value", "add the value"},
		{"home value needs a query", "w: {type: stat, title: W, value: {}}", home, "widgets.w.value.query", "add the name of the query"},
		{"query in place", "w: {type: table, title: W, query: {blueprint: s}}", home, "widgets.w.query", "name of a query declared in queries"},
		{"value query in place", "w: {type: stat, title: W, value: {query: {blueprint: s}}}", home, "widgets.w.value.query", "name of a query declared in queries"},
		{"link query in place", "w: {type: bar, title: W, groupBy: a, link: {query: {blueprint: s}}}", catalog, "widgets.w.link.query", "name of a query declared in queries"},
		{"unknown query", "w: {type: table, title: W, query: services}", home, "widgets.w.query", `no query "services"`},
		{"unknown value field", "w: {type: stat, title: W, value: {calc: count}}", catalog, "widgets.w.value.calc", "unknown field"},
		{"bad jq value", "w: {type: stat, title: W, value: {jq: 'map(.x'}}", catalog, "widgets.w.value.jq", "doesn't compile"},
		{"jq value with entity off its page", "w: {type: stat, title: W, value: {jq: '$entity.x'}}", catalog, "widgets.w.value.jq", "$entity"},
		{"two ops", "w: {type: stat, title: W, value: {sum: cost, avg: cost}}", catalog, "widgets.w.value.avg", "takes one of"},
		{"sum takes a path", "w: {type: stat, title: W, value: {sum: 'cost * 2'}}", catalog, "widgets.w.value.sum", "path of a number field"},
		{"share has no unit", "w: {type: stat, title: W, value: {share: {property: a, op: exists}, unit: h}}", catalog, "widgets.w.value.unit", "percent"},
		{"bare column", "w: {type: table, title: W, columns: [tier]}", catalog, "widgets.w.columns[0]", "must be a mapping"},
		{"width", "w: {type: markdown, text: x, width: 13}", home, "widgets.w.width", "1 to 12"},
		{"group by", "w: {type: bar, title: W}", catalog, "widgets.w.groupBy", "add the property"},
		{"split group by", "w: {type: split, title: W}", catalog, "widgets.w.groupBy", "add the property"},
		{"limit", "w: {type: table, title: W, limit: 500}", catalog, "widgets.w.limit", "1 to 50"},
		{"matrix group by", "w: {type: matrix, title: W, measures: [{title: M, share: {property: a, op: exists}}]}", catalog, "widgets.w.groupBy", "add the property"},
		{"matrix measures", "w: {type: matrix, title: W, groupBy: owner}", catalog, "widgets.w.measures", "add the measures"},
		{"measure share", "w: {type: matrix, title: W, groupBy: owner, measures: [{title: M}]}", catalog, "widgets.w.measures[0].share", "add the condition"},
		{"measure title", "w: {type: matrix, title: W, groupBy: owner, measures: [{share: {property: a, op: exists}}]}", catalog, "widgets.w.measures[0].title", "add the measure's title"},
		{"properties off entity", "w: {type: properties, title: W}", catalog, "widgets.w.type", "go on entity pages"},
		{"unknown text variable", "w: {type: markdown, text: 'Hi {{ $me.nmae }}'}", catalog, "widgets.w.text", "unknown variable $me.nmae; known: $me, $me.email, $me.name, $me.role, $me.groups"},
		{"empty token", "w: {type: markdown, text: 'Hi {{}}'}", catalog, "widgets.w.text", "{{}} must name a variable, like {{ $me.name }}"},
		{"title variable off home", "w: {type: table, title: '{{ $me.name }}'}", catalog, "widgets.w.title", "only have them in markdown text"},
		{"description variable off home", "w: {type: table, title: W, description: '{{ $me.name }}'}", catalog, "widgets.w.description", "only have them in markdown text"},
		{"unknown color", "w: {type: stat, title: W, value: {}, icon: rocket, color: mauve}", catalog, "widgets.w.color", "color must be one of"},
		{"matrix overall", "w: {type: matrix, title: W, groupBy: owner, overall: yes please, measures: [{title: M, share: {property: a, op: exists}}]}", catalog, "widgets.w.overall", "true or false"},
		{"color without icon", "w: {type: stat, title: W, value: {}, color: red}", catalog, "widgets.w.color", "add an icon"},
		{"icon off stat", "w: {type: table, title: W, icon: rocket}", catalog, "widgets.w.icon", "unknown field"},
		{"link is a mapping", "w: {type: table, title: W, link: path}", catalog, "widgets.w.link", "{query}, {url} or {path}"},
		{"link takes one", "w: {type: table, title: W, link: {url: 'https://x', path: /x}}", catalog, "widgets.w.link", "one of query, url or path"},
		{"link filter needs a query", "w: {type: table, title: W, link: {path: /x, filter: {property: a, op: exists}}}", catalog, "widgets.w.link.filter", "goes with query"},
		{"group needs a query", "w: {type: bar, title: W, groupBy: a, link: {path: /x, group: true}}", catalog, "widgets.w.link.group", "goes with query"},
		{"url scheme", "w: {type: table, title: W, link: {url: 'javascript:alert(1)'}}", catalog, "widgets.w.link.url", "https://"},
		{"path leaves", "w: {type: table, title: W, link: {path: '//evil.example.com'}}", catalog, "widgets.w.link.path", "path in the portal"},
		{"template jq", "w: {type: table, title: W, link: {path: '/x/{{ .a | }}'}}", catalog, "widgets.w.link.path", "doesn't compile"},
		{"row link query", "w: {type: table, title: W, rowLink: {query: {}}}", catalog, "widgets.w.rowLink.query", "unknown field"},
		{"column title", "w: {type: table, title: W, columns: [{jq: .x}]}", catalog, "widgets.w.columns[0].title", "add the column's title"},
		{"column jq", "w: {type: table, title: W, columns: [{title: X, jq: '.x |'}]}", catalog, "widgets.w.columns[0].jq", "doesn't compile"},
		{"unknown column field", "w: {type: table, title: W, columns: [{title: X, value: .x}]}", catalog, "widgets.w.columns[0].value", "unknown field"},
		{"column path and jq", "w: {type: table, title: W, columns: [{title: X, path: x, jq: .x}]}", catalog, "widgets.w.columns[0]", "not both"},
		{"scorecard query of nothing", "w: {type: scorecard, title: W, scorecard: s, query: nope}", catalog, "widgets.w.query", `no query "nope"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := parse(t, test.source, test.scope)
			for _, issue := range issues {
				if issue.Path == test.path && strings.Contains(issue.Message, test.message) {
					return
				}
			}
			t.Fatalf("issues = %+v, want %s: %s", issues, test.path, test.message)
		})
	}
}

func TestParseReportsProblemsInQueries(t *testing.T) {
	home, catalog := Scope{}, Scope{Blueprint: "service"}
	tests := []struct {
		name, queries, source string
		scope                 Scope
		path, message         string
	}{
		{"blueprint on home", "q: {filter: {property: a, op: exists}}", "w: {type: table, title: W, query: q}", home, "queries.q.blueprint", "add the blueprint"},
		{"in takes values", "q: {filter: {property: a, op: in, value: [x]}}", "w: {type: table, title: W, query: q}", catalog, "queries.q.filter", "in on a takes values"},
		{"values is a list or a variable", "q: {filter: {property: a, op: in, values: x}}", "w: {type: table, title: W, query: q}", catalog, "queries.q.filter", "values on a is a list"},
		{"entity off its page", "q: {filter: {property: owner, op: eq, value: $entity}}", "w: {type: table, title: W, query: q}", catalog, "queries.q.filter", "only entity pages"},
		{"input off home", "q: {filter: {property: owner, op: eq, value: $input.team}}", "w: {type: table, title: W, query: q}", catalog, "queries.q.filter", "only Home has it"},
		{"unknown me attribute", "q: {filter: {property: owner, op: in, values: [$me.nmae]}}", "w: {type: table, title: W, query: q}", catalog, "queries.q.filter", "unknown variable $me.nmae; known: $me, $me.email, $me.name, $me.role, $me.groups"},
		{"unknown variable", "q: {filter: {property: at, op: gte, value: $today-7d}}", "w: {type: table, title: W, query: q}", catalog, "queries.q.filter", "unknown variable $today-7d"},
		{"unknown variable in values", "q: {filter: {property: at, op: in, values: [$tier]}}", "w: {type: table, title: W, query: q}", catalog, "queries.q.filter", "unknown variable $tier"},
		{"variable as a property", "q: {filter: {property: $title, op: exists}}", "w: {type: table, title: W, query: q}", catalog, "queries.q.filter", "unknown variable $title"},
		{"scorecard as a property", "q: {filter: {property: $scorecard.readiness, op: exists}}", "w: {type: table, title: W, query: q}", catalog, "queries.q.filter", "unknown variable $scorecard.readiness"},
		{"undeclared input", "q: {blueprint: s, filter: {property: owner, op: in, values: $input.team}}", "w: {type: table, title: W, query: q}", Scope{Inputs: []string{"tier"}}, "queries.q.filter", `no input "team"`},
		{"group off groups", "q: {}", "w: {type: table, title: W, link: {query: q, group: true}}", catalog, "widgets.w.link.group", "only bars, donuts and splits"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := parseWith(t, test.queries, test.source, test.scope)
			for _, issue := range issues {
				if issue.Path == test.path && strings.Contains(issue.Message, test.message) {
					return
				}
			}
			t.Fatalf("issues = %+v, want %s: %s", issues, test.path, test.message)
		})
	}
}

func TestParseTabs(t *testing.T) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(`
deployments:
  title: Deployments
  icon: rocket
  widgets:
    count: {type: stat, title: Deployments, value: {}}
entities:
  widgets:
    note: {type: markdown, text: hi}
`), &node); err != nil {
		t.Fatal(err)
	}
	r := &configdoc.Reader{}
	tabs := ParseTabs(r, node.Content[0], "tabs", Scope{Blueprint: "service"}, "entities")

	if len(tabs) != 2 || tabs[0].Title != "Deployments" || tabs[0].Icon != "rocket" || tabs[0].Widgets[0].Path != "tabs.deployments.widgets.count" {
		t.Fatalf("tabs = %+v", tabs)
	}
	if len(r.Issues) != 1 || r.Issues[0].Path != "tabs.entities" {
		t.Fatalf("issues = %+v, want the reserved tab", r.Issues)
	}
}

func TestBind(t *testing.T) {
	f := &filter.Filter{And: []filter.Filter{
		{Property: "owner", Op: "eq", Value: ValueEntity},
		{Property: "members", Op: "in", Values: []any{ValueMe, "other"}},
	}}
	bound := Bind(f, Values{ValueEntity: {"payments"}, ValueMe: {"ada@example.com"}})

	if bound.And[0].Value != "payments" || bound.And[1].Values[0] != "ada@example.com" || bound.And[1].Values[1] != "other" {
		t.Fatalf("bound = %+v", bound)
	}
	if f.And[0].Value != ValueEntity {
		t.Fatalf("Bind changed the original: %+v", f)
	}
}

func TestBindFilters(t *testing.T) {
	team, tier := ValueInput+"team", ValueInput+"tier"
	owner := filter.Filter{Property: "owner", Op: "eq", Value: team}
	tiers := filter.Filter{Property: "tier", Op: "in", Values: []any{tier, "experimental"}}
	tests := []struct {
		name   string
		filter *filter.Filter
		values Values
		want   string
	}{
		{
			"unset drops the condition", &filter.Filter{And: []filter.Filter{owner, tiers}},
			Values{team: nil, tier: {"critical"}},
			`{"and":[{"property":"tier","op":"in","values":["critical","experimental"]}]}`,
		},
		{"unset drops the whole filter", &owner, Values{team: nil}, "null"},
		{"unset drops an empty not", &filter.Filter{Not: &owner}, Values{team: nil}, "null"},
		{"unset leaves the other values", &tiers, Values{tier: nil}, `{"property":"tier","op":"in","values":["experimental"]}`},
		{"nothing matches nothing", &owner, Values{team: Nothing}, `{"property":"owner","op":"in"}`},
		{
			"nothing keeps the and", &filter.Filter{And: []filter.Filter{owner, tiers}},
			Values{team: Nothing, tier: {"critical"}},
			`{"and":[{"property":"owner","op":"in"},{"property":"tier","op":"in","values":["critical","experimental"]}]}`,
		},
		{
			"nothing is not ne", &filter.Filter{Property: "owner", Op: "ne", Value: team},
			Values{team: Nothing},
			`{"not":{"property":"owner","op":"in"}}`,
		},
		{
			"nothing contains nothing", &filter.Filter{Property: "tags", Op: "contains", Value: team},
			Values{team: Nothing},
			`{"property":"tags","op":"in"}`,
		},
		{"nothing leaves the other values", &tiers, Values{tier: Nothing}, `{"property":"tier","op":"in","values":["experimental"]}`},
		{
			"nothing alone keeps an empty list", &filter.Filter{Property: "tier", Op: "in", Values: []any{tier}},
			Values{tier: Nothing},
			`{"property":"tier","op":"in"}`,
		},
		{
			"unset drops a list nothing shares", &filter.Filter{Property: "tier", Op: "in", Values: []any{tier, team}},
			Values{tier: Nothing, team: nil},
			"null",
		},
		{"several values turn eq into in", &owner, Values{team: {"a", "b"}}, `{"property":"owner","op":"in","values":["a","b"]}`},
		{
			"several values turn ne into not in", &filter.Filter{Property: "owner", Op: "ne", Value: team},
			Values{team: {"a", "b"}},
			`{"not":{"property":"owner","op":"in","values":["a","b"]}}`,
		},
		{
			"several values match any", &filter.Filter{Property: "tags", Op: "contains", Value: team},
			Values{team: {"a", "b"}},
			`{"or":[{"property":"tags","op":"contains","value":"a"},{"property":"tags","op":"contains","value":"b"}]}`,
		},
		{"several values join a list", &tiers, Values{tier: {"a", "b"}}, `{"property":"tier","op":"in","values":["a","b","experimental"]}`},
		{"values takes the variable's", &filter.Filter{Property: "owner", Op: "in", ValuesFrom: team}, Values{team: {"a"}}, `{"property":"owner","op":"in","values":["a"]}`},
		{"values drops unset", &filter.Filter{Property: "owner", Op: "in", ValuesFrom: team}, Values{team: nil}, "null"},
		{"values with nothing matches nothing", &filter.Filter{Property: "owner", Op: "in", ValuesFrom: team}, Values{team: Nothing}, `{"property":"owner","op":"in"}`},
		{"values stays until set", &filter.Filter{Property: "owner", Op: "in", ValuesFrom: team}, Values{}, `{"property":"owner","op":"in","values":"$input.team"}`},
		{"literals stay", &filter.Filter{Property: "tier", Op: "eq", Value: "critical"}, Values{team: nil}, `{"property":"tier","op":"eq","value":"critical"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := json.Marshal(Bind(test.filter, test.values))
			if string(got) != test.want {
				t.Fatalf("Bind() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestInputRefs(t *testing.T) {
	f := &filter.Filter{Or: []filter.Filter{
		{Property: "owner", Op: "eq", Value: ValueInput + "team"},
		{Not: &filter.Filter{Property: "tier", Op: "in", Values: []any{ValueInput + "tier", ValueInput + "team", ValueMe}}},
		{Property: "lang", Op: "in", ValuesFrom: ValueInput + "lang"},
	}}
	if got := strings.Join(InputRefs(f), ","); got != "$input.team,$input.tier,$input.lang" {
		t.Fatalf("InputRefs() = %s", got)
	}
}

func TestFilterJSONValues(t *testing.T) {
	f, err := filter.Parse(`{"and":[{"property":"owner","op":"in","values":"$input.team"},{"property":"tier","op":"in","values":["a"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if f.And[0].ValuesFrom != "$input.team" || f.And[0].Values != nil || f.And[1].Values[0] != "a" {
		t.Fatalf("parsed = %+v", f)
	}
	encoded, _ := json.Marshal(f)
	if string(encoded) != `{"and":[{"property":"owner","op":"in","values":"$input.team"},{"property":"tier","op":"in","values":["a"]}]}` {
		t.Fatalf("encoded = %s", encoded)
	}
	if _, err := filter.Parse(`{"property":"owner","op":"in","values":3}`); err == nil {
		t.Fatal("a number isn't values")
	}
}

func TestParseMeVariables(t *testing.T) {
	queries := "owned: {blueprint: s, filter: {property: owner, op: eq, value: $me.groups}}"
	source := `
hello: {type: markdown, text: "Hi {{ $me.name }}, you're in {{$me.groups}}. Pay $5."}
mine: {type: stat, title: "{{ $me.email }}", description: "As {{ $me.role }}", value: {query: owned}}
`
	widgets, issues := parseWith(t, queries, source, Scope{})
	if len(issues) > 0 {
		t.Fatalf("issues = %+v", issues)
	}
	if f := widgets[1].Value.Query.Filter; f.Value != ValueMe+".groups" {
		t.Fatalf("filter = %+v", f)
	}
	// Markdown text takes variables on a blueprint's pages too.
	_, issues = parseWith(t, queries, source, Scope{Blueprint: "s"})
	if len(issues) != 2 || issues[0].Path != "widgets.mine.title" || issues[1].Path != "widgets.mine.description" {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestMeValues(t *testing.T) {
	ada := auth.Principal{ID: "1", Email: "ada@example.com", DisplayName: "Ada", Role: auth.RoleBuilder, Groups: []string{"payments", "search"}}
	want := Values{
		ValueMe: {"ada@example.com"}, "$me.email": {"ada@example.com"}, "$me.name": {"Ada"},
		"$me.role": {"builder"}, "$me.groups": {"payments", "search"},
	}
	if got := MeValues(ada); !reflect.DeepEqual(got, want) {
		t.Fatalf("MeValues() = %v, want %v", got, want)
	}
	if got := strings.Join(MeVariables(), " "); got != "$me $me.email $me.name $me.role $me.groups" {
		t.Fatalf("MeVariables() = %s", got)
	}

	// Someone with no groups or no name matches nothing by them, not everything.
	nobody := MeValues(auth.Principal{Email: "x@example.com"})
	for _, name := range []string{"$me.groups", "$me.name", "$me.role"} {
		if got := nobody[name]; got == nil || len(got) != 0 {
			t.Errorf("%s = %#v, want Nothing", name, got)
		}
	}
}

func TestBindMe(t *testing.T) {
	groups := ValueMe + ".groups"
	ada := MeValues(auth.Principal{Email: "ada@example.com", DisplayName: "Ada", Groups: []string{"payments", "search"}})
	one := MeValues(auth.Principal{Email: "ada@example.com", Groups: []string{"payments"}})
	none := MeValues(auth.Principal{Email: "ada@example.com"})
	tests := []struct {
		name   string
		filter *filter.Filter
		values Values
		want   string
	}{
		{"alias", &filter.Filter{Property: "author", Op: "eq", Value: ValueMe}, ada, `{"property":"author","op":"eq","value":"ada@example.com"}`},
		{"single", &filter.Filter{Property: "author", Op: "eq", Value: ValueMe + ".name"}, ada, `{"property":"author","op":"eq","value":"Ada"}`},
		{"multi turns eq into in", &filter.Filter{Property: "owner", Op: "eq", Value: groups}, ada, `{"property":"owner","op":"in","values":["payments","search"]}`},
		{"multi turns ne into not in", &filter.Filter{Property: "owner", Op: "ne", Value: groups}, ada, `{"not":{"property":"owner","op":"in","values":["payments","search"]}}`},
		{"multi joins a list", &filter.Filter{Property: "$identifier", Op: "in", Values: []any{groups}}, ada, `{"property":"$identifier","op":"in","values":["payments","search"]}`},
		{"one group stays eq", &filter.Filter{Property: "owner", Op: "eq", Value: groups}, one, `{"property":"owner","op":"eq","value":"payments"}`},
		{"no groups matches nothing", &filter.Filter{Property: "owner", Op: "eq", Value: groups}, none, `{"property":"owner","op":"in"}`},
		{"no groups keeps the condition", &filter.Filter{And: []filter.Filter{{Property: "owner", Op: "eq", Value: groups}, {Property: "tier", Op: "eq", Value: "critical"}}}, none, `{"and":[{"property":"owner","op":"in"},{"property":"tier","op":"eq","value":"critical"}]}`},
		{"no name matches nothing", &filter.Filter{Property: "author", Op: "eq", Value: ValueMe + ".name"}, none, `{"property":"author","op":"in"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := json.Marshal(Bind(test.filter, test.values))
			if string(got) != test.want {
				t.Fatalf("Bind() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestRender(t *testing.T) {
	ada := MeValues(auth.Principal{Email: "ada@example.com", DisplayName: "Ada", Role: auth.RoleMember, Groups: []string{"payments", "search"}})
	none := MeValues(auth.Principal{Email: "x@example.com"})
	tests := []struct {
		name, text string
		values     Values
		want       string
	}{
		{"name", "Hello {{ $me.name }}!", ada, "Hello Ada!"},
		{"alias and email", "{{$me}} is {{ $me.email }}", ada, "ada@example.com is ada@example.com"},
		{"list joins", "In {{ $me.groups }} as {{ $me.role }}", ada, "In payments, search as member"},
		{"unavailable is empty", "Hello {{ $me.name }}, in {{ $me.groups }}.", none, "Hello , in ."},
		{"bare dollar stays", "Pay $5 to $me.name", ada, "Pay $5 to $me.name"},
		{"no tokens", "plain", ada, "plain"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Render(test.text, test.values); got != test.want {
				t.Fatalf("Render() = %q, want %q", got, test.want)
			}
		})
	}

	for text, want := range map[string]string{
		"{{ $me.nmae }}":  "unknown variable $me.nmae; known: $me, $me.email, $me.name, $me.role, $me.groups",
		"{{ $entity }}":   "$entity is only available on entity pages",
		"{{ }}":           "{{ }} must name a variable, like {{ $me.name }}",
		"{{ $me.name }}":  "",
		"{{ $me }} and $": "",
	} {
		err := CheckText(text)
		if (err == nil && want != "") || (err != nil && err.Error() != want) {
			t.Errorf("CheckText(%q) = %v, want %q", text, err, want)
		}
	}

	widgets := []Widget{{ID: "w", Title: "{{ $me.name }}", Description: "{{ $me.role }}", Text: "{{ $me.groups }}"}}
	rendered := RenderAll(widgets, ada)
	if w := rendered[0]; w.Title != "Ada" || w.Description != "member" || w.Text != "payments, search" {
		t.Errorf("RenderAll() = %+v", w)
	}
	if widgets[0].Title != "{{ $me.name }}" {
		t.Error("RenderAll changed the originals")
	}
}

func TestRenderTemplate(t *testing.T) {
	env := Env{
		Values: Values{
			ValueInput + "team": {"payments", "search"}, ValueInput + "tier": nil,
			"$me.email": {"ada@example.com"}, "$me.groups": {"payments"}, "$today": {"2026-10-25"},
		},
		Entity: map[string]any{"identifier": "ledger", "tier": "critical"},
	}
	row := map[string]any{"identifier": "a b/c?d", "stars": float64(3)}
	tests := []struct{ text, want string }{
		{`https://status.example.com/?team={{ $input.team | join(",") }}`, "https://status.example.com/?team=payments,search"},
		{`https://status.example.com/?team={{ $input.team }}&tier={{ $input.tier }}`, "https://status.example.com/?team=payments,search&tier="},
		{`/catalog/service/{{ .identifier }}`, "/catalog/service/a%20b/c%3Fd"},
		{`/x?stars={{ .stars }}&me={{ $me.email }}&on={{ $today }}&e={{ $entity.tier }}`, "/x?stars=3&me=ada%40example.com&on=2026-10-25&e=critical"},
		{`/x/{{ "&y=1#z" }}`, "/x/%26y%3D1%23z"},
	}
	for _, test := range tests {
		got, err := RenderTemplate(t.Context(), test.text, row, env)
		if err != nil || got != test.want {
			t.Errorf("RenderTemplate(%q) = %q, %v, want %q", test.text, got, err, test.want)
		}
	}
	if _, err := RenderTemplate(t.Context(), "/x/{{ error(\"no\") }}", nil, env); err == nil {
		t.Error("a failing template rendered")
	}
	if err := CheckTemplate("/x/{{ $entity.identifier }}", Scope{}); err == nil {
		t.Error("$entity compiled off entity pages")
	}
	if err := CheckTemplate("/x/{{ $entity.identifier }}", Scope{Entity: true}); err != nil {
		t.Error(err)
	}
}
