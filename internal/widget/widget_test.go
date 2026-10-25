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
		{"filter off home", "w: {type: table, title: W, filter: {property: owner, op: eq, value: $filter.team}}", catalog, "widgets.w.filter", "only Home has it"},
		{"unknown me attribute", "w: {type: table, title: W, filter: {property: owner, op: in, values: [$me.nmae]}}", catalog, "widgets.w.filter", "unknown variable $me.nmae; known: $me, $me.email, $me.name, $me.role, $me.groups"},
		{"unknown text variable", "w: {type: markdown, text: 'Hi {{ $me.nmae }}'}", catalog, "widgets.w.text", "unknown variable $me.nmae; known: $me, $me.email, $me.name, $me.role, $me.groups"},
		{"empty token", "w: {type: markdown, text: 'Hi {{}}'}", catalog, "widgets.w.text", "{{}} must name a variable, like {{ $me.name }}"},
		{"title variable off home", "w: {type: table, title: '{{ $me.name }}', blueprint: s}", catalog, "widgets.w.title", "only have them in markdown text"},
		{"description variable off home", "w: {type: table, title: W, description: '{{ $me.name }}', blueprint: s}", catalog, "widgets.w.description", "only have them in markdown text"},
		{"undeclared filter", "w: {type: table, title: W, blueprint: s, filter: {property: owner, op: in, values: [$filter.team]}}", Scope{Filters: []string{"tier"}}, "widgets.w.filter", `no filter "team"`},
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
	bound := Bind(f, Values{ValueEntity: {"payments"}, ValueMe: {"ada@example.com"}})

	if bound.And[0].Value != "payments" || bound.And[1].Values[0] != "ada@example.com" || bound.And[1].Values[1] != "other" {
		t.Fatalf("bound = %+v", bound)
	}
	if f.And[0].Value != ValueEntity {
		t.Fatalf("Bind changed the original: %+v", f)
	}
}

func TestBindFilters(t *testing.T) {
	team, tier := ValueFilter+"team", ValueFilter+"tier"
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

func TestFilterRefs(t *testing.T) {
	f := &filter.Filter{Or: []filter.Filter{
		{Property: "owner", Op: "eq", Value: ValueFilter + "team"},
		{Not: &filter.Filter{Property: "tier", Op: "in", Values: []any{ValueFilter + "tier", ValueFilter + "team", ValueMe}}},
	}}
	if got := strings.Join(FilterRefs(f), ","); got != "$filter.team,$filter.tier" {
		t.Fatalf("FilterRefs() = %s", got)
	}
}

func TestParseMeVariables(t *testing.T) {
	r := &configdoc.Reader{}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(`
hello: {type: markdown, text: "Hi {{ $me.name }}, you're in {{$me.groups}}. Pay $5."}
mine: {type: stat, title: "{{ $me.email }}", description: "As {{ $me.role }}", blueprint: s, filter: {property: owner, op: eq, value: $me.groups}}
`), &node); err != nil {
		t.Fatal(err)
	}
	widgets := Parse(r, node.Content[0], "widgets", Scope{})
	if len(r.Issues) > 0 {
		t.Fatalf("issues = %+v", r.Issues)
	}
	if widgets[1].Filter.Value != ValueMe+".groups" {
		t.Fatalf("filter = %+v", widgets[1].Filter)
	}
	// Markdown text takes variables on a blueprint's pages too.
	r = &configdoc.Reader{}
	Parse(r, node.Content[0], "widgets", Scope{Blueprint: "s"})
	if len(r.Issues) != 2 || r.Issues[0].Path != "widgets.mine.title" || r.Issues[1].Path != "widgets.mine.description" {
		t.Fatalf("issues = %+v", r.Issues)
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
		"{{ $entity }}":   "unknown variable $entity; known: $me, $me.email, $me.name, $me.role, $me.groups",
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
