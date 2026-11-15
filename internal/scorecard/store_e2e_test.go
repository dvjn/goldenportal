//go:build e2e

package scorecard

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/pgtest"
)

// seedResults stores results as an evaluation would.
func seedResults(t *testing.T, db configdoc.DB, scorecardID string, entityIDs []string, results []Result) {
	t.Helper()
	for i, r := range results {
		criteria, err := json.Marshal(r.Criteria)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(t.Context(), `
			INSERT INTO scorecard_results (scorecard_id, entity_id, tier, score, passed, failed, skipped, criteria)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			scorecardID, entityIDs[i], r.Tier, r.Score, r.Passed, r.Failed, r.Skipped, criteria); err != nil {
			t.Fatal(err)
		}
	}
}

// TestResultsSortAndFilter checks results sort up the scorecard's tiers
// rather than by tier name, by each count both ways, and narrow to the
// entities a filter matches, by tier, by a check's status or by their own
// fields, and that the summary counts only those entities.
func TestResultsSortAndFilter(t *testing.T) {
	db := pgtest.New(t)
	ctx := t.Context()
	blueprintID := pgtest.Publish(t, db, "blueprint", "service", "kind: Blueprint")
	connectionID := pgtest.Publish(t, db, "connection", "source", "kind: Connection")
	scorecardID := pgtest.Publish(t, db, Name, "health", "kind: Scorecard")
	graded, issues := blueprint.Parse("kind: Blueprint\nmetadata: {identifier: service, title: Service}\n" +
		"spec:\n  properties:\n    health: {type: scorecard, scorecard: health}\n")
	if blueprint.HasErrors(issues) {
		t.Fatal(issues)
	}
	score := func(n int) *int { return &n }
	criteria := func(owner string) map[string]CriterionResult {
		if owner == "" {
			return map[string]CriterionResult{}
		}
		return map[string]CriterionResult{"owner": {Status: owner}, "stars": {Status: StatusPassed}}
	}
	seeded := []struct {
		identifier string
		result     Result
	}{
		{"alpha", Result{Tier: "gold", Score: score(100), Passed: 4, Criteria: criteria(StatusPassed)}},
		{"bravo", Result{Tier: "silver", Score: score(75), Passed: 3, Failed: 1, Skipped: 2, Criteria: criteria(StatusSkipped)}},
		{"charlie", Result{Tier: "", Score: score(25), Passed: 1, Failed: 2, Criteria: criteria(StatusFailed)}},
		{"delta", Result{Tier: "bronze", Score: score(50), Passed: 2, Failed: 1, Skipped: 1, Criteria: criteria(StatusFailed)}},
		{"echo", Result{Tier: "", Criteria: criteria("")}},
	}
	var (
		ids     []string
		outcome []Result
	)
	for _, s := range seeded {
		var id string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO entities (blueprint_id, identifier, title, connection_id) VALUES ($1, $2, $2, $3) RETURNING id`,
			blueprintID, s.identifier, connectionID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids, outcome = append(ids, id), append(outcome, s.result)
	}
	seedResults(t, db, scorecardID, ids, outcome)
	spec := Spec{Tiers: []Tier{
		{Name: "bronze", Criteria: []Criterion{{ID: "owner"}}},
		{Name: "silver", Criteria: []Criterion{{ID: "stars"}}},
		{Name: "gold"},
	}}
	scope := func(t *testing.T, raw string) Scope {
		t.Helper()
		f, err := entity.ParseFilter(raw)
		if err != nil {
			t.Fatal(err)
		}
		specs, ladders := map[string]blueprint.Spec{"service": graded}, Ladders(map[string]Spec{"health": spec})
		if f != nil {
			if err := entity.CheckFilter(specs, ladders, graded, *f); err != nil {
				t.Fatal(err)
			}
		}
		return Scope{Blueprints: specs, Ladders: ladders, Blueprint: graded, Filter: f}
	}

	tests := []struct {
		sort, filter string
		want         []string
	}{
		// Alphabetically gold would come before silver.
		{"", "", []string{"echo", "charlie", "delta", "bravo", "alpha"}},
		{"tier", "", []string{"echo", "charlie", "delta", "bravo", "alpha"}},
		{"-tier", "", []string{"alpha", "bravo", "delta", "charlie", "echo"}},
		{"score", "", []string{"echo", "charlie", "delta", "bravo", "alpha"}},
		{"-score", "", []string{"alpha", "bravo", "delta", "charlie", "echo"}},
		// Equal counts go by tier.
		{"failed", "", []string{"echo", "alpha", "delta", "bravo", "charlie"}},
		{"-failed", "", []string{"charlie", "bravo", "delta", "alpha", "echo"}},
		{"-passed", "", []string{"alpha", "bravo", "delta", "charlie", "echo"}},
		{"-skipped", "", []string{"bravo", "delta", "alpha", "charlie", "echo"}},
		{"", `{"property": "health", "op": "in", "values": ["", "gold"]}`, []string{"echo", "charlie", "alpha"}},
		{"", `{"property": "health.checks.owner", "op": "eq", "value": "skipped"}`, []string{"bravo"}},
		{"-tier", `{"property": "health.checks.owner", "op": "in", "values": ["failed", "skipped"]}`, []string{"bravo", "delta", "charlie"}},
		{"", `{"and": [{"property": "health.checks.stars", "op": "eq", "value": "passed"}, {"property": "identifier", "op": "ne", "value": "bravo"}]}`, []string{"charlie", "delta", "alpha"}},
		{"", `{"property": "health.checks.stars", "op": "eq", "value": "failed"}`, []string{}},
	}
	for _, test := range tests {
		t.Run(test.sort+" "+test.filter, func(t *testing.T) {
			page, err := results(ctx, db, scorecardID, spec, ResultQuery{Scope: scope(t, test.filter), Sort: test.sort, Page: 1, PageSize: 10})
			if err != nil {
				t.Fatal(err)
			}
			got := []string{}
			for _, r := range page.Items {
				got = append(got, r.Identifier)
			}
			if !slices.Equal(got, test.want) || page.Total != len(test.want) {
				t.Errorf("got %v of %d, want %v", got, page.Total, test.want)
			}
		})
	}

	summary, err := summarize(ctx, db, scorecardID, spec, scope(t, `{"property": "health", "op": "ne", "value": "gold"}`))
	if err != nil {
		t.Fatal(err)
	}
	wantTiers := []TierCount{{"", 2}, {"bronze", 1}, {"silver", 1}, {"gold", 0}}
	wantCriteria := []CriterionAt{{ID: "owner", Failed: 2, Skipped: 1}, {ID: "stars", Passed: 3}}
	if summary.Entities != 4 || !slices.Equal(summary.Tiers, wantTiers) || !slices.Equal(summary.Criteria, wantCriteria) {
		t.Errorf("summary = %+v", summary)
	}
}

// TestLaddersReadChecks checks the ladders filters are checked against hold
// a published scorecard's checks, as its own spec does.
func TestLaddersReadChecks(t *testing.T) {
	db := pgtest.New(t)
	pgtest.Publish(t, db, Name, "readiness", readinessYAML)
	ladders, err := blueprint.Ladders(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"hasOwner", "hasRepository", "manyStars", "pagesOnCall"}
	if got := ladders["readiness"].Checks(); !slices.Equal(got, want) {
		t.Errorf("checks = %v, want %v", got, want)
	}
	if got := Ladders(map[string]Spec{"readiness": mustParse(t, readinessYAML)})["readiness"].Checks(); !slices.Equal(got, want) {
		t.Errorf("checks from the spec = %v, want %v", got, want)
	}
}
