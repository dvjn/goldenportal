package widget

import (
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Tab is a page tab of widgets.
type Tab struct {
	ID      string   `json:"identifier"`
	Title   string   `json:"title"`
	Icon    string   `json:"icon,omitempty"`
	Widgets []Widget `json:"widgets"`
}

// ParseTabs reads a mapping of tabs keyed by identifier, in order. Reserved
// identifiers belong to tabs the page always has.
func ParseTabs(r *configdoc.Reader, node *yaml.Node, path string, scope Scope, reserved ...string) []Tab {
	tabs := []Tab{}
	r.Each(node, path, func(id string, key, value *yaml.Node) {
		tabPath := path + "." + id
		switch {
		case !namePattern.MatchString(id):
			r.Errorf(key, tabPath, "%q must start with a letter and use only letters, digits, - and _", id)
		case slices.Contains(reserved, id):
			r.Errorf(key, tabPath, "%s are tabs every page has, so tabs can't be named after them", strings.Join(reserved, " and "))
		}
		tab := Tab{ID: id, Title: id, Widgets: []Widget{}}
		fields, ok := r.Fields(value, tabPath, "title", "icon", "widgets")
		if !ok {
			tabs = append(tabs, tab)
			return
		}
		if n := fields["title"]; n != nil {
			tab.Title = r.Text(n, tabPath+".title")
		}
		tab.Icon = r.Text(fields["icon"], tabPath+".icon")
		tab.Widgets = Parse(r, fields["widgets"], tabPath+".widgets", scope)
		if len(tab.Widgets) == 0 {
			r.Errorf(configdoc.OrNode(fields["widgets"], key), tabPath+".widgets", "add the tab's widgets, keyed by identifier")
		}
		tabs = append(tabs, tab)
	})

	return tabs
}

// FindTab returns the tab with an identifier.
func FindTab(tabs []Tab, id string) (Tab, bool) {
	i := slices.IndexFunc(tabs, func(t Tab) bool { return t.ID == id })
	if i < 0 {
		return Tab{}, false
	}

	return tabs[i], true
}
