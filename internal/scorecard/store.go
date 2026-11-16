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
	"github.com/dvjn/goldenportal/internal/keyset"
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
	Sort string
	// Cursor is a page's link to the page to read, empty for the first.
	Cursor   string
	PageSize int
}

type ResultPage = keyset.Page[EntityResult]

// ResultsList names the list of a scorecard's results, which cursors say
// they page.
func ResultsList(scorecard string) string { return ResultsListPrefix + scorecard }

// ResultsListPrefix starts the name of each scorecard's ResultsList.
const ResultsListPrefix = "results:"

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
	filter, err := json.Marshal(q.Filter)
	if err != nil {
		return ResultPage{}, err
	}
	list := keyset.List{Name: ResultsList(spec.Identifier), Scope: []string{q.Search, string(filter), q.Sort}}
	request, err := keyset.NewRequest(resultOrder(q.Sort), list, q.Cursor, q.PageSize)
	if err != nil {
		return ResultPage{}, err
	}

	page, err := keyset.Fetch(request, func(p keyset.Plan) ([]keyset.Row[EntityResult], error) {
		return readResults(ctx, db, spec, matching, slices.Clone(args), p)
	})
	if err != nil {
		return ResultPage{}, err
	}
	err = page.Count(request, func() (total int, err error) {
		err = db.QueryRowContext(ctx, `SELECT count(*)`+resultFrom+matching, args...).Scan(&total)
		return total, err
	})

	return page, err
}

// readResults reads a plan's rows of the results matching, with args its
// values. No index holds their order, so its ranges are read together.
func readResults(ctx context.Context, db configdoc.DB, spec Spec, matching string, args []any, p keyset.Plan) ([]keyset.Row[EntityResult], error) {
	arg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	after := keyset.Predicate(p.Ranges(arg))
	rows, err := db.QueryContext(ctx, `
		SELECT `+arg(spec.Blueprint)+`::text, e.identifier, e.title, r.changed_at, `+evaluatedAt+`, r.tier, r.score,
			r.passed, r.failed, r.skipped, r.criteria, `+strings.Join(p.Order.Keys(), ", ")+resultFrom+`
		LEFT JOIN scorecard_evaluations v ON v.scorecard_id = r.scorecard_id`+matching+`
			AND `+after+`
		ORDER BY `+p.Order.SQL()+`
		LIMIT `+arg(p.Limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []keyset.Row[EntityResult]{}
	for rows.Next() {
		row, err := keyset.ReadRow(rows.Scan, len(p.Order), scanResult)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}

	return out, rows.Err()
}

// resultOrder is the order of a ResultQuery's Sort, tiers going by their
// stored place up the ladder. Results without a score count as the
// lowest. Ties go by tier the same way, then by title and identifier so
// pages are stable.
func resultOrder(sort string) keyset.Order {
	field, desc := strings.CutPrefix(sort, "-")
	tier := keyset.Column{SQL: "r.tier_rank", Type: keyset.Number, Desc: desc}
	score := keyset.Column{SQL: "r.score", Type: keyset.Number, Desc: desc, Nullable: true}
	var by keyset.Order
	switch field {
	case "score":
		by = keyset.Order{score, tier}
	case "passed", "failed", "skipped":
		by = keyset.Order{{SQL: "r." + field, Type: keyset.Number, Desc: desc}, tier, score}
	default:
		by = keyset.Order{tier, score}
	}

	return append(by, keyset.Column{SQL: "e.title", Type: keyset.Text}, keyset.Column{SQL: "e.identifier", Type: keyset.Text})
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
