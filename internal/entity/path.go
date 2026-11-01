package entity

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/widget"
)

// field is where a path's value lives in SQL.
type field struct {
	// value is a jsonb expression, or a typed column for built-ins and
	// what scorecards store.
	value    string
	column   bool
	nullable bool
	many     bool
	kind     string
	// relation is the blueprint a relation points at.
	relation string
	// scorecard is the scorecard a tier is on.
	scorecard string
	// prop is the property the path ends on, and builtIn the built-in
	// field, for showing its values.
	prop    *blueprint.Property
	builtIn string
	// title names what the path ends on, for a column's heading.
	title string
	// jsonb and key say which of the entity's jsonb columns holds the
	// value, under which key, for conditions its GIN index can answer.
	jsonb, key string
}

// Kinds beside a property's type.
const (
	kindDate = "date-time"
	kindTier = "tier"
)

// ScorecardFields are what a dot after a scorecard property reads of the
// entity's result, beside checks.<id>, a check's status.
var ScorecardFields = []string{"score", "passed", "failed", "skipped", "errored", "failing"}

// reader finds what paths read, from an entity of one blueprint through
// the relations specs lead to.
type reader struct {
	specs map[string]blueprint.Spec
}

func (r reader) spec(identifier string) (blueprint.Spec, bool) {
	spec, ok := r.specs[identifier]
	return spec, ok
}

// fieldFor is where a path's value lives, read from e, an entity of spec.
func fieldFor(specs map[string]blueprint.Spec, spec blueprint.Spec, path string) (field, error) {
	return reader{specs: specs}.read(spec, "e", path, 0)
}

// read resolves path from alias, an entity of spec reached through hops
// relations. A relation it follows is a scalar subquery, so the value
// comes back one per row; a many-relation can't be read through that way.
func (r reader) read(spec blueprint.Spec, alias, path string, hops int) (field, error) {
	if err := widget.OldPath(path); err != nil {
		return field{}, err
	}
	name, rest, nested := strings.Cut(path, ".")
	if !nested && slices.Contains(blueprint.BuiltIns, name) {
		return builtInField(spec, alias, name), nil
	}
	prop, ok := spec.Property(name)
	if !ok {
		return field{}, fmt.Errorf("%s has no property %q", spec.Identifier, name)
	}
	switch {
	case prop.Lookup != "":
		if nested {
			return r.read(spec, alias, prop.Lookup+"."+rest, hops)
		}
		return r.read(spec, alias, prop.Lookup, hops)
	case prop.IsScorecard():
		if nested && hops >= MaxFilterHops {
			return field{}, fmt.Errorf("%s follows more than %d relations and scorecards", path, MaxFilterHops)
		}
		return scorecardField(alias, prop, rest)
	case !nested:
		return propertyField(alias, prop), nil
	case !prop.IsRelation():
		return field{}, fmt.Errorf("%s follows %s, which isn't a relation or a scorecard", path, name)
	case prop.Many:
		return field{}, fmt.Errorf("%s relates to many entities, so a path can't read through it; filter on it instead", name)
	case hops >= MaxFilterHops:
		return field{}, fmt.Errorf("%s follows more than %d relations and scorecards", path, MaxFilterHops)
	}
	target, ok := r.spec(prop.Relation)
	if !ok {
		return field{}, fmt.Errorf("%s follows %s to %s, which isn't published", path, name, prop.Relation)
	}
	inner := "h" + strconv.Itoa(hops+1)
	fld, err := r.read(target, inner, rest, hops+1)
	if err != nil {
		return field{}, err
	}
	fld.value = "(SELECT " + fld.value + " FROM entities " + inner + " WHERE " + inner + ".blueprint_id = " +
		blueprintID(target.Identifier) + " AND " + inner + ".identifier = " + alias + ".relations ->> " + quoteLiteral(prop.ID) + ")"
	fld.nullable = true
	// A related entity's name is best headed by the relation that leads there.
	if fld.builtIn == "title" || fld.builtIn == "identifier" {
		fld.title = prop.Title
	}

	return fld, nil
}

// blueprintID is SQL for a published blueprint's row, which the planner
// works out once per statement.
func blueprintID(identifier string) string {
	return "(SELECT id FROM config_documents WHERE kind = 'blueprint' AND identifier = " + quoteLiteral(identifier) + ")"
}

// builtInField reads a field every entity has. At the top of a query the
// blueprint and integration are joined in; further along they're looked up.
// builtInTitles head built-in fields' columns.
var builtInTitles = map[string]string{
	"identifier": "Identifier", "title": "Title", "blueprint": "Blueprint",
	"integration": "Integration", "createdAt": "Created", "updatedAt": "Updated",
}

func builtInField(spec blueprint.Spec, alias, name string) field {
	fld := field{column: true, kind: "string", builtIn: name, title: builtInTitles[name]}
	switch name {
	case "identifier":
		fld.value = alias + ".identifier"
	case "title":
		fld.value = alias + ".title"
	case "blueprint":
		fld.value = quoteLiteral(spec.Identifier)
		if alias == "e" {
			fld.value = "b.identifier"
		}
	case "integration":
		fld.value = "(SELECT identifier FROM config_documents WHERE id = " + alias + ".integration_id)"
		if alias == "e" {
			fld.value = "i.identifier"
		}
	case "createdAt":
		fld.value, fld.kind = alias+".created_at", kindDate
	case "updatedAt":
		fld.value, fld.kind = alias+".updated_at", kindDate
	}

	return fld
}

func propertyField(alias string, prop blueprint.Property) field {
	key := quoteLiteral(prop.ID)
	fld := field{prop: &prop, kind: prop.Type, title: prop.Title}
	switch {
	case prop.IsRelation():
		fld.jsonb, fld.many, fld.kind, fld.relation = alias+".relations", prop.Many, "relation", prop.Relation
	case prop.Compute != "":
		fld.jsonb, fld.many = alias+".computed", prop.Type == "array"
	default:
		fld.jsonb, fld.many = alias+".properties", prop.Type == "array"
	}
	fld.value, fld.key = fld.jsonb+" -> "+key, prop.ID
	if prop.Format == "date" || prop.Format == "date-time" {
		fld.kind = kindDate
	}

	return fld
}

// scorecardField reads an entity's result on the scorecard a property
// names: alone, its tier's name, "" for none, or null before it's
// evaluated; after a dot, what ScorecardFields lists, or checks.<id>.
func scorecardField(alias string, prop blueprint.Property, rest string) (field, error) {
	result := func(column string) string {
		return "(SELECT " + column + " FROM scorecard_results r WHERE r.entity_id = " + alias + ".id AND r.scorecard_id = " +
			"(SELECT id FROM config_documents WHERE kind = 'scorecard' AND identifier = " + quoteLiteral(prop.Scorecard) +
			" AND published_version IS NOT NULL))"
	}
	fld := field{column: true, nullable: true, kind: "integer", prop: &prop, title: prop.Title}
	switch check, isCheck := strings.CutPrefix(rest, "checks."); {
	case rest == "":
		fld.value, fld.kind, fld.scorecard = result("r.tier"), kindTier, prop.Scorecard
	case rest == "failing":
		fld.value = result(`coalesce((SELECT jsonb_agg(c.key ORDER BY c.key) FROM jsonb_each(r.criteria) c
 WHERE c.value ->> 'status' = 'failed'), '[]'::jsonb)`)
		fld.column, fld.many, fld.kind = false, true, "array"
	case isCheck && widget.ValidName(check):
		fld.value, fld.kind = result("r.criteria -> "+quoteLiteral(check)+" ->> 'status'"), "string"
	case slices.Contains(ScorecardFields, rest):
		fld.value = result("r." + rest)
	default:
		return field{}, fmt.Errorf("%s.%s isn't on a scorecard result; follow %s with %s or checks.<id>",
			prop.ID, rest, prop.ID, strings.Join(ScorecardFields, ", "))
	}
	if rest != "" {
		fld.prop, fld.title = nil, prop.Title+" › "+strings.ReplaceAll(rest, "checks.", "")
	}

	return fld, nil
}

// json is the field's value as jsonb, which reads the same whatever it is.
func (f field) json() string {
	switch {
	case f.kind == kindTier:
		return "to_jsonb(nullif(" + f.value + ", ''))"
	case f.column:
		return "to_jsonb(" + f.value + ")"
	}

	return f.value
}

// number is the field's value as a number, or null when it isn't one.
func (f field) number() string {
	if f.column {
		return "(" + f.value + ")::numeric"
	}

	return "(CASE WHEN jsonb_typeof(" + f.value + ") = 'number' THEN (" + f.value + ")::numeric END)"
}
