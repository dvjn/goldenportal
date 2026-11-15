//go:build e2e

package dashboard

import (
	"strings"
	"testing"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx/httpxtest"
	"github.com/dvjn/goldenportal/internal/pgtest"
	"github.com/dvjn/goldenportal/internal/scorecard"
)

// TestScorecardReadersRefuseItsDeletion checks a scorecard whose results a
// dashboard and a blueprint's tab read can't be deleted or moved to
// another blueprint, and that the refusal names them.
func TestScorecardReadersRefuseItsDeletion(t *testing.T) {
	blueprint.Register(httpxtest.New(), []blueprint.Checker{CheckPages}, nil)
	scorecard.Register(httpxtest.New(), scorecard.DefaultEvaluationSchedule, nil)
	Register(httpxtest.New(), nil)
	db := pgtest.New(t)
	ctx := auth.WithPrincipal(t.Context(), auth.Principal{Role: auth.RoleBuilder})
	logger := logging.NewLogger(logging.FATAL)
	gctx := &gofr.Context{
		Context: ctx, Container: &container.Container{Logger: logger, SQL: db},
		ContextLogger: logging.ContextLoggerFor(ctx, logger),
	}
	pgtest.Publish(t, db, "blueprint", "team", "kind: Blueprint\nmetadata: {identifier: team, title: Team}\nspec: {properties: {owner: {type: string}}}\n")
	pgtest.Publish(t, db, "blueprint", "service", `kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    owner: {type: string}
  catalog:
    queries:
      services: {blueprint: service}
    tabs:
      overview:
        title: Overview
        widgets:
          owned: {type: stat, title: Owned, value: {query: services, share: {property: _scorecards.health.checks.hasOwner, op: eq, value: passed}}}
`)
	health := `kind: Scorecard
metadata: {identifier: health, title: Health}
spec:
  blueprint: service
  tiers:
    - name: owned
      criteria:
        hasOwner: {check: {property: owner, op: exists}}
`
	pgtest.Publish(t, db, "scorecard", "health", health)
	dashboards, _ := configdoc.DocumentsOf("dashboards")
	if _, err := dashboards.SaveDraft(gctx, "overview", `kind: Dashboard
metadata: {identifier: overview, title: Overview}
spec:
  queries:
    services: {blueprint: service}
  widgets:
    tiers: {type: bar, title: Tiers, query: services, groupBy: _scorecards.health}
`); err != nil {
		t.Fatal(err)
	}
	if _, err := dashboards.Publish(gctx, "overview", true); err != nil {
		t.Fatal(err)
	}

	scorecards, _ := configdoc.DocumentsOf("scorecards")
	err := scorecards.Delete(gctx, "health", "health")
	for _, reader := range []string{"blueprint service: spec.catalog.tabs.overview.widgets.owned", "dashboard overview: spec.widgets.tiers.groupBy"} {
		if err == nil || !strings.Contains(err.Error(), reader) {
			t.Errorf("deleting health: err = %v, want it to name %s", err, reader)
		}
	}
	if _, err := scorecards.SaveDraft(gctx, "health", strings.Replace(health, "blueprint: service", "blueprint: team", 1)); err == nil ||
		!strings.Contains(err.Error(), "this change breaks dashboard overview") {
		t.Errorf("moving health to team: err = %v", err)
	}
}
