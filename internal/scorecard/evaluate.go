package scorecard

import (
	"slices"
	"strconv"
	"strings"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/entity"
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

// The statuses as SQL literals.
const (
	passedSQL  = "'" + StatusPassed + "'"
	failedSQL  = "'" + StatusFailed + "'"
	skippedSQL = "'" + StatusSkipped + "'"
)

// evaluation is one statement that scores every entity of bp in spec's
// scope, writes the results that changed and drops those of entities out
// of scope. It gives how many entities it scored and results it wrote.
// Unchanged results aren't touched, since even an update that changes
// nothing locks and logs its row, and a tier keeps the time it was reached
// until it changes.
func evaluation(specs map[string]blueprint.Spec, bp blueprint.Spec, spec Spec, scorecardID string) (string, []any, error) {
	var ids []string
	for _, tier := range spec.Tiers {
		for _, c := range tier.Criteria {
			ids = append(ids, c.ID)
		}
	}
	args := []any{scorecardID, pq.Array(ids)}
	arg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	// Filters on other scorecards' tiers and checks were checked when
	// published; one since changed matches nothing rather than failing.
	where := func(f entity.Filter) (string, error) {
		var (
			sql string
			err error
		)
		sql, args, err = entity.Where(specs, nil, bp, f, args)
		return "coalesce(" + sql + ", false)", err
	}

	scope := "true"
	if spec.Filter != nil {
		var err error
		if scope, err = where(*spec.Filter); err != nil {
			return "", nil, err
		}
	}
	var statuses, reached []string
	for _, tier := range spec.Tiers {
		for _, c := range tier.Criteria {
			check, err := where(c.Check)
			if err != nil {
				return "", nil, err
			}
			status := "WHEN " + check + " THEN " + passedSQL + " ELSE " + failedSQL
			if c.When != nil {
				when, err := where(*c.When)
				if err != nil {
					return "", nil, err
				}
				status = "WHEN NOT " + when + " THEN " + skippedSQL + " " + status
			}
			statuses = append(statuses, "CASE "+status+" END")
		}
		// A tier is reached when none of its criteria, nor any below, failed.
		reached = append(reached, "WHEN NOT "+failedSQL+" = ANY(statuses[1:"+strconv.Itoa(len(statuses))+"]) THEN "+arg(tier.Name))
	}
	slices.Reverse(reached)

	return `
		WITH checked AS (
			SELECT e.id, ARRAY[` + strings.Join(statuses, ", ") + `]::text[] AS statuses
			FROM entities e
			WHERE e.blueprint_id = ` + entity.BlueprintID(bp) + ` AND ` + scope + `
		), counted AS (
			SELECT id AS entity_id, CASE ` + strings.Join(reached, " ") + ` ELSE '' END AS tier,
				cardinality(array_positions(statuses, ` + passedSQL + `)) AS passed,
				cardinality(array_positions(statuses, ` + failedSQL + `)) AS failed,
				cardinality(array_positions(statuses, ` + skippedSQL + `)) AS skipped,
				(SELECT jsonb_object_agg(k, jsonb_build_object('status', v)) FROM unnest($2::text[], statuses) AS c (k, v)) AS criteria
			FROM checked
		), scored AS (
			SELECT *, CASE WHEN passed + failed > 0 THEN 100 * passed / (passed + failed) END AS score FROM counted
		), written AS (
			INSERT INTO scorecard_results AS r (scorecard_id, entity_id, tier, score, passed, failed, skipped, criteria)
			SELECT $1::uuid, s.entity_id, s.tier, s.score, s.passed, s.failed, s.skipped, s.criteria
			FROM scored s
			LEFT JOIN scorecard_results o ON o.scorecard_id = $1::uuid AND o.entity_id = s.entity_id
			WHERE o.entity_id IS NULL OR (o.tier, o.score, o.passed, o.failed, o.skipped, o.criteria)
				IS DISTINCT FROM (s.tier, s.score, s.passed, s.failed, s.skipped, s.criteria)
			ORDER BY s.entity_id
			ON CONFLICT (scorecard_id, entity_id) DO UPDATE
				SET tier = excluded.tier, score = excluded.score, passed = excluded.passed, failed = excluded.failed,
					skipped = excluded.skipped, criteria = excluded.criteria,
					changed_at = CASE WHEN r.tier = excluded.tier THEN r.changed_at ELSE now() END
			RETURNING 1
		), removed AS (
			DELETE FROM scorecard_results r
			WHERE r.scorecard_id = $1::uuid AND NOT EXISTS (SELECT 1 FROM checked c WHERE c.id = r.entity_id)
		)
		SELECT (SELECT count(*) FROM checked), (SELECT count(*) FROM written)`, args, nil
}
