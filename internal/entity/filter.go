package entity

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/filter"
)

// Filter is the catalog's filter language; see filter.Filter.
type Filter = filter.Filter

const (
	OpEq       = filter.OpEq
	OpNe       = filter.OpNe
	OpIn       = filter.OpIn
	OpContains = filter.OpContains
	OpExists   = filter.OpExists
	OpGt       = filter.OpGt
	OpGte      = filter.OpGte
	OpLt       = filter.OpLt
	OpLte      = filter.OpLte
	OpWithin   = filter.OpWithin
)

// MaxFilterDepth, MaxFilterConditions and MaxFilterHops keep filters cheap
// to compile and run.
const (
	MaxFilterDepth      = 5
	MaxFilterConditions = 50
	MaxFilterHops       = 3
)

// ParseFilter reads a filter from JSON. An empty string is no filter.
func ParseFilter(raw string) (*Filter, error) { return filter.Parse(raw) }

// CheckFilter reports whether a filter fits a blueprint, without running it.
// specs are the blueprints its relations lead to, for properties that
// follow them.
func CheckFilter(specs map[string]blueprint.Spec, spec blueprint.Spec, f Filter) error {
	c := &compiler{spec: spec, specs: specs}
	_, err := c.compile(f, 1)

	return err
}

// compiler turns a filter into SQL, adding values as query arguments.
// specs are the blueprints relations lead to, and hops how many it has
// followed to reach spec.
type compiler struct {
	spec       blueprint.Spec
	specs      map[string]blueprint.Spec
	args       []any
	conditions int
	hops       int
}

func (c *compiler) field(path string) (field, error) {
	return reader{specs: c.specs}.read(c.spec, "e", path, c.hops)
}

func (c *compiler) arg(value any) string {
	c.args = append(c.args, value)
	return "$" + strconv.Itoa(len(c.args))
}

func (c *compiler) compile(f Filter, depth int) (string, error) {
	if depth > MaxFilterDepth {
		return "", fmt.Errorf("filters can nest at most %d levels", MaxFilterDepth)
	}
	c.conditions++
	if c.conditions > MaxFilterConditions {
		return "", fmt.Errorf("filters can have at most %d conditions", MaxFilterConditions)
	}

	switch {
	case len(f.And) > 0 || len(f.Or) > 0:
		joiner, parts := " AND ", f.And
		if len(f.Or) > 0 {
			joiner, parts = " OR ", f.Or
		}
		sqls := make([]string, 0, len(parts))
		for _, part := range parts {
			sql, err := c.compile(part, depth+1)
			if err != nil {
				return "", err
			}
			sqls = append(sqls, sql)
		}
		return "(" + strings.Join(sqls, joiner) + ")", nil
	case f.Not != nil:
		sql, err := c.compile(*f.Not, depth+1)
		if err != nil {
			return "", err
		}
		return "NOT coalesce(" + sql + ", false)", nil
	}

	return c.condition(f)
}

func (c *compiler) condition(f Filter) (string, error) {
	if name, rest, ok := strings.Cut(f.Property, "."); ok {
		if prop, ok := c.spec.Property(name); ok && (prop.IsRelation() || prop.Lookup != "") {
			return c.follow(f, name, rest)
		}
	}
	if prop, ok := c.spec.Property(f.Property); ok && prop.Lookup != "" {
		f.Property = prop.Lookup
		return c.condition(f)
	}
	fld, err := c.field(f.Property)
	if err != nil {
		return "", err
	}
	if f.ValuesFrom != "" {
		return "", fmt.Errorf("values on %s names %s, which isn't set here", f.Property, f.ValuesFrom)
	}

	switch f.Op {
	case OpExists:
		if fld.kind == kindTier {
			return "(coalesce(" + fld.value + ", '') <> '')", nil
		}
		if fld.nullable && fld.column {
			return "(" + fld.value + " IS NOT NULL)", nil
		}
		if fld.column {
			return "true", nil
		}
		return "(" + fld.value + " IS NOT NULL AND " + fld.value + " <> 'null'::jsonb)", nil
	case OpEq, OpNe:
		sql, err := c.equals(fld, f.Property, f.Value)
		if err != nil {
			return "", err
		}
		if f.Op == OpNe {
			return "NOT coalesce(" + sql + ", false)", nil
		}
		return sql, nil
	case OpIn:
		if f.Value != nil {
			return "", fmt.Errorf("in on %s takes values, a list or a variable such as $input.team, not value", f.Property)
		}
		if f.Values == nil {
			return "", fmt.Errorf("in on %s needs values", f.Property)
		}
		// A list written empty, as a filter bound to no values is, matches nothing.
		if len(f.Values) == 0 {
			return "false", nil
		}
		sqls := make([]string, 0, len(f.Values))
		for _, value := range f.Values {
			sql, err := c.equals(fld, f.Property, value)
			if err != nil {
				return "", err
			}
			sqls = append(sqls, sql)
		}
		return "(" + strings.Join(sqls, " OR ") + ")", nil
	case OpContains:
		text, ok := f.Value.(string)
		if !ok {
			return "", fmt.Errorf("contains on %s needs a text value", f.Property)
		}
		if fld.many {
			return c.equals(fld, f.Property, text)
		}
		return "(" + textOf(fld) + " ILIKE " + c.arg("%"+likeEscaper.Replace(text)+"%") + ")", nil
	case OpGt, OpGte, OpLt, OpLte:
		return c.compare(fld, f)
	case OpWithin:
		return c.within(fld, f)
	}

	return "", fmt.Errorf("unknown op %q on %s; use eq, ne, in, contains, exists, gt, gte, lt, lte or within", f.Op, f.Property)
}

// within matches dates in the last duration, from that long ago on.
func (c *compiler) within(fld field, f Filter) (string, error) {
	text, _ := f.Value.(string)
	duration, ok := filter.ParseDuration(text)
	if !ok {
		return "", fmt.Errorf("within on %s takes a duration, such as 30m, 12h, 7d or 2w", f.Property)
	}
	if fld.kind != kindDate {
		return "", fmt.Errorf("within on %s only compares dates", f.Property)
	}

	return "(" + dateOf(fld) + " >= now() - " + c.arg(strconv.FormatInt(int64(duration/time.Second), 10)+" seconds") + "::interval)", nil
}

// dateOf is a date field's value as a timestamp.
func dateOf(fld field) string {
	if fld.column {
		return fld.value
	}

	return "(" + textOf(fld) + ")::timestamptz"
}

// follow matches entities whose relation name points at an entity matching
// the condition on rest, as service.owner does for a deployment. A lookup
// follows the relations it reads through. Many-relations match when any
// entity they point at does.
func (c *compiler) follow(f Filter, name, rest string) (string, error) {
	prop, _ := c.spec.Property(name)
	switch {
	case prop.Lookup != "":
		f.Property = prop.Lookup + "." + rest
		return c.condition(f)
	case c.hops >= MaxFilterHops:
		return "", fmt.Errorf("%s follows more than %d relations and scorecards", f.Property, MaxFilterHops)
	}
	target, ok := c.specs[prop.Relation]
	if !ok {
		return "", fmt.Errorf("%s follows %s to %s, which isn't published", f.Property, name, prop.Relation)
	}
	// The subquery's aliases shadow the outer ones, so the condition
	// compiles as it would on target's own entities.
	inner := &compiler{spec: target, specs: c.specs, args: c.args, conditions: c.conditions, hops: c.hops + 1}
	f.Property = rest
	sql, err := inner.condition(f)
	c.args, c.conditions = inner.args, inner.conditions
	if err != nil {
		return "", err
	}

	// Postgres hashes the matching identifiers once, rather than comparing
	// each entity's relation with all of them.
	matching := "(SELECT e.identifier" + entityFrom + " WHERE b.kind = 'blueprint' AND b.identifier = " +
		c.arg(target.Identifier) + " AND " + sql + ")"
	relation := "e.relations -> " + quoteLiteral(prop.ID)
	if !prop.Many {
		return "((e.relations ->> " + quoteLiteral(prop.ID) + ") IN " + matching + ")", nil
	}

	return "EXISTS (SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(" + relation + ") = 'array' THEN " +
		relation + " END) target(identifier) WHERE target.identifier IN " + matching + ")", nil
}

// equals matches a value, or for lists and many-relations, one of their
// items. A tier of "" matches no tier, evaluated or not.
func (c *compiler) equals(fld field, property string, value any) (string, error) {
	if value == nil {
		return "", fmt.Errorf("compare %s with a value, or use exists", property)
	}
	if fld.column && (fld.kind == "integer" || fld.kind == "number") {
		number, ok := value.(float64)
		if !ok {
			return "", fmt.Errorf("%s is a number", property)
		}
		return "((" + fld.value + ")::numeric = " + c.arg(number) + ")", nil
	}
	if fld.column {
		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("%s is text", property)
		}
		switch fld.kind {
		case kindDate:
			return "(" + fld.value + " = " + c.arg(text) + "::timestamptz)", nil
		case kindTier:
			return "(coalesce(" + fld.value + ", '') = " + c.arg(text) + ")", nil
		}
		return "(" + fld.value + " = " + c.arg(text) + ")", nil
	}
	if fld.many {
		value = []any{value}
	}
	// Containment on the whole column is what its GIN index answers.
	if fld.jsonb != "" {
		encoded, err := json.Marshal(map[string]any{fld.key: value})
		if err != nil {
			return "", err
		}
		return "(" + fld.jsonb + " @> " + c.arg(string(encoded)) + "::jsonb)", nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if fld.many {
		return "(" + fld.value + " @> " + c.arg(string(encoded)) + "::jsonb)", nil
	}

	return "(" + fld.value + " = " + c.arg(string(encoded)) + "::jsonb)", nil
}

func (c *compiler) compare(fld field, f Filter) (string, error) {
	operator := map[string]string{OpGt: ">", OpGte: ">=", OpLt: "<", OpLte: "<="}[f.Op]
	switch {
	case fld.kind == "number" || fld.kind == "integer":
		number, ok := f.Value.(float64)
		if !ok {
			return "", fmt.Errorf("%s on %s needs a number", f.Op, f.Property)
		}
		return "((" + fld.value + ")::numeric " + operator + " " + c.arg(number) + ")", nil
	case fld.kind == kindDate:
		text, ok := f.Value.(string)
		if !ok {
			return "", fmt.Errorf("%s on %s needs a date", f.Op, f.Property)
		}
		return "(" + dateOf(fld) + " " + operator + " " + c.arg(text) + "::timestamptz)", nil
	}

	return "", fmt.Errorf("%s only compares numbers and dates", f.Property)
}

// textOf is a field's value as text.
func textOf(fld field) string {
	if fld.column {
		return fld.value
	}

	return "(" + fld.value + " #>> '{}')"
}

// orderBy sorts by a path, ascending unless it starts with "-". Ties fall
// back to the title, then the identifier, so pages are stable. Tiers sort
// up their scorecard's ladder, lowest first, as ladders say.
func (c *compiler) orderBy(sort string, ladders map[string]blueprint.Ladder) (string, error) {
	if sort == "" {
		return "e.title, e.identifier", nil
	}
	direction := "ASC"
	if path, ok := strings.CutPrefix(sort, "-"); ok {
		sort, direction = path, "DESC"
	}
	fld, err := c.field(sort)
	if err != nil {
		return "", err
	}
	if fld.many || fld.kind == "object" {
		return "", fmt.Errorf("%s holds a list or an object, which can't be sorted", sort)
	}
	value := fld.value
	if ladder, ok := ladders[fld.scorecard]; ok && fld.kind == kindTier {
		value = "array_position(" + c.arg(pq.Array(ladder.Names())) + "::text[], coalesce(" + value + ", ''))"
	}

	return value + " " + direction + " NULLS LAST, e.title, e.identifier", nil
}

// quoteLiteral quotes a property name for SQL. Names are validated as
// letters, digits and _, so this is belt and braces.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
