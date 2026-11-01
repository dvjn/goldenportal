package entity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// Match is the condition a query's entities meet, compiled once, which
// every statement about them shares: its aggregates, groups and rows.
type Match struct {
	spec    blueprint.Spec
	specs   map[string]blueprint.Spec
	ladders map[string]blueprint.Ladder
	where   string
	args    []any
}

// NewMatch compiles q's blueprint and filter. Its search and page don't
// count. A condition that comes out null, as one on a missing value does,
// filters like false, so it isn't coalesced, which would hide it from the
// indexes.
func NewMatch(q Query) (Match, error) {
	c := &compiler{spec: q.Blueprint, specs: q.Blueprints, args: []any{q.Blueprint.Identifier}}
	where := "b.kind = 'blueprint' AND b.identifier = $1"
	if q.Filter != nil {
		sql, err := c.compile(*q.Filter, 1)
		if err != nil {
			return Match{}, FilterError{err}
		}
		where += " AND " + sql
	}

	return Match{spec: q.Blueprint, specs: q.Blueprints, ladders: q.Ladders, where: where, args: c.args}, nil
}

// compiler compiles more of a statement after the match's own arguments.
func (m Match) compiler() *compiler {
	return &compiler{spec: m.spec, specs: m.specs, args: slices.Clone(m.args)}
}

// Aggregate is one way to sum up the entities a query matches: count them,
// sum, avg, min or max a number field, or share, the percent of them that
// meet a condition.
type Aggregate struct {
	Op    string
	Path  string
	Share *Filter
}

// Aggregates are the ops an Aggregate can be.
var Aggregates = []string{"count", "sum", "avg", "min", "max", "share"}

// CheckAggregate reports whether a blueprint's entities can be summed up this way.
func CheckAggregate(specs map[string]blueprint.Spec, spec blueprint.Spec, a Aggregate) error {
	_, err := aggregateSQL(&compiler{spec: spec, specs: specs}, a)
	return err
}

func aggregateSQL(c *compiler, a Aggregate) (string, error) {
	switch a.Op {
	case "count":
		return "count(*)", nil
	case "share":
		if a.Share == nil {
			return "", fmt.Errorf("share needs the condition its entities meet")
		}
		condition, err := c.compile(*a.Share, 1)
		if err != nil {
			return "", err
		}
		return "100.0 * count(*) FILTER (WHERE " + condition + ") / nullif(count(*), 0)", nil
	case "sum", "avg", "min", "max":
		fld, err := c.field(a.Path)
		if err != nil {
			return "", err
		}
		if fld.many || (fld.kind != "number" && fld.kind != "integer") {
			return "", fmt.Errorf("%s works on numbers, and %s isn't one", a.Op, a.Path)
		}
		return a.Op + "(" + fld.number() + ")", nil
	}

	return "", fmt.Errorf("aggregate must be one of %s", strings.Join(Aggregates, ", "))
}

// Aggregates works out every aggregate of m's entities in one statement.
// Each is nil when there's nothing to work on, such as the average of no
// values.
func (s Store) Aggregates(ctx context.Context, m Match, aggregates []Aggregate) ([]*float64, error) {
	c := m.compiler()
	columns := make([]string, len(aggregates))
	for i, a := range aggregates {
		sql, err := aggregateSQL(c, a)
		if err != nil {
			return nil, FilterError{err}
		}
		columns[i] = "(" + sql + ")::float8"
	}
	values := make([]sql.NullFloat64, len(aggregates))
	targets := make([]any, len(values))
	for i := range values {
		targets[i] = &values[i]
	}
	if err := s.db.QueryRowContext(ctx, `SELECT `+strings.Join(columns, ", ")+entityFrom+`
	WHERE `+m.where, c.args...).Scan(targets...); err != nil {
		return nil, err
	}
	out := make([]*float64, len(values))
	for i, value := range values {
		if value.Valid {
			out[i] = &value.Float64
		}
	}

	return out, nil
}

// Facet is one value of a field and how many entities have it. Title names
// a related entity or a tier, and Color is a tier's.
type Facet struct {
	Value any    `json:"value"`
	Title string `json:"title,omitempty"`
	Color string `json:"color,omitempty"`
	Count int    `json:"count"`
}

// MaxFacets caps the values offered per property.
const MaxFacets = 50

// Facets counts the values a field has across all of q's blueprint's
// entities, most common first, for filter options. Lists count each item.
func (s Store) Facets(ctx context.Context, q Query, path string) ([]Facet, error) {
	q.Filter = nil
	m, err := NewMatch(q)
	if err != nil {
		return nil, err
	}
	facets, _, err := s.Group(ctx, m, path, MaxFacets)

	return facets, err
}

// CheckGroup reports whether entities can be counted by a field's values.
func CheckGroup(specs map[string]blueprint.Spec, spec blueprint.Spec, path string) error {
	_, err := groupField(&compiler{spec: spec, specs: specs}, path)
	return err
}

func groupField(c *compiler, path string) (field, error) {
	fld, err := c.field(path)
	if err != nil {
		return field{}, err
	}
	if fld.kind == "object" || fld.kind == kindDate || fld.builtIn == "identifier" || fld.builtIn == "title" {
		return field{}, fmt.Errorf("%s can't be counted by value", path)
	}

	return fld, nil
}

// Group counts the values a field has across m's entities in one
// statement, most common first, up to limit values. Lists count each item.
// None counts the entities without a value, or with an empty list.
func (s Store) Group(ctx context.Context, m Match, path string, limit int) (facets []Facet, none int, err error) {
	c := m.compiler()
	fld, err := groupField(c, path)
	if err != nil {
		return nil, 0, FilterError{err}
	}
	value, from := fld.json(), entityFrom
	if fld.many {
		from += `
	LEFT JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(` + fld.value + `) = 'array' THEN ` + fld.value + ` END) item ON true`
		value = "item"
	}
	title, join := "NULL::text", ""
	if fld.relation != "" {
		title = "t.title"
		join = `
	LEFT JOIN entities t ON t.blueprint_id = ` + blueprintID(fld.relation) + ` AND t.identifier = g.value #>> '{}'`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT g.value, g.count, `+title+` FROM (SELECT `+value+` AS value, count(*) AS count`+from+`
	WHERE `+m.where+`
	GROUP BY 1) g`+join+`
	ORDER BY g.value IS NULL DESC, g.count DESC, g.value
	LIMIT `+c.arg(limit+1), c.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	facets = []Facet{}
	for rows.Next() {
		var (
			raw   []byte
			count int
			named sql.NullString
		)
		if err := rows.Scan(&raw, &count, &named); err != nil {
			return nil, 0, err
		}
		if raw == nil {
			none = count
			continue
		}
		f := Facet{Count: count, Title: named.String}
		if err := json.Unmarshal(raw, &f.Value); err != nil {
			return nil, 0, err
		}
		facets = append(facets, f)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(facets) > limit {
		facets = facets[:limit]
	}
	if ladder, ok := m.ladders[fld.scorecard]; ok && fld.kind == kindTier {
		for i, f := range facets {
			if at := slices.IndexFunc(ladder.Tiers, func(r blueprint.Rung) bool { return r.Name == f.Value }); at >= 0 {
				facets[i].Title, facets[i].Color = ladder.Tiers[at].Title, ladder.Tiers[at].Color
			}
		}
	}

	return facets, none, nil
}

// CheckSort reports whether a blueprint's entities can be sorted this way.
func CheckSort(specs map[string]blueprint.Spec, spec blueprint.Spec, sort string) error {
	_, err := (&compiler{spec: spec, specs: specs}).orderBy(sort, nil)
	return err
}

// FieldInfo is what a path ends on, for showing its values: a property,
// from whichever blueprint the path reaches it on, a built-in field or a
// scorecard's tier.
type FieldInfo struct {
	Title     string
	Property  *blueprint.Property
	BuiltIn   string
	Scorecard string
	Kind      string
}

// Describe says what a path ends on, or why a blueprint can't read it.
func Describe(specs map[string]blueprint.Spec, spec blueprint.Spec, path string) (FieldInfo, error) {
	fld, err := fieldFor(specs, spec, path)
	if err != nil {
		return FieldInfo{}, err
	}

	return FieldInfo{Title: fld.title, Property: fld.prop, BuiltIn: fld.builtIn, Scorecard: fld.scorecard, Kind: fld.kind}, nil
}

// CheckColumn reports whether a path can be shown, in a table or on an
// entity page.
func CheckColumn(specs map[string]blueprint.Spec, spec blueprint.Spec, path string) error {
	_, err := fieldFor(specs, spec, path)
	return err
}

// Row is one entity a table shows: its identifier and title, and each
// column's value. Links are the entities a relation column's values point
// at, with their titles, or the one a tier column's scorecard scored.
type Row struct {
	ID         string   `json:"-"`
	Identifier string   `json:"identifier"`
	Title      string   `json:"title"`
	Values     []any    `json:"values"`
	Links      [][]Link `json:"links,omitempty"`
}

// Rows reads the first limit of m's entities in sort's order, with the
// value of each path, in one statement, and how many match in all.
func (s Store) Rows(ctx context.Context, m Match, sort string, limit int, paths []string) ([]Row, int, error) {
	c := m.compiler()
	order, err := c.orderBy(sort, m.ladders)
	if err != nil {
		return nil, 0, FilterError{err}
	}
	var columns []string
	for _, path := range paths {
		fld, err := c.field(path)
		if err != nil {
			return nil, 0, FilterError{err}
		}
		columns = append(columns, ", "+rowValue(fld))
		links, err := rowLinks(c, fld, path)
		if err != nil {
			return nil, 0, FilterError{err}
		}
		columns = append(columns, ", "+links)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id, e.identifier, e.title, count(*) OVER ()`+strings.Join(columns, "")+entityFrom+`
	WHERE `+m.where+`
	ORDER BY `+order+`
	LIMIT `+c.arg(limit), c.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []Row{}
	total := 0
	for rows.Next() {
		r := Row{Values: make([]any, len(paths)), Links: make([][]Link, len(paths))}
		raws := make([][]byte, 2*len(paths))
		targets := []any{&r.ID, &r.Identifier, &r.Title, &total}
		for i := range raws {
			targets = append(targets, &raws[i])
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, 0, err
		}
		for i := range paths {
			if raws[2*i] != nil {
				if err := json.Unmarshal(raws[2*i], &r.Values[i]); err != nil {
					return nil, 0, err
				}
			}
			if raws[2*i+1] != nil {
				if err := json.Unmarshal(raws[2*i+1], &r.Links[i]); err != nil {
					return nil, 0, err
				}
			}
		}
		out = append(out, r)
	}

	return out, total, rows.Err()
}

// rowValue is a column's value as jsonb. A tier keeps "" for none apart
// from null for not evaluated.
func rowValue(fld field) string {
	if fld.column {
		return "to_jsonb(" + fld.value + ")"
	}

	return fld.value
}

// rowLinks is SQL for what a column's value links to, or null: the related
// entities a relation names, in order and with their titles, or for a
// tier, the entity its path ends on, whose result it is.
func rowLinks(c *compiler, fld field, path string) (string, error) {
	switch {
	case fld.relation != "":
		value := fld.value
		return `(SELECT jsonb_agg(jsonb_build_object('identifier', x.id, 'title', t.title) ORDER BY x.n)
	FROM jsonb_array_elements_text(CASE jsonb_typeof(` + value + `) WHEN 'array' THEN ` + value + ` WHEN 'string' THEN jsonb_build_array(` + value + `) END) WITH ORDINALITY x(id, n)
	LEFT JOIN entities t ON t.blueprint_id = ` + blueprintID(fld.relation) + ` AND t.identifier = x.id)`, nil
	case fld.kind == kindTier:
		scored := "identifier"
		if at := strings.LastIndex(path, "."); at >= 0 {
			scored = path[:at] + ".identifier"
		}
		owner, err := c.field(scored)
		if err != nil {
			return "", err
		}
		return "jsonb_build_array(jsonb_build_object('identifier', " + owner.value + "))", nil
	}

	return "NULL::jsonb", nil
}

// Matching is SQL selecting the IDs of the entities of spec matching
// filter, with its arguments numbered after args. specs are the blueprints
// its relations lead to.
func Matching(specs map[string]blueprint.Spec, spec blueprint.Spec, filter *Filter, args []any) (string, []any, error) {
	c := &compiler{spec: spec, specs: specs, args: append(args, spec.Identifier)}
	where := "b.kind = 'blueprint' AND b.identifier = $" + fmt.Sprint(len(c.args))
	if filter != nil {
		sql, err := c.compile(*filter, 1)
		if err != nil {
			return "", nil, FilterError{err}
		}
		where += " AND " + sql
	}

	return `SELECT e.id` + entityFrom + ` WHERE ` + where, c.args, nil
}
