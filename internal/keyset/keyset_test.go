package keyset

import (
	"encoding/base64"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

var ties = Order{{SQL: "title", Type: Text}, {SQL: "id", Type: Text}}

func byKey(desc bool) Order {
	return append(Order{{SQL: "k", Type: Number, Desc: desc, Nullable: true}}, ties...)
}

var list = List{Name: "things", Scope: []string{"k"}}

func key(json string) *string { return &json }

// token is a cursor list could have given.
func token(dir direction, keys ...*string) string {
	return cursor{Dir: dir, List: list.Name, Hash: list.hash(), Keys: keys}.encode()
}

func numbered() func(any) string {
	n := 0
	return func(any) string {
		n++
		return "$" + strconv.Itoa(n)
	}
}

// TestRanges checks the rows after a cursor are runs an index seeks to,
// with empty values lowest: before every value going up, after every
// value coming down.
func TestRanges(t *testing.T) {
	tests := []struct {
		name  string
		order Order
		dir   direction
		keys  []*string
		want  []string
	}{
		{
			name: "title order compares as a row", order: ties, dir: after, keys: []*string{key(`"a"`), key(`"x"`)},
			want: []string{"((title, id) > ($1::text, $2::text))"},
		},
		{
			name: "title order backwards", order: ties, dir: before, keys: []*string{key(`"a"`), key(`"x"`)},
			want: []string{"((title, id) < ($1::text, $2::text))"},
		},
		{
			name: "ascending: ties, then greater; empty values came first", order: byKey(false), dir: after,
			keys: []*string{key("3"), key(`"a"`), key(`"x"`)},
			want: []string{"(k = $1::numeric AND (title, id) > ($2::text, $3::text))", "(k > $1::numeric)"},
		},
		{
			name: "ascending from an empty value: the other empty ones, then every value", order: byKey(false), dir: after,
			keys: []*string{nil, key(`"a"`), key(`"x"`)},
			want: []string{"(k IS NULL AND (title, id) > ($1::text, $2::text))", "(k IS NOT NULL)"},
		},
		{
			name: "descending: ties, then less, then empty values", order: byKey(true), dir: after,
			keys: []*string{key("3"), key(`"a"`), key(`"x"`)},
			want: []string{"(k = $1::numeric AND (title, id) > ($2::text, $3::text))", "(k < $1::numeric)", "(k IS NULL)"},
		},
		{
			name: "descending from an empty value: only empty ones are left", order: byKey(true), dir: after,
			keys: []*string{nil, key(`"a"`), key(`"x"`)},
			want: []string{"(k IS NULL AND (title, id) > ($1::text, $2::text))"},
		},
		{
			name: "before, ascending: reads down, ties backwards, then empty values", order: byKey(false), dir: before,
			keys: []*string{key("3"), key(`"a"`), key(`"x"`)},
			want: []string{"(k = $1::numeric AND (title, id) < ($2::text, $3::text))", "(k < $1::numeric)", "(k IS NULL)"},
		},
		{
			name: "before an empty value, descending: the empty ones before it, then every value", order: byKey(true), dir: before,
			keys: []*string{nil, key(`"a"`), key(`"x"`)},
			want: []string{"(k IS NULL AND (title, id) < ($1::text, $2::text))", "(k IS NOT NULL)"},
		},
		{
			name: "a column no value is empty in, then one that may be", order: append(Order{{SQL: "rank", Type: Number}, {SQL: "score", Type: Number, Nullable: true}}, ties...),
			dir: after, keys: []*string{key("2"), nil, key(`"a"`), key(`"x"`)},
			want: []string{
				"(rank = $1::numeric AND score IS NULL AND (title, id) > ($2::text, $3::text))",
				"(rank = $1::numeric AND score IS NOT NULL)",
				"(rank > $1::numeric)",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, err := NewRequest(test.order, list, token(test.dir, test.keys...), 10)
			if err != nil {
				t.Fatal(err)
			}
			if got := r.plan().Ranges(numbered()); !slices.Equal(got, test.want) {
				t.Errorf("ranges =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(test.want, "\n"))
			}
		})
	}
}

func TestOrderReadsBackwardsForPreviousAndLast(t *testing.T) {
	order := byKey(false)
	keys := []*string{key("1"), key(`"a"`), key(`"x"`)}
	for token, want := range map[string]string{
		"":                     "k ASC NULLS FIRST, title, id",
		token(after, keys...):  "k ASC NULLS FIRST, title, id",
		token(before, keys...): "k DESC NULLS LAST, title DESC, id DESC",
		token(last):            "k DESC NULLS LAST, title DESC, id DESC",
	} {
		r, err := NewRequest(order, list, token, 10)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.plan(); got.Order.SQL() != want || got.Limit != 11 {
			t.Errorf("%q reads %s limit %d, want %s", token, got.Order.SQL(), got.Limit, want)
		}
	}
	if ranges := (Request{order: order}).plan().Ranges(numbered()); !slices.Equal(ranges, []string{"true"}) {
		t.Errorf("first page ranges = %v", ranges)
	}
}

// TestTimesReadInUTC checks a time key is written in UTC and read back
// as it was, whatever offset a cursor gives it.
func TestTimesReadInUTC(t *testing.T) {
	at := Column{SQL: "at", Type: Time}
	if got := at.Key(); got != `to_jsonb(to_char((at) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))::text` {
		t.Errorf("key = %s", got)
	}
	value, err := at.value(key(`"2026-11-01T10:00:00.5+05:53:28"`))
	if err == nil {
		t.Errorf("an offset in seconds read as %v", value)
	}
	value, err = at.value(key(`"2026-11-01T10:00:00.5+05:30"`))
	if err != nil || value != "2026-11-01T04:30:00.5Z" {
		t.Errorf("value = %v, %v", value, err)
	}
	if _, err := time.Parse(time.RFC3339Nano, "2026-11-01T04:30:00.500000Z"); err != nil {
		t.Errorf("Key's format doesn't read back: %v", err)
	}
}

// TestTamperedCursors checks a cursor the list didn't give, for its
// search, filter and sort, or holding values its columns or Postgres
// can't take, is ErrCursor.
func TestTamperedCursors(t *testing.T) {
	order := append(Order{{SQL: "at", Type: Time, Nullable: true}}, ties...)
	good := []*string{key(`"2026-11-01T10:00:00.5+05:30"`), key(`"a"`), key(`"x"`)}
	if _, err := NewRequest(order, list, token(after, good...), 10); err != nil {
		t.Fatalf("a good cursor: %v", err)
	}
	b64 := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	other := List{Name: list.Name, Scope: []string{"-k"}}
	keys := func(k ...*string) []*string { return k }
	tests := map[string]string{
		"not base64":        "!!",
		"not JSON":          b64("{"),
		"another list":      cursor{Dir: after, List: "others", Hash: list.hash(), Keys: good}.encode(),
		"another scope":     cursor{Dir: after, List: list.Name, Hash: other.hash(), Keys: good}.encode(),
		"no direction":      token("", good...),
		"unknown direction": token("sideways", good...),
		"too few keys":      token(after, good[1:]...),
		"no keys":           token(before),
		"last with keys":    token(last, good...),
		"not a date":        token(after, keys(key(`"yesterday"`), key(`"a"`), key(`"x"`))...),
		"year zero":         token(after, keys(key(`"0000-12-31T23:00:00Z"`), key(`"a"`), key(`"x"`))...),
		"before year one":   token(after, keys(key(`"0001-01-01T00:30:00+01:00"`), key(`"a"`), key(`"x"`))...),
		"a number for text": token(after, keys(nil, key("1"), key(`"x"`))...),
		"empty title":       token(after, keys(nil, nil, key(`"x"`))...),
		"key not JSON":      token(after, keys(nil, key(`"a`), key(`"x"`))...),
		"two values":        token(after, keys(nil, key(`"a" "b"`), key(`"x"`))...),
		"NUL in text":       token(after, keys(nil, key(`"a\u0000"`), key(`"x"`))...),
		"injected SQL":      token(after, keys(key(`"x'; DROP TABLE entities; --"`), key(`"a"`), key(`"x"`))...),
	}
	for name, token := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := NewRequest(order, list, token, 10); !errors.Is(err, ErrCursor) {
				t.Errorf("err = %v, want ErrCursor", err)
			}
		})
	}

	numbers := Order{{SQL: "n", Type: Number, Nullable: true}, {SQL: "j", Type: JSON, Nullable: true}, {SQL: "id", Type: Text}}
	for name, keys := range map[string][]*string{
		"text for a number": {key(`"3"`), nil, key(`"x"`)},
		"overflows numeric": {key("1e999999"), nil, key(`"x"`)},
		"too many digits":   {key(strings.Repeat("9", 1001)), nil, key(`"x"`)},
		"NUL in JSON":       {nil, key(`{"a": "\u0000"}`), key(`"x"`)},
		"NUL in a JSON key": {nil, key(`{"\u0000": 1}`), key(`"x"`)},
		"number in JSON":    {nil, key(`[1, 2e5000]`), key(`"x"`)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewRequest(numbers, list, token(after, keys...), 10); !errors.Is(err, ErrCursor) {
				t.Errorf("err = %v, want ErrCursor", err)
			}
		})
	}
	if _, err := NewRequest(numbers, list, token(after, key("-1.5e3"), key(`{"a": [null, true, "b"]}`), key(`"x"`)), 10); err != nil {
		t.Errorf("good numbers and JSON: %v", err)
	}
}

func TestListOf(t *testing.T) {
	if got := ListOf(token(last)); got != "things" {
		t.Errorf("ListOf = %q", got)
	}
	if got := ListOf("nope"); got != "" {
		t.Errorf("ListOf(nope) = %q", got)
	}
}

// rows are what a fake statement returns: items named by their position.
func rows(names ...string) []Row[string] {
	out := make([]Row[string], len(names))
	for i, name := range names {
		out[i] = Row[string]{Item: name, Keys: []*string{key(`"` + name + `"`), key(`"` + name + `"`)}}
	}

	return out
}

func cursorOf(t *testing.T, token string) cursor {
	t.Helper()
	r, err := NewRequest(ties, list, token, 2)
	if err != nil {
		t.Fatal(err)
	}

	return r.cursor
}

// TestFetchLinks checks each page links to those beside it and the last,
// reading backwards for previous and last, and only the first counts.
func TestFetchLinks(t *testing.T) {
	read := func(t *testing.T, token string, got ...[]Row[string]) (Page[string], []Plan) {
		t.Helper()
		r, err := NewRequest(ties, list, token, 2)
		if err != nil {
			t.Fatal(err)
		}
		var plans []Plan
		page, err := Fetch(r, func(p Plan) ([]Row[string], error) {
			plans = append(plans, p)
			out := got[0]
			got = got[1:]
			return out, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		counted := false
		if err := page.Count(r, func() (int, error) { counted = true; return 9, nil }); err != nil {
			t.Fatal(err)
		}
		if counted && token != "" {
			t.Errorf("counted for %q", token)
		}
		return page, plans
	}

	first, _ := read(t, "", rows("a", "b", "c"))
	if !slices.Equal(first.Items, []string{"a", "b"}) || first.Total == nil || *first.Total != 9 || first.Previous != "" ||
		cursorOf(t, first.Next).Dir != after || *cursorOf(t, first.Next).Keys[0] != `"b"` || cursorOf(t, first.Last).Dir != last {
		t.Errorf("first = %+v", first)
	}

	next, _ := read(t, first.Next, rows("c", "d", "e"))
	if !slices.Equal(next.Items, []string{"c", "d"}) || next.Total != nil || *cursorOf(t, next.Previous).Keys[0] != `"c"` ||
		cursorOf(t, next.Previous).Dir != before || next.Next == "" {
		t.Errorf("next = %+v", next)
	}

	// The last page reads backwards and turns the rows round.
	final, plans := read(t, first.Last, rows("e", "d", "c"))
	if !slices.Equal(final.Items, []string{"d", "e"}) || final.Next != "" || final.Last != "" ||
		*cursorOf(t, final.Previous).Keys[0] != `"d"` || !plans[0].reverse {
		t.Errorf("last = %+v", final)
	}

	previous, _ := read(t, final.Previous, rows("c", "b", "a"))
	if !slices.Equal(previous.Items, []string{"b", "c"}) || previous.Previous == "" || *cursorOf(t, previous.Next).Keys[0] != `"c"` {
		t.Errorf("previous = %+v", previous)
	}

	// Back at the start, a short page reads the first page whole.
	start, plans := read(t, previous.Previous, rows("a"), rows("a", "b", "c"))
	if !slices.Equal(start.Items, []string{"a", "b"}) || start.Previous != "" || start.Next == "" || len(plans) != 2 || plans[1].reverse {
		t.Errorf("start = %+v", start)
	}

	// Past the end, as when rows went away, reads the last page.
	end, plans := read(t, next.Next, rows(), rows("e", "d"))
	if !slices.Equal(end.Items, []string{"d", "e"}) || end.Next != "" || end.Previous != "" || len(plans) != 2 || !plans[1].reverse {
		t.Errorf("end = %+v", end)
	}

	empty, _ := read(t, "", rows())
	if len(empty.Items) != 0 || *empty.Total != 0 || empty.Next != "" {
		t.Errorf("empty = %+v", empty)
	}
}
