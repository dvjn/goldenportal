package search

import (
	"slices"
	"testing"
)

func TestMatchRanksExactThenPrefixThenContains(t *testing.T) {
	all := []Result{
		{Identifier: "ledger-payments", Title: "Ledger Payments"},
		{Identifier: "payments", Title: "Payments"},
		{Identifier: "payments-api", Title: "Payments API"},
		{Identifier: "search", Title: "Search"},
	}
	var got []string
	for _, r := range Match(all, "PAYMENTS") {
		got = append(got, r.Identifier)
	}
	if want := []string{"payments", "payments-api", "ledger-payments"}; !slices.Equal(got, want) {
		t.Errorf("Match = %v, want %v", got, want)
	}
	if len(Match(all, "")) != 4 {
		t.Error("empty text should keep everything")
	}
}

func TestMergeListsSourcesClosestFirst(t *testing.T) {
	blueprints := []Result{{Kind: "blueprint", Identifier: "service", Title: "Service"}}
	entities := []Result{
		{Kind: "entity", Identifier: "auth-service", Title: "Auth Service"},
		{Kind: "entity", Identifier: "service-registry", Title: "Service Registry"},
	}
	scorecards := []Result{{Kind: "scorecard", Identifier: "service", Title: "Service"}}

	var got []string
	for _, r := range merge("service", blueprints, entities, scorecards) {
		got = append(got, r.Kind+":"+r.Identifier)
	}
	// Exact matches lead, in the order of sources, then a prefix, then a match inside.
	want := []string{"blueprint:service", "scorecard:service", "entity:service-registry", "entity:auth-service"}
	if !slices.Equal(got, want) {
		t.Errorf("merge = %v, want %v", got, want)
	}
}
