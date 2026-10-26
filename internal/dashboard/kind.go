package dashboard

import (
	"fmt"
	"reflect"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/widget"
)

// Kind serves dashboards as configuration documents.
var Kind = configdoc.Kind[Spec]{
	Name:     Name,
	Document: DocumentKind,
	Plural:   "dashboards",
	Parse:    Parse,
	Check:    Check,
	Diff:     Diff,
	Readable: func(s Spec, p auth.Principal) bool { return s.Visible(p) },
}

// Diff lists what publishing next over previous changes. Dashboards hold no
// data, so nothing is breaking.
func Diff(previous, next Spec) []configdoc.Change {
	changes := []configdoc.Change{}
	add := func(section, id, change, format string, args ...any) {
		changes = append(changes, configdoc.Change{Section: section, ID: id, Change: change, Detail: fmt.Sprintf(format, args...)})
	}

	if previous.Order != next.Order {
		add("dashboard", "", configdoc.ChangeChanged, "changes its order from %d to %d", previous.Order, next.Order)
	}
	if previous.Title != next.Title || previous.Description != next.Description || previous.Icon != next.Icon {
		add("dashboard", "", configdoc.ChangeChanged, "changes the title, description or icon")
	}

	if !reflect.DeepEqual(previous.Visibility, next.Visibility) {
		add("dashboard", "", configdoc.ChangeChanged, "changes who can see it")
	}
	if !reflect.DeepEqual(previous.DefaultFor, next.DefaultFor) {
		add("dashboard", "", configdoc.ChangeChanged, "changes whose default Home tab it is")
	}
	filters := map[string]Filter{}
	for _, f := range previous.Filters {
		f.line = 0
		filters[f.ID] = f
	}
	seenFilters := map[string]bool{}
	for _, f := range next.Filters {
		seenFilters[f.ID] = true
		f.line = 0
		old, ok := filters[f.ID]
		switch {
		case !ok:
			add("filters", f.ID, configdoc.ChangeAdded, "adds a %s filter", f.Type)
		case !reflect.DeepEqual(old, f):
			add("filters", f.ID, configdoc.ChangeChanged, "changes it")
		}
	}
	for _, f := range previous.Filters {
		if !seenFilters[f.ID] {
			add("filters", f.ID, configdoc.ChangeRemoved, "removes it")
		}
	}

	before := map[string]widget.Widget{}
	for _, w := range previous.Widgets {
		before[w.ID] = w
	}
	seen := map[string]bool{}
	for _, w := range next.Widgets {
		seen[w.ID] = true
		old, ok := before[w.ID]
		w.Path, w.Line, old.Path, old.Line = "", 0, "", 0
		switch {
		case !ok:
			add("widgets", w.ID, configdoc.ChangeAdded, "adds a %s widget", w.Type)
		case !reflect.DeepEqual(old, w):
			add("widgets", w.ID, configdoc.ChangeChanged, "changes it")
		}
	}
	for _, w := range previous.Widgets {
		if !seen[w.ID] {
			add("widgets", w.ID, configdoc.ChangeRemoved, "removes it")
		}
	}

	return changes
}
