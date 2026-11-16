//go:build e2e

package scorecard

import (
	"slices"
	"testing"
)

// walked reads a list by cursor, size items to a page, by read, which
// returns a page's identifiers, total and links: forwards from the first
// page by next, and backwards from the last by previous, where the first
// page is read whole, so the rows it shares with the page after it are
// left out. It fails t unless both give want, and only the first page has
// the total.
func walked(t *testing.T, want []string, read func(cursor string) (items []string, total *int, next, previous, last string)) {
	t.Helper()
	var forwards, backwards []string
	items, total, next, _, last := read("")
	if total == nil || *total != len(want) {
		t.Fatalf("the first page's total = %v, want %d", total, len(want))
	}
	forwards = append(forwards, items...)
	for next != "" {
		var page []string
		page, total, next, _, _ = read(next)
		if total != nil {
			t.Fatal("a page after the first has a total")
		}
		forwards = append(forwards, page...)
	}
	backwards = forwards
	if last != "" {
		backwards = nil
		for cursor := last; cursor != ""; {
			var page []string
			page, _, _, cursor, _ = read(cursor)
			if cursor == "" && len(backwards) > 0 {
				if at := slices.Index(page, backwards[0]); at >= 0 {
					page = page[:at]
				}
			}
			backwards = append(slices.Clone(page), backwards...)
		}
	}
	if !slices.Equal(forwards, want) || !slices.Equal(backwards, want) {
		t.Errorf("forwards %v, backwards %v, want %v", forwards, backwards, want)
	}
}
