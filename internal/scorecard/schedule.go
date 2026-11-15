package scorecard

import (
	"context"
	"sync"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/telemetry"
)

const (
	// EvaluationScheduleEnv names the setting for when every scorecard is
	// evaluated in full, a cron expression, DefaultEvaluationSchedule unless
	// set. Publishing a scorecard, or the blueprint it scores, evaluates it
	// in between.
	EvaluationScheduleEnv     = "SCORECARD_EVALUATION_SCHEDULE"
	DefaultEvaluationSchedule = "0 * * * *"
)

// evaluateAll evaluates every published scorecard not already being
// evaluated.
func evaluateAll(ctx context.Context, db TxDB) {
	records, err := configdoc.NewStore(db, Name).List(ctx)
	if err != nil {
		telemetry.Error(ctx, "list scorecards to evaluate", err)
		return
	}
	for _, record := range records {
		if !record.Published() {
			continue
		}
		// Evaluate logs how it went.
		_, _ = Evaluate(ctx, db, record.Identifier)
	}
}

// evaluateAtStart evaluates every scorecard in the background, so a fresh
// or seeded database has results before the first scheduled run.
func evaluateAtStart(ctx *gofr.Context) error {
	detached := telemetry.Resume(context.Background(), ctx)
	background(func() { evaluateAll(detached, ctx.SQL) })

	return nil
}

// EvaluateScoring evaluates, after the request, the scorecards that score
// a blueprint just published, as its properties feed their checks.
func EvaluateScoring(ctx *gofr.Context, blueprint string) {
	specs, err := Published(ctx, ctx.SQL)
	if err != nil {
		telemetry.Error(ctx, "list scorecards to evaluate", err)
		return
	}
	var names []string
	for name, spec := range specs {
		if spec.Blueprint == blueprint {
			names = append(names, name)
		}
	}
	evaluateSoon(ctx, ctx.SQL, names...)
}

type pendingEvaluation struct {
	ctx context.Context
	db  TxDB
}

// pending holds the scorecards waiting to be evaluated after a publish. One
// worker evaluates them in turn, so a bulk publish holds one connection
// rather than one per scorecard, and a scorecard published twice meanwhile
// is evaluated once.
var pending struct {
	sync.Mutex
	names   map[string]pendingEvaluation
	working bool
}

// background runs work outside the request or start-up that asked for it;
// tests run it in place.
var background = func(work func()) { go work() }

// evaluateSoon evaluates the named scorecards outside ctx, the request that
// published them, so a slow or failing evaluation doesn't hold it up. Each
// waits for an evaluation already under way, which may have read the
// version before. Failures are logged and left to the next scheduled run.
func evaluateSoon(ctx context.Context, db TxDB, names ...string) {
	if len(names) == 0 {
		return
	}
	pending.Lock()
	if pending.names == nil {
		pending.names = map[string]pendingEvaluation{}
	}
	for _, name := range names {
		pending.names[name] = pendingEvaluation{ctx: telemetry.Resume(context.Background(), ctx), db: db}
	}
	if pending.working {
		pending.Unlock()
		return
	}
	pending.working = true
	pending.Unlock()

	background(func() {
		for {
			name, next, ok := takePending()
			if !ok {
				return
			}
			// evaluateInTurn logs how it went, panics included, and is bounded
			// in time, so the worker carries on to the next.
			_, _ = evaluateInTurn(next.ctx, next.db, name)
		}
	})
}

// takePending takes a waiting scorecard, or stops the worker when none is.
func takePending() (string, pendingEvaluation, bool) {
	pending.Lock()
	defer pending.Unlock()
	for name, next := range pending.names {
		delete(pending.names, name)
		return name, next, true
	}
	pending.working = false

	return "", pendingEvaluation{}, false
}
