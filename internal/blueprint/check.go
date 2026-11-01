package blueprint

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Check finds problems that depend on other blueprints: relation targets and
// lookup paths. published holds the published blueprints by identifier.
// Unknown targets are warnings, so blueprints that relate to each other can
// be published in either order.
func Check(spec Spec, published map[string]Spec) []Issue {
	find := func(identifier string) (Spec, bool) {
		if identifier == spec.Identifier {
			return spec, true
		}
		other, ok := published[identifier]
		return other, ok
	}

	var issues []Issue
	for _, rel := range spec.Properties {
		if !rel.IsRelation() {
			continue
		}
		if _, ok := find(rel.Relation); !ok && ValidIdentifier(rel.Relation) {
			issues = append(issues, Issue{
				Severity: SeverityWarning, Path: "spec.properties." + rel.ID + ".relation", Line: rel.line,
				Message: fmt.Sprintf("no blueprint %q is published yet, so these relations won't resolve until it is", rel.Relation),
			})
		}
	}

	for _, lookup := range spec.Properties {
		if lookup.Lookup == "" {
			continue
		}
		segments := strings.Split(lookup.Lookup, ".")
		if len(segments) < 2 || len(segments) > MaxLookupHops+1 {
			continue
		}
		issue := func(severity, format string, args ...any) {
			issues = append(issues, Issue{
				Severity: severity, Path: "spec.properties." + lookup.ID + ".lookup",
				Line: lookup.line, Message: fmt.Sprintf(format, args...),
			})
		}

		current := spec
		for i, segment := range segments {
			if i == len(segments)-1 {
				if !slices.Contains(BuiltIns, segment) && !current.hasValue(segment) {
					issue(SeverityError, "%s has no property %q to look up", current.Identifier, segment)
				}
				break
			}
			rel, ok := current.relation(segment)
			if !ok {
				issue(SeverityError, "%s has no relation %q", current.Identifier, segment)
				break
			}
			if rel.Many {
				issue(SeverityError, "%s.%s relates to many entities, so it can't be looked up through", current.Identifier, segment)
				break
			}
			next, ok := find(rel.Relation)
			if !ok {
				issue(SeverityWarning, "this can't be checked until %q is published", rel.Relation)
				break
			}
			current = next
		}
	}

	return issues
}

func (s Spec) relation(id string) (Property, bool) {
	prop, ok := s.property(id)

	return prop, ok && prop.IsRelation()
}

func (s Spec) property(id string) (Property, bool) {
	for _, prop := range s.Properties {
		if prop.ID == id {
			return prop, true
		}
	}

	return Property{}, false
}

// hasValue reports whether id is a stored or computed property, the
// values a lookup can end on. Lookups of lookups aren't followed, and a
// scorecard's result is read by path, as a column or filter.
func (s Spec) hasValue(id string) bool {
	prop, ok := s.property(id)

	return ok && prop.Lookup == "" && !prop.IsRelation() && !prop.IsScorecard()
}

// RelatesTo reports whether any relation of s targets identifier.
func (s Spec) RelatesTo(identifier string) bool {
	return slices.ContainsFunc(s.Properties, func(prop Property) bool { return prop.Relation == identifier })
}

// HasProperty reports whether id is a stored property or relation, which integrations write.
func (s Spec) HasProperty(id string) bool {
	prop, ok := s.property(id)
	return ok && !prop.Derived()
}

// Property returns the property named id.
func (s Spec) Property(id string) (Property, bool) { return s.property(id) }

// Ladder is what blueprints need of a published scorecard, which this
// package can't import: the blueprint it scores and its tiers, lowest first.
type Ladder struct {
	Blueprint string `json:"blueprint"`
	Tiers     []Rung `json:"tiers"`
}

// Rung is one tier of a ladder.
type Rung struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Color string `json:"color,omitempty"`
}

// Names lists the tier names lowest first, after "" for no tier, which is
// the order tiers sort in.
func (l Ladder) Names() []string {
	names := []string{""}
	for _, rung := range l.Tiers {
		names = append(names, rung.Name)
	}

	return names
}

// Ladders reads every published scorecard's ladder, by identifier, with any
// that ctx has pending in their place. Sources that don't read are left out.
func Ladders(ctx context.Context, db configdoc.DB) (map[string]Ladder, error) {
	sources, err := configdoc.NewStore(db, "scorecard").PublishedSources(ctx)
	if err != nil {
		return nil, err
	}
	for name, source := range configdoc.PendingSources(ctx, "Scorecard") {
		sources[name] = source
	}
	ladders := map[string]Ladder{}
	for identifier, source := range sources {
		r := &configdoc.Reader{}
		doc := configdoc.ReadDocument(r, source, "Scorecard")
		target, tiers := configdoc.Field(doc.Spec, "blueprint"), configdoc.Field(doc.Spec, "tiers")
		if configdoc.HasErrors(r.Issues) || target == nil || tiers == nil {
			continue
		}
		ladder := Ladder{Blueprint: target.Value}
		if tiers.Decode(&ladder.Tiers) != nil {
			continue
		}
		for i, rung := range ladder.Tiers {
			if rung.Title == "" {
				ladder.Tiers[i].Title = rung.Name
			}
		}
		ladders[identifier] = ladder
	}

	return ladders, nil
}

// CheckScorecards checks each scorecard property names a published
// scorecard of this blueprint. One not yet published is a warning, so a
// blueprint and its scorecard can be published in either order.
func CheckScorecards(spec Spec, ladders map[string]Ladder) []Issue {
	var issues []Issue
	for _, prop := range spec.Properties {
		if !prop.IsScorecard() {
			continue
		}
		path := "spec.properties." + prop.ID + ".scorecard"
		switch ladder, ok := ladders[prop.Scorecard]; {
		case !ok:
			issues = append(issues, Issue{
				Severity: SeverityWarning, Path: path, Line: prop.line,
				Message: fmt.Sprintf("no scorecard %q is published yet, so %s stays empty until it is", prop.Scorecard, prop.ID),
			})
		case ladder.Blueprint != spec.Identifier:
			issues = append(issues, Issue{
				Severity: SeverityError, Path: path, Line: prop.line,
				Message: fmt.Sprintf("%s scores %s, not %s", prop.Scorecard, ladder.Blueprint, spec.Identifier),
			})
		}
	}

	return issues
}
