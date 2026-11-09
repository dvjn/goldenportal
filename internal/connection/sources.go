package connection

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// CheckSources checks a blueprint's sources against the published
// connections, with any applied together with the blueprint in their place.
// A connection not published yet is a warning, since the blueprint may come
// first; a resource its connector doesn't offer, or can't read as it's set
// up, is an error.
func CheckSources(ctx context.Context, db configdoc.DB, bp blueprint.Spec) ([]configdoc.Issue, error) {
	if len(bp.Sources) == 0 {
		return nil, nil
	}
	sources, err := configdoc.NewStore(db, Name).PublishedSources(ctx)
	if err != nil {
		return nil, err
	}
	maps.Copy(sources, configdoc.PendingSources(ctx, DocumentKind))
	var issues []configdoc.Issue
	for i, src := range bp.Sources {
		path := fmt.Sprintf("spec.sources[%d]", i)
		source, ok := sources[src.Connection]
		if !ok {
			if configdoc.ValidIdentifier(src.Connection) {
				issues = append(issues, configdoc.Issue{
					Severity: configdoc.SeverityWarning, Path: path + ".connection", Line: src.Line(),
					Message: fmt.Sprintf("no connection %q is published yet, so this source reads nothing until it is", src.Connection),
				})
			}
			continue
		}
		spec, _ := Parse(source)
		if spec.config == nil {
			continue
		}
		if problem := resourceProblemOf(spec.config, src.Resource); problem != "" {
			issues = append(issues, configdoc.Issue{
				Severity: configdoc.SeverityError, Path: path + ".resource", Line: src.Line(), Message: problem,
			})
		}
	}

	return issues, nil
}

// resourceProblemOf says why config can't read resource, or nothing when it
// can.
func resourceProblemOf(config Config, resource string) string {
	resources := config.Resources()
	if !slices.Contains(resources, resource) {
		return "resource must be one of " + strings.Join(resources, ", ")
	}
	if rp, ok := config.(resourceProblemer); ok {
		return rp.resourceProblem(resource)
	}

	return ""
}

// checkSources checks that the connector still reads what the published
// blueprints' sources, with any pending in their place, map from it.
func checkSources(spec Spec, published map[string]blueprint.Spec) []configdoc.Issue {
	if spec.config == nil {
		return nil
	}
	var issues []configdoc.Issue
	for _, id := range slices.Sorted(maps.Keys(published)) {
		for _, src := range published[id].Sources {
			if src.Connection != spec.Identifier {
				continue
			}
			problem := resourceProblemOf(spec.config, src.Resource)
			if problem == "" {
				continue
			}
			message := fmt.Sprintf("%s's sources read %s: %s", id, src.Resource, problem)
			if !slices.Contains(spec.config.Resources(), src.Resource) {
				message = fmt.Sprintf("%s's sources read %s, which the %s connector doesn't offer", id, src.Resource, spec.Connector.Type)
			}
			issues = append(issues, configdoc.Issue{Severity: configdoc.SeverityError, Path: "spec.connector", Message: message})
		}
	}

	return issues
}

// deletable refuses while published blueprints' sources read the
// connection, since they'd read nothing.
func deletable(ctx context.Context, db configdoc.DB, identifier string) error {
	published, err := blueprint.Published(ctx, db)
	if err != nil {
		return err
	}
	var reading []string
	for _, id := range slices.Sorted(maps.Keys(published)) {
		if slices.ContainsFunc(published[id].Sources, func(s blueprint.Source) bool { return s.Connection == identifier }) {
			reading = append(reading, id)
		}
	}
	if len(reading) == 0 {
		return nil
	}

	return httpx.Conflict("remove the sources that read it from " + joinNames(reading) + " first")
}

// joinNames lists names as a sentence does, such as a, b and c.
func joinNames(names []string) string {
	if len(names) == 1 {
		return names[0]
	}

	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
