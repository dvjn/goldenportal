//go:build e2e

package entity

import (
	"database/sql"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

const shapesYAML = `kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    score: {type: number}
    seenAt: {type: string, format: date-time}
    deployedAt: {type: string, format: date-time}
    bornOn: {type: string, format: date}
    parent: {relation: service}
    dependsOn: {relation: service, many: true}
  retention: {by: seenAt, keep: 3650d}
`

// shapesFixture holds services whose numbers and dates aren't all numbers
// and dates, as values written before a property's type changed can be.
func shapesFixture(t *testing.T) (*storeFixture, map[string]blueprint.Spec) {
	t.Helper()
	f := newStoreFixture(t)
	f.publishBlueprint(shapesYAML)
	seen := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	alpha := entityWrite("alpha", "Alpha", map[string]any{"score": 3, "seenAt": seen.Format(time.RFC3339), "deployedAt": "2026-11-02T10:00:00+05:30", "bornOn": "2026-10-25"})
	alpha.OccurredAt = seen
	f.write(f.mine,
		alpha,
		entityWrite("bravo", "Bravo", map[string]any{"score": "high", "deployedAt": "yesterday"}),
		entityWrite("charlie", "Kalamata", map[string]any{"deployedAt": "2026-02-30T00:00:00Z"}),
		entityWrite("delta", "Delta", map[string]any{"deployedAt": "2026-11-02T00:00:00"}),
	)
	parent := entityWrite("echo", "Echo", map[string]any{"bornOn": "2026-10-20"})
	parent.Entity.Relations = map[string]any{"parent": "alpha"}
	depends := entityWrite("foxtrot", "Foxtrot", map[string]any{})
	depends.Entity.Relations = map[string]any{"dependsOn": []any{"bravo", "alpha"}}
	f.write(f.mine, parent, depends)
	specs, err := blueprint.Published(t.Context(), f.db)
	if err != nil {
		t.Fatal(err)
	}

	return f, specs
}

func (f *storeFixture) list(specs map[string]blueprint.Spec, q Query) []string {
	f.t.Helper()
	q.Blueprint, q.Blueprints, q.Page, q.PageSize = specs["service"], specs, 1, 10
	page, err := f.store.List(f.t.Context(), q)
	if err != nil {
		f.t.Fatal(err)
	}
	identifiers := make([]string, len(page.Items))
	for i, e := range page.Items {
		identifiers[i] = e.Identifier
	}
	if page.Total != len(identifiers) {
		f.t.Errorf("total = %d for %v", page.Total, identifiers)
	}

	return identifiers
}

// TestAccessorsReadOnlyNumbersAndDates checks a value that isn't a number,
// or a date with an offset, reads as empty, where a cast would fail the
// statement or depend on the session's time zone.
func TestAccessorsReadOnlyNumbersAndDates(t *testing.T) {
	f, specs := shapesFixture(t)
	for _, test := range []struct{ filter, want string }{
		{`{"property":"score","op":"gt","value":1}`, "alpha"},
		{`{"property":"deployedAt","op":"lt","value":"2027-01-01T00:00:00Z"}`, "alpha"},
		{`{"property":"deployedAt","op":"within","value":"36500d"}`, "alpha"},
		{`{"property":"bornOn","op":"gte","value":"2026-10-22T00:00:00Z"}`, "alpha"},
		{`{"property":"bornOn","op":"lt","value":"2026-10-22T00:00:00Z"}`, "echo"},
	} {
		filter, err := ParseFilter(test.filter)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(f.list(specs, Query{Filter: filter}), ","); got != test.want {
			t.Errorf("%s = %s, want %s", test.filter, got, test.want)
		}
	}

	m, err := NewMatch(Query{Blueprint: specs["service"], Blueprints: specs})
	if err != nil {
		t.Fatal(err)
	}
	sums, err := f.store.Aggregates(t.Context(), m, []Aggregate{{Op: "sum", Path: "score"}})
	if err != nil || sums[0] == nil || *sums[0] != 3 {
		t.Errorf("sum = %v, %v, want 3", sums, err)
	}

	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	days, all, err := f.store.Trend(t.Context(), m, "bornOn", from, from.AddDate(0, 1, 0), nil)
	if err != nil || len(days) != 2 || all.Count != 2 || days[0].Day.Day()+days[1].Day.Day() != 45 {
		t.Errorf("days = %+v, all %+v, %v; want the 20th and the 25th", days, all, err)
	}

	var at sql.NullTime
	for value, valid := range map[string]bool{
		"2026-11-02T10:00:00+05:30": true, "2026-11-02T10:00:00.5z": true, "2026-02-30T00:00:00Z": false,
		"2026-11-02T00:00:00": false, "2026-10-25": true, "2026-02-30": false, "now": false, "": false,
	} {
		if err := f.db.QueryRowContext(t.Context(), `SELECT gp_time($1)`, value).Scan(&at); err != nil || at.Valid != valid {
			t.Errorf("gp_time(%q) = %v, %v, want a time %t", value, at, err, valid)
		}
	}
}

// TestEmptyValuesSortLowest checks empty values come first going up and
// last coming down, by a property and by the time retention keeps rows by.
func TestEmptyValuesSortLowest(t *testing.T) {
	f, specs := shapesFixture(t)
	for sort, want := range map[string]string{
		"score":       "bravo,delta,echo,foxtrot,charlie,alpha",
		"-score":      "alpha,bravo,delta,echo,foxtrot,charlie",
		"deployedAt":  "bravo,delta,echo,foxtrot,charlie,alpha",
		"-deployedAt": "alpha,bravo,delta,echo,foxtrot,charlie",
		"seenAt":      "bravo,delta,echo,foxtrot,charlie,alpha",
		"-seenAt":     "alpha,bravo,delta,echo,foxtrot,charlie",
		"":            "alpha,bravo,delta,echo,foxtrot,charlie",
		"bornOn":      "bravo,delta,foxtrot,charlie,echo,alpha",
		"-bornOn":     "alpha,echo,bravo,delta,foxtrot,charlie",
	} {
		if got := strings.Join(f.list(specs, Query{Sort: sort}), ","); got != want {
			t.Errorf("sort %q = %s, want %s", sort, got, want)
		}
	}
}

// TestIncomingFindsBothShapesOfRelation checks the entities pointing at
// one are found by containment, through a relation to one and to many.
func TestIncomingFindsBothShapesOfRelation(t *testing.T) {
	f, specs := shapesFixture(t)
	incoming, err := f.store.Incoming(t.Context(), specs, "service", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, in := range incoming {
		var identifiers []string
		for _, link := range in.Entities {
			identifiers = append(identifiers, link.Identifier)
		}
		got[in.Property] = strings.Join(identifiers, ",")
		if in.Total != len(in.Entities) {
			t.Errorf("%s total = %d", in.Property, in.Total)
		}
	}
	if got["parent"] != "echo" || got["dependsOn"] != "foxtrot" || len(got) != 2 {
		t.Errorf("incoming = %v", got)
	}
}

// TestSearchMatchesAnywhereFromThreeCharacters checks search finds text
// anywhere in a title or identifier from three characters, and shorter
// text only at the start.
func TestSearchMatchesAnywhereFromThreeCharacters(t *testing.T) {
	f, specs := shapesFixture(t)
	all := slices.Collect(maps.Values(specs))
	for text, want := range map[string]string{
		"ala": "charlie", "ALP": "alpha", "al": "alpha", "a": "alpha", "harl": "charlie", "ka": "charlie", "%": "",
	} {
		found, err := f.store.Search(t.Context(), all, text, 10)
		if err != nil {
			t.Fatal(err)
		}
		var identifiers []string
		for _, r := range found {
			identifiers = append(identifiers, r.Identifier)
			if r.Blueprint != "service" {
				t.Errorf("%q found %+v", text, r)
			}
		}
		if got := strings.Join(identifiers, ","); got != want {
			t.Errorf("search %q = %s, want %s", text, got, want)
		}
		if got := strings.Join(f.list(specs, Query{Search: text}), ","); got != want {
			t.Errorf("catalog search %q = %s, want %s", text, got, want)
		}
	}
}
