package entity

import (
	"database/sql/driver"
	"slices"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/widget"
)

func TestHiddenTabs(t *testing.T) {
	spec, issues := blueprint.Parse(serviceYAML + `  entity:
    tabs:
      always:
        widgets: {n: {type: stat, title: N, value: {query: {}}}}
      critical:
        when: {property: tier, op: eq, value: critical}
        widgets: {n: {type: stat, title: N, value: {query: {}}}}
      tagged:
        when: {property: tags, op: contains, value: $entity.title}
        widgets: {n: {type: stat, title: N, value: {query: {}}}}
      mine:
        when: {property: owner, op: in, values: $me.groups}
        widgets: {n: {type: stat, title: N, value: {query: {}}}}
`)
	if blueprint.HasErrors(issues) {
		t.Fatal(issues)
	}
	e := &Entity{Identifier: "ledger", Title: "Ledger", Properties: map[string]any{"tier": "standard"}}
	tests := []struct {
		name   string
		values widget.Values
		met    []driver.Value
		hidden []string
	}{
		{"a tab its entity doesn't meet hides", widget.Values{"$me.groups": {"payments"}}, []driver.Value{0, 100, 100}, []string{"critical"}},
		{"shares come back in tab order", widget.Values{"$me.groups": {"payments"}}, []driver.Value{100, 0, nil}, []string{"tagged", "mine"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, mock := mockStore(t)
			columns := make([]string, len(tt.met))
			for i := range columns {
				columns[i] = string(rune('a' + i))
			}
			mock.ExpectQuery(`^SELECT .*FILTER \(WHERE .*`).
				WillReturnRows(sqlmock.NewRows(columns).AddRow(tt.met...))
			hidden, err := store.HiddenTabs(t.Context(), Query{Blueprint: spec}, e, tt.values)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(hidden, tt.hidden) {
				t.Errorf("hidden = %v, want %v", hidden, tt.hidden)
			}
		})
	}
}

func TestHiddenTabsAskNothingWithoutWhen(t *testing.T) {
	store, _ := mockStore(t)
	hidden, err := store.HiddenTabs(t.Context(), Query{Blueprint: service(t)}, &Entity{Identifier: "ledger"}, widget.Values{})
	if err != nil || hidden != nil {
		t.Fatalf("hidden = %v, %v", hidden, err)
	}
}
