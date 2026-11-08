//go:build e2e

package scorecard

import (
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/pgtest"
)

// TestSaveResultsWritesOnlyChanges checks a result is written only when it
// changed, and keeps the time its tier was reached until the tier changes.
func TestSaveResultsWritesOnlyChanges(t *testing.T) {
	db := pgtest.New(t)
	ctx := t.Context()
	blueprintID := pgtest.Publish(t, db, "blueprint", "service", "kind: Blueprint")
	integrationID := pgtest.Publish(t, db, "integration", "source", "kind: Integration")
	scorecardID := pgtest.Publish(t, db, Name, "health", "kind: Scorecard")
	var entities []string
	for _, identifier := range []string{"api", "web"} {
		var id string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO entities (blueprint_id, identifier, title, integration_id) VALUES ($1, $2, $2, $3) RETURNING id`,
			blueprintID, identifier, integrationID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		entities = append(entities, id)
	}
	score := func(n int) *int { return &n }
	result := func(tier string, points int) Result {
		return Result{Tier: tier, Score: score(points), Passed: 1, Criteria: map[string]CriterionResult{"owner": {Status: StatusPassed}}}
	}
	save := func(api, web Result) int {
		t.Helper()
		n, err := saveResults(ctx, db, scorecardID, entities, []Result{api, web})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	changedAt := func() (api, web time.Time) {
		t.Helper()
		if err := db.QueryRowContext(ctx, `
			SELECT (SELECT changed_at FROM scorecard_results WHERE entity_id = $1), (SELECT changed_at FROM scorecard_results WHERE entity_id = $2)`,
			entities[0], entities[1]).Scan(&api, &web); err != nil {
			t.Fatal(err)
		}
		return api, web
	}

	if n := save(result("gold", 100), result("", 0)); n != 2 {
		t.Fatalf("first save wrote %d", n)
	}
	api, web := changedAt()
	if n := save(result("gold", 100), result("", 0)); n != 0 {
		t.Fatalf("an unchanged save wrote %d", n)
	}
	if n := save(result("gold", 90), result("bronze", 50)); n != 2 {
		t.Fatalf("a changed save wrote %d", n)
	}
	if apiNow, webNow := changedAt(); !apiNow.Equal(api) || !webNow.After(web) {
		t.Errorf("changed at %v and %v, from %v and %v: want only the new tier's time moved", apiNow, webNow, api, web)
	}
}
