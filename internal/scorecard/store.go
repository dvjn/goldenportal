package scorecard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// Name is the configdoc kind scorecards are stored as.
const Name = "scorecard"

// Summary is how a scorecard's entities are doing overall.
type Summary struct {
	Entities    int        `json:"entities"`
	EvaluatedAt *time.Time `json:"evaluatedAt"`
	// Evaluating is set while the results are for an older version than
	// the published one, as they are just after a publish.
	Evaluating bool          `json:"evaluating"`
	Tiers      []TierCount   `json:"tiers"`
	Criteria   []CriterionAt `json:"criteria"`
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
}

// resultFrom joins results to their entities as e, which a compiled filter
// reads.
const resultFrom = `
	FROM scorecard_results r
	JOIN entities e ON e.id = r.entity_id`

// Scope narrows a scorecard's results to the entities Filter matches, a
// filter on the blueprint the scorecard grades. Without a Filter it keeps
// every result.
type Scope struct {
	entity.Schema
	Filter *entity.Filter
}

// where is SQL for the scope on resultFrom, its values numbered after args.
func (s Scope) where(args []any) (string, []any, error) {
	if s.Filter == nil {
		return "true", args, nil
	}

	return entity.Where(s.Schema, *s.Filter, args)
}

func summarize(ctx context.Context, db configdoc.DB, scorecardID string, spec Spec, scope Scope) (Summary, error) {
	summary := Summary{Tiers: []TierCount{}, Criteria: []CriterionAt{}}
	tiers := map[string]int{}
	err := db.QueryRowContext(ctx, `
		SELECT v.last_evaluated_at, coalesce(v.evaluated_version, 0) < d.published_version
		FROM config_documents d LEFT JOIN scorecard_evaluations v ON v.scorecard_id = d.id
		WHERE d.id = $1`, scorecardID).Scan(&summary.EvaluatedAt, &summary.Evaluating)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Summary{}, err
	}
	where, args, err := scope.where([]any{scorecardID})
	if err != nil {
		return Summary{}, err
	}
	rows, err := db.QueryContext(ctx, `SELECT r.tier, count(*)`+resultFrom+`
		WHERE r.scorecard_id = $1 AND `+where+`
		GROUP BY r.tier`, args...)
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
		SELECT c.key, c.value ->> 'status', count(*)`+resultFrom+`
		CROSS JOIN LATERAL jsonb_each(r.criteria) c
		WHERE r.scorecard_id = $1 AND `+where+`
		GROUP BY 1, 2`, args...)
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

// ResultQuery picks a page of a scorecard's results in Scope, worst tier
// first unless Sort says otherwise.
type ResultQuery struct {
	Scope
	Search string
	// Sort is one of ResultSorts, or -field for descending.
	Sort     string
	Page     int
	PageSize int
}

type ResultPage = httpx.Page[EntityResult]

func results(ctx context.Context, db configdoc.DB, scorecardID string, spec Spec, q ResultQuery) (ResultPage, error) {
	search := ""
	if q.Search != "" {
		search = entity.TextPattern(q.Search)
	}
	where, args, err := q.where([]any{scorecardID, search})
	if err != nil {
		return ResultPage{}, err
	}
	matching := `
		WHERE r.scorecard_id = $1
			AND ($2 = '' OR lower(e.title) LIKE lower($2) OR lower(e.identifier) LIKE lower($2))
			AND ` + where
	matchArgs := slices.Clone(args)
	graded := "$" + strconv.Itoa(len(args)+1)
	limit := "$" + strconv.Itoa(len(args)+2)
	offset := "$" + strconv.Itoa(len(args)+3)
	skip := (q.Page - 1) * q.PageSize
	args = append(args, spec.Blueprint, q.PageSize, skip)

	rows, err := db.QueryContext(ctx, `
		SELECT `+graded+`::text, e.identifier, e.title, r.changed_at, `+evaluatedAt+`, r.tier, r.score,
			r.passed, r.failed, r.skipped, r.criteria`+resultFrom+`
		LEFT JOIN scorecard_evaluations v ON v.scorecard_id = r.scorecard_id`+matching+`
		ORDER BY `+resultOrder(q.Sort)+`
		LIMIT `+limit+` OFFSET `+offset, args...)
	if err != nil {
		return ResultPage{}, err
	}
	defer rows.Close()

	page := ResultPage{Items: []EntityResult{}}
	for rows.Next() {
		r, err := scanResult(rows.Scan)
		if err != nil {
			return ResultPage{}, err
		}
		page.Items = append(page.Items, r)
	}
	if err := rows.Err(); err != nil {
		return ResultPage{}, err
	}
	var known bool
	if page.Total, known = httpx.PageTotal(skip, len(page.Items), q.PageSize); !known {
		err = db.QueryRowContext(ctx, `SELECT count(*)`+resultFrom+matching,
			matchArgs...).Scan(&page.Total)
	}

	return page, err
}

// resultOrder is the ORDER BY for a ResultQuery's Sort, tiers going by
// their stored place up the ladder. Results without a score count as the
// lowest. Ties go by tier the same way, then by title and identifier so
// pages are stable.
func resultOrder(sort string) string {
	field, desc := strings.CutPrefix(sort, "-")
	direction, nulls := "ASC", "NULLS FIRST"
	if desc {
		direction, nulls = "DESC", "NULLS LAST"
	}
	tier := "r.tier_rank " + direction
	score := "r.score " + direction + " " + nulls
	var by []string
	switch field {
	case "score":
		by = []string{score, tier}
	case "passed", "failed", "skipped":
		by = []string{"r." + field + " " + direction, tier, score}
	default:
		by = []string{tier, score}
	}

	return strings.Join(append(by, "e.title", "e.identifier"), ", ")
}

func scanResult(scan func(...any) error) (EntityResult, error) {
	var (
		r        EntityResult
		criteria []byte
	)
	if err := scan(
		&r.Blueprint, &r.Identifier, &r.Title, &r.ChangedAt, &r.EvaluatedAt, &r.Tier, &r.Score,
		&r.Passed, &r.Failed, &r.Skipped, &criteria,
	); err != nil {
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
		SELECT s.identifier, $1::text, e.identifier, e.title, r.changed_at, `+evaluatedAt+`, r.tier, r.score,
			r.passed, r.failed, r.skipped, r.criteria
		FROM scorecard_results r
		LEFT JOIN scorecard_evaluations v ON v.scorecard_id = r.scorecard_id
		JOIN config_documents s ON s.id = r.scorecard_id
		JOIN entities e ON e.id = r.entity_id
		WHERE e.blueprint_id = `+entity.BlueprintID(blueprint.Spec{Identifier: blueprintIdentifier})+` AND e.identifier = $2
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
