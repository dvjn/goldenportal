package scorecard

import (
	"context"
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

const readinessYAML = `
kind: Scorecard
metadata:
  name: readiness
  title: Readiness
spec:
  blueprint: service
  filter: {property: tier, op: ne, value: experimental}
  tiers:
    - name: bronze
      title: Bronze
      color: amber
      criteria:
        hasOwner:
          title: Has an owner
          check: .properties.owner != null
          fix: Set the owner.
    - name: silver
      criteria:
        hasRepository:
          check: .properties.repository != null
        pagesOnCall:
          when: .properties.tier == "critical"
          check: .properties.oncall != null
    - name: gold
      criteria:
        manyStars:
          check: (.properties.stars | tonumber) > 100
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
	spec := mustParse(t, readinessYAML)
	if spec.Blueprint != "service" || len(spec.Tiers) != 3 || spec.Tiers[1].Title != "silver" || spec.Filter == nil {
		t.Fatalf("spec = %+v", spec)
	}
	if got := spec.Stats(); got["criteria"] != 4 {
		t.Errorf("stats = %v", got)
	}

	head := "kind: Scorecard\nmetadata: {name: s, title: S}\nspec:\n  blueprint: service\n"
	tests := []struct{ name, source, want string }{
		{"no tiers", head, "add the tiers"},
		{"empty tier", head + "  tiers:\n    - name: a\n", "add the criteria"},
		{"no check", head + "  tiers:\n    - name: a\n      criteria:\n        c: {title: C}\n", "add a jq check"},
		{"bad jq", head + "  tiers:\n    - name: a\n      criteria:\n        c: {check: '.a |'}\n", "not valid jq"},
		{"repeated criterion", head + "  tiers:\n    - name: a\n      criteria:\n        c: {check: 'true'}\n    - name: b\n      criteria:\n        c: {check: 'true'}\n", "named across tiers"},
		{"repeated tier", head + "  tiers:\n    - name: a\n      criteria:\n        c: {check: 'true'}\n    - name: a\n      criteria:\n        d: {check: 'true'}\n", "already named \"a\""},
		{"bad color", head + "  tiers:\n    - name: a\n      color: gold\n      criteria:\n        c: {check: 'true'}\n", "color must be one of"},
		{"bad filter", head + "  filter: [1]\n  tiers:\n    - name: a\n      criteria:\n        c: {check: 'true'}\n", "filter is not valid JSON"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := Parse(test.source)
			var messages []string
			for _, issue := range issues {
				messages = append(messages, issue.Message)
			}
			if !strings.Contains(strings.Join(messages, "\n"), test.want) {
				t.Fatalf("issues = %v, want %q", messages, test.want)
			}
		})
	}
}

func TestEvaluate(t *testing.T) {
	spec := mustParse(t, readinessYAML)
	checks, err := compileSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	evaluate := func(properties map[string]any) Result {
		return checks.evaluate(context.Background(), spec, map[string]any{"properties": properties})
	}

	tests := []struct {
		name       string
		properties map[string]any
		tier       string
		score      int
		status     map[string]string
	}{
		{
			"nothing",
			map[string]any{},
			"", 0,
			map[string]string{"hasOwner": StatusFailed, "pagesOnCall": StatusSkipped},
		},
		{"bronze only", map[string]any{"owner": "payments"}, "bronze", 33, nil},
		{
			"silver, on-call skipped",
			map[string]any{"owner": "a", "repository": "r", "stars": 5.0},
			"silver", 67,
			map[string]string{"manyStars": StatusFailed},
		},
		{
			"silver needs on-call when critical",
			map[string]any{"owner": "a", "repository": "r", "tier": "critical", "stars": 500.0},
			"bronze", 75,
			map[string]string{"pagesOnCall": StatusFailed},
		},
		{"gold", map[string]any{"owner": "a", "repository": "r", "stars": 500.0}, "gold", 100, nil},
		{"a failed lower tier caps the tier", map[string]any{"repository": "r", "stars": 500.0}, "", 67, nil},
		{
			"an error blocks its tier",
			map[string]any{"owner": "a", "repository": "r", "stars": "many"},
			"silver", 67,
			map[string]string{"manyStars": StatusErrored},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := evaluate(test.properties)
			if r.Tier != test.tier {
				t.Errorf("tier = %q, want %q (%+v)", r.Tier, test.tier, r.Criteria)
			}
			if r.Score == nil || *r.Score != test.score {
				t.Errorf("score = %v, want %d", r.Score, test.score)
			}
			for id, status := range test.status {
				if got := r.Criteria[id].Status; got != status {
					t.Errorf("%s = %s, want %s", id, got, status)
				}
			}
		})
	}
}

func TestChecksMustBeBoolean(t *testing.T) {
	spec := mustParse(t, strings.Replace(readinessYAML, "check: .properties.owner != null", "check: .properties.owner", 1))
	checks, _ := compileSpec(spec)
	r := checks.evaluate(context.Background(), spec, map[string]any{"properties": map[string]any{"owner": "a"}})
	if got := r.Criteria["hasOwner"]; got.Status != StatusErrored || !strings.Contains(got.Message, "true or false") {
		t.Fatalf("hasOwner = %+v", got)
	}
}

func TestDiff(t *testing.T) {
	before := mustParse(t, readinessYAML)
	if changes := Diff(before, before); len(changes) != 0 {
		t.Fatalf("no change: %+v", changes)
	}
	after := mustParse(t, strings.Replace(readinessYAML, "> 100", "> 10", 1))
	changes := Diff(before, after)
	if len(changes) != 1 || changes[0].ID != "manyStars" || changes[0].Breaking {
		t.Fatalf("changes = %+v", changes)
	}
}
