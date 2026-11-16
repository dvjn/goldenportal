// Package keyset pages lists by cursor. A page starts after, or ends
// before, the row whose sort values a cursor holds, so reading any page
// costs about what the first does, where an offset reads every row before
// it.
package keyset

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Type is the SQL type a column's values compare as.
type Type string

const (
	Text   Type = "text"
	Number Type = "numeric"
	Time   Type = "timestamptz"
	JSON   Type = "jsonb"
)

// Column is one a list is ordered by. Nullable columns sort empty values
// lowest: first ascending, last descending.
type Column struct {
	SQL      string
	Type     Type
	Desc     bool
	Nullable bool
}

func (c Column) direction() string {
	switch {
	case !c.Nullable && c.Desc:
		return " DESC"
	case !c.Nullable:
		return ""
	case c.Desc:
		return " DESC NULLS LAST"
	}

	return " ASC NULLS FIRST"
}

// Key is SQL for the column's value as JSON text, as a cursor holds it.
// Times are in UTC, so any session's time zone reads them back.
func (c Column) Key() string {
	if c.Type == Time {
		return `to_jsonb(to_char((` + c.SQL + `) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))::text`
	}

	return "to_jsonb(" + c.SQL + ")::text"
}

// Order is what a list is ordered by, the last columns telling every row
// apart, so pages neither skip nor repeat rows.
type Order []Column

// SQL is the ORDER BY for o, with exprs in place of the columns' own SQL
// when given.
func (o Order) SQL(exprs ...string) string {
	if exprs != nil {
		o = o.withSQL(exprs)
	}
	by := make([]string, len(o))
	for i, c := range o {
		by[i] = c.SQL + c.direction()
	}

	return strings.Join(by, ", ")
}

// Keys are SQL for each column's Key, which ReadRow reads after a row's
// own columns.
func (o Order) Keys() []string {
	keys := make([]string, len(o))
	for i, c := range o {
		keys[i] = c.Key()
	}

	return keys
}

func (o Order) withSQL(exprs []string) Order {
	o = slices.Clone(o)
	for i := range o {
		o[i].SQL = exprs[i]
	}

	return o
}

func (o Order) reversed() Order {
	r := slices.Clone(o)
	for i := range r {
		r[i].Desc = !r[i].Desc
	}

	return r
}

// direction is which way a cursor reads from its row.
type direction string

const (
	after  direction = "after"
	before direction = "before"
	last   direction = "last"
)

// List is the list a cursor pages. Name says which, readably, and Scope
// is what else picks and orders its rows, such as its search, filter and
// sort, which a cursor holds a hash of, so it pages no other list.
type List struct {
	Name  string
	Scope []string
}

func (l List) hash() string {
	sum := sha256.Sum256([]byte(strings.Join(append([]string{l.Name}, l.Scope...), "\x00")))
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

// cursor is what a page's links encode: which way to read from the row
// whose values Keys holds, as JSON, nil where empty, in the list List
// names and Hash is of.
type cursor struct {
	Dir  direction `json:"d"`
	List string    `json:"l"`
	Hash string    `json:"h"`
	Keys []*string `json:"k,omitempty"`
}

func (c cursor) encode() string {
	encoded, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decode(token string) (cursor, bool) {
	var c cursor
	raw, err := base64.RawURLEncoding.DecodeString(token)

	return c, err == nil && json.Unmarshal(raw, &c) == nil
}

// ListOf is the name of the list a cursor pages, or "" when token isn't one.
func ListOf(token string) string {
	c, _ := decode(token)
	return c.List
}

// ErrCursor is a cursor that isn't one the list gave.
var ErrCursor = errors.New("the cursor isn't one this list gave; start from the first page")

// Request is the page a list request asks for: the first without a
// cursor, else the last, or the one after or before a row.
type Request struct {
	order  Order
	list   List
	size   int
	cursor cursor
	// values are the cursor's keys as arguments, nil where empty.
	values []any
}

// NewRequest reads the page token asks for of list, in order, size rows
// to a page. A token this list didn't give is ErrCursor.
func NewRequest(order Order, list List, token string, size int) (Request, error) {
	r := Request{order: order, list: list, size: size}
	if token == "" {
		return r, nil
	}
	c, ok := decode(token)
	if !ok || c.List != list.Name || c.Hash != list.hash() {
		return Request{}, ErrCursor
	}
	r.cursor = c
	switch c.Dir {
	case last:
		if c.Keys != nil {
			return Request{}, ErrCursor
		}
	case after, before:
		values, err := order.values(c.Keys)
		if err != nil {
			return Request{}, err
		}
		r.values = values
	default:
		return Request{}, ErrCursor
	}

	return r, nil
}

// First reports whether r reads the first page, which alone counts the rows.
func (r Request) First() bool { return r.cursor.Dir == "" }

// values checks keys fit o's columns, as values Postgres takes, and
// returns them as arguments for them.
func (o Order) values(keys []*string) ([]any, error) {
	if len(keys) != len(o) {
		return nil, ErrCursor
	}
	values := make([]any, len(o))
	for i, c := range o {
		value, err := c.value(keys[i])
		if err != nil {
			return nil, err
		}
		values[i] = value
	}

	return values, nil
}

func (c Column) value(key *string) (any, error) {
	if key == nil {
		if !c.Nullable {
			return nil, ErrCursor
		}
		return nil, nil
	}
	raw := *key
	value, ok := parseJSON(raw)
	if !ok {
		return nil, ErrCursor
	}
	if c.Type == JSON {
		// Written again, escapes Postgres turns down, such as a lone surrogate, aren't passed on.
		encoded, err := json.Marshal(value)
		return string(encoded), err
	}
	switch v := value.(type) {
	case json.Number:
		if c.Type == Number {
			return raw, nil
		}
	case string:
		switch c.Type {
		case Text:
			return v, nil
		case Time:
			t, err := time.Parse(time.RFC3339Nano, v)
			if err != nil || t.UTC().Year() < 1 {
				return nil, ErrCursor
			}
			return t.UTC().Format(time.RFC3339Nano), nil
		}
	}

	return nil, ErrCursor
}

// parseJSON reads raw as JSON whose text and numbers Postgres takes: no
// NUL character, and numbers numeric can hold.
func parseJSON(raw string) (any, bool) {
	d := json.NewDecoder(bytes.NewReader([]byte(raw)))
	d.UseNumber()
	var value any
	if d.Decode(&value) != nil || d.More() {
		return nil, false
	}

	return value, takes(value)
}

func takes(value any) bool {
	switch v := value.(type) {
	case string:
		return !strings.ContainsRune(v, 0)
	case json.Number:
		return fitsNumeric(string(v))
	case []any:
		return !slices.ContainsFunc(v, func(item any) bool { return !takes(item) })
	case map[string]any:
		for key, item := range v {
			if strings.ContainsRune(key, 0) || !takes(item) {
				return false
			}
		}
	}

	return true
}

var exponent = regexp.MustCompile(`[eE]([-+]?[0-9]+)$`)

// fitsNumeric reports whether a JSON number is one numeric holds, far
// short of its limits on digits either side of the point.
func fitsNumeric(number string) bool {
	if len(number) > 1000 {
		return false
	}
	if m := exponent.FindStringSubmatch(number); m != nil {
		e, err := strconv.Atoi(m[1])
		return err == nil && e >= -1000 && e <= 1000
	}

	return true
}

// Plan is a statement reading part of a page: Limit rows in Order, from
// those Ranges picks.
type Plan struct {
	Order   Order
	Limit   int
	reverse bool
	values  []any
}

func (r Request) plan() Plan {
	p := Plan{Order: r.order, Limit: r.size + 1, values: r.values}
	if r.cursor.Dir == before || r.cursor.Dir == last {
		p.Order, p.reverse = r.order.reversed(), true
	}

	return p
}

// FromRow reports whether p reads on from a cursor's row, rather than from
// an end of the list.
func (p Plan) FromRow() bool { return p.values != nil }

// WithSQL is p with exprs in place of its columns' SQL, for a statement
// that reads them from a subquery.
func (p Plan) WithSQL(exprs []string) Plan {
	p.Order = p.Order.withSQL(exprs)
	return p
}

// Ranges are conditions picking the rows after the cursor's in p.Order,
// each a run of rows that sort together, in order, with the cursor's
// values as arguments arg numbers. Each is what an index on the order
// seeks to, where their OR, Predicate, can only filter. Without a cursor,
// one range has every row.
func (p Plan) Ranges(arg func(any) string) []string {
	if !p.FromRow() {
		return []string{"true"}
	}
	values := make([]string, len(p.values))
	for i, value := range p.values {
		if value != nil {
			values[i] = arg(value) + "::" + string(p.Order[i].Type)
		}
	}

	return ranges(p.Order, values, nil)
}

// Predicate is the ranges as one condition, for statements no index seeks.
func Predicate(ranges []string) string {
	return "(" + strings.Join(ranges, " OR ") + ")"
}

// ranges are the runs of rows after values, "" where empty, on cols,
// given the conditions in equal hold.
func ranges(cols Order, values, equal []string) []string {
	if len(cols) == 0 {
		return nil
	}
	if rowComparable(cols) {
		exprs := make([]string, len(cols))
		for i, c := range cols {
			exprs[i] = c.SQL
		}
		return []string{and(equal, row(exprs)+" "+laterThan(cols[0])+" "+row(values))}
	}
	c, value, rest := cols[0], values[0], values[1:]
	if value == "" {
		out := ranges(cols[1:], rest, slices.Concat(equal, []string{c.SQL + " IS NULL"}))
		if !c.Desc {
			out = append(out, and(equal, c.SQL+" IS NOT NULL"))
		}
		return out
	}
	out := ranges(cols[1:], rest, slices.Concat(equal, []string{c.SQL + " = " + value}))
	out = append(out, and(equal, c.SQL+" "+laterThan(c)+" "+value))
	if c.Desc && c.Nullable {
		out = append(out, and(equal, c.SQL+" IS NULL"))
	}

	return out
}

// rowComparable reports whether cols compare as one row: none empty, all
// one way.
func rowComparable(cols Order) bool {
	return !slices.ContainsFunc(cols, func(c Column) bool { return c.Nullable || c.Desc != cols[0].Desc })
}

// laterThan is the operator for values that come later in c's order.
func laterThan(c Column) string {
	if c.Desc {
		return "<"
	}

	return ">"
}

func row(exprs []string) string {
	if len(exprs) == 1 {
		return exprs[0]
	}

	return "(" + strings.Join(exprs, ", ") + ")"
}

func and(conditions []string, condition string) string {
	return "(" + strings.Join(append(slices.Clone(conditions), condition), " AND ") + ")"
}

// Row is an item a statement read, with its values in the list's order as
// JSON, nil where empty.
type Row[T any] struct {
	Item T
	Keys []*string
}

// ReadRow reads a row's item with read, and the keys keys columns after
// it hold, as Order.Keys selects them.
func ReadRow[T any](scan func(...any) error, keys int, read func(scan func(...any) error) (T, error)) (Row[T], error) {
	row := Row[T]{Keys: make([]*string, keys)}
	var err error
	row.Item, err = read(func(dest ...any) error {
		for i := range row.Keys {
			dest = append(dest, &row.Keys[i])
		}
		return scan(dest...)
	})

	return row, err
}

// Page is one page of a list. Total is how many items match in all, on
// the first page only. Next, Previous and Last are cursors to the pages
// beside it and the last one, where there are any.
type Page[T any] struct {
	Items    []T    `json:"items"`
	Total    *int   `json:"total,omitempty"`
	Next     string `json:"next,omitempty"`
	Previous string `json:"previous,omitempty"`
	Last     string `json:"last,omitempty"`
}

// Fetch reads the page r asks for with query, which runs a plan's
// statement. A page after the cursor's that comes up empty, as when rows
// went away, reads the last page instead, and one before it that comes up
// short, the first, so the first page is always whole.
func Fetch[T any](r Request, query func(Plan) ([]Row[T], error)) (Page[T], error) {
	rows, err := query(r.plan())
	if err != nil {
		return Page[T]{}, err
	}
	switch {
	case r.cursor.Dir == after && len(rows) == 0:
		r.cursor, r.values = cursor{Dir: last}, nil
		rows, err = query(r.plan())
	case r.cursor.Dir == before && len(rows) < r.size:
		r.cursor, r.values = cursor{}, nil
		rows, err = query(r.plan())
	}
	if err != nil {
		return Page[T]{}, err
	}

	more := len(rows) > r.size
	if more {
		rows = rows[:r.size]
	}
	p := r.plan()
	if p.reverse {
		slices.Reverse(rows)
	}
	page := Page[T]{Items: make([]T, len(rows))}
	for i, row := range rows {
		page.Items[i] = row.Item
	}
	if len(rows) == 0 {
		return page, nil
	}
	link := func(dir direction, keys []*string) string {
		return cursor{Dir: dir, List: r.list.Name, Hash: r.list.hash(), Keys: keys}.encode()
	}
	// Reading forwards, there's a row before; backwards, one after.
	hasPrevious, hasNext := more, more
	if p.reverse {
		hasNext = r.cursor.Dir == before
	} else {
		hasPrevious = !r.First()
	}
	if hasPrevious {
		page.Previous = link(before, rows[0].Keys)
	}
	if hasNext {
		page.Next, page.Last = link(after, rows[len(rows)-1].Keys), link(last, nil)
	}

	return page, nil
}

// Count sets the first page's Total: how many it holds when it's the
// only page, else what count says. Other pages leave it to the caller,
// who has it from the first.
func (p *Page[T]) Count(r Request, count func() (int, error)) error {
	if !r.First() {
		return nil
	}
	total := len(p.Items)
	if p.Next != "" {
		var err error
		if total, err = count(); err != nil {
			return err
		}
	}
	p.Total = &total

	return nil
}
