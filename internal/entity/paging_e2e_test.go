//go:build e2e

package entity

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

const pagingYAML = `kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    score: {type: number}
    status: {type: string}
    deployedAt: {type: string, format: date-time}
    parent: {relation: service}
`

// pagingFixture holds services whose values repeat and are often empty,
// and whose titles repeat too, so pages break inside runs of ties.
func pagingFixture(t *testing.T) (*storeFixture, map[string]blueprint.Spec) {
	t.Helper()
	f := newStoreFixture(t)
	f.publishBlueprint(pagingYAML)
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	var writes []Write
	for i := range 37 {
		properties := map[string]any{}
		if i%4 != 0 {
			properties["score"] = i % 3
		}
		if i%5 != 0 {
			properties["status"] = []string{"ok", "late", "ok", "failed"}[i%4]
		}
		switch i % 6 {
		case 0:
		case 1:
			properties["deployedAt"] = "not a date"
		default:
			properties["deployedAt"] = base.Add(time.Duration(i%4) * time.Hour).Format(time.RFC3339)
		}
		w := entityWrite(fmt.Sprintf("svc-%02d", i), []string{"Alpha", "Bravo", "Charlie"}[i%3], properties)
		if i%3 != 0 {
			w.Entity.Relations = map[string]any{"parent": fmt.Sprintf("svc-%02d", i%7)}
		}
		writes = append(writes, w)
	}
	f.write(f.mine, writes...)
	specs, err := blueprint.Published(t.Context(), f.db)
	if err != nil {
		t.Fatal(err)
	}

	return f, specs
}

// pageOf is what a page read by a cursor shows of a list.
type pageOf struct {
	items                []string
	total                *int
	next, previous, last string
}

// walk reads a list page by page: forwards from the first page by next,
// and backwards from the last by previous. Only the first page has the
// total. Going back, the first page is read whole, so the rows it shares
// with the page after it are left out.
func walk(t *testing.T, read func(cursor string) pageOf) (forwards, backwards []string) {
	t.Helper()
	first := read("")
	if first.total == nil {
		t.Fatal("the first page has no total")
	}
	pages := []pageOf{first}
	for page := first; page.next != ""; {
		page = read(page.next)
		if page.total != nil {
			t.Fatal("a page after the first has a total")
		}
		// Back from each page is the one before it.
		if back := read(page.previous); !slices.Equal(back.items, pages[len(pages)-1].items) {
			t.Fatalf("previous from %v = %v, want %v", page.items, back.items, pages[len(pages)-1].items)
		}
		pages = append(pages, page)
	}
	for _, page := range pages {
		forwards = append(forwards, page.items...)
	}
	if len(forwards) != *first.total {
		t.Errorf("read %d of a total of %d", len(forwards), *first.total)
	}
	if first.last == "" {
		return forwards, forwards
	}
	for page := read(first.last); ; page = read(page.previous) {
		if page.total != nil {
			t.Fatal("a page read backwards has a total")
		}
		if page.previous == "" {
			if !slices.Equal(page.items, first.items) {
				t.Fatalf("back at the start %v, want the first page %v", page.items, first.items)
			}
			backwards = append(startOf(t, page.items, backwards), backwards...)
			break
		}
		backwards = append(slices.Clone(page.items), backwards...)
	}

	return forwards, backwards
}

// startOf is what of the first page comes before rest, which may start
// inside it.
func startOf(t *testing.T, first, rest []string) []string {
	t.Helper()
	at := len(first)
	if len(rest) > 0 {
		if i := slices.Index(first, rest[0]); i >= 0 {
			at = i
		}
	}
	if shared := first[at:]; len(rest) < len(shared) || !slices.Equal(shared, rest[:len(shared)]) {
		t.Fatalf("the first page %v doesn't lead into %v", first, rest)
	}

	return slices.Clone(first[:at])
}

// TestCursorPagesMatchTheWholeList checks reading a list by cursor, either
// way, gives every entity once, in the order a single read gives, for
// sorts an index can hold and those it can't, through empty values and
// runs of equal ones.
func TestCursorPagesMatchTheWholeList(t *testing.T) {
	f, specs := pagingFixture(t)
	sorts := []string{
		"", "title", "-title", "identifier", "-identifier", "score", "-score", "status", "-status",
		"deployedAt", "-deployedAt", "parent", "-parent", "parent.title", "-parent.title", "parent.score",
		"-createdAt", "connection",
	}
	for _, sort := range sorts {
		t.Run(sort, func(t *testing.T) {
			q := Query{Blueprint: specs["service"], Blueprints: specs, Sort: sort, PageSize: 100}
			whole, err := f.store.List(t.Context(), q)
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, e := range whole.Items {
				want = append(want, e.Identifier)
			}
			for _, size := range []int{1, 4, 10} {
				q.PageSize = size
				forwards, backwards := walk(t, func(cursor string) pageOf {
					q.Cursor = cursor
					page, err := f.store.List(t.Context(), q)
					if err != nil {
						t.Fatal(err)
					}
					got := pageOf{total: page.Total, next: page.Next, previous: page.Previous, last: page.Last}
					for _, e := range page.Items {
						got.items = append(got.items, e.Identifier)
					}
					return got
				})
				if !slices.Equal(forwards, want) || !slices.Equal(backwards, want) {
					t.Errorf("size %d:\n forwards %v\nbackwards %v\n     want %v", size, forwards, backwards, want)
				}
			}
		})
	}
}

// TestCursorFiltersAndSearches checks a cursor pages what the filter and
// search match, and that one given for another sort is turned down.
func TestCursorFiltersAndSearches(t *testing.T) {
	f, specs := pagingFixture(t)
	filter, err := ParseFilter(`{"property": "status", "op": "eq", "value": "ok"}`)
	if err != nil {
		t.Fatal(err)
	}
	q := Query{Blueprint: specs["service"], Blueprints: specs, Search: "alp", Filter: filter, Sort: "-score", PageSize: 2}
	forwards, backwards := walk(t, func(cursor string) pageOf {
		q.Cursor = cursor
		page, err := f.store.List(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		got := pageOf{total: page.Total, next: page.Next, previous: page.Previous, last: page.Last}
		for _, e := range page.Items {
			if e.Title != "Alpha" || e.Properties["status"] != "ok" {
				t.Errorf("%s doesn't match: %s, %v", e.Identifier, e.Title, e.Properties["status"])
			}
			got.items = append(got.items, e.Identifier)
		}
		return got
	})
	if len(forwards) == 0 || !slices.Equal(forwards, backwards) {
		t.Errorf("forwards %v, backwards %v", forwards, backwards)
	}

	q.Cursor = ""
	page, err := f.store.List(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	// A cursor pages only the list it came from.
	other, err := ParseFilter(`{"property": "status", "op": "eq", "value": "late"}`)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(q *Query){
		"sort":      func(q *Query) { q.Sort = "score" },
		"search":    func(q *Query) { q.Search = "alpha" },
		"filter":    func(q *Query) { q.Filter = other },
		"no filter": func(q *Query) { q.Filter = nil },
	} {
		changed := q
		changed.Cursor = page.Next
		change(&changed)
		var filterErr FilterError
		if _, err := f.store.List(t.Context(), changed); !errors.As(err, &filterErr) {
			t.Errorf("another %s: err = %v, want a FilterError", name, err)
		}
	}
}
