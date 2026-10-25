package scorecard

import (
	"context"
	"fmt"
	"reflect"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Kind serves scorecards as configuration documents.
var Kind = configdoc.Kind[Spec]{
	Name:     Name,
	Document: DocumentKind,
	Plural:   "scorecards",
	Parse:    Parse,
	Check:    Check,
	Diff:     Diff,
	// Results follow straight away, rather than at the next scheduled run.
	Published: func(ctx context.Context, tx configdoc.DB, _ *Spec, next Spec) error {
		_, err := evaluateIn(ctx, tx, next)
		return err
	},
}

// Diff lists what publishing next over previous changes. Results are
// re-evaluated on publish, so nothing is lost and nothing is breaking.
func Diff(previous, next Spec) []configdoc.Change {
	changes := []configdoc.Change{}
	add := func(section, id, change, format string, args ...any) {
		changes = append(changes, configdoc.Change{Section: section, ID: id, Change: change, Detail: fmt.Sprintf(format, args...)})
	}

	if previous.Blueprint != next.Blueprint {
		add("scorecard", "", configdoc.ChangeChanged, "scores %s instead of %s", next.Blueprint, previous.Blueprint)
	}
	if !reflect.DeepEqual(previous.Filter, next.Filter) {
		add("scorecard", "", configdoc.ChangeChanged, "changes which entities are scored")
	}
	if previous.Title != next.Title || previous.Description != next.Description || previous.Icon != next.Icon {
		add("scorecard", "", configdoc.ChangeChanged, "changes the title, description or icon")
	}

	tierNames := func(s Spec) []string {
		names := make([]string, len(s.Tiers))
		for i, tier := range s.Tiers {
			names[i] = tier.Name
		}
		return names
	}
	if !reflect.DeepEqual(tierNames(previous), tierNames(next)) {
		add("tiers", "", configdoc.ChangeChanged, "changes the tiers to %v", tierNames(next))
	}

	before := map[string]Criterion{}
	tierOf := map[string]string{}
	for _, tier := range previous.Tiers {
		for _, c := range tier.Criteria {
			before[c.ID] = c
			tierOf[c.ID] = tier.Name
		}
	}
	seen := map[string]bool{}
	for _, tier := range next.Tiers {
		for _, c := range tier.Criteria {
			seen[c.ID] = true
			old, ok := before[c.ID]
			switch {
			case !ok:
				add("criteria", c.ID, configdoc.ChangeAdded, "adds it to %s", tier.Name)
			case tierOf[c.ID] != tier.Name:
				add("criteria", c.ID, configdoc.ChangeChanged, "moves it from %s to %s", tierOf[c.ID], tier.Name)
			case old.Check != c.Check || old.When != c.When:
				add("criteria", c.ID, configdoc.ChangeChanged, "changes what it checks")
			case old.Title != c.Title || old.Description != c.Description || old.Fix != c.Fix:
				add("criteria", c.ID, configdoc.ChangeChanged, "changes its title, description or fix")
			}
		}
	}
	for _, tier := range previous.Tiers {
		for _, c := range tier.Criteria {
			if !seen[c.ID] {
				add("criteria", c.ID, configdoc.ChangeRemoved, "removes it from %s", tier.Name)
			}
		}
	}

	return changes
}
