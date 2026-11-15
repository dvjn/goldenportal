package entity

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/filter"
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
	// scorecard is the scorecard a tier is on, and check with checkOf the
	// check a status is of and its scorecard.
	scorecard, check, checkOf string
	// prop is the property the path ends on, and builtIn the built-in
	// field, for showing its values.
	prop    *blueprint.Property
	builtIn string
	// title names what the path ends on, for a column's heading.
	title string
	// jsonb and key say which of the entity's jsonb columns holds the
	// value, under which key, for conditions its GIN index can answer.
	jsonb, key string
	// timestamp is a column holding a date's value as a timestamp, which an
	// index ranges over and sorts by: occurred_at, for the property its
	// blueprint's retention keeps rows by.
	timestamp string
	// connection is the column holding the connection's row, which
	// conditions and groups read rather than looking up each row's name.
	connection string
	// result is the scorecard result the value is read from, when no
	// relation leads there: conditions on it are semi-joins, and sorts,
	// groups and aggregates join it rather than look it up per row.
	result *result
	// rank is a tier's place up its scorecard's ladder, which tiers sort by.
	rank string
	// owner is the path of the entity a tier is of, which its cell links to.
	owner string
}

// result is an entity's row in scorecard_results: of is the entity's
// alias, and read what the field reads of a row aliased as given.
type result struct {
	scorecard string
	of        string
	read      func(row string) string
}

// resultRow aliases the result row a semi-join's condition is written
// over, and with a number, each result a statement joins.
const resultRow = "sr"

func joinedRow(i int) string { return resultRow + strconv.Itoa(i+1) }

// tierRank reads a result's tier's place up its ladder.
func tierRank(row string) string { return row + ".tier_rank" }

// scorecardID is SQL for a published scorecard's document row.
func scorecardID(identifier string) string {
	return "(SELECT id FROM config_documents WHERE kind = 'scorecard' AND identifier = " + quoteLiteral(identifier) +
		" AND published_version IS NOT NULL)"
}

// readEach reads what read does of the entity's result, one entity at a
// time.
func (r result) readEach(read func(row string) string) string {
	return "(SELECT " + read("r") + " FROM scorecard_results r WHERE r.entity_id = " + r.of + ".id AND r.scorecard_id = " +
		scorecardID(r.scorecard) + ")"
}

// matching is whether the entity has a result meeting condition, written
// over resultRow, as a semi-join the indexes on scorecard_results answer.
func (r result) matching(condition string) string {
	return "(" + r.of + ".id IN (SELECT " + resultRow + ".entity_id FROM scorecard_results " + resultRow +
		" WHERE " + resultRow + ".scorecard_id = " + scorecardID(r.scorecard) + " AND " + condition + "))"
}

// Kinds beside a property's type.
const (
	kindDate = "date-time"
	kindTier = "tier"
)

// CheckStatuses are what a check's status can be.
var CheckStatuses = []string{"passed", "failed", "skipped"}

// ScorecardFields are what a dot after a scorecard property reads of the
// entity's result, beside checks.<id>, a check's status.
var ScorecardFields = []string{"score", "passed", "failed", "skipped", "failing"}

// ResultsPath starts a path to an entity's result on any scorecard that
// grades its blueprint: _scorecards.<id> reads as a scorecard property
// naming it would.
const ResultsPath = filter.ResultsPath

// ScorecardPath is how paths on spec reach its entities' results on a
// scorecard: through the property naming it, else _scorecards.<scorecard>.
func ScorecardPath(spec blueprint.Spec, scorecard string) string {
	if prop, ok := spec.ScorecardProperty(scorecard); ok {
		return prop.ID
	}

	return ResultsPath + "." + scorecard
}

// Schema is what paths on Blueprint's entities read: the published
// Blueprints its relations lead to, and the published scorecards' Ladders.
type Schema struct {
	Blueprint  blueprint.Spec
	Blueprints map[string]blueprint.Spec
	Ladders    map[string]blueprint.Ladder
}

// compiler compiles s's paths; strict as publishing checks them.
func (s Schema) compiler(strict bool) *compiler {
	return &compiler{spec: s.Blueprint, specs: s.Blueprints, ladders: s.Ladders, strict: strict}
}

// reader finds what paths read, from an entity of one blueprint through
// the relations specs lead to. Strict, as publishing checks, a scorecard a
// path reads must grade the blueprint it's on and have the check it names;
// run as stored, one that doesn't reads empty, as its results do.
type reader struct {
	specs   map[string]blueprint.Spec
	ladders map[string]blueprint.Ladder
	strict  bool
}

func (r reader) spec(identifier string) (blueprint.Spec, bool) {
	spec, ok := r.specs[identifier]
	return spec, ok
}

// fieldFor is where a path's value lives, read from e, an entity of s's
// blueprint.
func fieldFor(s Schema, strict bool, path string) (field, error) {
	return s.compiler(strict).field(path)
}

// read resolves path from alias, an entity of spec reached through hops
// relations. A relation it follows is a scalar subquery, so the value
// comes back one per row; a many-relation can't be read through that way.
func (r reader) read(spec blueprint.Spec, alias, path string, hops int) (field, error) {
	name, rest, nested := strings.Cut(path, ".")
	if !nested && slices.Contains(blueprint.BuiltIns, name) {
		return builtInField(spec, alias, name), nil
	}
	if name == ResultsPath {
		return r.results(spec, alias, path, rest, hops)
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
		return r.scorecard(spec, alias, prop.Scorecard, prop.ID, &prop, rest)
	case !nested:
		return propertyField(alias, prop, spec.RetainedBy() == prop.ID), nil
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
	related := func(value string) string {
		return "(SELECT " + value + " FROM entities " + inner + " WHERE " + inner + ".blueprint_id = " +
			BlueprintID(target) + " AND " + inner + ".identifier = " + alias + ".relations ->> " + quoteLiteral(prop.ID) + ")"
	}
	fld.value, fld.nullable, fld.result = related(fld.value), true, nil
	if fld.rank != "" {
		fld.rank = related(fld.rank)
	}
	if fld.owner != "" {
		fld.owner = name + "." + fld.owner
	}
	// A related entity's name is best headed by the relation that leads there.
	if fld.builtIn == "title" || fld.builtIn == "identifier" {
		fld.title = prop.Title
	}

	return fld, nil
}

// BlueprintID is SQL for a blueprint's document row: its ID written out,
// since the planner only matches a partial index's predicate to a
// constant, or looked up by identifier for a spec that isn't stored yet.
func BlueprintID(spec blueprint.Spec) string {
	if spec.ID != "" {
		return quoteLiteral(spec.ID)
	}

	return "(SELECT id FROM config_documents WHERE kind = 'blueprint' AND identifier = " + quoteLiteral(spec.Identifier) + ")"
}

// specNamed is the blueprint specs has by identifier, or one known only by
// it when it isn't published.
func specNamed(specs map[string]blueprint.Spec, identifier string) blueprint.Spec {
	if spec, ok := specs[identifier]; ok {
		return spec
	}

	return blueprint.Spec{Identifier: identifier}
}

// connectionOf is SQL for the identifier of the connection whose row
// column holds, looked up only for the rows that read it.
func connectionOf(column string) string {
	return "(SELECT identifier FROM config_documents WHERE id = " + column + ")"
}

// builtInTitles head built-in fields' columns.
var builtInTitles = map[string]string{
	"identifier": "Identifier", "title": "Title", "blueprint": "Blueprint",
	"connection": "Connection", "createdAt": "Created", "updatedAt": "Updated",
}

// builtInField reads a field every entity has.
func builtInField(spec blueprint.Spec, alias, name string) field {
	fld := field{column: true, kind: "string", builtIn: name, title: builtInTitles[name]}
	switch name {
	case "identifier":
		fld.value = alias + ".identifier"
	case "title":
		fld.value = alias + ".title"
	case "blueprint":
		fld.value = quoteLiteral(spec.Identifier)
	case "connection":
		fld.connection = alias + ".connection_id"
		fld.value = connectionOf(fld.connection)
	case "createdAt":
		fld.value, fld.kind = alias+".created_at", kindDate
	case "updatedAt":
		fld.value, fld.kind = alias+".updated_at", kindDate
	}

	return fld
}

func propertyField(alias string, prop blueprint.Property, retainedBy bool) field {
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
	if retainedBy {
		fld.timestamp = alias + ".occurred_at"
	}

	return fld
}

// results reads _scorecards.<id>, alias's result on a scorecard, as a
// scorecard property naming it would.
func (r reader) results(spec blueprint.Spec, alias, path, rest string, hops int) (field, error) {
	scorecard, rest, nested := strings.Cut(rest, ".")
	if scorecard == "" {
		return field{}, fmt.Errorf("%s names a scorecard after it, as in %s.<scorecard>", ResultsPath, ResultsPath)
	}
	if _, ok := r.ladders[scorecard]; r.strict && !ok {
		return field{}, fmt.Errorf("%s names no published scorecard; %s", path, r.graders(spec))
	}
	if nested && hops >= MaxFilterHops {
		return field{}, fmt.Errorf("%s follows more than %d relations and scorecards", path, MaxFilterHops)
	}

	return r.scorecard(spec, alias, scorecard, ResultsPath+"."+scorecard, nil, rest)
}

// scorecard reads alias's result on a scorecard, which name reaches and
// prop, if any, holds, titled as the property naming it, else as the
// scorecard. Strict, a published scorecard must grade spec and have the
// check rest names; one not yet published has nothing to compare.
func (r reader) scorecard(spec blueprint.Spec, alias, scorecard, name string, prop *blueprint.Property, rest string) (field, error) {
	ladder, published := r.ladders[scorecard]
	if r.strict && published {
		switch {
		case ladder.Deleted:
			return field{}, fmt.Errorf("%s reads scorecard %s, which is being deleted", name, scorecard)
		case ladder.Blueprint != spec.Identifier:
			return field{}, fmt.Errorf("%s grades %s, not %s; %s", scorecard, ladder.Blueprint, spec.Identifier, r.graders(spec))
		}
	}
	title := scorecard
	if own, ok := spec.ScorecardProperty(scorecard); ok {
		title = own.Title
	} else if published && ladder.Title != "" {
		title = ladder.Title
	}
	fld, err := scorecardField(alias, scorecard, name, title, prop, rest)
	if err != nil {
		return field{}, err
	}
	if checks := ladder.Checks(); r.strict && published && fld.check != "" && !slices.Contains(checks, fld.check) {
		return field{}, fmt.Errorf("%s has no check %q; its checks are %s", scorecard, fld.check, strings.Join(checks, ", "))
	}

	return fld, nil
}

// graders says which published scorecards grade spec.
func (r reader) graders(spec blueprint.Spec) string {
	var names []string
	for name, ladder := range r.ladders {
		if ladder.Blueprint == spec.Identifier && !ladder.Deleted {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "no published scorecard grades " + spec.Identifier
	}
	slices.Sort(names)

	return spec.Identifier + " is graded by " + strings.Join(names, ", ")
}

// scorecardField reads alias's result on a scorecard, which name reaches
// and prop, if any, holds: alone, its tier's name, "" for none, or null
// before it's evaluated; after a dot, what ScorecardFields lists, or
// checks.<id>.
func scorecardField(alias, scorecard, name, title string, prop *blueprint.Property, rest string) (field, error) {
	res := &result{scorecard: scorecard, of: alias}
	fld := field{column: true, nullable: true, kind: "integer", prop: prop, title: title, result: res}
	switch check, isCheck := strings.CutPrefix(rest, "checks."); {
	case rest == "":
		res.read = func(row string) string { return row + ".tier" }
		fld.kind, fld.scorecard, fld.owner = kindTier, scorecard, "identifier"
		fld.rank = res.readEach(tierRank)
	case rest == "failing":
		res.read = func(row string) string {
			return `coalesce((SELECT jsonb_agg(c.key ORDER BY c.key) FROM jsonb_each(` + row + `.criteria) c
 WHERE c.value ->> 'status' = 'failed'), '[]'::jsonb)`
		}
		fld.column, fld.many, fld.kind = false, true, "array"
	case isCheck && widget.ValidName(check):
		res.read = func(row string) string { return row + ".criteria -> " + quoteLiteral(check) + " ->> 'status'" }
		fld.kind, fld.check, fld.checkOf = "string", check, scorecard
	case slices.Contains(ScorecardFields, rest):
		res.read = func(row string) string { return row + "." + rest }
	default:
		return field{}, fmt.Errorf("%s.%s isn't on a scorecard result; follow %s with %s or checks.<id>",
			name, rest, name, strings.Join(ScorecardFields, ", "))
	}
	fld.value = res.readEach(res.read)
	if rest != "" {
		fld.prop, fld.title = nil, title+" › "+strings.ReplaceAll(rest, "checks.", "")
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
// Filters, sorts and aggregates all read numbers this way, so one
// expression index serves them all.
func (f field) number() string {
	if f.column {
		return "(" + f.value + ")::numeric"
	}

	return "(CASE WHEN jsonb_typeof(" + f.value + ") = 'number' THEN (" + f.value + ")::numeric END)"
}

// time is a date field's value as a timestamp, or null when it isn't one.
// gp_time is immutable, unlike a cast, so it can be indexed.
func (f field) time() string {
	switch {
	case f.timestamp != "":
		return f.timestamp
	case f.column:
		return f.value
	}

	return "gp_time(" + textOf(f) + ")"
}

// sortKey is the field's value as sorts compare it.
func (f field) sortKey() string {
	switch {
	case f.kind == kindDate:
		return f.time()
	case !f.column && (f.kind == "number" || f.kind == "integer"):
		return f.number()
	}

	return f.value
}
