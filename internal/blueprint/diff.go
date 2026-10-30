package blueprint

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/widget"
)

const (
	ChangeAdded   = configdoc.ChangeAdded
	ChangeRemoved = configdoc.ChangeRemoved
	ChangeChanged = configdoc.ChangeChanged
)

// Diff lists what publishing next over previous would change.
func Diff(previous, next Spec) []Change {
	changes := []Change{}

	var meta []string
	if previous.Title != next.Title {
		meta = append(meta, "title")
	}
	if previous.Description != next.Description {
		meta = append(meta, "description")
	}
	if previous.Icon != next.Icon {
		meta = append(meta, "icon")
	}
	if len(meta) > 0 {
		changes = append(changes, Change{
			Section: "blueprint", Change: ChangeChanged,
			Detail: "changes the " + joinWords(meta),
		})
	}

	for _, page := range []struct {
		name           string
		previous, next *Page
	}{{"catalog", previous.Catalog, next.Catalog}, {"entity", previous.Entity, next.Entity}} {
		changes = append(changes, diffTabs(page.name, page.previous, page.next)...)
	}
	if layout(previous.Entity) != layout(next.Entity) {
		changes = append(changes, Change{Section: "entity", Change: ChangeChanged, Detail: "changes the header or sections"})
	}

	changes = append(changes, diffList(previous.Properties, next.Properties, "properties",
		func(p Property) string { return p.ID }, propertyChange,
		func(p Property) (bool, string) {
			switch {
			case p.Lookup != "":
				return false, "looks up " + p.Lookup
			case p.Compute != "":
				return false, "computes " + article(p.Type)
			case p.IsRelation() && p.Required:
				return true, "adds a required relation to " + p.Relation
			case p.IsRelation():
				return false, "adds a relation to " + p.Relation
			case p.Required:
				return true, "adds a required " + p.Type + " property"
			}
			return false, "adds " + article(p.Type) + " property"
		},
		func(p Property) (bool, string) {
			// Derived values are worked out, so dropping one loses nothing stored.
			if p.Derived() {
				return false, "removes it"
			}
			return true, "removes it and the values entities have for it"
		})...)

	return changes
}

func diffList[T any](previous, next []T, section string, id func(T) string,
	changed func(a, b T) (bool, []string), added, removed func(T) (bool, string),
) []Change {
	var changes []Change
	old := map[string]T{}
	for _, item := range previous {
		old[id(item)] = item
	}
	seen := map[string]bool{}

	for _, item := range next {
		key := id(item)
		seen[key] = true
		before, ok := old[key]
		if !ok {
			breaking, detail := added(item)
			changes = append(changes, Change{Section: section, ID: key, Change: ChangeAdded, Breaking: breaking, Detail: detail})
			continue
		}
		breaking, fields := changed(before, item)
		if len(fields) > 0 {
			changes = append(changes, Change{
				Section: section, ID: key, Change: ChangeChanged, Breaking: breaking,
				Detail: "changes the " + joinWords(fields),
			})
		}
	}
	for _, item := range previous {
		if key := id(item); !seen[key] {
			breaking, detail := removed(item)
			changes = append(changes, Change{Section: section, ID: key, Change: ChangeRemoved, Breaking: breaking, Detail: detail})
		}
	}

	return changes
}

func propertyChange(a, b Property) (bool, []string) {
	breaking, fields := valueChange(a, b)
	// Display changes only how values look, so they never break anything.
	if !reflect.DeepEqual(a.Display, b.Display) {
		fields = append(fields, "display")
	}

	return breaking, fields
}

func valueChange(a, b Property) (bool, []string) {
	if a.Derived() || b.Derived() {
		fields := changedFields(
			a.Title != b.Title, "title",
			a.Description != b.Description, "description",
			a.Type != b.Type || a.Format != b.Format || !reflect.DeepEqual(a.Items, b.Items), "type",
			a.Lookup != b.Lookup, "lookup",
			a.Compute != b.Compute, "compute",
		)
		// Switching between stored and derived drops or orphans stored values.
		return a.Derived() != b.Derived(), fields
	}
	if a.IsRelation() || b.IsRelation() {
		return relationChange(a, b)
	}

	typeChanged := a.Type != b.Type || a.Format != b.Format || !reflect.DeepEqual(a.Items, b.Items)
	nowRequired := !a.Required && b.Required
	enumNarrowed := len(b.Enum) > 0 && (len(a.Enum) == 0 || slices.ContainsFunc(a.Enum, func(v any) bool {
		return !slices.Contains(b.Enum, v)
	}))
	tightened := tighterString(a.Pattern, b.Pattern) ||
		tighterMin(a.MinLength, b.MinLength) || tighterMax(a.MaxLength, b.MaxLength) ||
		tighterMinFloat(a.Minimum, b.Minimum) || tighterMaxFloat(a.Maximum, b.Maximum)

	fields := changedFields(
		a.Title != b.Title, "title",
		a.Description != b.Description, "description",
		typeChanged, "type",
		a.Required != b.Required, "required",
		!reflect.DeepEqual(a.Enum, b.Enum), "enum",
		a.Pattern != b.Pattern || !reflect.DeepEqual(a.MinLength, b.MinLength) || !reflect.DeepEqual(a.MaxLength, b.MaxLength) ||
			!reflect.DeepEqual(a.Minimum, b.Minimum) || !reflect.DeepEqual(a.Maximum, b.Maximum), "constraints",
	)

	return typeChanged || nowRequired || enumNarrowed || tightened, fields
}

func relationChange(a, b Property) (bool, []string) {
	fields := changedFields(
		a.Title != b.Title, "title",
		a.Description != b.Description, "description",
		a.Relation != b.Relation, "relation",
		a.Many != b.Many, "many",
		a.Required != b.Required, "required",
	)

	// Switching to or from a relation, or to a single target, can't keep the values.
	return a.Relation != b.Relation || (a.Many && !b.Many) || (!a.Required && b.Required), fields
}

// changedFields takes pairs of (changed, name) and returns the changed names.
func changedFields(pairs ...any) []string {
	var fields []string
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i].(bool) {
			fields = append(fields, pairs[i+1].(string))
		}
	}

	return fields
}

// A new pattern counts as tighter unless the old and new are the same.
func tighterString(a, b string) bool { return b != "" && a != b }

func tighterMin(a, b *int) bool { return b != nil && (a == nil || *b > *a) }
func tighterMax(a, b *int) bool { return b != nil && (a == nil || *b < *a) }

func tighterMinFloat(a, b *float64) bool { return b != nil && (a == nil || *b > *a) }
func tighterMaxFloat(a, b *float64) bool { return b != nil && (a == nil || *b < *a) }

func joinWords(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	}

	return fmt.Sprintf("%s and %s", strings.Join(words[:len(words)-1], ", "), words[len(words)-1])
}

func article(word string) string {
	if strings.ContainsRune("aeiou", rune(word[0])) {
		return "an " + word
	}

	return "a " + word
}

// diffTabs lists the tabs added to, changed on or removed from a page.
// Tabs only show data, so none of it is breaking.
func diffTabs(section string, previous, next *Page) []Change {
	tabs := func(p *Page) []widget.Tab {
		if p == nil {
			return nil
		}
		return p.Tabs
	}
	before := map[string]string{}
	for _, tab := range tabs(previous) {
		before[tab.ID] = encode(tab)
	}
	changes := []Change{}
	seen := map[string]bool{}
	for _, tab := range tabs(next) {
		seen[tab.ID] = true
		old, ok := before[tab.ID]
		switch {
		case !ok:
			changes = append(changes, Change{Section: section, ID: tab.ID, Change: ChangeAdded, Detail: "adds the tab"})
		case old != encode(tab):
			changes = append(changes, Change{Section: section, ID: tab.ID, Change: ChangeChanged, Detail: "changes the tab"})
		}
	}
	for _, tab := range tabs(previous) {
		if !seen[tab.ID] {
			changes = append(changes, Change{Section: section, ID: tab.ID, Change: ChangeRemoved, Detail: "removes the tab"})
		}
	}

	return changes
}

// layout encodes an entity page's header and sections, to compare them.
func layout(page *Page) string {
	if page == nil || (page.Header == nil && len(page.Sections) == 0) {
		return ""
	}
	encoded, _ := json.Marshal(struct {
		Header   *Header
		Sections []Section
	}{page.Header, page.Sections})

	return string(encoded)
}

// encode compares tabs by what they show, not where they're written.
func encode(tab widget.Tab) string {
	encoded, _ := json.Marshal(tab)
	return string(encoded)
}
