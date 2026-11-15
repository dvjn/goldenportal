package entity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/widget"
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
	c := &compiler{spec: q.Blueprint, specs: q.Blueprints}
	q.Search = ""
	where, err := q.where(c)
	if err != nil {
		return Match{}, err
	}

	return Match{spec: q.Blueprint, specs: q.Blueprints, ladders: q.Ladders, where: where, args: c.args}, nil
}

// compiler compiles more of a statement after the match's own arguments.
func (m Match) compiler() *compiler {
	return &compiler{spec: m.spec, specs: m.specs, ladders: m.ladders, args: slices.Clone(m.args)}
}

// Aggregate is one way to sum up the entities a query matches: count them,
// count the distinct values of a field, sum, avg, min, max or take an exact
// percentile of a number field, min or max of a date, or share, the percent
// of them that meet a condition. Filter narrows them to those it applies to
// first. Age gives a date's min or max as how many seconds ago it was.
type Aggregate struct {
	Op     string
	Path   string
	Share  *Filter
	Filter *Filter
	Age    bool
}

// Aggregates are the ops an Aggregate can be.
var Aggregates = []string{widget.OpCount, widget.OpDistinct, "sum", "avg", widget.OpMin, widget.OpMax, "p50", "p90", "p95", "p99", widget.OpShare}

// percentiles are the fractions the percentile ops stand for.
var percentiles = map[string]string{"p50": "0.5", "p90": "0.9", "p95": "0.95", "p99": "0.99"}

// CheckAggregate reports whether a blueprint's entities can be summed up this way.
func CheckAggregate(specs map[string]blueprint.Spec, spec blueprint.Spec, a Aggregate) error {
	_, err := aggregateSQL(&compiler{spec: spec, specs: specs}, a)
	return err
}

// aggregateSQL is an aggregate as SQL over the rows of one group, which
// for a narrowed one is only its filter's. min or max of a date is in
// seconds since the Unix epoch, or before now for its age.
func aggregateSQL(c *compiler, a Aggregate) (string, error) {
	var narrow string
	if a.Filter != nil {
		condition, err := c.compile(*a.Filter, 1)
		if err != nil {
			return "", err
		}
		narrow = condition
	}
	// of is an aggregate over the rows the conditions given and the
	// filter's all hold for.
	of := func(aggregate string, conditions ...string) string {
		if narrow != "" {
			conditions = append([]string{narrow}, conditions...)
		}
		if len(conditions) == 0 {
			return aggregate
		}
		return aggregate + " FILTER (WHERE " + strings.Join(conditions, " AND ") + ")"
	}
	switch a.Op {
	case "count":
		return of("count(*)"), nil
	case "share":
		if a.Share == nil {
			return "", fmt.Errorf("share needs the condition its entities meet")
		}
		condition, err := c.compile(*a.Share, 1)
		if err != nil {
			return "", err
		}
		return "100.0 * " + of("count(*)", condition) + " / nullif(" + of("count(*)") + ", 0)", nil
	case widget.OpDistinct:
		fld, err := c.field(a.Path)
		if err != nil {
			return "", err
		}
		if fld.many || fld.kind == "object" {
			return "", fmt.Errorf("distinct counts the values of a field holding one, and %s holds a list or an object", a.Path)
		}
		return of("count(DISTINCT nullif(" + fld.json() + ", 'null'::jsonb))"), nil
	case "sum", "avg", widget.OpMin, widget.OpMax, "p50", "p90", "p95", "p99":
		fld, err := c.field(a.Path)
		if err != nil {
			return "", err
		}
		switch {
		case dated(a.Op, fld) && a.Age:
			return "extract(epoch FROM now() - " + of(a.Op+"("+fld.time()+")") + ")", nil
		case dated(a.Op, fld):
			return "extract(epoch FROM " + of(a.Op+"("+fld.time()+")") + ")", nil
		case fld.many || (fld.kind != "number" && fld.kind != "integer"):
			if a.Op == widget.OpMin || a.Op == widget.OpMax {
				return "", fmt.Errorf("%s works on numbers and dates, and %s is neither", a.Op, a.Path)
			}
			return "", fmt.Errorf("%s works on numbers, and %s isn't one", a.Op, a.Path)
		}
		if fraction, ok := percentiles[a.Op]; ok {
			return of("percentile_cont(" + fraction + ") WITHIN GROUP (ORDER BY " + fld.number() + ")"), nil
		}
		return of(a.Op + "(" + fld.number() + ")"), nil
	}

	return "", fmt.Errorf("aggregate must be one of %s", strings.Join(Aggregates, ", "))
}

// Dated reports whether an aggregate of a blueprint's entities is the min
// or max of a date, which comes in seconds rather than the date's own unit.
func Dated(specs map[string]blueprint.Spec, spec blueprint.Spec, op, path string) bool {
	fld, err := fieldFor(specs, spec, path)
	return err == nil && dated(op, fld)
}

func dated(op string, fld field) bool {
	return (op == widget.OpMin || op == widget.OpMax) && !fld.many && fld.kind == kindDate
}

// aggregateColumns are aggregates as SQL columns, each a float8.
func aggregateColumns(c *compiler, aggregates []Aggregate) ([]string, error) {
	columns := make([]string, len(aggregates))
	for i, a := range aggregates {
		sql, err := aggregateSQL(c, a)
		if err != nil {
			return nil, FilterError{err}
		}
		columns[i] = "(" + sql + ")::float8"
	}

	return columns, nil
}

// scanValues reads aggregates as columns read them, nil for SQL's null.
func scanValues(values []sql.NullFloat64) []*float64 {
	out := make([]*float64, len(values))
	for i, value := range values {
		if value.Valid {
			out[i] = &value.Float64
		}
	}

	return out
}

// Aggregates works out every aggregate of m's entities in one statement.
// Each is nil when there's nothing to work on, such as the average of no
// values.
func (s Store) Aggregates(ctx context.Context, m Match, aggregates []Aggregate) ([]*float64, error) {
	c := m.compiler()
	columns, err := aggregateColumns(c, aggregates)
	if err != nil {
		return nil, err
	}
	values := make([]sql.NullFloat64, len(aggregates))
	targets := make([]any, len(values))
	for i := range values {
		targets[i] = &values[i]
	}
	if err := s.db.QueryRowContext(ctx, `SELECT `+strings.Join(columns, ", ")+` FROM entities e
	WHERE `+m.where, c.args...).Scan(targets...); err != nil {
		return nil, err
	}

	return scanValues(values), nil
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
	groups, without, err := s.GroupBy(ctx, m, path, nil, MostFirst, limit)
	if err != nil {
		return nil, 0, err
	}
	facets = make([]Facet, len(groups))
	for i, g := range groups {
		facets[i] = g.Facet
	}
	if without != nil {
		none = without.Count
	}

	return facets, none, nil
}

// GroupSort is the order groups come in: by how many entities each has, by
// one of the aggregates asked for, or by the group's value, in the order
// its property lists its values or its scorecard its tiers.
type GroupSort struct {
	// By is SortCount, SortGroup or the index of an aggregate.
	By         int
	Descending bool
}

// What a GroupSort can be by, beside an aggregate's index.
const (
	SortCount = -1
	SortGroup = -2
)

// MostFirst is the order groups come in unless asked for another.
var MostFirst = GroupSort{By: SortCount, Descending: true}

// Measured is a group and its aggregates, in the order they were asked for.
type Measured struct {
	Facet
	Values []*float64
}

// GroupBy works out aggregates for each value a field has across m's
// entities in one statement, up to limit values in sort's order. Lists
// count each item. None is the entities without a value, or with an empty
// list, or nil when there are none.
func (s Store) GroupBy(ctx context.Context, m Match, path string, aggregates []Aggregate, sort GroupSort, limit int) (groups []Measured, none *Measured, err error) {
	c := m.compiler()
	fld, err := groupField(c, path)
	if err != nil {
		return nil, nil, FilterError{err}
	}
	columns, err := aggregateColumns(c, aggregates)
	if err != nil {
		return nil, nil, err
	}
	var inner, outer, named string
	for i, column := range columns {
		inner += ", " + column + " AS v" + strconv.Itoa(i)
		outer += ", g.v" + strconv.Itoa(i)
		named += ", k.v" + strconv.Itoa(i)
	}
	value, from := fld.json(), " FROM entities e"
	if fld.many {
		from += `
	LEFT JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(` + fld.value + `) = 'array' THEN ` + fld.value + ` END) item ON true`
		value = "item"
	}
	if fld.connection != "" {
		value = fld.connection
	}
	grouped := `SELECT ` + value + ` AS value, count(*) AS count` + inner + from + `
	WHERE ` + m.where + `
	GROUP BY 1`
	// Groups are named after counting, once each, rather than per row.
	if fld.connection != "" {
		grouped = `SELECT to_jsonb(` + connectionOf("k.value") + `) AS value, k.count` + named + ` FROM (` + grouped + `) k`
	}
	title, join := "NULL::text", ""
	if fld.relation != "" {
		title = "t.title"
		join = `
	LEFT JOIN entities t ON t.blueprint_id = ` + BlueprintID(specNamed(c.specs, fld.relation)) + ` AND t.identifier = g.value #>> '{}'`
	}
	// The group without a value comes first, so the limit never cuts it.
	rows, err := s.db.QueryContext(ctx, `SELECT g.value, g.count, `+title+outer+` FROM (`+grouped+`) g`+join+`
	ORDER BY g.value IS NULL DESC, `+groupOrder(c, fld, m.ladders, sort)+`
	LIMIT `+c.arg(limit+1), c.args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	groups = []Measured{}
	for rows.Next() {
		var (
			raw   []byte
			named sql.NullString
			g     Measured
		)
		values := make([]sql.NullFloat64, len(aggregates))
		targets := []any{&raw, &g.Count, &named}
		for i := range values {
			targets = append(targets, &values[i])
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, nil, err
		}
		if len(aggregates) > 0 {
			g.Values = scanValues(values)
		}
		if raw == nil {
			none = &g
			continue
		}
		g.Title = named.String
		if err := json.Unmarshal(raw, &g.Value); err != nil {
			return nil, nil, err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(groups) > limit {
		groups = groups[:limit]
	}
	if ladder, ok := m.ladders[fld.scorecard]; ok && fld.kind == kindTier {
		for i, g := range groups {
			if at := slices.IndexFunc(ladder.Tiers, func(r blueprint.Rung) bool { return r.Name == g.Value }); at >= 0 {
				groups[i].Title, groups[i].Color = ladder.Tiers[at].Title, ladder.Tiers[at].Color
			}
		}
	}

	return groups, none, nil
}

// groupOrder is SQL ordering groups by sort. Empty keys sort lowest, as
// entities do. Ties go to the larger group, then the value, so the groups a
// limit keeps are always the same.
func groupOrder(c *compiler, fld field, ladders map[string]blueprint.Ladder, sort GroupSort) string {
	direction, empty := " ASC", " ASC NULLS FIRST"
	if sort.Descending {
		direction, empty = " DESC", " DESC NULLS LAST"
	}
	switch {
	case sort.By == SortCount:
		return "g.count" + direction + ", g.value"
	case sort.By >= 0:
		return "g.v" + strconv.Itoa(sort.By) + empty + ", g.count DESC, g.value"
	}
	var keys []string
	if listed := listedValues(fld, ladders); len(listed) > 0 {
		keys = append(keys, "array_position("+c.arg(pq.Array(listed))+"::text[], g.value #>> '{}')"+empty)
	}
	if fld.relation != "" {
		keys = append(keys, "t.title"+empty)
	}

	return strings.Join(append(keys, "g.value"+direction), ", ")
}

// listedValues are a field's values in the order its definition lists
// them: a scorecard's tiers up its ladder, or a property's enum, as text.
func listedValues(fld field, ladders map[string]blueprint.Ladder) []string {
	if fld.kind == kindTier {
		return ladders[fld.scorecard].Names()
	}
	if fld.prop == nil {
		return nil
	}
	values := fld.prop.Enum
	if values == nil && fld.prop.Items != nil {
		values = fld.prop.Items.Enum
	}
	listed := make([]string, len(values))
	for i, value := range values {
		listed[i] = fmt.Sprint(value)
	}

	return listed
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

// Column is what a table reads of each row: the value at Path, or with
// Condition, whether the row meets Filter. A condition without a filter, as
// one whose variables are all unset, is met by every row, as a query's is.
type Column struct {
	Path      string
	Condition bool
	Filter    *Filter
}

// Order is the order Rows reads rows in: Sort, a path with a leading - for
// descending, as the catalog writes sorts, or with ByCondition, whether
// each row meets the condition column at Column, false first unless
// Descending.
type Order struct {
	Sort        string
	ByCondition bool
	Column      int
	Descending  bool
}

// Rows reads the first limit of m's entities in order, with each column's
// value, and how many match in all.
func (s Store) Rows(ctx context.Context, m Match, order Order, limit int, columns []Column) ([]Row, int, error) {
	c := m.compiler()
	orderBy, err := rowOrder(c, m, order, columns)
	if err != nil {
		return nil, 0, FilterError{err}
	}
	var selects []string
	for i, column := range columns {
		value, links, err := rowColumn(c, column)
		if err != nil {
			return nil, 0, FilterError{err}
		}
		selects = append(selects, ", "+value+" AS k"+strconv.Itoa(i), ", "+links)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id, e.identifier, e.title`+strings.Join(selects, "")+` FROM entities e
	WHERE `+m.where+`
	ORDER BY `+orderBy+`
	LIMIT `+c.arg(limit), c.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []Row{}
	for rows.Next() {
		r := Row{Values: make([]any, len(columns)), Links: make([][]Link, len(columns))}
		raws := make([][]byte, 2*len(columns))
		targets := []any{&r.ID, &r.Identifier, &r.Title}
		for i := range raws {
			targets = append(targets, &raws[i])
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, 0, err
		}
		for i := range columns {
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
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	total, err := s.total(ctx, m.where, m.args, 0, len(out), limit)

	return out, total, err
}

// rowOrder is SQL ordering rows as order says. A condition column sorts
// false first ascending, as a boolean property does.
func rowOrder(c *compiler, m Match, order Order, columns []Column) (string, error) {
	if !order.ByCondition {
		return c.orderBy(order.Sort, m.ladders)
	}
	if order.Column < 0 || order.Column >= len(columns) || !columns[order.Column].Condition {
		return "", fmt.Errorf("column %d isn't a condition to sort by", order.Column)
	}
	direction := " ASC"
	if order.Descending {
		direction = " DESC"
	}

	return "k" + strconv.Itoa(order.Column) + direction + ", e.title, e.identifier", nil
}

// rowColumn is SQL for a column's value as jsonb and what it links to. A
// condition that comes out null, as one on a missing value does, is false.
func rowColumn(c *compiler, column Column) (value, links string, err error) {
	if column.Condition {
		condition := "true"
		if column.Filter != nil {
			if condition, err = c.compile(*column.Filter, 1); err != nil {
				return "", "", err
			}
		}
		return "to_jsonb(coalesce(" + condition + ", false))", "NULL::jsonb", nil
	}
	fld, err := c.field(column.Path)
	if err != nil {
		return "", "", err
	}
	links, err = rowLinks(c, fld, column.Path)

	return rowValue(fld), links, err
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
	LEFT JOIN entities t ON t.blueprint_id = ` + BlueprintID(specNamed(c.specs, fld.relation)) + ` AND t.identifier = x.id)`, nil
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
	c := &compiler{spec: spec, specs: specs, args: args}
	where, err := Query{Blueprint: spec, Filter: filter}.where(c)
	if err != nil {
		return "", nil, err
	}

	return `SELECT e.id FROM entities e WHERE ` + where, c.args, nil
}
