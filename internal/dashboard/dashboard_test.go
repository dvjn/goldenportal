package dashboard

import (
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/widget"
)

const overviewYAML = `
kind: Dashboard
metadata:
  name: overview
  title: Overview
spec:
  order: 2
  widgets:
    count:
      type: stat
      title: Services
      blueprint: service
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
	next := mustParse(t, strings.Replace(strings.Replace(overviewYAML, "order: 2", "order: 3", 1),
		"      type: donut", "      type: bar", 1))
	next.Widgets = next.Widgets[:2]

	got := []string{}
	for _, c := range Diff(previous, next) {
		got = append(got, c.Section+"/"+c.ID+"/"+c.Change)
	}
	if want := "dashboard//changed widgets/byTier/changed widgets/note/removed"; strings.Join(got, " ") != want {
		t.Fatalf("Diff() = %v, want %s", got, want)
	}
}
