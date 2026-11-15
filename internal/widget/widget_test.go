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
  columns: [{path: tier}, {title: Owner, path: owner.title}, {title: Hours, path: uptime, format: . / 3600 | floor, unit: h}, {title: PCI, filter: {property: tags, op: contains, value: pci}}]
  sort: -PCI
  rowLink: {path: '/catalog/service/{{ identifier }}?team={{ owner.title | ascii_downcase }}'}
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
	pci := &filter.Filter{Property: "tags", Op: filter.OpContains, Value: "pci"}
	if want := []Column{{Path: "tier"}, {Title: "Owner", Path: "owner.title"}, {Title: "Hours", Path: "uptime", Format: ". / 3600 | floor", Unit: "h"}, {Title: "PCI", Filter: pci}}; !reflect.DeepEqual(services.Columns, want) || services.RowLink.Path == "" {
		t.Errorf("table columns = %+v, row link %+v", services.Columns, services.RowLink)
	}
	if got := RowTemplatePaths(services.RowLink.Path); !reflect.DeepEqual(got, []string{"identifier", "owner.title"}) {
		t.Errorf("row link paths = %v", got)
	}
	if got := len(services.Filters()); got != 2 {
		t.Errorf("table filters = %d, want the query's and the PCI column's", got)
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
		{Value{Summary: Summary{Distinct: "owner"}}, OpDistinct, "owner"},
		{Value{Summary: Summary{P95: "duration"}}, "p95", "duration"},
	} {
		if op, path := test.value.Op(); op != test.op || path != test.path {
			t.Errorf("Op(%+v) = %s %s, want %s %s", test.value, op, path, test.op, test.path)
		}
	}
}

func TestParseMeasuresAndTrends(t *testing.T) {
	widgets, issues := parse(t, `
p95:
  type: stat
  title: P95 Duration
  value: {p95: duration, unit: s}
failing:
  type: bar
  title: Failure Share by Stage
  groupBy: stage
  measure: {filter: {property: status, op: ne, value: skipped}, share: {property: status, op: eq, value: failed}}
  sort: -measure
byStage:
  type: matrix
  title: By Stage
  groupBy: stage
  sort: -P95
  measures:
    - {title: Jobs}
    - {title: P95, p95: duration}
perDay:
  type: trend
  title: Jobs per Day
  date: finishedAt
  link: {query: jobs, group: true}
p95PerDay:
  type: trend
  title: P95 per Day
  date: finishedAt
  range: 2w
  measure: {p95: duration}
`, Scope{Blueprint: "job", Queries: []Query{{Name: "jobs", Blueprint: "job"}}})
	if configdoc.HasErrors(issues) {
		t.Fatalf("issues = %+v", issues)
	}
	p95, failing, byStage, perDay, p95PerDay := widgets[0], widgets[1], widgets[2], widgets[3], widgets[4]
	if op, path := p95.Value.Op(); op != "p95" || path != "duration" || p95.Value.Unit != "s" {
		t.Errorf("stat = %+v", p95.Value)
	}
	if op, _ := failing.Measure.Op(); op != OpShare || failing.Measure.Filter.Value != "skipped" || failing.Sort != "-measure" || failing.Width != 6 {
		t.Errorf("bar = %+v", failing)
	}
	if op, _ := byStage.Measures[0].Op(); op != OpCount || byStage.Measures[1].P95 != "duration" || byStage.Sort != "-P95" {
		t.Errorf("matrix = %+v", byStage)
	}
	if perDay.Width != 6 || perDay.Date != "finishedAt" || perDay.Range != DefaultRange || perDay.Measure != nil || !perDay.Link.Group {
		t.Errorf("trend defaults = %+v", perDay)
	}
	if days, ok := TrendDays(p95PerDay.Range); !ok || days != 14 || p95PerDay.Measure.P95 != "duration" {
		t.Errorf("trend = %+v", p95PerDay)
	}
	if got := len(failing.Filters()); got != 2 {
		t.Errorf("bar filters = %d, want the measure's filter and share", got)
	}
	bound := failing.MapFilters(func(*filter.Filter) *filter.Filter { return nil })
	if bound.Measure.Filter != nil || bound.Measure.Share != nil || failing.Measure.Share == nil {
		t.Errorf("MapFilters = %+v, original %+v", bound.Measure, failing.Measure)
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
		{"jq value", "w: {type: stat, title: W, value: {jq: length}}", catalog, "widgets.w.value.jq", valueJQ},
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
		{"measure ops", "w: {type: matrix, title: W, groupBy: owner, measures: [{title: M, p95: a, avg: a}]}", catalog, "widgets.w.measures[0].p95", "a measure takes one of"},
		{"percentile takes a path", "w: {type: stat, title: W, value: {p95: 'a b'}}", catalog, "widgets.w.value.p95", "p95 takes the path of a number field"},
		{"value ops", "w: {type: stat, title: W, value: {p99: a, distinct: a}}", catalog, "widgets.w.value.p99", "a value takes one of distinct, sum, avg, min, max, p50, p90, p95, p99, share"},
		{"distinct takes a path", "w: {type: stat, title: W, value: {distinct: 'a b'}}", catalog, "widgets.w.value.distinct", "distinct takes the path of a field, such as owner"},
		{"max takes a path", "w: {type: stat, title: W, value: {max: 'a b'}}", catalog, "widgets.w.value.max", "max takes the path of a number or date field"},
		{"bar measure title", "w: {type: bar, title: W, groupBy: a, measure: {title: M}}", catalog, "widgets.w.measure.title", "unknown field"},
		{"measure share unit", "w: {type: bar, title: W, groupBy: a, measure: {share: {property: a, op: exists}, unit: s}}", catalog, "widgets.w.measure.unit", "percent"},
		{"bar sort", "w: {type: bar, title: W, groupBy: a, sort: -size}", catalog, "widgets.w.sort", "sort is count or group, with a leading - for descending"},
		{"bar sort without measure", "w: {type: bar, title: W, groupBy: a, sort: -measure}", catalog, "widgets.w.sort", "sort is count or group"},
		{"donut measure", "w: {type: donut, title: W, groupBy: a, measure: {p95: a}}", catalog, "widgets.w.measure", "unknown field"},
		{"matrix sort", "w: {type: matrix, title: W, groupBy: a, sort: -Nope, measures: [{title: Failed, share: {property: a, op: exists}}]}", catalog, "widgets.w.sort", "a measure's title, with a leading - for descending, such as -Failed"},
		{"measure titles", "w: {type: matrix, title: W, groupBy: a, measures: [{title: M}, {title: M, p95: a}]}", catalog, "widgets.w.measures[1].title", "another measure is titled M"},
		{"trend date", "w: {type: trend, title: W}", catalog, "widgets.w.date", "add the date field"},
		{"trend date path", "w: {type: trend, title: W, date: 'a b'}", catalog, "widgets.w.date", "isn't a field's path"},
		{"trend range hours", "w: {type: trend, title: W, date: a, range: 36h}", catalog, "widgets.w.range", "1d to 365d"},
		{"trend range too long", "w: {type: trend, title: W, date: a, range: 400d}", catalog, "widgets.w.range", "1d to 365d"},
		{"trend sort", "w: {type: trend, title: W, date: a, sort: -count}", catalog, "widgets.w.sort", "unknown field"},
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
		{"template jq", "w: {type: table, title: W, link: {path: '/x/{{ $me | }}'}}", catalog, "widgets.w.link.path", "doesn't compile"},
		{"template over no input", "w: {type: table, title: W, link: {path: '/x/{{ .identifier }}'}}", catalog, "widgets.w.link.path", "{{ .identifier }} reads ., which a widget's link doesn't have"},
		{"empty template", "w: {type: table, title: W, link: {path: '/x/{{ }}'}}", catalog, "widgets.w.link.path", "like {{ $me.email }}"},
		{"widget link starting with a token", "w: {type: table, title: W, link: {url: '{{ $me.email }}'}}", catalog, "widgets.w.link.url", "url must start with https:// or http://"},
		{"row link url without its scheme", "w: {type: table, title: W, rowLink: {url: 'gitlab.example.com/{{ path }}'}}", catalog, "widgets.w.rowLink.url", "url must start with https:// or http://, or in a row link"},
		{"row link query", "w: {type: table, title: W, rowLink: {query: {}}}", catalog, "widgets.w.rowLink.query", "unknown field"},
		{"column jq", "w: {type: table, title: W, columns: [{title: X, jq: .x}]}", catalog, "widgets.w.columns[0].jq", columnJQ},
		{"column needs a path or a filter", "w: {type: table, title: W, columns: [{title: X}]}", catalog, "widgets.w.columns[0]", "a column has a path, or a title and a filter"},
		{"filter column title", "w: {type: table, title: W, columns: [{filter: {property: a, op: exists}}]}", catalog, "widgets.w.columns[0].title", "add the column's title"},
		{"filter column titles", "w: {type: table, title: W, columns: [{title: X, filter: {property: a, op: exists}}, {title: X, filter: {property: b, op: exists}}]}", catalog, "widgets.w.columns[1].title", "another filter column is titled X"},
		{"filter column format", "w: {type: table, title: W, columns: [{title: X, filter: {property: a, op: exists}, format: .}]}", catalog, "widgets.w.columns[0]", "takes no format or unit"},
		{"filter column entity off its page", "w: {type: table, title: W, columns: [{title: X, filter: {property: a, op: eq, value: $entity}}]}", catalog, "widgets.w.columns[0].filter", "only entity pages"},
		{"column format", "w: {type: table, title: W, columns: [{path: x, format: '. |'}]}", catalog, "widgets.w.columns[0].format", "doesn't compile"},
		{"unknown column field", "w: {type: table, title: W, columns: [{title: X, value: .x}]}", catalog, "widgets.w.columns[0].value", "unknown field"},
		{"column path and filter", "w: {type: table, title: W, columns: [{title: X, path: x, filter: {property: a, op: exists}}]}", catalog, "widgets.w.columns[0]", "not both"},
		{"row link over the entity", "w: {type: table, title: W, rowLink: {path: '/x/{{ .identifier }}'}}", catalog, "widgets.w.rowLink.path", rowToken},
		{"row link over a variable", "w: {type: table, title: W, rowLink: {url: 'https://x.example.com/{{ $me.name }}'}}", catalog, "widgets.w.rowLink.url", rowToken},
		{"row link jq", "w: {type: table, title: W, rowLink: {path: '/x/{{ url | sub( }}'}}", catalog, "widgets.w.rowLink.path", "doesn't compile"},
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
		{"group off groups", "q: {}", "w: {type: table, title: W, link: {query: q, group: true}}", catalog, "widgets.w.link.group", "only bars, donuts, splits and trends"},
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
	tests := []struct{ text, want string }{
		{`https://status.example.com/?team={{ $input.team | join(",") }}`, "https://status.example.com/?team=payments,search"},
		{`https://status.example.com/?team={{ $input.team }}&tier={{ $input.tier }}`, "https://status.example.com/?team=payments,search&tier="},
		{`/x?me={{ $me.email }}&on={{ $today }}&e={{ $entity.tier }}`, "/x?me=ada%40example.com&on=2026-10-25&e=critical"},
		{`/x/{{ "&y=1#z" }}`, "/x/%26y%3D1%23z"},
	}
	for _, test := range tests {
		got, err := RenderTemplate(t.Context(), test.text, env)
		if err != nil || got != test.want {
			t.Errorf("RenderTemplate(%q) = %q, %v, want %q", test.text, got, err, test.want)
		}
	}
	if _, err := RenderTemplate(t.Context(), "/x/{{ error(\"no\") }}", env); err == nil {
		t.Error("a failing template rendered")
	}

	row := map[string]any{"identifier": "a b/c?d", "stars": float64(3), "url": "https://GitLab.example.com/a"}
	for _, test := range []struct{ text, want string }{
		{`/catalog/service/{{ identifier }}`, "/catalog/service/a%20b/c%3Fd"},
		{`/x?stars={{stars|. * 2}}&me={{ url | ascii_downcase | $me.email + . }}`, "/x?stars=6&me=ada%40example.comhttps://gitlab.example.com/a"},
		{`/x?missing={{ owner.title | ascii_downcase }}`, "/x?missing="},
		{`{{ url }}/-/pipelines?ref={{ identifier }}`, "https://GitLab.example.com/a/-/pipelines?ref=a%20b/c%3Fd"},
	} {
		got, err := RenderRowTemplate(t.Context(), test.text, row, env)
		if err != nil || got != test.want {
			t.Errorf("RenderRowTemplate(%q) = %q, %v, want %q", test.text, got, err, test.want)
		}
	}
	if _, err := RenderRowTemplate(t.Context(), "/x/{{ .identifier }}", row, env); err == nil {
		t.Error("a row link read beyond its row")
	}
	if err := CheckTemplate("/x/{{ $entity.identifier }}", Scope{}); err == nil {
		t.Error("$entity compiled off entity pages")
	}
	if err := CheckTemplate("/x/{{ $entity.identifier }}", Scope{Entity: true}); err != nil {
		t.Error(err)
	}
}

// A row link can be the row's own URL, which the loader checks once it's
// filled in.
func TestRowLinkFromAStoredURL(t *testing.T) {
	widgets, issues := parse(t, "w: {type: table, title: W, rowLink: {url: '{{ url }}'}}", Scope{Blueprint: "repository"})
	if len(issues) > 0 || widgets[0].RowLink.URL != "{{ url }}" {
		t.Fatalf("row link = %+v, issues %+v", widgets[0].RowLink, issues)
	}
}
