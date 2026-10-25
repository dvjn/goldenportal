package scorecard

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
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
// the results, dropping those of entities no longer in scope.
func Evaluate(ctx context.Context, db TxDB, name string) (int, error) {
	record, err := configdoc.NewStore(db, Name).Get(ctx, name)
	if err != nil {
		return 0, err
	}
	if !record.Published() {
		return 0, nil
	}
	spec, _ := Parse(record.PublishedSource)

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var locked bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('scorecard-evaluate:' || $1))`,
		name).Scan(&locked); err != nil {
		return 0, err
	}
	if !locked {
		return 0, ErrEvaluating
	}

	scored, err := evaluateIn(ctx, tx, spec)
	if err != nil {
		return 0, err
	}

	return scored, tx.Commit()
}

// evaluateIn scores and stores within db, which callers make a transaction.
func evaluateIn(ctx context.Context, db configdoc.DB, spec Spec) (int, error) {
	scorecardID, err := configdoc.NewStore(db, Name).PublishedID(ctx, spec.Identifier)
	if err != nil {
		return 0, err
	}
	specs, err := blueprint.Published(ctx, db)
	if err != nil {
		return 0, err
	}
	bp, ok := specs[spec.Blueprint]
	if !ok {
		_, err := db.ExecContext(ctx, `DELETE FROM scorecard_results WHERE scorecard_id = $1`, scorecardID)
		return 0, err
	}
	checks, err := compileSpec(spec)
	if err != nil {
		return 0, err
	}

	entities := entity.NewStore(db)
	scored := []string{}
	for page := 1; ; page++ {
		result, err := entities.List(ctx, entity.Query{Blueprint: bp, Filter: spec.Filter, Page: page, PageSize: evaluationPage})
		if err != nil {
			return 0, err
		}
		if err := entities.Resolve(ctx, specs, bp, result.Entities); err != nil {
			return 0, err
		}
		for _, e := range result.Entities {
			outcome := checks.evaluate(ctx, spec, Input(e))
			if err := saveResult(ctx, db, scorecardID, e.ID, outcome); err != nil {
				return 0, err
			}
			scored = append(scored, e.ID)
		}
		if len(result.Entities) < evaluationPage {
			break
		}
	}

	if _, err := db.ExecContext(ctx, `
		DELETE FROM scorecard_results WHERE scorecard_id = $1 AND NOT (entity_id = ANY($2::uuid[]))`,
		scorecardID, pq.Array(scored)); err != nil {
		return 0, err
	}

	return len(scored), nil
}

// Input is the document checks run against: the entity with its
// properties, including relations, computed values and resolved lookups.
func Input(e *entity.Entity) map[string]any {
	encoded, _ := json.Marshal(map[string]any{
		"identifier":  e.Identifier,
		"title":       e.Title,
		"blueprint":   e.Blueprint,
		"integration": e.Integration,
		"properties":  e.Properties,
		"createdAt":   e.CreatedAt,
		"updatedAt":   e.UpdatedAt,
	})
	var input map[string]any
	_ = json.Unmarshal(encoded, &input)

	return input
}

func saveResult(ctx context.Context, db configdoc.DB, scorecardID, entityID string, r Result) error {
	criteria, err := json.Marshal(r.Criteria)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO scorecard_results (scorecard_id, entity_id, tier, score, passed, failed, skipped, errored, criteria, evaluated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		ON CONFLICT (scorecard_id, entity_id) DO UPDATE
			SET tier = excluded.tier, score = excluded.score, passed = excluded.passed, failed = excluded.failed,
				skipped = excluded.skipped, errored = excluded.errored, criteria = excluded.criteria,
				evaluated_at = excluded.evaluated_at`,
		scorecardID, entityID, r.Tier, r.Score, r.Passed, r.Failed, r.Skipped, r.Errored, criteria)

	return err
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
	rows, err := db.QueryContext(ctx, `
		SELECT tier, count(*), max(evaluated_at) FROM scorecard_results WHERE scorecard_id = $1 GROUP BY tier`, scorecardID)
	if err != nil {
		return Summary{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			tier  string
			count int
			at    time.Time
		)
		if err := rows.Scan(&tier, &count, &at); err != nil {
			return Summary{}, err
		}
		tiers[tier] = count
		summary.Entities += count
		if summary.EvaluatedAt == nil || at.After(*summary.EvaluatedAt) {
			summary.EvaluatedAt = &at
		}
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

// EntityResult is one entity's result on a scorecard.
type EntityResult struct {
	Blueprint   string    `json:"blueprint"`
	Identifier  string    `json:"identifier"`
	Title       string    `json:"title"`
	EvaluatedAt time.Time `json:"evaluatedAt"`
	Result
}

// ResultQuery picks a page of a scorecard's results, worst tier first.
type ResultQuery struct {
	// Tier filters to entities whose highest tier it is; "none" for those with none.
	Tier     string
	Search   string
	Page     int
	PageSize int
}

type ResultPage struct {
	Results []EntityResult `json:"results"`
	Total   int            `json:"total"`
}

func results(ctx context.Context, db configdoc.DB, scorecardID string, spec Spec, q ResultQuery) (ResultPage, error) {
	tierOrder := []string{""}
	for _, tier := range spec.Tiers {
		tierOrder = append(tierOrder, tier.Name)
	}
	tier := q.Tier
	if tier == "none" {
		tier = ""
	}
	search := ""
	if q.Search != "" {
		search = "%" + likeEscaper.Replace(q.Search) + "%"
	}

	rows, err := db.QueryContext(ctx, `
		SELECT b.identifier, e.identifier, e.title, r.evaluated_at, r.tier, r.score,
			r.passed, r.failed, r.skipped, r.errored, r.criteria, count(*) OVER ()
		FROM scorecard_results r
		JOIN entities e ON e.id = r.entity_id
		JOIN config_documents b ON b.id = e.blueprint_id
		WHERE r.scorecard_id = $1
			AND ($2 = false OR r.tier = $3)
			AND ($4 = '' OR e.identifier ILIKE $4 OR e.title ILIKE $4)
		ORDER BY array_position($5::text[], r.tier), r.score NULLS FIRST, e.title, e.identifier
		LIMIT $6 OFFSET $7`,
		scorecardID, q.Tier != "", tier, search, pq.Array(tierOrder), q.PageSize, (q.Page-1)*q.PageSize)
	if err != nil {
		return ResultPage{}, err
	}
	defer rows.Close()

	page := ResultPage{Results: []EntityResult{}}
	for rows.Next() {
		r, err := scanResult(rows.Scan, &page.Total)
		if err != nil {
			return ResultPage{}, err
		}
		page.Results = append(page.Results, r)
	}

	return page, rows.Err()
}

func scanResult(scan func(...any) error, extra ...any) (EntityResult, error) {
	var (
		r        EntityResult
		criteria []byte
	)
	if err := scan(append([]any{
		&r.Blueprint, &r.Identifier, &r.Title, &r.EvaluatedAt, &r.Tier, &r.Score,
		&r.Passed, &r.Failed, &r.Skipped, &r.Errored, &criteria,
	}, extra...)...); err != nil {
		return EntityResult{}, err
	}
	err := json.Unmarshal(criteria, &r.Criteria)

	return r, err
}

// scorecardResult is an entity's result on one scorecard, for its page.
type scorecardResult struct {
	Scorecard      string `json:"scorecard"`
	ScorecardTitle string `json:"scorecardTitle"`
	EntityResult
}

func entityResults(ctx context.Context, db configdoc.DB, blueprintIdentifier, identifier string) ([]scorecardResult, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.identifier, b.identifier, e.identifier, e.title, r.evaluated_at, r.tier, r.score,
			r.passed, r.failed, r.skipped, r.errored, r.criteria
		FROM scorecard_results r
		JOIN config_documents s ON s.id = r.scorecard_id
		JOIN entities e ON e.id = r.entity_id
		JOIN config_documents b ON b.id = e.blueprint_id
		WHERE b.kind = 'blueprint' AND b.identifier = $1 AND e.identifier = $2
		ORDER BY s.identifier`, blueprintIdentifier, identifier)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []scorecardResult{}
	for rows.Next() {
		var sr scorecardResult
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
