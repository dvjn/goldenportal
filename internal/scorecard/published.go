package scorecard

import (
	"context"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
)

// Published returns every published scorecard, by identifier, with any that
// ctx has pending in their place. Sources that don't parse are left out.
func Published(ctx context.Context, db configdoc.DB) (map[string]Spec, error) {
	sources, err := configdoc.NewStore(db, Name).PublishedSources(ctx)
	if err != nil {
		return nil, err
	}
	for name, source := range configdoc.PendingSources(ctx, DocumentKind) {
		sources[name] = source
	}
	specs := map[string]Spec{}
	for identifier, source := range sources {
		if spec, issues := Parse(source); !configdoc.HasErrors(issues) {
			specs[identifier] = spec
		}
	}

	return specs, nil
}

// Checks counts the outcomes of a scorecard's criteria across entities.
// Failed takes in those that errored, since they didn't pass either, and
// skipped ones aren't counted.
type Checks struct {
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// Tiers counts the scored entities matching filter by the highest tier they
// reached, lowest first after those that reached none, and how their checks
// came out. specs are the blueprints the filter's relations lead to.
func Tiers(ctx context.Context, db configdoc.DB, spec Spec, specs map[string]blueprint.Spec, bp blueprint.Spec, filter *entity.Filter) ([]TierCount, Checks, error) {
	var checks Checks
	id, err := configdoc.NewStore(db, Name).PublishedID(ctx, spec.Identifier)
	if err != nil {
		return nil, checks, err
	}
	matching, args, err := entity.Matching(specs, bp, filter, []any{id})
	if err != nil {
		return nil, checks, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT tier, count(*), sum(passed), sum(failed + errored) FROM scorecard_results
		WHERE scorecard_id = $1 AND entity_id IN (`+matching+`)
		GROUP BY tier`, args...)
	if err != nil {
		return nil, checks, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var (
			tier                  string
			count, passed, failed int
		)
		if err := rows.Scan(&tier, &count, &passed, &failed); err != nil {
			return nil, checks, err
		}
		counts[tier] = count
		checks.Passed += passed
		checks.Failed += failed
	}
	if err := rows.Err(); err != nil {
		return nil, checks, err
	}

	tiers := []TierCount{{Name: "", Count: counts[""]}}
	for _, tier := range spec.Tiers {
		tiers = append(tiers, TierCount{Name: tier.Name, Count: counts[tier.Name]})
	}

	return tiers, checks, nil
}
