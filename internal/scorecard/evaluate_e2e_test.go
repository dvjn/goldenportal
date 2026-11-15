//go:build e2e

package scorecard

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

const (
	gradedYAML = `kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    tier: {type: string}
    repository: {type: string}
    stars: {type: number}
    deployedAt: {type: string, format: date-time}
    critical: {type: boolean, compute: '.tier == "critical"'}
    team: {relation: team}
    teamSlack: {lookup: team.slack}
`
	// readinessFilters is the readiness scorecard of the unit tests, with
	// a relation, a lookup, a computed property and a date among its checks.
	readinessFilters = `kind: Scorecard
metadata: {identifier: readiness, title: Readiness}
spec:
  blueprint: service
  filter: {property: tier, op: ne, value: experimental}
  tiers:
    - name: bronze
      criteria:
        owned: {check: {property: team, op: exists}}
    - name: silver
      criteria:
        reachable: {check: {property: teamSlack, op: exists}}
        criticalRepo:
          when: {property: critical, op: eq, value: true}
          check: {property: repository, op: exists}
    - name: gold
      criteria:
        manyStars: {check: {property: stars, op: gt, value: 100}}
        recent: {check: {property: deployedAt, op: within, value: 7d}}
`
)

// addGraded adds a service with its properties, relations and computed values.
func (f *publishFixture) addGraded(identifier, properties, relations, computed string) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.t.Context(), `
		INSERT INTO entities (blueprint_id, identifier, title, connection_id, properties, relations, computed)
		SELECT id, $1, $1, $2, $3, $4, $5 FROM config_documents WHERE kind = 'blueprint' AND identifier = 'service'
		ON CONFLICT (blueprint_id, identifier) DO UPDATE
		SET properties = excluded.properties, relations = excluded.relations, computed = excluded.computed`,
		identifier, f.sourceID, properties, relations, computed); err != nil {
		f.t.Fatal(err)
	}
}

type stored struct {
	tier     string
	score    *int
	passed   int
	failed   int
	skipped  int
	statuses map[string]string
}

func (f *publishFixture) results(scorecard string) map[string]stored {
	f.t.Helper()
	rows, err := f.db.QueryContext(f.t.Context(), `
		SELECT e.identifier, r.tier, r.score, r.passed, r.failed, r.skipped, r.criteria FROM scorecard_results r
		JOIN entities e ON e.id = r.entity_id
		JOIN config_documents d ON d.id = r.scorecard_id
		WHERE d.kind = 'scorecard' AND d.identifier = $1`, scorecard)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]stored{}
	for rows.Next() {
		var (
			identifier string
			s          stored
			criteria   []byte
		)
		if err := rows.Scan(&identifier, &s.tier, &s.score, &s.passed, &s.failed, &s.skipped, &criteria); err != nil {
			f.t.Fatal(err)
		}
		var outcomes map[string]CriterionResult
		if err := json.Unmarshal(criteria, &outcomes); err != nil {
			f.t.Fatal(err)
		}
		s.statuses = map[string]string{}
		for id, outcome := range outcomes {
			s.statuses[id] = outcome.Status
		}
		out[identifier] = s
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}

	return out
}

// TestEvaluateFilterChecks checks filter checks reach the tiers, scores and
// statuses jq checks did on the same entities: a when that doesn't match
// skips, a failed lower tier caps the tier, and the scope's filter leaves
// entities out, dropping results of those that left it.
func TestEvaluateFilterChecks(t *testing.T) {
	f := newPublishFixture(t)
	f.publish("blueprints", "team", teamYAML)
	f.publish("blueprints", "service", gradedYAML)
	f.addEntity("team", "core", `{}`)
	f.addEntity("team", "ops", `{"slack": "#ops"}`)
	now := time.Now().UTC().Format(time.RFC3339)
	starred := `{"stars": 500, "deployedAt": "` + now + `"}`
	f.addGraded("bare", `{}`, `{}`, `{"critical": false}`)
	f.addGraded("owned", `{"stars": 5}`, `{"team": "core"}`, `{"critical": false}`)
	f.addGraded("reachable", `{"stars": 5}`, `{"team": "ops"}`, `{"critical": false}`)
	f.addGraded("critical", strings.Replace(starred, "{", `{"tier": "critical", `, 1), `{"team": "ops"}`, `{"critical": true}`)
	f.addGraded("gold", starred, `{"team": "ops"}`, `{"critical": false}`)
	f.addGraded("unowned", starred, `{}`, `{"critical": false}`)
	f.addGraded("trial", `{"tier": "experimental"}`, `{"team": "ops"}`, `{"critical": false}`)
	f.publish("scorecards", "readiness", readinessFilters)
	f.evaluateQueued()

	score := func(n int) *int { return &n }
	want := map[string]stored{
		"bare": {tier: "", score: score(0), failed: 4, skipped: 1, statuses: map[string]string{
			"owned": StatusFailed, "reachable": StatusFailed, "criticalRepo": StatusSkipped, "manyStars": StatusFailed, "recent": StatusFailed,
		}},
		"owned":     {tier: "bronze", score: score(25), passed: 1, failed: 3, skipped: 1},
		"reachable": {tier: "silver", score: score(50), passed: 2, failed: 2, skipped: 1},
		"critical": {tier: "bronze", score: score(80), passed: 4, failed: 1, statuses: map[string]string{
			"criticalRepo": StatusFailed, "manyStars": StatusPassed, "recent": StatusPassed,
		}},
		"gold":    {tier: "gold", score: score(100), passed: 4, skipped: 1},
		"unowned": {tier: "", score: score(50), passed: 2, failed: 2, skipped: 1},
	}
	got := f.results("readiness")
	if len(got) != len(want) {
		t.Errorf("scored %v, want %v", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
	for identifier, w := range want {
		g, ok := got[identifier]
		if !ok {
			t.Errorf("%s has no result", identifier)
			continue
		}
		if g.tier != w.tier || *g.score != *w.score || g.passed != w.passed || g.failed != w.failed || g.skipped != w.skipped {
			t.Errorf("%s = %s %d (%d passed, %d failed, %d skipped), want %s %d (%d, %d, %d)", identifier,
				g.tier, *g.score, g.passed, g.failed, g.skipped, w.tier, *w.score, w.passed, w.failed, w.skipped)
		}
		for id, status := range w.statuses {
			if g.statuses[id] != status {
				t.Errorf("%s %s = %s, want %s", identifier, id, g.statuses[id], status)
			}
		}
	}

	// Unchanged results aren't written again; a tier keeps the time it was
	// reached until it changes, and an entity leaving the scope loses its result.
	changedAt := func(identifier string) (at time.Time) {
		t.Helper()
		if err := f.db.QueryRowContext(t.Context(), `
			SELECT r.changed_at FROM scorecard_results r JOIN entities e ON e.id = r.entity_id WHERE e.identifier = $1`,
			identifier).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	gold, owned := changedAt("gold"), changedAt("owned")
	if scored, written, err := evaluateIn(t.Context(), f.db, mustParse(t, readinessFilters), 1); err != nil || scored != 6 || written != 0 {
		t.Fatalf("evaluating again scored %d and wrote %d (%v), want 6 and none", scored, written, err)
	}
	f.addGraded("gold", `{"stars": 50, "deployedAt": "`+now+`"}`, `{"team": "ops"}`, `{"critical": false}`)
	f.addGraded("owned", `{"stars": 5}`, `{"team": "ops"}`, `{"critical": false}`)
	f.addGraded("bare", `{"tier": "experimental"}`, `{}`, `{"critical": false}`)
	if scored, written, err := evaluateIn(t.Context(), f.db, mustParse(t, readinessFilters), 1); err != nil || scored != 5 || written != 2 {
		t.Fatalf("evaluating after changes scored %d and wrote %d (%v), want 5 and 2", scored, written, err)
	}
	if !changedAt("gold").After(gold) || !changedAt("owned").After(owned) {
		t.Error("a new tier kept the time the old one was reached")
	}
	if _, ok := f.results("readiness")["bare"]; ok {
		t.Error("an entity that left the scope kept its result")
	}
}

// TestPublishChecksFilters checks a scorecard whose check is jq, or reads a
// property its blueprint doesn't have, can't be saved or published.
func TestPublishChecksFilters(t *testing.T) {
	f := newPublishFixture(t)
	f.publish("blueprints", "team", teamYAML)
	f.publish("blueprints", "service", gradedYAML)
	d, _ := configdoc.DocumentsOf("scorecards")

	tests := []struct{ name, from, to, want string }{
		{
			"jq check", "check: {property: team, op: exists}", "check: .team != null",
			"a check is a filter, as the catalog writes them, such as {property: hasReadme, op: eq, value: true}; " +
				"jq over one entity belongs in a computed property on service, which the check reads",
		},
		{
			"jq when", "when: {property: critical, op: eq, value: true}", "when: .critical",
			"when is a filter, as the catalog writes them",
		},
		{"unknown property", "property: repository, op: exists", "property: repo, op: exists", `service has no property "repo"`},
		{"date compared as a number", "property: stars, op: gt", "property: deployedAt, op: gt", "gt on deployedAt needs a date"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := strings.Replace(readinessFilters, test.from, test.to, 1)
			if _, err := d.SaveDraft(f.ctx, "readiness", source); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("err = %v, want %q", err, test.want)
			}
		})
	}
}

// TestBlueprintPublishKeepsScorecardsAnswerable checks a blueprint change
// that leaves a published scorecard's filter unanswerable, on the blueprint
// it grades or one a relation leads to, is refused, naming the scorecard.
func TestBlueprintPublishKeepsScorecardsAnswerable(t *testing.T) {
	f := newPublishFixture(t)
	f.publish("blueprints", "team", teamYAML)
	f.publish("blueprints", "service", gradedYAML)
	f.publish("scorecards", "readiness", readinessFilters)
	blueprints, _ := configdoc.DocumentsOf("blueprints")

	tests := []struct{ name, blueprint, source, want string }{
		{
			"removed property", "service", strings.Replace(gradedYAML, "    repository: {type: string}\n", "", 1),
			`scorecard readiness reads this blueprint at spec.tiers[1].criteria.criticalRepo.check, which this change breaks: service has no property "repository"`,
		},
		{
			"changed type", "service", strings.Replace(gradedYAML, "stars: {type: number}", "stars: {type: string}", 1),
			"spec.tiers[2].criteria.manyStars.check, which this change breaks: stars only compares numbers and dates",
		},
		{
			"related blueprint", "team", strings.Replace(teamYAML, "    slack: {type: string}\n", "    channel: {type: string}\n", 1),
			`scorecard readiness reads this blueprint at spec.tiers[1].criteria.reachable.check, which this change breaks: team has no property "slack"`,
		},
		{
			"relation to an unpublished blueprint", "service", strings.Replace(gradedYAML, "team: {relation: team}", "team: {relation: squad}", 1),
			"spec.tiers[1].criteria.reachable.check, which this change breaks: team.slack follows team to squad, which isn't published",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := blueprints.SaveDraft(f.ctx, test.blueprint, test.source); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("err = %v, want %q", err, test.want)
			}
		})
	}

	// A change no scorecard reads publishes.
	f.publish("blueprints", "service", strings.Replace(gradedYAML, "    tier: {type: string}\n", "    tier: {type: string}\n    language: {type: string}\n", 1))
}
