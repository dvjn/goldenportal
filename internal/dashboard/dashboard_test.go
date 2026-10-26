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
  filters:
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
        from: team
        where: {property: members, op: contains, value: $me}
  widgets:
    count:
      type: stat
      title: Services
      blueprint: service
      filter:
        and:
          - {property: owner, op: eq, value: $filter.team}
          - {property: owner, op: eq, value: $filter.mine}
    byTier:
      type: donut
      title: By tier
      blueprint: service
      groupBy: tier
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
	team, tier := spec.Filters[0], spec.Filters[1]
	if team.Title != "Team" || team.Type != FilterRelation || team.Relation != "team" || team.Default != nil {
		t.Errorf("team = %+v", team)
	}
	if tier.Title != "tier" || !tier.Multiple || strings.Join(tier.Values, ",") != "critical,standard" || strings.Join(tier.Default, ",") != "critical" {
		t.Errorf("tier = %+v", tier)
	}
	mine := spec.Filters[2]
	if mine.Default != nil || mine.Lookup == nil || mine.Lookup.From != "team" || mine.Lookup.Where == nil || mine.Lookup.Where.Value != widget.ValueMe {
		t.Errorf("mine = %+v", mine)
	}

	for source, path := range map[string]string{
		"kind: Dashboard\nmetadata: {name: d, title: D}\nspec: {order: 1}":                                               "spec.widgets",
		"kind: Dashboard\nmetadata: {name: d, title: D}\nspec:\n  page: home\n  widgets: {w: {type: markdown, text: x}}": "spec.page",
		"kind: Dashboard\nmetadata: {name: d, title: D}\nspec:\n  widgets:\n    w: {type: properties, title: W}":         "spec.widgets.w.type",
	} {
		_, issues := Parse(source)
		if !strings.Contains(issuePaths(issues), path) {
			t.Errorf("Parse(%q) issues = %+v, want one on %s", source, issues, path)
		}
	}
}

func TestParseFilterProblems(t *testing.T) {
	tests := []struct {
		name, filters, path, message string
	}{
		{"type", "f: {title: F}", "spec.filters.f.type", "type must be one of"},
		{"name", "1f: {type: string}", "spec.filters.1f", "must start with a letter"},
		{"reserved", "d: {type: string}", "spec.filters.d", "can't be named d"},
		{"enum values", "f: {type: enum}", "spec.filters.f.values", "list the values"},
		{"enum twice", "f: {type: enum, values: [a, a]}", "spec.filters.f.values", "twice"},
		{"enum relation", "f: {type: enum, values: [a], relation: team}", "spec.filters.f.relation", "only relation filters"},
		{"relation", "f: {type: relation}", "spec.filters.f.relation", "name the blueprint"},
		{"relation values", "f: {type: relation, relation: team, values: [a]}", "spec.filters.f.values", "only enum filters"},
		{"string values", "f: {type: string, values: [a]}", "spec.filters.f.values", "any text"},
		{"default off the enum", "f: {type: enum, values: [a], default: b}", "spec.filters.f.default", "not one of the values"},
		{"several defaults", "f: {type: enum, values: [a, b], default: [a, b]}", "spec.filters.f.default", "one value"},
		{"empty default", "f: {type: string, default: ''}", "spec.filters.f.default", "can't be empty"},
		{"lookup on enum", "f: {type: enum, values: [a], default: {from: team, where: {property: p, op: eq, value: x}}}", "spec.filters.f.default", "multiple: true"},
		{"lookup on one relation", "f: {type: relation, relation: team, default: {from: team, where: {property: p, op: eq, value: x}}}", "spec.filters.f.default", "multiple: true"},
		{"lookup without from", "f: {type: relation, relation: team, multiple: true, default: {where: {property: p, op: eq, value: x}}}", "spec.filters.f.default.from", "name the blueprint"},
		{"lookup from elsewhere", "f: {type: relation, relation: team, multiple: true, default: {from: group, where: {property: p, op: eq, value: x}}}", "spec.filters.f.default.from", "must be team"},
		{"lookup without where", "f: {type: relation, relation: team, multiple: true, default: {from: team}}", "spec.filters.f.default.where", "add where"},
		{"lookup where is no filter", "f: {type: relation, relation: team, multiple: true, default: {from: team, where: text}}", "spec.filters.f.default.where", "not valid JSON"},
		{"lookup where uses a filter", "f: {type: relation, relation: team, multiple: true, default: {from: team, where: {property: p, op: eq, value: $filter.f}}}", "spec.filters.f.default.where", "not with $entity or the dashboard's filters"},
		{"lookup where uses the entity", "f: {type: relation, relation: team, multiple: true, default: {from: team, where: {property: p, op: eq, value: $entity}}}", "spec.filters.f.default.where", "not with $entity or the dashboard's filters"},
		{"lookup where misspells me", "f: {type: relation, relation: team, multiple: true, default: {from: team, where: {property: p, op: eq, value: $me.grops}}}", "spec.filters.f.default.where", "unknown variable $me.grops; known: $me, $me.email, $me.name, $me.role, $me.groups"},
		{"undeclared", "f: {type: string}", "spec.widgets.w.filter", `no filter "g"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := Parse("kind: Dashboard\nmetadata: {name: d, title: D}\nspec:\n  filters:\n    " + test.filters +
				"\n  widgets:\n    w: {type: stat, title: W, blueprint: s, filter: {property: p, op: eq, value: $filter.g}}\n")
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
  filters:
    team:
      type: relation
      relation: team
      multiple: true
      default: {from: team, where: {property: $identifier, op: in, values: [$me.groups]}}
  widgets:
    hello: {type: markdown, text: "Hello {{ $me.name }}, a $5 coffee?"}
    mine: {type: stat, title: "Owned by {{ $me.email }}", blueprint: s, filter: {property: owner, op: eq, value: $me.groups}}
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
		{"widget text", "metadata: {name: d, title: D}\nspec:\n  widgets:\n    w: {type: markdown, text: 'Hi {{ $filter.team }}'}", "spec.widgets.w.text", "unknown variable $filter.team"},
		{"widget title", "metadata: {name: d, title: D}\nspec:\n  widgets:\n    w: {type: stat, title: 'Hi {{ }}', blueprint: s}", "spec.widgets.w.title", "{{ }} must name a variable"},
		{"filter", "metadata: {name: d, title: D}\nspec:\n  widgets:\n    w: {type: stat, title: W, blueprint: s, filter: {property: p, op: eq, value: $me.id}}", "spec.widgets.w.filter", "unknown variable $me.id"},
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
	team, tier := widget.ValueFilter+"team", widget.ValueFilter+"tier"
	tests := []struct {
		name, query string
		want        widget.Values
		message     string
	}{
		{"nothing picked", "", widget.Values{}, ""},
		{"picked", "filter.team=payments&filter.tier=standard&filter.tier=critical", widget.Values{team: {"payments"}, tier: {"standard", "critical"}}, ""},
		{"cleared is unset, not Nothing", "filter.tier=", widget.Values{tier: nil}, ""},
		{"unknown", "filter.owner=x", nil, "no filter owner"},
		{"off the enum", "filter.tier=gold", nil, "must be one of critical, standard"},
		{"several for one", "filter.team=a&filter.team=b", nil, "takes one value"},
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
	team, tier, mine := widget.ValueFilter+"team", widget.ValueFilter+"tier", widget.ValueFilter+"mine"
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

var ada = widget.MeValues(auth.Principal{Email: "ada@example.com", DisplayName: "Ada", Role: auth.RoleMember, Groups: []string{"payments"}})

func TestViewKeepsHomeUpWhenALookupFails(t *testing.T) {
	good := mustParse(t, overviewYAML)
	broken := mustParse(t, strings.NewReplacer("name: overview", "name: broken", "title: Overview", "title: 'Hello {{ $me.name }}'",
		"property: members", "property: people").Replace(overviewYAML))
	team := blueprint.Spec{Identifier: "team"}
	find := func(ctx context.Context, _ string, where *filter.Filter) ([]string, error) {
		if where.Property == "people" {
			// Compiling fails before the store touches the database.
			return entity.NewStore(nil).Identifiers(ctx, team, where, 1)
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
	for _, want := range []string{`"identifier":"overview"`, `"defaults":{"mine":[],"tier":["critical"]}`, `"lookup":{"from":"team","where":{"property":"members","op":"contains","value":"$me"}}`} {
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

func TestCheckFilters(t *testing.T) {
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
	lookup := func(property string) *Lookup {
		return &Lookup{From: "team", Where: &filter.Filter{Property: property, Op: "contains", Value: widget.ValueMe}}
	}
	tests := []struct {
		name   string
		filter Filter
		want   string
	}{
		{"members", Filter{ID: "f", Type: FilterRelation, Relation: "team", Multiple: true, Lookup: lookup("members")}, ""},
		{"groups", Filter{ID: "f", Type: FilterRelation, Relation: "team", Multiple: true, Lookup: &Lookup{From: "team", Where: &filter.Filter{Property: "$identifier", Op: "in", Values: []any{widget.ValueMe + ".groups"}}}}, ""},
		{"no such property", Filter{ID: "f", Type: FilterRelation, Relation: "team", Multiple: true, Lookup: lookup("people")}, "error spec.filters.f.default.where"},
		{"unpublished", Filter{ID: "f", Type: FilterRelation, Relation: "group", Multiple: true, Lookup: lookup("members")}, "warning spec.filters.f.relation"},
		{"enum", Filter{ID: "f", Type: FilterEnum, Values: []string{"a"}}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := []string{}
			for _, issue := range checkFilters([]Filter{test.filter}, blueprints) {
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
    ownerName: {lookup: owner.$title}
`)
	if configdoc.HasErrors(issues) {
		t.Fatal(issues)
	}
	tests := []struct {
		name   string
		widget widget.Widget
		field  string
	}{
		{"count", widget.Widget{Type: widget.TypeStat, Aggregate: "count"}, ""},
		{"average", widget.Widget{Type: widget.TypeStat, Aggregate: "avg", Property: "coverage"}, ""},
		{"average of text", widget.Widget{Type: widget.TypeStat, Aggregate: "avg", Property: "tier"}, "aggregate"},
		{"group by tags", widget.Widget{Type: widget.TypeBar, GroupBy: "tags"}, ""},
		{"group by lookup", widget.Widget{Type: widget.TypeBar, GroupBy: "ownerName"}, "groupBy"},
		{"unknown column", widget.Widget{Type: widget.TypeTable, Columns: []string{"$title", "nope"}}, "columns"},
		{"sort a list", widget.Widget{Type: widget.TypeTable, Sort: "tags"}, "sort"},
		{"filter with me", widget.Widget{Type: widget.TypeTable, Filter: &filter.Filter{Property: "owner", Op: "eq", Value: widget.ValueMe}}, ""},
		{"bad filter", widget.Widget{Type: widget.TypeTable, Filter: &filter.Filter{Property: "nope", Op: "eq", Value: "x"}}, "filter"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			problems := checkWidget(bp, test.widget)
			switch {
			case test.field == "" && len(problems) > 0:
				t.Fatalf("problems = %+v", problems)
			case test.field != "" && (len(problems) == 0 || problems[0].field != test.field):
				t.Fatalf("problems = %+v, want one on %s", problems, test.field)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	previous := mustParse(t, overviewYAML)
	next := mustParse(t, strings.NewReplacer("order: 2", "order: 3", "      type: donut", "      type: bar",
		"default: [critical]", "default: [standard]", "op: contains, value: $me", "op: eq, value: $me").Replace(overviewYAML))
	next.Widgets = next.Widgets[:2]
	next.Filters = append(next.Filters[1:], Filter{ID: "env", Type: FilterString})

	got := []string{}
	changes := Diff(previous, next)
	for _, c := range changes {
		got = append(got, c.Section+"/"+c.ID+"/"+c.Change)
	}
	want := "dashboard//changed filters/tier/changed filters/mine/changed filters/env/added filters/team/removed widgets/byTier/changed widgets/note/removed"
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
          coverage: {type: stat, title: Coverage, blueprint: other, filter: {property: coverage, op: gte, value: $entity.coverage}}
          critical: {type: stat, title: Critical, blueprint: other, filter: {property: critical, op: eq, value: $entity.critical}}
          owners: {type: stat, title: Owners, blueprint: other, filter: {property: owner, op: eq, value: $entity.owners}}
`)
	if configdoc.HasErrors(issues) {
		t.Fatal(issues)
	}
	target := blueprint.Spec{Identifier: "other", Properties: []blueprint.Property{
		{ID: "coverage", Type: "number"}, {ID: "critical", Type: "boolean"}, {ID: "owner", Type: "relation", Relation: "team"},
	}}
	for _, w := range own.Entity.Tabs[0].Widgets {
		w.Filter = widget.BindData(w.Filter, sampleEntity(own, nil))
		if err := checkFilter(target, w.Filter); err != nil {
			t.Errorf("%s: %v", w.ID, err)
		}
	}
}

func TestScorecardColumns(t *testing.T) {
	bp := blueprint.Spec{Identifier: "service"}
	w := widget.Widget{ID: "services", Type: widget.TypeTable, Blueprint: "service", Columns: []string{"$scorecard.readiness"}}
	specs := map[string]blueprint.Spec{"service": bp}
	for _, test := range []struct{ target, severity string }{{"service", ""}, {"deployment", configdoc.SeverityError}, {"", configdoc.SeverityWarning}} {
		cards := map[string]scorecard.Spec{}
		if test.target != "" {
			cards["readiness"] = scorecard.Spec{Identifier: "readiness", Blueprint: test.target}
		}
		issues := checkWidgets([]widget.Widget{w}, specs, cards)
		if test.severity == "" && len(issues) != 0 {
			t.Fatal(issues)
		}
		if test.severity != "" && (len(issues) == 0 || issues[0].Severity != test.severity) {
			t.Fatalf("%s: %+v", test.target, issues)
		}
	}
}
