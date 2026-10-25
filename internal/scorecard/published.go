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

// Tiers counts the scored entities matching filter by the highest tier they
// reached, lowest first after those that reached none.
func Tiers(ctx context.Context, db configdoc.DB, spec Spec, bp blueprint.Spec, filter *entity.Filter) ([]TierCount, error) {
	id, err := configdoc.NewStore(db, Name).PublishedID(ctx, spec.Identifier)
	if err != nil {
		return nil, err
	}
	matching, args, err := entity.Matching(bp, filter, []any{id})
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT tier, count(*) FROM scorecard_results
		WHERE scorecard_id = $1 AND entity_id IN (`+matching+`)
		GROUP BY tier`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var (
			tier  string
			count int
		)
		if err := rows.Scan(&tier, &count); err != nil {
			return nil, err
		}
		counts[tier] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tiers := []TierCount{{Name: "", Count: counts[""]}}
	for _, tier := range spec.Tiers {
		tiers = append(tiers, TierCount{Name: tier.Name, Count: counts[tier.Name]})
	}

	return tiers, nil
}
