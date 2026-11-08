package integration

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

// CheckRetention refuses retention on a blueprint any integration writes
// with a reconcile mapping, since a complete sync reads that mapping's
// resource in full and writes back what retention deleted. It checks the
// published integrations, with any applied together with the blueprint in
// their place.
func CheckRetention(ctx context.Context, db configdoc.DB, spec blueprint.Spec) ([]configdoc.Issue, error) {
	if spec.Retention == nil {
		return nil, nil
	}
	sources, err := configdoc.NewStore(db, Name).PublishedSources(ctx)
	if err != nil {
		return nil, err
	}
	maps.Copy(sources, configdoc.PendingSources(ctx, DocumentKind))
	var reconciling []string
	for _, identifier := range slices.Sorted(maps.Keys(sources)) {
		integration, _ := Parse(sources[identifier])
		if slices.ContainsFunc(integration.Mappings, func(m Mapping) bool { return m.Blueprint == spec.Identifier && !m.Upsert() }) {
			reconciling = append(reconciling, identifier)
		}
	}
	if len(reconciling) == 0 {
		return nil, nil
	}

	return []configdoc.Issue{{
		Severity: configdoc.SeverityError, Path: "spec.retention",
		Message: fmt.Sprintf("%s has retention, so its mappings must use mode: upsert, and %s %s with mode: reconcile",
			spec.Identifier, joinNames(reconciling), writes(len(reconciling))),
	}}, nil
}

func writes(n int) string {
	if n == 1 {
		return "writes it"
	}

	return "write it"
}

// joinNames lists names as a sentence does, such as a, b and c.
func joinNames(names []string) string {
	if len(names) == 1 {
		return names[0]
	}

	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
