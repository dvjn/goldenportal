package blueprint

import (
	"strings"
	"testing"
	"time"
)

const deploymentYAML = `kind: Blueprint
metadata: {identifier: deployment, title: Deployment}
spec:
  properties:
    environment: {type: string}
    startedAt: {type: string, format: date-time}
    finishedAt: {type: string, format: date-time}
    tags: {type: array}
    repo: {relation: repository}
    production: {type: boolean, compute: '.environment == "production"'}
    team: {lookup: repo.owner}
`

func TestRetention(t *testing.T) {
	forever, issues := Parse(deploymentYAML)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	if forever.Keep() != 0 || forever.Longest() != 0 || forever.RetainedBy() != "" {
		t.Errorf("a blueprint without retention keeps %s by %q, want for ever", forever.Keep(), forever.RetainedBy())
	}

	spec, issues := Parse(deploymentYAML + `  retention:
    by: startedAt
    keep: 2w
    rules:
      - filter: {property: production, op: eq, value: true}
        keep: 365d
      - filter: {or: [{property: repo, op: eq, value: acme/api}, {property: tags, op: in, values: [keep]}]}
        keep: 52w
`)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	day := 24 * time.Hour
	if spec.RetainedBy() != "startedAt" || spec.Keep() != 14*day || spec.Longest() != 365*day {
		t.Errorf("by = %s, keep = %s, longest = %s", spec.RetainedBy(), spec.Keep(), spec.Longest())
	}
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	if got := spec.RetainedAt(Entity{Properties: map[string]any{"startedAt": "2026-11-01T10:00:00Z"}}); !got.Equal(at) {
		t.Errorf("retained at %s, want %s", got, at)
	}
	if got := spec.RetainedAt(Entity{Properties: map[string]any{}}); !got.IsZero() {
		t.Errorf("a row without startedAt is retained at %s, want never", got)
	}
	for _, test := range []struct {
		name       string
		properties map[string]any
		relations  map[string]any
		computed   map[string]any
		want       time.Duration
	}{
		{"none matches", map[string]any{"environment": "review/x"}, nil, map[string]any{"production": false}, 14 * day},
		{"computed", nil, nil, map[string]any{"production": true}, 365 * day},
		{"relation", nil, map[string]any{"repo": "acme/api"}, nil, 52 * 7 * day},
		{"list holds it", map[string]any{"tags": []any{"x", "keep"}}, nil, nil, 52 * 7 * day},
		{"longest of those it matches", map[string]any{"tags": []any{"keep"}}, nil, map[string]any{"production": true}, 365 * day},
	} {
		e := Entity{Properties: test.properties, Relations: test.relations}
		if got := spec.KeepFor(e, test.computed); got != test.want {
			t.Errorf("%s: kept %s, want %s", test.name, got, test.want)
		}
	}

	if changes := Diff(forever, spec); len(changes) != 1 || !changes[0].Breaking || !strings.Contains(changes[0].Detail, "keeps rows 2w by startedAt") {
		t.Errorf("changes = %+v, want adding retention to be breaking", changes)
	}
	if changes := Diff(spec, forever); len(changes) != 1 || changes[0].Breaking {
		t.Errorf("changes = %+v, want dropping retention not to be breaking", changes)
	}
	shorter, _ := Parse(deploymentYAML + "  retention: {by: startedAt, keep: 7d}\n")
	if changes := Diff(spec, shorter); len(changes) != 1 || !changes[0].Breaking || !strings.Contains(changes[0].Detail, "keeps rows 7d instead of 2w") {
		t.Errorf("changes = %+v, want a shorter keep to be breaking", changes)
	}
	longer, _ := Parse(deploymentYAML + "  retention: {by: startedAt, keep: 60d}\n")
	if changes := Diff(shorter, longer); len(changes) != 1 || changes[0].Breaking {
		t.Errorf("changes = %+v, want a longer keep not to be breaking", changes)
	}
	other, _ := Parse(deploymentYAML + "  retention: {by: finishedAt, keep: 60d}\n")
	if changes := Diff(longer, other); len(changes) != 1 || !changes[0].Breaking || !strings.Contains(changes[0].Detail, "by finishedAt instead of startedAt") {
		t.Errorf("changes = %+v, want another by to be breaking", changes)
	}
	if !SameRetention(forever, forever) || SameRetention(forever, longer) || SameRetention(longer, other) {
		t.Error("SameRetention is wrong")
	}
}

func TestRetentionRefused(t *testing.T) {
	for _, test := range []struct{ name, retention, want string }{
		{"no by", "{keep: 30d}", "add by, the date-time property rows are kept by"},
		{"no keep", "{by: startedAt}", "add how long rows are kept"},
		{"unknown by", "{by: endedAt, keep: 30d}", `deployment has no property "endedAt"`},
		{"by not a time", "{by: environment, keep: 30d}", "rows are kept by a stored date-time property, and environment isn't one"},
		{"by computed", "{by: production, keep: 30d}", "production isn't one"},
		{"short keep", "{by: startedAt, keep: 12h}", "keep must be a duration of at least 1d"},
		{"keep in days", "{by: startedAt, keep: 30}", "keep must be a duration of at least 1d"},
		{"rule keeps less", "{by: startedAt, keep: 30d, rules: [{filter: {property: environment, op: eq, value: x}, keep: 7d}]}", "keep must be at least 30d"},
		{"rule without keep", "{by: startedAt, keep: 30d, rules: [{filter: {property: environment, op: eq, value: x}}]}", "add how long the rows it matches are kept"},
		{"lookup", "{by: startedAt, keep: 30d, rules: [{filter: {property: team, op: eq, value: x}, keep: 90d}]}", "team changes without its row being written"},
		{"relation hop", "{by: startedAt, keep: 30d, rules: [{filter: {property: repo.owner, op: eq, value: x}, keep: 90d}]}", "can't follow a relation"},
		{"built-in", "{by: startedAt, keep: 30d, rules: [{filter: {property: identifier, op: eq, value: x}, keep: 90d}]}", "not the built-in identifier"},
		{"contains", "{by: startedAt, keep: 30d, rules: [{filter: {property: environment, op: contains, value: x}, keep: 90d}]}", "compares with eq, ne, in, exists"},
		{"variable", "{by: startedAt, keep: 30d, rules: [{filter: {property: environment, op: in, values: $input.env}, keep: 90d}]}", "values written in it"},
		{"unknown", "{by: startedAt, keep: 30d, rules: [{filter: {property: nope, op: exists}, keep: 90d}]}", `has no property "nope"`},
	} {
		_, issues := Parse(deploymentYAML + "  retention: " + test.retention + "\n")
		if len(issues) == 0 || !strings.Contains(issues[0].Message, test.want) {
			t.Errorf("%s: issues = %v, want %q", test.name, issues, test.want)
		}
	}
}

func TestKeepsLonger(t *testing.T) {
	parse := func(retention string) Spec {
		spec, issues := Parse(deploymentYAML + retention)
		if len(issues) > 0 {
			t.Fatal(issues)
		}
		return spec
	}
	forever := parse("")
	month := parse("  retention: {by: startedAt, keep: 30d}\n")
	week := parse("  retention: {by: startedAt, keep: 7d}\n")
	ruled := parse("  retention: {by: startedAt, keep: 30d, rules: [{filter: {property: environment, op: eq, value: production}, keep: 365d}]}\n")
	finished := parse("  retention: {by: finishedAt, keep: 30d}\n")
	for _, test := range []struct {
		name           string
		previous, next Spec
		want           bool
	}{
		{"a longer keep", week, month, true},
		{"a shorter keep", month, week, false},
		{"a rule that keeps longer", month, ruled, true},
		{"dropping a rule", ruled, month, false},
		{"another by", month, finished, true},
		{"dropping retention", month, forever, true},
		{"adding retention", forever, month, false},
		{"the same", month, month, false},
	} {
		if got := KeepsLonger(test.previous, test.next); got != test.want {
			t.Errorf("%s: KeepsLonger = %v, want %v", test.name, got, test.want)
		}
	}
}
