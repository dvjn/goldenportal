package scorecard

import (
	"context"
	"errors"
	"fmt"

	"github.com/dvjn/goldenportal/internal/jq"
)

// Criterion outcomes. Skipped is neutral; errored blocks its tier without
// counting as a fail, since the check is at fault, not the entity.
const (
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
	StatusErrored = "errored"
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
	Errored  int                        `json:"errored"`
	Criteria map[string]CriterionResult `json:"criteria"`
}

// CriterionResult is how an entity did on one criterion. Message says why
// it errored.
type CriterionResult struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// compiled holds a scorecard's checks, compiled once for all entities.
type compiled struct {
	check, when map[string]*jq.Code
}

func compileSpec(spec Spec) (compiled, error) {
	c := compiled{check: map[string]*jq.Code{}, when: map[string]*jq.Code{}}
	for _, tier := range spec.Tiers {
		for _, criterion := range tier.Criteria {
			code, err := jq.Compile(criterion.Check)
			if err != nil {
				return c, fmt.Errorf("%s check: %w", criterion.ID, err)
			}
			c.check[criterion.ID] = code
			if criterion.When != "" {
				if c.when[criterion.ID], err = jq.Compile(criterion.When); err != nil {
					return c, fmt.Errorf("%s when: %w", criterion.ID, err)
				}
			}
		}
	}

	return c, nil
}

// evaluate scores one entity, given as the document checks run against.
func (c compiled) evaluate(ctx context.Context, spec Spec, input map[string]any) Result {
	result := Result{Criteria: map[string]CriterionResult{}}
	reaching := true
	for _, tier := range spec.Tiers {
		met := true
		for _, criterion := range tier.Criteria {
			outcome := c.run(ctx, criterion.ID, input)
			result.Criteria[criterion.ID] = outcome
			switch outcome.Status {
			case StatusPassed:
				result.Passed++
			case StatusFailed:
				result.Failed++
				met = false
			case StatusSkipped:
				result.Skipped++
			case StatusErrored:
				result.Errored++
				met = false
			}
		}
		if reaching && met {
			result.Tier = tier.Name
		} else {
			reaching = false
		}
	}

	if applicable := result.Passed + result.Failed + result.Errored; applicable > 0 {
		// Whole numbers round down, so 100 means every check passed.
		score := 100 * result.Passed / applicable
		result.Score = &score
	}

	return result
}

func (c compiled) run(ctx context.Context, id string, input map[string]any) CriterionResult {
	if when, ok := c.when[id]; ok {
		applies, err := runBool(ctx, when, input)
		if err != nil {
			return CriterionResult{Status: StatusErrored, Message: "when: " + err.Error()}
		}
		if !applies {
			return CriterionResult{Status: StatusSkipped}
		}
	}

	passed, err := runBool(ctx, c.check[id], input)
	switch {
	case err != nil:
		return CriterionResult{Status: StatusErrored, Message: err.Error()}
	case passed:
		return CriterionResult{Status: StatusPassed}
	}

	return CriterionResult{Status: StatusFailed}
}

var errNotBool = errors.New("gave something other than true or false")

// runBool runs a check and takes its first result, which must be a boolean.
func runBool(ctx context.Context, code *jq.Code, input map[string]any) (bool, error) {
	value, ok, err := code.First(ctx, jq.DefaultTimeout, input)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, errors.New("gave no result")
	}
	passed, ok := value.(bool)
	if !ok {
		return false, errNotBool
	}

	return passed, nil
}
