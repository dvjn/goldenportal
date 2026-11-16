package scorecard

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"go.opentelemetry.io/otel/attribute"
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/pglock"
	"github.com/dvjn/goldenportal/internal/telemetry"
)

// Criterion outcomes. Skipped is neutral: the criterion's when doesn't
// match the entity.
const (
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// Result is how one entity did on a scorecard.
type Result struct {
	// Tier is the highest tier reached, or empty for none.
	Tier string `json:"tier"`
	// Score is the share of applicable criteria passed, 0 to 100, or nil
	// when none applied.
	Score    *int                       `json:"score"`
	Passed   int                        `json:"passed"`
	Failed   int                        `json:"failed"`
	Skipped  int                        `json:"skipped"`
	Criteria map[string]CriterionResult `json:"criteria"`
}

// CriterionResult is how an entity did on one criterion.
type CriterionResult struct {
	Status string `json:"status"`
}

var ErrEvaluating = errors.New("this scorecard is already being evaluated")

// Database is what evaluations run on: one of its connections holds a
// run's lock from its first batch to its last.
type Database interface {
	configdoc.DB
	pglock.Lender
}

// database is ctx's database for evaluations, or false, with a warning,
// when it can't lend a connection.
func database(ctx *gofr.Context) (Database, bool) {
	db, ok := ctx.SQL.(Database)
	if !ok {
		telemetry.Warn(ctx, "scorecards not evaluated: the database can't lend a connection", nil)
	}

	return db, ok
}

// Evaluate scores every entity in a published scorecard's scope and stores
// the results that changed, dropping those of entities no longer in scope.
// It gives up with ErrEvaluating when the scorecard is already being
// evaluated, as the scheduled runs do.
func Evaluate(ctx context.Context, db Database, name string) (scored int, err error) {
	return runEvaluation(ctx, db, name, tryLock)
}

// evaluateInTurn is Evaluate after any evaluation already under way, for
// when that one may have read an older version. It gives up with
// ErrEvaluating after lockWait, and with the context after
// evaluationTimeout.
func evaluateInTurn(ctx context.Context, db Database, name string) (scored int, err error) {
	ctx, cancel := context.WithTimeout(ctx, evaluationTimeout)
	defer cancel()

	return runEvaluation(ctx, db, name, waitForLock)
}

const evaluationTimeout = 10 * time.Minute

// lockWait bounds the wait for an evaluation under way; tests shorten it.
var lockWait = time.Minute

// batchSize is how many entities an evaluation scores and writes at a
// time, each batch its own transaction; tests lower it.
var batchSize = 1000

// lockTaker takes a scorecard's evaluation lock, or says another run has it.
type lockTaker func(ctx context.Context, db pglock.Lender, name string) (*pglock.Lock, error)

const lockKey = `hashtext('scorecard-evaluate:' || $1)`

func tryLock(ctx context.Context, db pglock.Lender, name string) (*pglock.Lock, error) {
	return evaluationLock(pglock.Take(ctx, db, 0, lockKey, name))
}

func waitForLock(ctx context.Context, db pglock.Lender, name string) (*pglock.Lock, error) {
	return evaluationLock(pglock.Take(ctx, db, lockWait, lockKey, name))
}

func evaluationLock(lock *pglock.Lock, err error) (*pglock.Lock, error) {
	if errors.Is(err, pglock.ErrBusy) {
		return nil, ErrEvaluating
	}

	return lock, err
}

// tally is what a run did.
type tally struct {
	scored, written, removed int
}

func runEvaluation(ctx context.Context, db Database, name string, take lockTaker) (scored int, err error) {
	ctx, span := telemetry.Start(ctx, "evaluate scorecard", telemetry.Scorecard.String(name))
	started := time.Now()
	var counts tally
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("evaluation panicked: %v", r)
		}
		attrs := []attribute.KeyValue{
			telemetry.Items.Int(counts.scored), telemetry.Written.Int(counts.written), telemetry.Deleted.Int(counts.removed),
		}
		span.SetAttributes(append(attrs, telemetry.Unchanged.Int(counts.scored-counts.written))...)
		took := telemetry.Duration.Int64(time.Since(started).Milliseconds())
		switch {
		case errors.Is(err, ErrEvaluating):
			telemetry.Debug(ctx, "scorecard already being evaluated")
			span.End()
			return
		case errors.Is(err, configdoc.ErrNotFound):
			telemetry.Debug(ctx, "scorecard deleted before it was evaluated")
			span.End()
			return
		case err != nil:
			telemetry.Error(ctx, "scorecard not evaluated", err, append(attrs, took)...)
		default:
			telemetry.Info(ctx, "scorecard evaluated", append(attrs, took)...)
		}
		telemetry.End(span, err)
	}()

	counts, err = evaluatePublished(ctx, db, name, take)

	return counts.scored, err
}

// evaluatePublished holds the scorecard's lock on one connection while it
// scores the published version, so another run's batches can't interleave
// with its own.
func evaluatePublished(ctx context.Context, db Database, name string, take lockTaker) (tally, error) {
	lock, err := take(ctx, db, name)
	if err != nil {
		return tally{}, err
	}
	defer lock.Release(ctx)

	// Read under the lock, so an evaluation that waited scores the version
	// published while it waited.
	record, err := configdoc.NewStore(lock.Conn, Name).Get(ctx, name)
	if err != nil {
		return tally{}, err
	}
	if !record.Published() {
		return tally{}, nil
	}
	spec, _ := Parse(record.PublishedSource)

	return scoreVersion(ctx, lock.Conn, spec, record.PublishedVersion)
}

// scoreVersion scores version, spec, a batch of entities at a time, each
// in a statement of its own, then drops the results of entities out of
// scope and marks version evaluated. A run that fails partway leaves the
// batches it wrote and version unmarked, so the scorecard reads as
// Evaluating until one succeeds.
func scoreVersion(ctx context.Context, db configdoc.DB, spec Spec, version int) (counts tally, err error) {
	scorecardID, err := configdoc.NewStore(db, Name).PublishedID(ctx, spec.Identifier)
	if err != nil {
		return counts, err
	}
	specs, err := blueprint.Published(ctx, db)
	if err != nil {
		return counts, err
	}
	plan, err := compileEvaluation(specs, spec, scorecardID, version)
	if err != nil {
		return counts, err
	}
	for after := firstID; plan.batch != ""; {
		var scored, written int
		if err := db.QueryRowContext(ctx, plan.batch, plan.batchAfter(after)...).Scan(&scored, &written, &after); err != nil {
			return counts, err
		}
		counts.scored += scored
		counts.written += written
		if scored < batchSize {
			break
		}
	}
	err = db.QueryRowContext(ctx, plan.finish, plan.finishArgs...).Scan(&counts.removed)

	return counts, err
}

// firstID sorts before every entity's ID.
const firstID = "00000000-0000-0000-0000-000000000000"

// The statuses as SQL literals.
const (
	passedSQL  = "'" + StatusPassed + "'"
	failedSQL  = "'" + StatusFailed + "'"
	skippedSQL = "'" + StatusSkipped + "'"
)

// evaluationPlan is a scorecard's evaluation as SQL. batch, empty when the
// scorecard's blueprint isn't published, scores the batchSize entities in
// scope after an ID, writes the results that changed, and gives how many
// it scored and wrote and the last entity's ID. finish deletes the results
// of entities out of scope, all of them without a batch, marks the version
// evaluated and gives how many it deleted.
type evaluationPlan struct {
	batch, finish         string
	batchArgs, finishArgs []any
	// after is the place in batchArgs of the ID a batch starts after.
	after int
}

// batchAfter is the arguments of the batch after the entity id.
func (p evaluationPlan) batchAfter(id string) []any {
	args := slices.Clone(p.batchArgs)
	args[p.after] = id

	return args
}

// compileEvaluation writes the evaluation of version, spec. Unchanged
// results aren't touched, since even an update that changes nothing locks
// and logs its row, and a tier keeps the time it was reached until it
// changes.
func compileEvaluation(specs map[string]blueprint.Spec, spec Spec, scorecardID string, version int) (evaluationPlan, error) {
	args := []any{scorecardID}
	arg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	bp, graded := specs[spec.Blueprint]
	// Filters on other scorecards' tiers and checks were checked when
	// published; one since changed matches nothing rather than failing.
	schema := entity.Schema{Blueprint: bp, Blueprints: specs}
	where := func(f entity.Filter) (sql string, err error) {
		sql, args, err = entity.Where(schema, f, args)
		return "coalesce(" + sql + ", false)", err
	}

	inScope := "false"
	if graded {
		inScope = `e.blueprint_id = ` + entity.BlueprintID(bp)
		if spec.Filter != nil {
			scope, err := where(*spec.Filter)
			if err != nil {
				return evaluationPlan{}, err
			}
			inScope += " AND " + scope
		}
	}
	plan := evaluationPlan{finishArgs: slices.Clone(args)}
	plan.finishArgs = append(plan.finishArgs, version)
	plan.finish = `
		WITH removed AS (
			DELETE FROM scorecard_results r
			WHERE r.scorecard_id = $1::uuid AND NOT EXISTS (SELECT 1 FROM entities e WHERE e.id = r.entity_id AND ` + inScope + `)
			RETURNING 1
		), marked AS (
			INSERT INTO scorecard_evaluations (scorecard_id, last_evaluated_at, evaluated_version)
			VALUES ($1::uuid, now(), $` + strconv.Itoa(len(plan.finishArgs)) + `)
			ON CONFLICT (scorecard_id) DO UPDATE
			SET last_evaluated_at = excluded.last_evaluated_at, evaluated_version = excluded.evaluated_version
		)
		SELECT count(*) FROM removed`
	if !graded {
		return plan, nil
	}

	var ids []string
	for _, tier := range spec.Tiers {
		for _, c := range tier.Criteria {
			ids = append(ids, c.ID)
		}
	}
	criteria := arg(pq.Array(ids))
	after := arg(firstID)
	plan.after = len(args) - 1
	limit := arg(batchSize)
	var statuses, reached, ranks []string
	for _, tier := range spec.Tiers {
		for _, c := range tier.Criteria {
			check, err := where(c.Check)
			if err != nil {
				return evaluationPlan{}, err
			}
			status := "WHEN " + check + " THEN " + passedSQL + " ELSE " + failedSQL
			if c.When != nil {
				when, err := where(*c.When)
				if err != nil {
					return evaluationPlan{}, err
				}
				status = "WHEN NOT " + when + " THEN " + skippedSQL + " " + status
			}
			statuses = append(statuses, "CASE "+status+" END")
		}
		// A tier is reached when none of its criteria, nor any below, failed.
		when := "WHEN NOT " + failedSQL + " = ANY(statuses[1:" + strconv.Itoa(len(statuses)) + "])"
		reached = append(reached, when+" THEN "+arg(tier.Name))
		ranks = append(ranks, when+" THEN "+strconv.Itoa(len(ranks)+1))
	}
	slices.Reverse(reached)
	slices.Reverse(ranks)

	// Looked up laterally, a batch reads only its own entities' stored
	// results; joined, the planner may hash every result of the scorecard
	// for each batch.
	plan.batch = `
		WITH checked AS (
			SELECT e.id, ARRAY[` + strings.Join(statuses, ", ") + `]::text[] AS statuses
			FROM entities e
			WHERE ` + inScope + ` AND e.id > ` + after + `::uuid
			ORDER BY e.id
			LIMIT ` + limit + `
		), counted AS (
			SELECT id AS entity_id, CASE ` + strings.Join(reached, " ") + ` ELSE '' END AS tier,
				CASE ` + strings.Join(ranks, " ") + ` ELSE 0 END AS tier_rank,
				cardinality(array_positions(statuses, ` + passedSQL + `)) AS passed,
				cardinality(array_positions(statuses, ` + failedSQL + `)) AS failed,
				cardinality(array_positions(statuses, ` + skippedSQL + `)) AS skipped,
				(SELECT jsonb_object_agg(k, jsonb_build_object('status', v)) FROM unnest(` + criteria + `::text[], statuses) AS c (k, v)) AS criteria
			FROM checked
		), scored AS (
			SELECT *, CASE WHEN passed + failed > 0 THEN 100 * passed / (passed + failed) END AS score FROM counted
		), written AS (
			INSERT INTO scorecard_results AS r (scorecard_id, entity_id, tier, tier_rank, score, passed, failed, skipped, criteria)
			SELECT $1::uuid, s.entity_id, s.tier, s.tier_rank, s.score, s.passed, s.failed, s.skipped, s.criteria
			FROM scored s
			LEFT JOIN LATERAL (
				SELECT * FROM scorecard_results o WHERE o.scorecard_id = $1::uuid AND o.entity_id = s.entity_id LIMIT 1
			) o ON true
			WHERE o.entity_id IS NULL OR (o.tier, o.tier_rank, o.score, o.passed, o.failed, o.skipped, o.criteria)
				IS DISTINCT FROM (s.tier, s.tier_rank, s.score, s.passed, s.failed, s.skipped, s.criteria)
			ORDER BY s.entity_id
			ON CONFLICT (scorecard_id, entity_id) DO UPDATE
				SET tier = excluded.tier, tier_rank = excluded.tier_rank, score = excluded.score, passed = excluded.passed, failed = excluded.failed,
					skipped = excluded.skipped, criteria = excluded.criteria,
					changed_at = CASE WHEN r.tier = excluded.tier THEN r.changed_at ELSE now() END
			RETURNING 1
		)
		SELECT (SELECT count(*) FROM checked), (SELECT count(*) FROM written),
			coalesce((SELECT id FROM checked ORDER BY id DESC LIMIT 1), ` + after + `::uuid)::text`
	plan.batchArgs = args

	return plan, nil
}
