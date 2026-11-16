//go:build e2e

package scorecard

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
)

const deploymentYAML = `kind: Blueprint
metadata: {identifier: deployment, title: Deployment}
spec:
  properties:
    service: {relation: service}
`

// TestReservedPathsReadResults checks _scorecards.<id> filters, sorts,
// groups, sums up and shows an evaluation's results on a blueprint with no
// scorecard property, as one naming the scorecard does, and through a
// relation.
func TestReservedPathsReadResults(t *testing.T) {
	f := newPublishFixture(t)
	f.publish("blueprints", "team", teamYAML)
	f.publish("blueprints", "service", gradedYAML)
	f.publish("blueprints", "deployment", deploymentYAML)
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
	for _, d := range [][2]string{{"to-gold", "gold"}, {"to-owned", "owned"}, {"to-bare", "bare"}, {"to-trial", "trial"}} {
		if _, err := f.db.ExecContext(t.Context(), `
			INSERT INTO entities (blueprint_id, identifier, title, connection_id, relations)
			SELECT id, $1, $1, $2, jsonb_build_object('service', $3::text) FROM config_documents WHERE kind = 'blueprint' AND identifier = 'deployment'`,
			d[0], f.sourceID, d[1]); err != nil {
			t.Fatal(err)
		}
	}
	f.publish("scorecards", "readiness", readinessFilters)
	f.evaluateQueued()

	ctx := t.Context()
	specs, err := blueprint.Published(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	ladders, err := blueprint.Ladders(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	service, deployment := specs["service"], specs["deployment"]
	named := service
	named.Properties = append(slices.Clone(service.Properties), blueprint.Property{ID: "health", Title: "Health", Scorecard: "readiness"})
	store := entity.NewStore(f.db)
	list := func(t *testing.T, spec blueprint.Spec, filter, sort string) []string {
		t.Helper()
		f, err := entity.ParseFilter(filter)
		if err != nil {
			t.Fatal(err)
		}
		page, err := store.List(ctx, entity.Query{Blueprint: spec, Blueprints: specs, Ladders: ladders, Filter: f, Sort: sort, PageSize: 20})
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, e := range page.Items {
			got = append(got, e.Identifier)
		}
		if page.Total == nil || *page.Total != len(got) {
			t.Errorf("total %v of %d listed", page.Total, len(got))
		}
		// Read a page at a time, either way, the list is the same.
		for _, size := range []int{1, 3} {
			walked(t, got, func(cursor string) ([]string, *int, string, string, string) {
				page, err := store.List(ctx, entity.Query{Blueprint: spec, Blueprints: specs, Ladders: ladders, Filter: f, Sort: sort, PageSize: size, Cursor: cursor})
				if err != nil {
					t.Fatal(err)
				}
				items := []string{}
				for _, e := range page.Items {
					items = append(items, e.Identifier)
				}
				return items, page.Total, page.Next, page.Previous, page.Last
			})
		}
		return got
	}

	tests := []struct {
		filter, sort string
		want         []string
	}{
		{`{"property":"%s","op":"in","values":["silver","gold"]}`, "", []string{"gold", "reachable"}},
		// No tier is evaluated with none; trial, out of the scorecard's scope, matches no tier.
		{`{"property":"%s","op":"eq","value":""}`, "", []string{"bare", "unowned"}},
		{`{"property":"%s","op":"in","values":["","bronze"]}`, "", []string{"bare", "critical", "owned", "unowned"}},
		{`{"property":"%s","op":"ne","value":"bronze"}`, "", []string{"bare", "gold", "reachable", "trial", "unowned"}},
		{`{"property":"%s","op":"exists"}`, "", []string{"critical", "gold", "owned", "reachable"}},
		{`{"property":"%s.checks.manyStars","op":"eq","value":"passed"}`, "", []string{"critical", "gold", "unowned"}},
		{`{"property":"%s.checks.criticalRepo","op":"in","values":["passed","failed"]}`, "", []string{"critical"}},
		{`{"not":{"property":"%s.checks.manyStars","op":"eq","value":"failed"}}`, "", []string{"critical", "gold", "trial", "unowned"}},
		{`{"property":"%s.score","op":"gte","value":50}`, "-%s.score", []string{"gold", "critical", "reachable", "unowned"}},
		{`{"property":"%s.failing","op":"contains","value":"owned"}`, "", []string{"bare", "unowned"}},
		// Tiers sort up the ladder, not by name; not evaluated sorts below no tier.
		{"", "%s", []string{"trial", "bare", "unowned", "critical", "owned", "reachable", "gold"}},
		{"", "-%s", []string{"gold", "reachable", "critical", "owned", "bare", "unowned", "trial"}},
	}
	for _, test := range tests {
		t.Run(test.filter+" "+test.sort, func(t *testing.T) {
			for _, by := range []struct {
				spec blueprint.Spec
				path string
			}{{service, "_scorecards.readiness"}, {named, "health"}} {
				filter, sort := strings.ReplaceAll(test.filter, "%s", by.path), strings.ReplaceAll(test.sort, "%s", by.path)
				if got := list(t, by.spec, filter, sort); !slices.Equal(got, test.want) {
					t.Errorf("%s: got %v, want %v", by.path, got, test.want)
				}
			}
		})
	}

	// Through a relation, the path reads the related entity's result.
	if got := list(t, deployment, `{"property":"service._scorecards.readiness","op":"eq","value":"gold"}`, ""); !slices.Equal(got, []string{"to-gold"}) {
		t.Errorf("deployments to gold services = %v", got)
	}
	if got := list(t, deployment, `{"property":"service._scorecards.readiness","op":"eq","value":""}`, ""); !slices.Equal(got, []string{"to-bare"}) {
		t.Errorf("deployments to services evaluated with no tier = %v", got)
	}
	if got := list(t, deployment, "", "-service._scorecards.readiness"); !slices.Equal(got, []string{"to-gold", "to-owned", "to-bare", "to-trial"}) {
		t.Errorf("deployments by their service's tier = %v", got)
	}

	m, err := entity.NewMatch(entity.Query{Blueprint: service, Blueprints: specs, Ladders: ladders})
	if err != nil {
		t.Fatal(err)
	}
	groups, none, err := store.GroupBy(ctx, m, "_scorecards.readiness", nil, entity.GroupSort{By: entity.SortGroup}, 10)
	if err != nil {
		t.Fatal(err)
	}
	var counts []string
	for _, g := range groups {
		counts = append(counts, g.Title+":"+strings.Repeat("*", g.Count))
	}
	if none == nil || none.Count != 3 || !slices.Equal(counts, []string{"bronze:**", "silver:*", "gold:*"}) {
		t.Errorf("groups = %v, none %+v", counts, none)
	}
	values, err := store.Aggregates(ctx, m, []entity.Aggregate{{Op: "avg", Path: "_scorecards.readiness.score"}})
	if err != nil || values[0] == nil || math.Abs(*values[0]-305.0/6) > 0.001 {
		t.Errorf("average score = %v, %v", values, err)
	}

	dm, err := entity.NewMatch(entity.Query{Blueprint: deployment, Blueprints: specs, Ladders: ladders})
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := store.Rows(ctx, dm, entity.Order{Sort: "service._scorecards.readiness"}, 10,
		[]entity.Column{{Path: "service._scorecards.readiness"}, {Path: "service._scorecards.readiness.score"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[0].Identifier != "to-trial" || rows[0].Values[0] != nil || rows[1].Values[0] != "" ||
		rows[3].Values[0] != "gold" || rows[3].Values[1] != float64(100) || rows[3].Links[0][0].Identifier != "gold" {
		t.Errorf("rows = %+v", rows)
	}

	bad, _ := entity.ParseFilter(`{"property":"_scorecards.readiness","op":"exists"}`)
	if err := entity.CheckFilter(entity.Schema{Blueprint: deployment, Blueprints: specs, Ladders: ladders}, *bad); err == nil || !strings.Contains(err.Error(), "readiness grades service, not deployment") {
		t.Errorf("err = %v, want readiness grading service", err)
	}
}

// TestScorecardChangesKeepReadersAnswerable checks deleting a scorecard,
// moving it to another blueprint or dropping a check is refused while a
// published document reads its results, by its results path or a property
// naming it, and names what reads them.
func TestScorecardChangesKeepReadersAnswerable(t *testing.T) {
	f := newPublishFixture(t)
	f.publish("blueprints", "team", teamYAML)
	f.publish("blueprints", "service", serviceYAML)
	f.publish("scorecards", "health", healthYAML)
	f.publish("blueprints", "service", serviceYAML+"    health: {type: scorecard, scorecard: health}\n")
	f.publish("scorecards", "gate", `kind: Scorecard
metadata: {identifier: gate, title: Gate}
spec:
  blueprint: service
  tiers:
    - name: open
      criteria:
        healthy: {check: {property: _scorecards.health.checks.hasOwner, op: eq, value: passed}}
`)
	f.evaluateQueued()
	scorecards, _ := configdoc.DocumentsOf("scorecards")

	err := scorecards.Delete(f.ctx, "health", "health")
	if err == nil || !strings.Contains(err.Error(), "blueprint service: spec.properties.health.scorecard: health names scorecard health, which is being deleted") ||
		!strings.Contains(err.Error(), "scorecard gate: ") || !strings.Contains(err.Error(), "_scorecards.health reads scorecard health, which is being deleted") {
		t.Fatalf("deleting a scorecard others read: %v", err)
	}
	for name, change := range map[string]string{
		"moved":         strings.Replace(healthYAML, "blueprint: service", "blueprint: team", 1),
		"check dropped": strings.Replace(healthYAML, "hasOwner: {check: {property: owner, op: exists}}", "named: {check: {property: owner, op: exists}}", 1),
	} {
		_, err := scorecards.SaveDraft(f.ctx, "health", change)
		if err == nil || !strings.Contains(err.Error(), "this change breaks scorecard gate") {
			t.Errorf("%s: err = %v, want the change refused for gate", name, err)
		}
	}
	// A change no reader notices goes through.
	if _, err := scorecards.SaveDraft(f.ctx, "health", strings.Replace(healthYAML, "title: Health", "title: Service Health", 1)); err != nil {
		t.Errorf("retitling: %v", err)
	}

	// Once nothing reads it, it can go.
	if err := scorecards.Delete(f.ctx, "gate", "gate"); err != nil {
		t.Fatal(err)
	}
	f.publish("blueprints", "service", serviceYAML)
	if err := scorecards.Delete(f.ctx, "health", "health"); err != nil {
		t.Errorf("deleting a scorecard nothing reads: %v", err)
	}
}
