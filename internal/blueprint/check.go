package blueprint

import (
	"fmt"
	"slices"
	"strings"
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
// values a lookup can end on. Lookups of lookups aren't followed.
func (s Spec) hasValue(id string) bool {
	prop, ok := s.property(id)

	return ok && prop.Lookup == "" && !prop.IsRelation()
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
