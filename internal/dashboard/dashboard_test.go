package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/scorecard"
	"github.com/dvjn/goldenportal/internal/widget"
)

const overviewYAML = `
kind: Dashboard
metadata:
  name: overview
  title: Overview
spec:
  order: 2
  inputs:
    team:
      title: Team
      type: relation
      relation: team
    tier:
      type: enum
      values: [critical, standard]
      multiple: true
      default: [critical]
    mine:
      title: My teams
      type: relation
      relation: team
      multiple: true
      default:
        query:
          blueprint: team
          filter: {property: members, op: contains, value: $me}
  queries:
    services:
      blueprint: service
      filter:
        and:
          - {property: owner, op: eq, value: $input.team}
          - {property: owner, op: in, values: $input.mine}
          - {property: tier, op: in, values: $input.tier}
  widgets:
    count:
      type: stat
      title: Services
      value: {query: services}
      link: {query: services, filter: {property: tier, op: eq, value: critical}}
    byTier:
      type: donut
      title: By tier
      query: services
      groupBy: tier
      link: {query: services, group: true}
    note:
      type: markdown
      text: Hello
`

func mustParse(t *testing.T, source string) Spec {
	t.Helper()
	spec, issues := Parse(source)
	if configdoc.HasErrors(issues) {
		t.Fatalf("Parse() issues = %+v", issues)
	}

	return spec
}

func TestParse(t *testing.T) {
	spec := mustParse(t, overviewYAML)
	if spec.Order != 2 || len(spec.Widgets) != 3 || spec.Widgets[0].Path != "spec.widgets.count" {
		t.Fatalf("spec = %+v", spec)
	}
	services := spec.Queries[0]
	if services.Name != "services" || services.Blueprint != "service" || services.Path != "spec.queries.services" || len(services.Filter.And) != 3 {
		t.Errorf("queries = %+v", spec.Queries)
	}
	count, byTier := spec.Widgets[0], spec.Widgets[1]
	if count.Value.Query.Name != "services" || count.Value.Query.Path != "" || func() bool { op, _ := count.Value.Op(); return op != widget.OpCount }() || count.Link.Filter.Value != "critical" {
		t.Errorf("count = %+v", count)
	}
	if byTier.Query.Name != "services" || !byTier.Link.Group {
		t.Errorf("byTier = %+v", byTier)
	}
	team, tier := spec.Inputs[0], spec.Inputs[1]
	if team.Title != "Team" || team.Type != InputRelation || team.Relation != "team" || team.Default != nil {
		t.Errorf("team = %+v", team)
	}
	if tier.Title != "tier" || !tier.Multiple || strings.Join(tier.Values, ",") != "critical,standard" || strings.Join(tier.Default, ",") != "critical" {
		t.Errorf("tier = %+v", tier)
	}
	mine := spec.Inputs[2]
	if mine.Default != nil || mine.DefaultQuery == nil || mine.DefaultQuery.Query.Blueprint != "team" || mine.DefaultQuery.Query.Filter == nil || mine.DefaultQuery.Query.Filter.Value != widget.ValueMe {
		t.Errorf("mine = %+v", mine)
	}

	for source, path := range map[string]string{
		"kind: Dashboard\nmetadata: {name: d, title: D}\nspec: {order: 1}":                                                "spec.widgets",
		"kind: Dashboard\nmetadata: {name: d, title: D}\nspec:\n  page: home\n  widgets: {w: {type: markdown, text: x}}":  "spec.page",
		"kind: Dashboard\nmetadata: {name: d, title: D}\nspec:\n  widgets:\n    w: {type: properties, title: W}":          "spec.widgets.w.type",
		"kind: Dashboard\nmetadata: {name: d, title: D}\nspec:\n  filters: {}\n  widgets: {w: {type: markdown, text: x}}": "spec.filters",
	} {
		_, issues := Parse(source)
		if !strings.Contains(issuePaths(issues), path) {
			t.Errorf("Parse(%q) issues = %+v, want one on %s", source, issues, path)
		}
	}
}

func TestParseInputProblems(t *testing.T) {
	tests := []struct {
		name, inputs, path, message string
	}{
		{"type", "f: {title: F}", "spec.inputs.f.type", "type must be one of"},
		{"name", "1f: {type: string}", "spec.inputs.1f", "must start with a letter"},
		{"reserved", "d: {type: string}", "spec.inputs.d", "can't be named d"},
		{"enum values", "f: {type: enum}", "spec.inputs.f.values", "list the values"},
		{"enum twice", "f: {type: enum, values: [a, a]}", "spec.inputs.f.values", "twice"},
		{"enum relation", "f: {type: enum, values: [a], relation: team}", "spec.inputs.f.relation", "only relation inputs"},
		{"relation", "f: {type: relation}", "spec.inputs.f.relation", "name the blueprint"},
		{"relation values", "f: {type: relation, relation: team, values: [a]}", "spec.inputs.f.values", "only enum inputs"},
		{"string values", "f: {type: string, values: [a]}", "spec.inputs.f.values", "any text"},
		{"default off the enum", "f: {type: enum, values: [a], default: b}", "spec.inputs.f.default", "not one of the values"},
		{"several defaults", "f: {type: enum, values: [a, b], default: [a, b]}", "spec.inputs.f.default", "one value"},
		{"empty default", "f: {type: string, default: ''}", "spec.inputs.f.default", "can't be empty"},
		{"query on enum", "f: {type: enum, values: [a], default: {query: {blueprint: team}}}", "spec.inputs.f.default", "multiple: true"},
		{"query on one relation", "f: {type: relation, relation: team, default: {query: {blueprint: team}}}", "spec.inputs.f.default", "multiple: true"},
		{"old from and where", "f: {type: relation, relation: team, multiple: true, default: {from: team, where: {property: p, op: eq, value: x}}}", "spec.inputs.f.default", "a default is now a query"},
		{"query without blueprint", "f: {type: relation, relation: team, multiple: true, default: {query: {filter: {property: p, op: eq, value: x}}}}", "spec.inputs.f.default.query.blueprint", "name the blueprint"},
		{"query of elsewhere", "f: {type: relation, relation: team, multiple: true, default: {query: {blueprint: group}}}", "spec.inputs.f.default.query.blueprint", "must be team"},
		{"default without query", "f: {type: relation, relation: team, multiple: true, default: {otherwise: nothing}}", "spec.inputs.f.default.query", "add the query"},
		{"query filter is no filter", "f: {type: relation, relation: team, multiple: true, default: {query: {blueprint: team, filter: text}}}", "spec.inputs.f.default.query.filter", "not valid JSON"},
		{"query filter uses an input", "f: {type: relation, relation: team, multiple: true, default: {query: {blueprint: team, filter: {property: p, op: eq, value: $input.f}}}}", "spec.inputs.f.default.query.filter", "not with $entity or the dashboard's inputs"},
		{"query filter uses the entity", "f: {type: relation, relation: team, multiple: true, default: {query: {blueprint: team, filter: {property: p, op: eq, value: $entity}}}}", "spec.inputs.f.default.query.filter", "not with $entity or the dashboard's inputs"},
		{"query filter misspells me", "f: {type: relation, relation: team, multiple: true, default: {query: {blueprint: team, filter: {property: p, op: eq, value: $me.grops}}}}", "spec.inputs.f.default.query.filter", "unknown variable $me.grops; known: $me, $me.email, $me.name, $me.role, $me.groups"},
		{"otherwise", "f: {type: relation, relation: team, multiple: true, default: {query: {blueprint: team}, otherwise: all}}", "spec.inputs.f.default.otherwise", "nothing or everything"},
		{"property", "f: {type: string, property: a}", "spec.inputs.f.property", "unknown field"},
		{"undeclared", "f: {type: string}", "spec.widgets.w.value.query.filter", `no input "g"`},
		{"unused", "f: {type: string}", "spec.inputs.f", "no query compares with $input.f, so the input narrows nothing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := Parse("kind: Dashboard\nmetadata: {name: d, title: D}\nspec:\n  inputs:\n    " + test.inputs +
				"\n  widgets:\n    w: {type: stat, title: W, value: {query: {blueprint: s, filter: {property: p, op: eq, value: $input.g}}}}\n")
			for _, issue := range issues {
				if issue.Path == test.path && strings.Contains(issue.Message, test.message) {
					return
				}
			}
			t.Fatalf("issues = %+v, want %s: %s", issues, test.path, test.message)
		})
	}
}

func TestParseMeAttributes(t *testing.T) {
	spec := mustParse(t, `
kind: Dashboard
metadata: {name: d, title: "{{ $me.name }}'s services", description: "You're in {{$me.groups}}."}
spec:
  inputs:
    team:
      type: relation
      relation: team
      multiple: true
      default: {query: {blueprint: team, filter: {property: identifier, op: in, values: $me.groups}}}
  queries:
    teams: {blueprint: team, filter: {property: identifier, op: in, values: $input.team}}
  widgets:
    hello: {type: markdown, text: "Hello {{ $me.name }}, a $5 coffee?"}
    mine: {type: stat, title: "Owned by {{ $me.email }}", value: {query: {blueprint: s, filter: {property: owner, op: eq, value: $me.groups}}}}
`)
	me := widget.MeValues(auth.Principal{Email: "ada@example.com", DisplayName: "Ada", Role: auth.RoleBuilder, Groups: []string{"payments", "search"}})
	got := spec.render(me)
	if got.Title != "Ada's services" || got.Description != "You're in payments, search." {
		t.Errorf("rendered = %q, %q", got.Title, got.Description)
	}
	if got.Widgets[0].Text != "Hello Ada, a $5 coffee?" || got.Widgets[1].Title != "Owned by ada@example.com" {
		t.Errorf("widgets = %+v", got.Widgets)
	}
	if spec.Title != "{{ $me.name }}'s services" || spec.Widgets[0].Text != "Hello {{ $me.name }}, a $5 coffee?" {
		t.Errorf("render changed the spec: %+v", spec)
	}

	tests := []struct{ name, source, path, message string }{
		{"title", "metadata: {name: d, title: 'Hi {{ $me.nmae }}'}\nspec:\n  widgets:\n    w: {type: markdown, text: x}", "metadata.title", "unknown variable $me.nmae; known: $me, $me.email, $me.name, $me.role, $me.groups"},
		{"description", "metadata: {name: d, title: D, description: '{{ $entity }}'}\nspec:\n  widgets:\n    w: {type: markdown, text: x}", "metadata.description", "$entity is only available on entity pages"},
		{"widget text", "metadata: {name: d, title: D}\nspec:\n  widgets:\n    w: {type: markdown, text: 'Hi {{ $input.team }}'}", "spec.widgets.w.text", "unknown variable $input.team"},
		{"widget title", "metadata: {name: d, title: D}\nspec:\n  widgets:\n    w: {type: stat, title: 'Hi {{ }}', value: {query: {blueprint: s}}}", "spec.widgets.w.title", "{{ }} must name a variable"},
		{"filter", "metadata: {name: d, title: D}\nspec:\n  widgets:\n    w: {type: stat, title: W, value: {query: {blueprint: s, filter: {property: p, op: eq, value: $me.id}}}}", "spec.widgets.w.value.query.filter", "unknown variable $me.id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := Parse("kind: Dashboard\n" + test.source + "\n")
			for _, issue := range issues {
				if issue.Path == test.path && strings.Contains(issue.Message, test.message) && issue.Line > 0 {
					return
				}
			}
			t.Fatalf("issues = %+v, want %s: %s", issues, test.path, test.message)
		})
	}
}

func TestPicked(t *testing.T) {
	spec := mustParse(t, overviewYAML)
	team, tier := widget.ValueInput+"team", widget.ValueInput+"tier"
	tests := []struct {
		name, query string
		want        widget.Values
		message     string
	}{
		{"nothing picked", "", widget.Values{}, ""},
		{"picked", "input.team=payments&input.tier=standard&input.tier=critical", widget.Values{team: {"payments"}, tier: {"standard", "critical"}}, ""},
		{"cleared is unset, not Nothing", "input.tier=", widget.Values{tier: nil}, ""},
		{"unknown", "input.owner=x", nil, "no input owner"},
		{"off the enum", "input.tier=gold", nil, "must be one of critical, standard"},
		{"several for one", "input.team=a&input.team=b", nil, "takes one value"},
		{"other parameters", "filter.team=a&draft=true", widget.Values{}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, _ := url.ParseQuery(test.query)
			got, err := picked(spec, query)
			var httpErr httpx.Error
			switch {
			case test.message == "" && err != nil:
				t.Fatalf("err = %v", err)
			case test.message == "":
				if !reflect.DeepEqual(got, test.want) {
					t.Fatalf("values = %v, want %v", got, test.want)
				}
			case !errors.As(err, &httpErr) || httpErr.Code != http.StatusBadRequest || !strings.Contains(httpErr.Message, test.message):
				t.Fatalf("err = %v, want a 400 saying %q", err, test.message)
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	spec := mustParse(t, overviewYAML)
	team, tier, mine := widget.ValueInput+"team", widget.ValueInput+"tier", widget.ValueInput+"mine"
	tests := []struct {
		name   string
		found  []string
		picked widget.Values
		want   widget.Values
	}{
		{"found", []string{"payments", "search"}, nil, widget.Values{team: nil, tier: {"critical"}, mine: {"payments", "search"}}},
		{"found nothing is Nothing, not unset", nil, nil, widget.Values{team: nil, tier: {"critical"}, mine: widget.Nothing}},
		{"picked wins", []string{"payments"}, widget.Values{mine: {"search"}, tier: nil}, widget.Values{team: nil, tier: nil, mine: {"search"}}},
		{"cleared stays unset", []string{"payments"}, widget.Values{mine: nil}, widget.Values{team: nil, tier: {"critical"}, mine: nil}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var where *filter.Filter
			find := func(_ context.Context, from string, w *filter.Filter) ([]string, error) {
				if from != "team" {
					t.Fatalf("from = %s", from)
				}
				where = w
				return test.found, nil
			}
			got, problems := defaults(t.Context(), find, spec, ada, test.picked)
			if len(problems) > 0 {
				t.Fatal(problems)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("defaults = %#v, want %#v", got, test.want)
			}
			if _, set := test.picked[mine]; set && where != nil {
				t.Fatal("looked the default up although mine was picked")
			}
			if _, set := test.picked[mine]; !set && (where == nil || where.Value != "ada@example.com") {
				t.Fatalf("where = %+v, want $me bound", where)
			}
		})
	}

	// A lookup that fails leaves the filter at nothing, like one that
	// found none, and says so, rather than failing the whole dashboard.
	fails := func(context.Context, string, *filter.Filter) ([]string, error) { return nil, errors.New("down") }
	got, problems := defaults(t.Context(), fails, spec, ada, nil)
	if want := (widget.Values{team: nil, tier: {"critical"}, mine: widget.Nothing}); !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %#v, want %#v", got, want)
	}
	if err := problems["mine"]; err == nil || err.Error() != "down" || len(problems) != 1 {
		t.Fatalf("problems = %v", problems)
	}
}

func TestDefaultsOtherwise(t *testing.T) {
	spec := mustParse(t, strings.Replace(overviewYAML, "value: $me}", "value: $me}\n        otherwise: everything", 1))
	mine := widget.ValueInput + "mine"
	none := func(context.Context, string, *filter.Filter) ([]string, error) { return nil, nil }
	got, _ := defaults(t.Context(), none, spec, ada, nil)
	if value, set := got[mine]; !set || value != nil {
		t.Fatalf("mine = %#v, want unset, which shows everything", value)
	}
	v, _ := view(t.Context(), none, spec, auth.Principal{Email: "ada@example.com"})
	if _, ok := v.Defaults["mine"]; ok {
		t.Fatalf("defaults = %v, want mine left out, as if it had no default", v.Defaults)
	}

	// A lookup that fails still shows nothing, rather than more than meant.
	fails := func(context.Context, string, *filter.Filter) ([]string, error) { return nil, errors.New("down") }
	got, _ = defaults(t.Context(), fails, spec, ada, nil)
	if value := got[mine]; value == nil || len(value) != 0 {
		t.Fatalf("mine = %#v, want nothing", value)
	}
}

var ada = widget.MeValues(auth.Principal{Email: "ada@example.com", DisplayName: "Ada", Role: auth.RoleMember, Groups: []string{"payments"}})

func TestViewKeepsHomeUpWhenALookupFails(t *testing.T) {
	good := mustParse(t, overviewYAML)
	broken := mustParse(t, strings.NewReplacer("name: overview", "name: broken", "title: Overview", "title: 'Hello {{ $me.name }}'",
		"property: members", "property: people").Replace(overviewYAML))
	team := blueprint.Spec{Identifier: "team"}
	find := func(ctx context.Context, _ string, where *filter.Filter) ([]string, error) {
		if where.Property == "people" {
			// Compiling fails before the store touches the database.
			return entity.NewStore(nil).Identifiers(ctx, nil, team, where, 1)
		}
		return []string{"payments"}, nil
	}
	p := auth.Principal{Email: "ada@example.com", DisplayName: "Ada", Role: auth.RoleMember}

	v, problems := view(t.Context(), find, good, p)
	if len(problems) > 0 || len(v.Problems) > 0 || !reflect.DeepEqual(v.Defaults["mine"], []string{"payments"}) {
		t.Fatalf("good = %+v, problems %v", v, problems)
	}

	v, problems = view(t.Context(), find, broken, p)
	if v.Title != "Hello Ada" || v.Identifier != "broken" {
		t.Errorf("rendered = %+v", v.Spec)
	}
	if !reflect.DeepEqual(v.Defaults["mine"], widget.Nothing) {
		t.Errorf("defaults = %v, want mine at nothing", v.Defaults)
	}
	if problems["mine"] == nil || !strings.HasPrefix(v.Problems["mine"], "the default can't be found: ") {
		t.Errorf("problems = %v, %v", v.Problems, problems)
	}

	down := func(context.Context, string, *filter.Filter) ([]string, error) {
		return nil, errors.New("connection refused")
	}
	v, _ = view(t.Context(), down, broken, p)
	if v.Problems["mine"] != "the default couldn't be looked up" {
		t.Errorf("problems = %v, want the server's problem kept to the log", v.Problems)
	}
}

func TestHomeDashboardJSON(t *testing.T) {
	spec := mustParse(t, overviewYAML)
	encoded, err := json.Marshal(homeDashboard{Spec: spec, Defaults: map[string][]string{"tier": {"critical"}, "mine": widget.Nothing}, Problems: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"identifier":"overview"`, `"defaults":{"mine":[],"tier":["critical"]}`, `"defaultQuery":{"query":{"blueprint":"team","filter":{"property":"members","op":"contains","value":"$me"}}}`} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("home = %s, want %s", encoded, want)
		}
	}
	if strings.Contains(string(encoded), `"problems"`) {
		t.Errorf("home = %s, want no problems when there are none", encoded)
	}
	encoded, _ = json.Marshal(homeDashboard{Spec: spec, Defaults: map[string][]string{}, Problems: map[string]string{"mine": "down"}})
	if !strings.Contains(string(encoded), `"problems":{"mine":"down"}`) {
		t.Errorf("home = %s, want the problem", encoded)
	}
}

func TestCheckInputs(t *testing.T) {
	team, issues := blueprint.Parse(`
kind: Blueprint
metadata: {name: team, title: Team}
spec:
  properties:
    members: {relation: user, many: true}
`)
	if configdoc.HasErrors(issues) {
		t.Fatal(issues)
	}
	blueprints := map[string]blueprint.Spec{"team": team}
	query := func(f *filter.Filter) *DefaultQuery {
		return &DefaultQuery{Query: widget.Query{Blueprint: "team", Filter: f}}
	}
	members := func(property string) *DefaultQuery {
		return query(&filter.Filter{Property: property, Op: "contains", Value: widget.ValueMe})
	}
	tests := []struct {
		name  string
		input Input
		want  string
	}{
		{"members", Input{ID: "f", Type: InputRelation, Relation: "team", Multiple: true, DefaultQuery: members("members")}, ""},
		{"groups", Input{ID: "f", Type: InputRelation, Relation: "team", Multiple: true, DefaultQuery: query(&filter.Filter{Property: "identifier", Op: "in", ValuesFrom: widget.ValueMe + ".groups"})}, ""},
		{"old built-in", Input{ID: "f", Type: InputRelation, Relation: "team", Multiple: true, DefaultQuery: members("$identifier")}, "error spec.inputs.f.default.query.filter"},
		{"no such property", Input{ID: "f", Type: InputRelation, Relation: "team", Multiple: true, DefaultQuery: members("people")}, "error spec.inputs.f.default.query.filter"},
		{"unpublished", Input{ID: "f", Type: InputRelation, Relation: "group", Multiple: true, DefaultQuery: members("members")}, "warning spec.inputs.f.relation"},
		{"enum", Input{ID: "f", Type: InputEnum, Values: []string{"a"}}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := []string{}
			for _, issue := range checkInputs([]Input{test.input}, blueprints) {
				got = append(got, issue.Severity+" "+issue.Path)
			}
			if strings.Join(got, " ") != test.want {
				t.Fatalf("issues = %q, want %q", got, test.want)
			}
		})
	}
}

func issuePaths(issues []configdoc.Issue) string {
	paths := []string{}
	for _, issue := range issues {
		paths = append(paths, issue.Path)
	}

	return strings.Join(paths, " ")
}

func TestCheckWidget(t *testing.T) {
	bp, issues := blueprint.Parse(`
kind: Blueprint
metadata: {name: service, title: Service}
spec:
  properties:
    tier: {type: string, enum: [a, b]}
    coverage: {type: number}
    tags: {type: array}
    owner: {relation: team}
    ownerName: {lookup: owner.title}
    readiness: {type: scorecard, scorecard: readiness}
`)
	if configdoc.HasErrors(issues) {
		t.Fatal(issues)
	}
	blueprints := map[string]blueprint.Spec{"service": bp, "team": {Identifier: "team"}}
	scorecards := map[string]scorecard.Spec{"readiness": {Identifier: "readiness", Blueprint: "service"}}
	services := &widget.Query{Blueprint: "service"}
	bad := &filter.Filter{Property: "nope", Op: "eq", Value: "x"}
	value := func(v widget.Value) *widget.Value { v.Query = *services; return &v }
	tests := []struct {
		name   string
		widget widget.Widget
		want   string
	}{
		{"count", widget.Widget{Type: widget.TypeStat, Value: value(widget.Value{})}, ""},
		{"average", widget.Widget{Type: widget.TypeStat, Value: value(widget.Value{Avg: "coverage"})}, ""},
		{"average score", widget.Widget{Type: widget.TypeStat, Value: value(widget.Value{Avg: "readiness.score"})}, ""},
		{"average of text", widget.Widget{Type: widget.TypeStat, Value: value(widget.Value{Avg: "tier"})}, "error w.value.avg"},
		{"share", widget.Widget{Type: widget.TypeStat, Value: value(widget.Value{Share: &filter.Filter{Property: "tier", Op: "eq", Value: "a"}})}, ""},
		{"share of nothing", widget.Widget{Type: widget.TypeStat, Value: value(widget.Value{Share: bad})}, "error w.value.share"},
		{"jq", widget.Widget{Type: widget.TypeStat, Value: value(widget.Value{JQ: "map(.coverage) | add"})}, ""},
		{"unpublished", widget.Widget{Type: widget.TypeStat, Value: &widget.Value{Query: widget.Query{Blueprint: "nope"}}}, "warning w.value.query.blueprint"},
		{"group by tags", widget.Widget{Type: widget.TypeBar, Query: services, GroupBy: "tags"}, ""},
		{"group by tier", widget.Widget{Type: widget.TypeBar, Query: services, GroupBy: "readiness"}, ""},
		{"group by title", widget.Widget{Type: widget.TypeBar, Query: services, GroupBy: "title"}, "error w.groupBy"},
		{"unknown column", widget.Widget{Type: widget.TypeTable, Query: services, Columns: []widget.Column{{Path: "title"}, {Path: "nope"}, {Title: "Age", JQ: ".x"}}}, "error w.columns"},
		{"path columns", widget.Widget{Type: widget.TypeTable, Query: services, Columns: []widget.Column{{Path: "owner.title"}, {Path: "readiness"}, {Path: "readiness.failed"}, {Path: "readiness.checks.hasOwner"}, {Path: "ownerName"}}}, ""},
		{"scorecard field", widget.Widget{Type: widget.TypeTable, Query: services, Columns: []widget.Column{{Path: "readiness.tier"}}}, "error w.columns"},
		{"sort a list", widget.Widget{Type: widget.TypeTable, Query: services, Sort: "tags"}, "error w.sort"},
		{"sort by tier", widget.Widget{Type: widget.TypeTable, Query: services, Sort: "-readiness"}, ""},
		{"filter with me", widget.Widget{Type: widget.TypeTable, Query: &widget.Query{Blueprint: "service", Filter: &filter.Filter{Property: "owner", Op: "eq", Value: widget.ValueMe}}}, ""},
		{"filter through a relation", widget.Widget{Type: widget.TypeTable, Query: &widget.Query{Blueprint: "service", Filter: &filter.Filter{Property: "owner.title", Op: "eq", Value: "x"}}}, ""},
		{"filter by no tier", widget.Widget{Type: widget.TypeTable, Query: &widget.Query{Blueprint: "service", Filter: &filter.Filter{Property: "readiness", Op: "in", Values: []any{""}}}}, ""},
		{"bad filter", widget.Widget{Type: widget.TypeTable, Query: &widget.Query{Blueprint: "service", Filter: bad}}, "error w.query.filter"},
		{"named query's own problems", widget.Widget{Type: widget.TypeTable, Query: &widget.Query{Name: "q", Blueprint: "service", Filter: bad}}, ""},
		{"link filter", widget.Widget{Type: widget.TypeTable, Query: services, Link: &widget.Link{Query: services, Filter: bad}}, "error w.link.filter"},
		{"link groups", widget.Widget{Type: widget.TypeBar, Query: services, GroupBy: "tier", Link: &widget.Link{Query: &widget.Query{Blueprint: "team"}, Group: true}}, "error w.link.group"},
		{"scorecard of another blueprint", widget.Widget{Type: widget.TypeScorecard, Scorecard: "readiness", Query: &widget.Query{Blueprint: "team"}}, "error w.query"},
		{"scorecard narrowed", widget.Widget{Type: widget.TypeScorecard, Scorecard: "readiness", Query: &widget.Query{Filter: &filter.Filter{Property: "tier", Op: "eq", Value: "a"}}}, ""},
		{"properties", widget.Widget{Type: widget.TypeProperties, Properties: []string{"tier", "nope"}}, "error w.properties"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.widget.Path = "w"
			got := []string{}
			for _, issue := range checkWidgets([]widget.Widget{test.widget}, "service", blueprints, scorecards) {
				got = append(got, issue.Severity+" "+issue.Path)
			}
			if strings.Join(got, ", ") != test.want {
				t.Fatalf("issues = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCheckQueries(t *testing.T) {
	blueprints := map[string]blueprint.Spec{"service": {Identifier: "service", Properties: []blueprint.Property{{ID: "tier", Type: "string"}}}}
	queries := []widget.Query{
		{Name: "ok", Blueprint: "service", Filter: &filter.Filter{Property: "tier", Op: "in", ValuesFrom: widget.ValueInput + "tier"}, Path: "spec.queries.ok"},
		{Name: "bad", Blueprint: "service", Filter: &filter.Filter{Property: "nope", Op: "exists"}, Path: "spec.queries.bad"},
		{Name: "later", Blueprint: "deployment", Path: "spec.queries.later"},
	}
	got := []string{}
	for _, issue := range checkQueries(queries, blueprints) {
		got = append(got, issue.Severity+" "+issue.Path)
	}
	if want := "error spec.queries.bad.filter, warning spec.queries.later.blueprint"; strings.Join(got, ", ") != want {
		t.Fatalf("issues = %q, want %q", got, want)
	}
}

func TestLink(t *testing.T) {
	spec := mustParse(t, overviewYAML)
	l := loader{
		blueprints: map[string]blueprint.Spec{"service": {Identifier: "service"}},
		env:        widget.Env{Values: widget.Values{widget.ValueInput + "team": nil, widget.ValueInput + "mine": {"payments", "search"}, widget.ValueInput + "tier": {"critical"}}},
	}
	encode := func(link *linkData) string {
		encoded, _ := json.Marshal(link)
		return string(encoded)
	}
	// The unset team drops, and the link's own filter joins the query's.
	want := `{"blueprint":"service","filter":{"and":[{"property":"owner","op":"in","values":["payments","search"]},{"property":"tier","op":"in","values":["critical"]},{"property":"tier","op":"eq","value":"critical"}]}}`
	if got := encode(l.link(spec.Widgets[0], nil)); got != want {
		t.Errorf("stat link = %s\nwant %s", got, want)
	}
	want = `{"blueprint":"service","filter":{"and":[{"property":"owner","op":"in","values":["payments","search"]},{"property":"tier","op":"in","values":["critical"]},{"property":"tier","op":"eq","value":"standard"}]}}`
	if got := encode(l.link(spec.Widgets[1], &filter.Filter{Property: "tier", Op: "eq", Value: "standard"})); got != want {
		t.Errorf("group link = %s\nwant %s", got, want)
	}
	// A group's own value stands in for what the query said of its property.
	want = `{"blueprint":"service","filter":{"and":[{"property":"owner","op":"in","values":["payments","search"]},{"property":"tier","op":"eq","value":"standard"}]}}`
	if got := encode(l.groupLink(spec.Widgets[1], true, &filter.Filter{Property: "tier", Op: "eq", Value: "standard"})); got != want {
		t.Errorf("group link = %s\nwant %s", got, want)
	}
	if l.link(spec.Widgets[2], nil) != nil {
		t.Error("a widget without a link isn't one")
	}
	unpublished := loader{blueprints: map[string]blueprint.Spec{}}
	if unpublished.link(spec.Widgets[0], nil) != nil {
		t.Error("linked to an unpublished blueprint")
	}
}

func TestCheckHref(t *testing.T) {
	for href, path := range map[string]bool{"/catalog/service/a": true, "https://status.example.com/?team=a,b": false} {
		if err := checkHref(href, path); err != nil {
			t.Errorf("%s: %v", href, err)
		}
	}
	for href, path := range map[string]bool{"//evil.example.com": true, "/\\evil": true, "javascript:alert(1)": false, "https:///x": false} {
		if checkHref(href, path) == nil {
			t.Errorf("accepted %s", href)
		}
	}
}

func TestDiff(t *testing.T) {
	previous := mustParse(t, overviewYAML)
	next := mustParse(t, strings.NewReplacer("order: 2", "order: 3", "      type: donut", "      type: bar",
		"default: [critical]", "default: [standard]", "op: contains, value: $me", "op: eq, value: $me").Replace(overviewYAML))
	next.Widgets = next.Widgets[:2]
	next.Inputs = append(next.Inputs[1:], Input{ID: "env", Type: InputString})

	got := []string{}
	changes := Diff(previous, next)
	for _, c := range changes {
		got = append(got, c.Section+"/"+c.ID+"/"+c.Change)
	}
	want := "dashboard//changed inputs/tier/changed inputs/mine/changed inputs/env/added inputs/team/removed widgets/byTier/changed widgets/note/removed"
	if strings.Join(got, " ") != want {
		t.Fatalf("Diff() = %v, want %s", got, want)
	}
	if configdoc.Breaking(changes) {
		t.Fatal("Diff() reports a breaking change")
	}
}

func TestEntityVariableTypes(t *testing.T) {
	own, issues := blueprint.Parse(`kind: Blueprint
metadata: {name: service, title: Service}
spec:
  properties:
    coverage: {type: number}
    critical: {type: boolean}
    owners: {relation: team, many: true}
  entity:
    tabs:
      overview:
        widgets:
          coverage: {type: stat, title: Coverage, value: {query: {blueprint: other, filter: {property: coverage, op: gte, value: $entity.coverage}}}}
          critical: {type: stat, title: Critical, value: {query: {blueprint: other, filter: {property: critical, op: eq, value: $entity.critical}}}}
          owners: {type: stat, title: Owners, value: {query: {blueprint: other, filter: {property: owner, op: eq, value: $entity.owners}}}}
`)
	if configdoc.HasErrors(issues) {
		t.Fatal(issues)
	}
	target := blueprint.Spec{Identifier: "other", Properties: []blueprint.Property{
		{ID: "coverage", Type: "number"}, {ID: "critical", Type: "boolean"}, {ID: "owner", Type: "relation", Relation: "team"},
	}}
	for _, w := range own.Entity.Tabs[0].Widgets {
		f := widget.BindData(w.Value.Query.Filter, sampleEntity(own, nil))
		if err := checkFilter(nil, target, f); err != nil {
			t.Errorf("%s: %v", w.ID, err)
		}
	}
}
