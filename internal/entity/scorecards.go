package entity

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// Result is an entity's result on a scorecard, as its scorecard property
// holds it: what paths read after a dot, and jq reads as an object.
type Result struct {
	// Tier is the highest tier reached, or "" for none.
	Tier    string `json:"tier"`
	Score   *int   `json:"score"`
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
	Skipped int    `json:"skipped"`
	Errored int    `json:"errored"`
	// Failing are the identifiers of the checks it fails.
	Failing []string `json:"failing"`
	// Checks are each check's status, by identifier.
	Checks map[string]string `json:"checks"`
}

// resolveScorecards sets each scorecard property of spec to the entity's
// result, loading every one for the page in one query. An entity the
// scorecard hasn't evaluated has no value.
func (s Store) resolveScorecards(ctx context.Context, spec blueprint.Spec, entities []*Entity) error {
	byScorecard := map[string][]string{}
	var names []string
	for _, prop := range spec.Properties {
		if prop.IsScorecard() {
			if _, ok := byScorecard[prop.Scorecard]; !ok {
				names = append(names, prop.Scorecard)
			}
			byScorecard[prop.Scorecard] = append(byScorecard[prop.Scorecard], prop.ID)
		}
	}
	if len(names) == 0 || len(entities) == 0 {
		return nil
	}
	ids := make([]string, len(entities))
	byID := make(map[string]*Entity, len(entities))
	for i, e := range entities {
		ids[i], byID[e.ID] = e.ID, e
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.entity_id, s.identifier, r.tier, r.score,
 r.passed, r.failed, r.skipped, r.errored, r.criteria
 FROM scorecard_results r JOIN config_documents s ON s.id = r.scorecard_id
 WHERE r.entity_id = ANY($1::uuid[]) AND s.kind = 'scorecard' AND s.published_version IS NOT NULL
 AND s.identifier = ANY($2)`, pq.Array(ids), pq.Array(names))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, name string
			result   Result
			criteria []byte
		)
		if err := rows.Scan(&id, &name, &result.Tier, &result.Score, &result.Passed, &result.Failed,
			&result.Skipped, &result.Errored, &criteria); err != nil {
			return err
		}
		var statuses map[string]struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(criteria, &statuses); err != nil {
			return err
		}
		result.Failing, result.Checks = []string{}, make(map[string]string, len(statuses))
		for check, outcome := range statuses {
			result.Checks[check] = outcome.Status
			if outcome.Status == "failed" {
				result.Failing = append(result.Failing, check)
			}
		}
		slices.Sort(result.Failing)
		value := result.object()
		for _, prop := range byScorecard[name] {
			byID[id].Properties[prop] = value
		}
	}

	return rows.Err()
}

// object is the result as JSON decodes it, which jq and the API read.
func (r Result) object() map[string]any {
	encoded, _ := json.Marshal(r)
	var out map[string]any
	_ = json.Unmarshal(encoded, &out)

	return out
}
