package entity

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
)

func TestSearchEscapesAndRanks(t *testing.T) {
	store, mock := mockStore(t)
	// LIKE wildcards in the text match themselves; the exact text ranks first.
	mock.ExpectQuery(regexp.QuoteMeta(`ORDER BY lower(e.title) = lower($3)`)).
		WithArgs(pq.Array([]string{"service", "team"}), `%50\%%`, "50%", `50\%%`, 20).
		WillReturnRows(sqlmock.NewRows([]string{"b", "e", "t"}).AddRow("service", "ledger", "Ledger 50%"))

	found, err := store.Search(t.Context(), []string{"service", "team"}, "50%", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0] != (SearchResult{Blueprint: "service", Identifier: "ledger", Title: "Ledger 50%"}) {
		t.Errorf("found = %+v", found)
	}
}
