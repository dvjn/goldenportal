package scorecard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/telemetry"
)

// Name is the configdoc kind scorecards are stored as.
const Name = "scorecard"

var (
	ErrEvaluating = errors.New("this scorecard is already being evaluated")
	likeEscaper   = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
)

// TxDB is a database that can start transactions.
type TxDB interface {
	configdoc.DB
	Begin() (*gofrSQL.Tx, error)
}

// evaluationPage is how many entities are scored per query.
const evaluationPage = 100

// Evaluate scores every entity in a published scorecard's scope and stores
// the results that changed, dropping those of entities no longer in scope.
func Evaluate(ctx context.Context, db TxDB, name string) (scored int, err error) {
	ctx, span := telemetry.Start(ctx, "evaluate scorecard", telemetry.Scorecard.String(name))
	started := time.Now()
	var written int
	defer func() {
		span.SetAttributes(telemetry.Items.Int(scored), telemetry.Written.Int(written), telemetry.Unchanged.Int(scored-written))
		took := telemetry.Duration.Int64(time.Since(started).Milliseconds())
		switch {
		case errors.Is(err, ErrEvaluating):
			telemetry.Debug(ctx, "scorecard already being evaluated")
			span.End()
			return
		case err != nil:
			telemetry.Error(ctx, "scorecard not evaluated", err, took)
		default:
			// Every scorecard is evaluated every minute, so this is routine.
			telemetry.Debug(ctx, "scorecard evaluated", telemetry.Items.Int(scored), telemetry.Written.Int(written), took)
		}
		telemetry.End(span, err)
	}()

	scored, written, err = evaluate(ctx, db, name)

	return scored, err
}

func evaluate(ctx context.Context, db TxDB, name string) (scored, written int, err error) {
	record, err := configdoc.NewStore(db, Name).Get(ctx, name)
	if err != nil {
		return 0, 0, err
	}
	if !record.Published() {
		return 0, 0, nil
	}
	spec, _ := Parse(record.PublishedSource)

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var locked bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('scorecard-evaluate:' || $1))`,
		name).Scan(&locked); err != nil {
		return 0, 0, err
	}
	if !locked {
		return 0, 0, ErrEvaluating
	}

	if scored, written, err = evaluateIn(ctx, tx, spec); err != nil {
		return 0, 0, err
	}

	return scored, written, tx.Commit()
}

// evaluateIn scores and stores within db, which callers make a transaction,
// and says how many entities it scored and how many results it wrote.
func evaluateIn(ctx context.Context, db configdoc.DB, spec Spec) (scored, written int, err error) {
	scorecardID, err := configdoc.NewStore(db, Name).PublishedID(ctx, spec.Identifier)
	if err != nil {
		return 0, 0, err
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scorecard_evaluations (scorecard_id, last_evaluated_at) VALUES ($1, now())
		ON CONFLICT (scorecard_id) DO UPDATE SET last_evaluated_at = excluded.last_evaluated_at`, scorecardID); err != nil {
		return 0, 0, err
	}
	specs, err := blueprint.Published(ctx, db)
	if err != nil {
		return 0, 0, err
	}
	bp, ok := specs[spec.Blueprint]
	if !ok {
		_, err := db.ExecContext(ctx, `DELETE FROM scorecard_results WHERE scorecard_id = $1`, scorecardID)
		return 0, 0, err
	}
	checks, err := compileSpec(spec)
	if err != nil {
		return 0, 0, err
	}

	entities := entity.NewStore(db)
	ids := []string{}
	for page := 1; ; page++ {
		result, err := entities.List(ctx, entity.Query{Blueprint: bp, Blueprints: specs, Filter: spec.Filter, Page: page, PageSize: evaluationPage})
		if err != nil {
			return 0, 0, err
		}
		if err := entities.Resolve(ctx, specs, bp, result.Items); err != nil {
			return 0, 0, err
		}
		pageIDs := make([]string, len(result.Items))
		outcomes := make([]Result, len(result.Items))
		for i, e := range result.Items {
			pageIDs[i], outcomes[i] = e.ID, checks.evaluate(ctx, spec, Input(e))
		}
		n, err := saveResults(ctx, db, scorecardID, pageIDs, outcomes)
		if err != nil {
			return 0, 0, err
		}
		written += n
		ids = append(ids, pageIDs...)
		if len(result.Items) < evaluationPage {
			break
		}
	}

	if _, err := db.ExecContext(ctx, `
		DELETE FROM scorecard_results WHERE scorecard_id = $1 AND NOT (entity_id = ANY($2::uuid[]))`,
		scorecardID, pq.Array(ids)); err != nil {
		return 0, 0, err
	}

	return len(ids), written, nil
}

// Input is the document checks run against: the entity flat, with its
// relations, computed values, resolved lookups and scorecard results
// beside its built-in fields.
func Input(e *entity.Entity) map[string]any { return entity.Document(e) }

// saveResults writes the results of entities that are new or changed, and
// says how many. Results are compared in Postgres, as jsonb, and those that
// didn't change aren't touched at all, since even an update that changes
// nothing locks and logs its row. A result's tier keeps the time it was
// reached until it changes.
func saveResults(ctx context.Context, db configdoc.DB, scorecardID string, entityIDs []string, results []Result) (int, error) {
	if len(results) == 0 {
		return 0, nil
	}
	var (
		tiers, scores, criteria          []string
		passed, failed, skipped, errored []int64
	)
	for _, r := range results {
		encoded, err := json.Marshal(r.Criteria)
		if err != nil {
			return 0, err
		}
		score := ""
		if r.Score != nil {
			score = strconv.Itoa(*r.Score)
		}
		tiers, scores, criteria = append(tiers, r.Tier), append(scores, score), append(criteria, string(encoded))
		passed, failed = append(passed, int64(r.Passed)), append(failed, int64(r.Failed))
		skipped, errored = append(skipped, int64(r.Skipped)), append(errored, int64(r.Errored))
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO scorecard_results AS r (scorecard_id, entity_id, tier, score, passed, failed, skipped, errored, criteria)
		SELECT $1, i.entity_id, i.tier, nullif(i.score, '')::integer, i.passed, i.failed, i.skipped, i.errored, i.criteria::jsonb
		FROM unnest($2::uuid[], $3::text[], $4::text[], $5::integer[], $6::integer[], $7::integer[], $8::integer[], $9::text[])
			AS i (entity_id, tier, score, passed, failed, skipped, errored, criteria)
		LEFT JOIN scorecard_results s ON s.scorecard_id = $1 AND s.entity_id = i.entity_id
		WHERE s.entity_id IS NULL OR (s.tier, s.score, s.passed, s.failed, s.skipped, s.errored, s.criteria)
			IS DISTINCT FROM (i.tier, nullif(i.score, '')::integer, i.passed, i.failed, i.skipped, i.errored, i.criteria::jsonb)
		ORDER BY i.entity_id
		ON CONFLICT (scorecard_id, entity_id) DO UPDATE
			SET tier = excluded.tier, score = excluded.score, passed = excluded.passed, failed = excluded.failed,
				skipped = excluded.skipped, errored = excluded.errored, criteria = excluded.criteria,
				changed_at = CASE WHEN r.tier = excluded.tier THEN r.changed_at ELSE now() END`,
		scorecardID, pq.Array(entityIDs), pq.Array(tiers), pq.Array(scores), pq.Array(passed), pq.Array(failed),
		pq.Array(skipped), pq.Array(errored), pq.Array(criteria))
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()

	return int(n), err
}

// Summary is how a scorecard's entities are doing overall.
type Summary struct {
	Entities    int           `json:"entities"`
	EvaluatedAt *time.Time    `json:"evaluatedAt"`
	Tiers       []TierCount   `json:"tiers"`
	Criteria    []CriterionAt `json:"criteria"`
}

// TierCount is how many entities reached a tier as their highest. Name is
// empty for entities that reached none.
type TierCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// CriterionAt counts the outcomes of one criterion across entities.
type CriterionAt struct {
	ID      string `json:"identifier"`
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
	Skipped int    `json:"skipped"`
	Errored int    `json:"errored"`
}

func summarize(ctx context.Context, db configdoc.DB, scorecardID string, spec Spec) (Summary, error) {
	summary := Summary{Tiers: []TierCount{}, Criteria: []CriterionAt{}}
	tiers := map[string]int{}
	err := db.QueryRowContext(ctx, `SELECT last_evaluated_at FROM scorecard_evaluations WHERE scorecard_id = $1`,
		scorecardID).Scan(&summary.EvaluatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Summary{}, err
	}
	rows, err := db.QueryContext(ctx, `SELECT tier, count(*) FROM scorecard_results WHERE scorecard_id = $1 GROUP BY tier`, scorecardID)
	if err != nil {
		return Summary{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			tier  string
			count int
		)
		if err := rows.Scan(&tier, &count); err != nil {
			return Summary{}, err
		}
		tiers[tier] = count
		summary.Entities += count
	}
	if err := rows.Err(); err != nil {
		return Summary{}, err
	}
	summary.Tiers = append(summary.Tiers, TierCount{Name: "", Count: tiers[""]})
	for _, tier := range spec.Tiers {
		summary.Tiers = append(summary.Tiers, TierCount{Name: tier.Name, Count: tiers[tier.Name]})
	}

	counts := map[string]*CriterionAt{}
	criteria, err := db.QueryContext(ctx, `
		SELECT c.key, c.value ->> 'status', count(*)
		FROM scorecard_results r, jsonb_each(r.criteria) c
		WHERE r.scorecard_id = $1
		GROUP BY 1, 2`, scorecardID)
	if err != nil {
		return Summary{}, err
	}
	defer criteria.Close()
	for criteria.Next() {
		var (
			id, status string
			count      int
		)
		if err := criteria.Scan(&id, &status, &count); err != nil {
			return Summary{}, err
		}
		at, ok := counts[id]
		if !ok {
			at = &CriterionAt{ID: id}
			counts[id] = at
		}
		switch status {
		case StatusPassed:
			at.Passed = count
		case StatusFailed:
			at.Failed = count
		case StatusSkipped:
			at.Skipped = count
		case StatusErrored:
			at.Errored = count
		}
	}
	if err := criteria.Err(); err != nil {
		return Summary{}, err
	}
	for _, tier := range spec.Tiers {
		for _, criterion := range tier.Criteria {
			at := counts[criterion.ID]
			if at == nil {
				at = &CriterionAt{ID: criterion.ID}
			}
			summary.Criteria = append(summary.Criteria, *at)
		}
	}

	return summary, nil
}

// EntityResult is one entity's result on a scorecard: since when it has
// had its tier, and when the scorecard was last evaluated.
type EntityResult struct {
	Blueprint   string    `json:"blueprint"`
	Identifier  string    `json:"identifier"`
	Title       string    `json:"title"`
	ChangedAt   time.Time `json:"changedAt"`
	EvaluatedAt time.Time `json:"evaluatedAt"`
	Result
}

// evaluatedAt is when a result's scorecard was last evaluated, for queries
// that join scorecard_evaluations as v.
const evaluatedAt = `coalesce(v.last_evaluated_at, r.changed_at)`

// ResultQuery picks a page of a scorecard's results, worst tier first.
type ResultQuery struct {
	// Tiers filter to entities whose highest tier is one of them; "none"
	// for those with none.
	Tiers    []string
	Search   string
	Page     int
	PageSize int
}

type ResultPage = httpx.Page[EntityResult]

func results(ctx context.Context, db configdoc.DB, scorecardID string, spec Spec, q ResultQuery) (ResultPage, error) {
	tierOrder := []string{""}
	for _, tier := range spec.Tiers {
		tierOrder = append(tierOrder, tier.Name)
	}
	tiers := make([]string, len(q.Tiers))
	for i, tier := range q.Tiers {
		if tier != "none" {
			tiers[i] = tier
		}
	}
	search := ""
	if q.Search != "" {
		search = "%" + likeEscaper.Replace(q.Search) + "%"
	}

	rows, err := db.QueryContext(ctx, `
		SELECT b.identifier, e.identifier, e.title, r.changed_at, `+evaluatedAt+`, r.tier, r.score,
			r.passed, r.failed, r.skipped, r.errored, r.criteria, count(*) OVER ()
		FROM scorecard_results r
		LEFT JOIN scorecard_evaluations v ON v.scorecard_id = r.scorecard_id
		JOIN entities e ON e.id = r.entity_id
		JOIN config_documents b ON b.id = e.blueprint_id
		WHERE r.scorecard_id = $1
			AND (cardinality($2::text[]) = 0 OR r.tier = ANY($2))
			AND ($3 = '' OR e.identifier ILIKE $3 OR e.title ILIKE $3)
		ORDER BY array_position($4::text[], r.tier), r.score NULLS FIRST, e.title, e.identifier
		LIMIT $5 OFFSET $6`,
		scorecardID, pq.Array(tiers), search, pq.Array(tierOrder), q.PageSize, (q.Page-1)*q.PageSize)
	if err != nil {
		return ResultPage{}, err
	}
	defer rows.Close()

	page := ResultPage{Items: []EntityResult{}}
	for rows.Next() {
		r, err := scanResult(rows.Scan, &page.Total)
		if err != nil {
			return ResultPage{}, err
		}
		page.Items = append(page.Items, r)
	}

	return page, rows.Err()
}

func scanResult(scan func(...any) error, extra ...any) (EntityResult, error) {
	var (
		r        EntityResult
		criteria []byte
	)
	if err := scan(append([]any{
		&r.Blueprint, &r.Identifier, &r.Title, &r.ChangedAt, &r.EvaluatedAt, &r.Tier, &r.Score,
		&r.Passed, &r.Failed, &r.Skipped, &r.Errored, &criteria,
	}, extra...)...); err != nil {
		return EntityResult{}, err
	}
	err := json.Unmarshal(criteria, &r.Criteria)

	return r, err
}

// EntityScorecard is an entity's result on one scorecard, for its page.
type EntityScorecard struct {
	Scorecard      string `json:"scorecard"`
	ScorecardTitle string `json:"scorecardTitle"`
	EntityResult
}

func entityResults(ctx context.Context, db configdoc.DB, blueprintIdentifier, identifier string) ([]EntityScorecard, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.identifier, b.identifier, e.identifier, e.title, r.changed_at, `+evaluatedAt+`, r.tier, r.score,
			r.passed, r.failed, r.skipped, r.errored, r.criteria
		FROM scorecard_results r
		LEFT JOIN scorecard_evaluations v ON v.scorecard_id = r.scorecard_id
		JOIN config_documents s ON s.id = r.scorecard_id
		JOIN entities e ON e.id = r.entity_id
		JOIN config_documents b ON b.id = e.blueprint_id
		WHERE b.kind = 'blueprint' AND b.identifier = $1 AND e.identifier = $2
		ORDER BY s.identifier`, blueprintIdentifier, identifier)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []EntityScorecard{}
	for rows.Next() {
		var sr EntityScorecard
		r, err := scanResult(func(dest ...any) error { return rows.Scan(append([]any{&sr.Scorecard}, dest...)...) })
		if err != nil {
			return nil, err
		}
		sr.EntityResult = r
		out = append(out, sr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sources, err := configdoc.NewStore(db, Name).PublishedSources(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		spec, _ := Parse(sources[out[i].Scorecard])
		out[i].ScorecardTitle = spec.Title
	}

	return out, nil
}
