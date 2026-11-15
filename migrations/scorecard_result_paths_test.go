package migrations

import (
	"slices"
	"testing"
)

func TestTierNamesRankUpTheLadder(t *testing.T) {
	// A scorecard whose checks are still jq ranks the same: only its tiers are read.
	source := `kind: Scorecard
metadata: {identifier: health, title: Health}
spec:
  blueprint: repository
  tiers:
    - name: bronze
      criteria:
        described: {check: .hasReadme == true}
    - {name: silver, title: Silver, criteria: {}}
    - {name: gold, criteria: {}}
`
	names, ok := tierNames(source)
	if !ok || !slices.Equal(names, []string{"bronze", "silver", "gold"}) {
		t.Fatalf("tiers = %v, %v", names, ok)
	}
	if names, ok := tierNames("kind: Scorecard\nspec: [not, a, mapping]\n"); ok {
		t.Errorf("a source that doesn't read has tiers %v", names)
	}
}
