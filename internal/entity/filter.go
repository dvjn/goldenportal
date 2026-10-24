package entity

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// Filter is a condition on entities, written as JSON. It's a tree: and, or
// and not combine conditions, and a leaf compares one property:
//
//	{"and": [{"property": "tier", "op": "in", "values": ["critical"]},
//	         {"not": {"property": "owner", "op": "exists"}}]}
//
// Built-in fields are $identifier, $title, $integration, $createdAt and $updatedAt.
type Filter struct {
	And []Filter `json:"and,omitempty"`
	Or  []Filter `json:"or,omitempty"`
	Not *Filter  `json:"not,omitempty"`

	Property string `json:"property,omitempty"`
	Op       string `json:"op,omitempty"`
	Value    any    `json:"value,omitempty"`
	Values   []any  `json:"values,omitempty"`
}

// Comparison operators. contains matches a list item, a related entity or
// text within a string.
const (
	OpEq       = "eq"
	OpNe       = "ne"
	OpIn       = "in"
	OpContains = "contains"
	OpExists   = "exists"
	OpGt       = "gt"
	OpGte      = "gte"
	OpLt       = "lt"
	OpLte      = "lte"
)

// MaxFilterDepth and MaxFilterConditions keep filters cheap to compile and run.
const (
	MaxFilterDepth      = 5
	MaxFilterConditions = 50
)

// ParseFilter reads a filter from JSON. An empty string is no filter.
func ParseFilter(raw string) (*Filter, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var f Filter
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return nil, fmt.Errorf("the filter is not valid JSON: %w", err)
	}

	return &f, nil
}

// field is where a property's value lives in SQL.
type field struct {
	// value is a jsonb expression, or a typed column for the built-ins.
	value  string
	column bool
	many   bool
	kind   string
}

func fieldFor(spec blueprint.Spec, property string) (field, error) {
	switch property {
	case "$identifier":
		return field{value: "e.identifier", column: true, kind: "string"}, nil
	case "$title":
		return field{value: "e.title", column: true, kind: "string"}, nil
	case "$integration":
		return field{value: "i.identifier", column: true, kind: "string"}, nil
	case "$createdAt":
		return field{value: "e.created_at", column: true, kind: "date-time"}, nil
	case "$updatedAt":
		return field{value: "e.updated_at", column: true, kind: "date-time"}, nil
	}

	prop, ok := spec.Property(property)
	if !ok {
		return field{}, fmt.Errorf("%s has no property %q", spec.Identifier, property)
	}
	key := quoteLiteral(prop.ID)
	switch {
	case prop.Lookup != "":
		return field{}, fmt.Errorf("%s is a lookup, which can't be filtered or sorted yet", prop.ID)
	case prop.IsRelation():
		return field{value: "e.relations -> " + key, many: prop.Many, kind: "relation"}, nil
	case prop.Compute != "":
		return field{value: "e.computed -> " + key, many: prop.Type == "array", kind: prop.Type}, nil
	}
	kind := prop.Type
	if prop.Format == "date" || prop.Format == "date-time" {
		kind = "date-time"
	}

	return field{value: "e.properties -> " + key, many: prop.Type == "array", kind: kind}, nil
}

// compiler turns a filter into SQL, adding values as query arguments.
type compiler struct {
	spec       blueprint.Spec
	args       []any
	conditions int
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
	fld, err := fieldFor(c.spec, f.Property)
	if err != nil {
		return "", err
	}

	switch f.Op {
	case OpExists:
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
		if len(f.Values) == 0 {
			return "", fmt.Errorf("in on %s needs values", f.Property)
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
			return "(" + fld.value + " @> " + c.arg(jsonArray(text)) + "::jsonb)", nil
		}
		return "(" + textOf(fld) + " ILIKE " + c.arg("%"+likeEscaper.Replace(text)+"%") + ")", nil
	case OpGt, OpGte, OpLt, OpLte:
		return c.compare(fld, f)
	}

	return "", fmt.Errorf("unknown op %q on %s; use eq, ne, in, contains, exists, gt, gte, lt or lte", f.Op, f.Property)
}

// equals matches a value, or for lists and many-relations, one of their items.
func (c *compiler) equals(fld field, property string, value any) (string, error) {
	if value == nil {
		return "", fmt.Errorf("compare %s with a value, or use exists", property)
	}
	if fld.column {
		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("%s is text", property)
		}
		if fld.kind == "date-time" {
			return "(" + fld.value + " = " + c.arg(text) + "::timestamptz)", nil
		}
		return "(" + fld.value + " = " + c.arg(text) + ")", nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if fld.many {
		return "(" + fld.value + " @> " + c.arg("["+string(encoded)+"]") + "::jsonb)", nil
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
	case fld.kind == "date-time":
		text, ok := f.Value.(string)
		if !ok {
			return "", fmt.Errorf("%s on %s needs a date", f.Op, f.Property)
		}
		value := fld.value
		if !fld.column {
			value = "(" + textOf(fld) + ")::timestamptz"
		}
		return "(" + value + " " + operator + " " + c.arg(text) + "::timestamptz)", nil
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

// orderBy sorts by a property, ascending unless it starts with "-". Ties
// fall back to the title, then the identifier, so pages are stable.
func orderBy(spec blueprint.Spec, sort string) (string, error) {
	if sort == "" {
		return "e.title, e.identifier", nil
	}
	direction := "ASC"
	if property, ok := strings.CutPrefix(sort, "-"); ok {
		sort, direction = property, "DESC"
	}
	fld, err := fieldFor(spec, sort)
	if err != nil {
		return "", err
	}
	if fld.many {
		return "", fmt.Errorf("%s holds a list, which can't be sorted", sort)
	}

	return fld.value + " " + direction + " NULLS LAST, e.title, e.identifier", nil
}

func jsonArray(values ...string) string {
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

// quoteLiteral quotes a property name for SQL. Names are validated as
// letters, digits and _, so this is belt and braces.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
