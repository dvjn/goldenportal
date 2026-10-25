package widget

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/filter"
)

func parse(t *testing.T, source string, scope Scope) ([]Widget, []configdoc.Issue) {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(source), &node); err != nil {
		t.Fatal(err)
	}
	r := &configdoc.Reader{}
	widgets := Parse(r, node.Content[0], "widgets", scope)

	return widgets, r.Issues
}

func TestParse(t *testing.T) {
	widgets, issues := parse(t, `
count:
  type: stat
  title: Services
byTier:
  type: donut
  title: By tier
  groupBy: tier
services:
  type: table
  title: Services
  blueprint: service
  filter: {property: owner, op: eq, value: $entity}
  columns: [tier, language]
  sort: -lastDeployedAt
about:
  type: properties
  title: About
`, Scope{Blueprint: "team", Entity: true})
	if configdoc.HasErrors(issues) {
		t.Fatalf("issues = %+v", issues)
	}

	ids := []string{}
	for _, w := range widgets {
		ids = append(ids, w.ID)
	}
	if got := strings.Join(ids, ","); got != "count,byTier,services,about" {
		t.Fatalf("widget order = %s", got)
	}
	count, byTier, services := widgets[0], widgets[1], widgets[2]
	if count.Aggregate != "count" || count.Width != 3 || count.Blueprint != "team" || count.Path != "widgets.count" {
		t.Errorf("stat defaults = %+v", count)
	}
	if byTier.Limit != 8 || byTier.Width != 4 {
		t.Errorf("donut defaults = %+v", byTier)
	}
	if services.Limit != 10 || services.Width != 12 || services.Blueprint != "service" || services.Filter.Value != ValueEntity {
		t.Errorf("table = %+v", services)
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
		{"home needs blueprint", "w: {type: stat, title: W}", home, "widgets.w.blueprint", "name the blueprint"},
		{"width", "w: {type: markdown, text: x, width: 13}", home, "widgets.w.width", "1 to 12"},
		{"group by", "w: {type: bar, title: W}", catalog, "widgets.w.groupBy", "add the property"},
		{"limit", "w: {type: table, title: W, limit: 500}", catalog, "widgets.w.limit", "1 to 50"},
		{"entity off its page", "w: {type: table, title: W, filter: {property: owner, op: eq, value: $entity}}", catalog, "widgets.w.filter", "only entity pages"},
		{"properties off entity", "w: {type: properties, title: W}", catalog, "widgets.w.type", "go on entity pages"},
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

func TestParseTabs(t *testing.T) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(`
deployments:
  title: Deployments
  icon: rocket
  widgets:
    count: {type: stat, title: Deployments, blueprint: deployment}
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
	bound := Bind(f, map[string]string{ValueEntity: "payments", ValueMe: "ada@example.com"})

	if bound.And[0].Value != "payments" || bound.And[1].Values[0] != "ada@example.com" || bound.And[1].Values[1] != "other" {
		t.Fatalf("bound = %+v", bound)
	}
	if f.And[0].Value != ValueEntity {
		t.Fatalf("Bind changed the original: %+v", f)
	}
}
