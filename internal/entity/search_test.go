package entity

import (
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

func TestSearchEscapesAndRanks(t *testing.T) {
	store, mock := mockStore(t)
	// LIKE wildcards in the text match themselves; the exact text ranks
	// first, and blueprints tie in identifier order.
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE e.blueprint_id = ANY($1::uuid[])
			AND (lower(e.title) LIKE lower($2) OR lower(e.identifier) LIKE lower($2))
		ORDER BY lower(e.title) = lower($3)`)).
		WithArgs(pq.Array([]string{serviceID, teamID}), `%50\%%`, "50%", `50\%%`, 20).
		WillReturnRows(sqlmock.NewRows([]string{"b", "e", "t"}).AddRow(serviceID, "ledger", "Ledger 50%"))

	specs := []blueprint.Spec{{ID: teamID, Identifier: "team"}, {ID: serviceID, Identifier: "service"}}
	found, err := store.Search(t.Context(), specs, "50%", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0] != (SearchResult{Blueprint: "service", Identifier: "ledger", Title: "Ledger 50%"}) {
		t.Errorf("found = %+v", found)
	}
}

func TestTextPattern(t *testing.T) {
	// Trigram indexes find text of three characters or more anywhere;
	// shorter text matches the start.
	tests := map[string]string{"drift": "%drift%", "dri": "%dri%", "dr": "dr%", "é": "é%", "a_": `a\_%`, "日本語": "%日本語%"}
	for text, want := range tests {
		if got := TextPattern(text); got != want {
			t.Errorf("TextPattern(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestListSearchesWithTrigramsAndCountsApart(t *testing.T) {
	store, mock := mockStore(t)
	spec := service(t)
	spec.ID = serviceID
	where := `WHERE e.blueprint_id = '` + serviceID + `' AND (lower(e.title) LIKE lower($1) OR lower(e.identifier) LIKE lower($1))`
	columns := []string{"id", "b", "i", "t", "p", "r", "c", "conn", "ca", "ua"}
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	row := func(rows *sqlmock.Rows, identifier string) *sqlmock.Rows {
		return rows.AddRow("id-"+identifier, "service", identifier, identifier, []byte(`{}`), []byte(`{}`), []byte(`{}`), "gitlab", t0, t0)
	}
	mock.ExpectQuery(`^SELECT e.id, 'service', .* FROM entities e\s+`+regexp.QuoteMeta(where)+`\s+ORDER BY e.title, e.identifier\s+LIMIT \$2 OFFSET \$3$`).
		WithArgs("%led%", 2, 0).
		WillReturnRows(row(row(sqlmock.NewRows(columns), "ledger"), "ledgers"))
	mock.ExpectQuery(`^SELECT count\(\*\) FROM entities e ` + regexp.QuoteMeta(where) + `$`).
		WithArgs("%led%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))

	page, err := store.List(t.Context(), Query{Blueprint: spec, Search: "led", Page: 1, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Total != 7 {
		t.Errorf("page = %d items of %d", len(page.Items), page.Total)
	}

	// A page with room to spare is the last, so it needs no count.
	mock.ExpectQuery(`^SELECT e.id, 'service', .*LIMIT \$2 OFFSET \$3$`).
		WithArgs("le%", 2, 2).
		WillReturnRows(row(sqlmock.NewRows(columns), "ledger"))
	page, err = store.List(t.Context(), Query{Blueprint: spec, Search: "le", Page: 2, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 {
		t.Errorf("total = %d, want 3", page.Total)
	}
}
