//go:build e2e

package scorecard

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx/httpxtest"
	"github.com/dvjn/goldenportal/internal/pgtest"
)

const (
	serviceYAML = `kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    owner: {type: string}
`
	teamYAML = `kind: Blueprint
metadata: {identifier: team, title: Team}
spec:
  properties:
    slack: {type: string}
`
	healthYAML = `kind: Scorecard
metadata: {identifier: health, title: Health}
spec:
  blueprint: service
  tiers:
    - name: owned
      criteria:
        hasOwner: {check: {property: owner, op: exists}}
`
	reachableYAML = `kind: Scorecard
metadata: {identifier: reachable, title: Reachable}
spec:
  blueprint: team
  tiers:
    - name: reachable
      criteria:
        hasSlack: {check: {property: slack, op: exists}}
`
)

var registerKinds = sync.OnceFunc(func() {
	blueprint.Register(httpxtest.New(), []blueprint.Checker{CheckGraders}, EvaluateScoring)
	Register(httpxtest.New(), DefaultEvaluationSchedule)
})

// publishFixture publishes through the documents service as a builder, and
// keeps the evaluations publishing starts until the test runs them.
type publishFixture struct {
	t        *testing.T
	db       *gofrSQL.DB
	ctx      *gofr.Context
	queued   []func()
	sourceID string
}

func newPublishFixture(t *testing.T) *publishFixture {
	t.Helper()
	registerKinds()
	db := pgtest.New(t)
	ctx := auth.WithPrincipal(t.Context(), auth.Principal{Role: auth.RoleBuilder})
	logger := logging.NewLogger(logging.FATAL)
	f := &publishFixture{
		t: t, db: db,
		ctx: &gofr.Context{
			Context: ctx, Container: &container.Container{Logger: logger, SQL: db},
			ContextLogger: logging.ContextLoggerFor(ctx, logger),
		},
		sourceID: pgtest.Publish(t, db, "connection", "source", "kind: Connection"),
	}
	previous := background
	background = func(work func()) { f.queued = append(f.queued, work) }
	t.Cleanup(func() {
		f.evaluateQueued()
		background = previous
	})

	return f
}

func (f *publishFixture) publish(plural, identifier, source string) {
	f.t.Helper()
	d, ok := configdoc.DocumentsOf(plural)
	if !ok {
		f.t.Fatalf("no %s registered", plural)
	}
	if _, err := d.SaveDraft(f.ctx, identifier, source); err != nil {
		f.t.Fatal(err)
	}
	if _, err := d.Publish(f.ctx, identifier, true); err != nil {
		f.t.Fatal(err)
	}
}

func (f *publishFixture) evaluateQueued() {
	queued := f.queued
	f.queued = nil
	for _, work := range queued {
		work()
	}
}

func (f *publishFixture) addEntity(blueprintName, identifier, properties string) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.t.Context(), `
		INSERT INTO entities (blueprint_id, identifier, title, connection_id, properties)
		SELECT id, $2, $2, $3, $4 FROM config_documents WHERE kind = 'blueprint' AND identifier = $1`,
		blueprintName, identifier, f.sourceID, properties); err != nil {
		f.t.Fatal(err)
	}
}

// tiers gives each scored entity's tier, by identifier.
func (f *publishFixture) tiers(scorecard string) map[string]string {
	f.t.Helper()
	rows, err := f.db.QueryContext(f.t.Context(), `
		SELECT e.identifier, r.tier FROM scorecard_results r
		JOIN entities e ON e.id = r.entity_id
		JOIN config_documents d ON d.id = r.scorecard_id
		WHERE d.kind = 'scorecard' AND d.identifier = $1`, scorecard)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var identifier, tier string
		if err := rows.Scan(&identifier, &tier); err != nil {
			f.t.Fatal(err)
		}
		out[identifier] = tier
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}

	return out
}

// TestPublishEvaluatesAfterCommit checks publishing a scorecard evaluates it,
// but outside the publish, which is done before the evaluation starts.
func TestPublishEvaluatesAfterCommit(t *testing.T) {
	f := newPublishFixture(t)
	f.publish("blueprints", "service", serviceYAML)
	f.addEntity("service", "api", `{"owner": "platform"}`)
	f.addEntity("service", "web", `{}`)
	f.evaluateQueued()

	f.publish("scorecards", "health", healthYAML)
	if got := f.tiers("health"); len(got) != 0 {
		t.Fatalf("results %v before the evaluation ran, want the publish not to wait for it", got)
	}
	if !f.evaluating("health") {
		t.Error("the summary doesn't say the published version is being evaluated")
	}
	if len(f.queued) != 1 {
		t.Fatalf("%d evaluations queued, want one", len(f.queued))
	}
	f.evaluateQueued()
	if got := f.tiers("health"); got["api"] != "owned" || got["web"] != "" || len(got) != 2 {
		t.Errorf("results = %v, want api owned and web at no tier", got)
	}
	if f.evaluating("health") {
		t.Error("the summary says the scorecard is being evaluated after its results caught up")
	}

	f.publish("scorecards", "health", strings.Replace(healthYAML, "title: Health", "title: Healthy", 1))
	if !f.evaluating("health") {
		t.Error("the summary doesn't say a second version is being evaluated")
	}
}

// evaluating says whether a scorecard's summary has its results behind its
// published version.
func (f *publishFixture) evaluating(scorecard string) bool {
	f.t.Helper()
	view, err := GetSummary(f.ctx, scorecard, SummaryRequest{})
	if err != nil {
		f.t.Fatal(err)
	}

	return view.Evaluating
}

// TestPublishingBlueprintEvaluatesItsScorecards checks publishing a
// blueprint evaluates the scorecards that score it, and no others.
func TestPublishingBlueprintEvaluatesItsScorecards(t *testing.T) {
	f := newPublishFixture(t)
	f.publish("blueprints", "service", serviceYAML)
	f.publish("blueprints", "team", teamYAML)
	f.addEntity("service", "web", `{}`)
	f.addEntity("team", "core", `{}`)
	f.publish("scorecards", "health", healthYAML)
	f.publish("scorecards", "reachable", reachableYAML)
	f.evaluateQueued()

	if _, err := f.db.ExecContext(t.Context(), `UPDATE entities SET properties = '{"owner": "platform", "slack": "#core"}'`); err != nil {
		t.Fatal(err)
	}
	f.publish("blueprints", "service", strings.Replace(serviceYAML, "title: Service", "title: Services", 1))
	f.evaluateQueued()

	if got := f.tiers("health"); got["web"] != "owned" {
		t.Errorf("health = %v, want web re-evaluated as owned", got)
	}
	if got := f.tiers("reachable"); got["core"] != "" {
		t.Errorf("reachable = %v, want core left until its own blueprint or scorecard changes", got)
	}
}

// TestEvaluateNowWaitsForOneUnderWay checks Evaluate Now, like an
// evaluation a publish starts, waits for one already under way, then scores
// the version published meanwhile rather than the one before.
func TestEvaluateNowWaitsForOneUnderWay(t *testing.T) {
	f := newPublishFixture(t)
	f.publish("blueprints", "service", serviceYAML)
	f.addEntity("service", "api", `{"owner": "platform"}`)
	f.publish("scorecards", "health", healthYAML)
	f.evaluateQueued()

	holder, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(t.Context(), `SELECT pg_advisory_xact_lock(hashtext('scorecard-evaluate:' || $1))`, "health"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := EvaluateNow(f.ctx, "health")
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("evaluation finished (%v) while another held the scorecard, want it to wait", err)
	case <-time.After(300 * time.Millisecond):
	}
	f.publish("scorecards", "health", strings.Replace(healthYAML, "name: owned", "name: claimed", 1))
	if err := holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := f.tiers("health"); got["api"] != "claimed" {
		t.Errorf("results = %v, want the version published while it waited", got)
	}
}

// TestEvaluateInTurnGivesUp checks an evaluation waiting for one that never
// finishes gives up, rather than holding up the evaluations queued after it.
func TestEvaluateInTurnGivesUp(t *testing.T) {
	f := newPublishFixture(t)
	f.publish("blueprints", "service", serviceYAML)
	f.publish("scorecards", "health", healthYAML)
	f.evaluateQueued()
	previous := lockWait
	lockWait = 200 * time.Millisecond
	t.Cleanup(func() { lockWait = previous })

	holder, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(t.Context(), `SELECT pg_advisory_xact_lock(hashtext('scorecard-evaluate:' || $1))`, "health"); err != nil {
		t.Fatal(err)
	}
	if _, err := evaluateInTurn(context.Background(), f.db, "health"); !errors.Is(err, ErrEvaluating) {
		t.Errorf("err = %v, want ErrEvaluating once the wait runs out", err)
	}
}

// TestEvaluationPanicIsAnError checks a panic while evaluating comes back
// as an error, so the worker carries on.
func TestEvaluationPanicIsAnError(t *testing.T) {
	f := newPublishFixture(t)
	panicking := func(context.Context, *gofrSQL.Tx, string) error { panic("boom") }
	if _, err := evaluateTraced(context.Background(), f.db, "health", panicking); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want the panic as an error", err)
	}
}

// TestEvaluateAtStart checks the server evaluates every scorecard as it
// starts, so seeded scorecards have results before the first scheduled run.
func TestEvaluateAtStart(t *testing.T) {
	f := newPublishFixture(t)
	f.publish("blueprints", "service", serviceYAML)
	f.addEntity("service", "api", `{"owner": "platform"}`)
	pgtest.Publish(t, f.db, Name, "seeded", strings.Replace(healthYAML, "identifier: health", "identifier: seeded", 1))

	r := httpxtest.New()
	Register(r, DefaultEvaluationSchedule)
	if len(r.OnStarts) != 1 {
		t.Fatalf("%d start-up hooks, want one", len(r.OnStarts))
	}
	if err := r.OnStarts[0](f.ctx); err != nil {
		t.Fatal(err)
	}
	f.evaluateQueued()
	if got := f.tiers("seeded"); got["api"] != "owned" {
		t.Errorf("results = %v, want the seeded scorecard evaluated at start", got)
	}
}
