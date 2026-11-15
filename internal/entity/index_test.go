package entity

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// indexFixture is service, related to team and deployed by deployment,
// whose retention keeps rows by finishedAt.
func indexFixture(t *testing.T) (map[string]blueprint.Spec, blueprint.Spec, blueprint.Spec) {
	t.Helper()
	team := blueprint.Spec{ID: teamID, Identifier: "team", Properties: []blueprint.Property{
		{ID: "slackChannel", Type: "string"}, {ID: "size", Type: "integer"},
	}}
	svc := service(t)
	svc.ID = serviceID
	deployment, issues := blueprint.Parse(`kind: Blueprint
metadata: {identifier: deployment, title: Deployment}
spec:
  properties:
    service: {relation: service}
    seconds: {type: number}
    finishedAt: {type: string, format: date-time}
  retention: {by: finishedAt, keep: 30d}
`)
	if blueprint.HasErrors(issues) {
		t.Fatalf("issues = %+v", issues)
	}
	deployment.ID = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"
	specs := map[string]blueprint.Spec{"service": svc, "team": team, "deployment": deployment}

	return specs, svc, deployment
}

// TestIndexKeysAreTheCompilersExpressions holds the keys to what the
// compiler writes, as the planner only uses an index on an expression a
// statement has word for word.
func TestIndexKeysAreTheCompilersExpressions(t *testing.T) {
	specs, svc, _ := indexFixture(t)
	unaliased := func(sql string) string { return alias.ReplaceAllString(sql, "") }

	for _, sort := range []string{"-lastDeployedAt", "lastDeployedAt", "-stars", "stars", "tier", "-owner", "-createdAt"} {
		key, ok := SortKey(specs, svc, sort)
		if !ok {
			t.Fatalf("SortKey(%s) = none", sort)
		}
		order, err := (&compiler{spec: svc, specs: specs}).orderBy(sort)
		if err != nil {
			t.Fatal(err)
		}
		direction := " ASC NULLS FIRST"
		if key.Descending {
			direction = " DESC NULLS LAST"
		}
		if want := key.Expression + direction + ", title, identifier"; unaliased(order) != want {
			t.Errorf("%s: order %q, but the key is %q", sort, unaliased(order), key.Expression)
		}
		if key.Use != UseSort || key.Blueprint != "service" || key.Descending != strings.HasPrefix(sort, "-") {
			t.Errorf("%s: key = %+v", sort, key)
		}
	}

	conditions := []struct{ filter, sql string }{
		{`{"property":"lastDeployedAt","op":"gte","value":"2026-10-01T00:00:00Z"}`, "(%[1]s >= $1::timestamptz)"},
		{`{"property":"lastDeployedAt","op":"within","value":"7d"}`, "(%[1]s >= now() - $1::interval)"},
		{`{"property":"stars","op":"lt","value":3}`, "(%[1]s < $1)"},
		{`{"property":"createdAt","op":"gt","value":"2026-10-01T00:00:00Z"}`, "(%[1]s > $1::timestamptz)"},
	}
	for _, c := range conditions {
		f, err := ParseFilter(c.filter)
		if err != nil {
			t.Fatal(err)
		}
		keys := FilterKeys(specs, svc, f)
		if len(keys) != 1 {
			t.Fatalf("%s: keys = %+v", c.filter, keys)
		}
		sql, _, err := Where(Schema{Blueprint: svc, Blueprints: specs}, *f, nil)
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf(c.sql, keys[0].Expression); unaliased(sql) != want {
			t.Errorf("%s: compiled %q, but the key is %q", c.filter, unaliased(sql), keys[0].Expression)
		}
	}

	// A trend ranges over the date as a filter on it does.
	key, ok := DateKey(specs, svc, "lastDeployedAt")
	if want, _ := SortKey(specs, svc, "lastDeployedAt"); !ok || key.Expression != want.Expression || key.Use != UseRange {
		t.Errorf("DateKey = %+v, want %q", key, want.Expression)
	}
}

func TestIndexKeysFollowRelations(t *testing.T) {
	specs, _, deployment := indexFixture(t)
	f, err := ParseFilter(`{"and":[
		{"property":"service.owner.size","op":"gt","value":5},
		{"property":"service.dependsOn.stars","op":"gt","value":5},
		{"property":"service.ownerSlack","op":"exists"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, key := range FilterKeys(specs, deployment, f) {
		got = append(got, key.Blueprint+" "+key.Use+" "+key.Expression)
	}
	want := []string{
		"deployment relation relations ->> 'service'",
		"service relation relations ->> 'owner'",
		"team range (CASE WHEN jsonb_typeof(properties -> 'size') = 'number' THEN (properties -> 'size')::numeric END)",
		"deployment relation relations ->> 'service'",
		// A many-relation is read as a list, which no index on it serves.
		"service range (CASE WHEN jsonb_typeof(properties -> 'stars') = 'number' THEN (properties -> 'stars')::numeric END)",
		"deployment relation relations ->> 'service'",
		"service relation relations ->> 'owner'",
	}
	if !slices.Equal(got, want) {
		t.Errorf("keys =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The relation's key is what a hop compiles to.
	sql, _, err := Where(Schema{Blueprint: deployment, Blueprints: specs}, *f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "(e.relations ->> 'service') IN") || !strings.Contains(sql, "(e.relations ->> 'owner') IN") {
		t.Errorf("hops compile to %s", sql)
	}
}

func TestIndexKeysLeaveOutWhatNoIndexServes(t *testing.T) {
	specs, svc, deployment := indexFixture(t)
	for _, raw := range []string{
		// Equality and containment are the GIN indexes'.
		`{"property":"tier","op":"eq","value":"critical"}`,
		`{"property":"tags","op":"contains","value":"go"}`,
		`{"property":"tier","op":"contains","value":"crit"}`,
		// The planner doesn't answer exists from an index on the value.
		`{"property":"tier","op":"exists"}`,
		// A negated condition reads every entity it doesn't hold for.
		`{"not":{"property":"stars","op":"gt","value":3}}`,
		// Scorecard results are another table's.
		`{"property":"readiness","op":"exists"}`,
		`{"property":"readiness.score","op":"gt","value":50}`,
	} {
		f, err := ParseFilter(raw)
		if err != nil {
			t.Fatal(err)
		}
		if keys := FilterKeys(specs, svc, f); len(keys) > 0 {
			t.Errorf("%s: keys = %+v", raw, keys)
		}
	}
	// Retention's date and the title already have indexes of their own.
	for _, sort := range []string{"-finishedAt", "title", "identifier"} {
		if key, ok := SortKey(specs, deployment, sort); ok {
			t.Errorf("SortKey(%s) = %+v", sort, key)
		}
	}
	for _, sort := range []string{"tags", "-readiness"} {
		if key, ok := SortKey(specs, svc, sort); ok {
			t.Errorf("SortKey(%s) = %+v", sort, key)
		}
	}
}

func TestScorecardResultsAreNotEntityKeys(t *testing.T) {
	// Their indexes are scorecard_results' own, not one on entities.
	svc := service(t)
	for _, sort := range []string{"readiness", "-_scorecards.service-readiness", "_scorecards.service-readiness.score"} {
		if key, ok := SortKey(nil, svc, sort); ok {
			t.Errorf("SortKey(%s) = %+v", sort, key)
		}
	}
	f := &Filter{And: []Filter{
		{Property: "_scorecards.service-readiness.score", Op: OpGte, Value: float64(50)},
		{Property: "readiness.failed", Op: OpLt, Value: float64(2)},
	}}
	if keys := FilterKeys(nil, svc, f); len(keys) != 0 {
		t.Errorf("keys = %+v", keys)
	}
}
