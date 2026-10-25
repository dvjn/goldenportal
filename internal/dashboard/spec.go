// Package dashboard serves Home's dashboards, and checks and loads every
// widget: those on dashboards and those on blueprints' pages.
package dashboard

import (
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/widget"
)

const (
	// Name is the configdoc kind dashboards are stored as.
	Name = "dashboard"
	// DocumentKind is how dashboard documents name their kind.
	DocumentKind = "Dashboard"
)

// Spec is a parsed dashboard: a tab of widgets on Home.
type Spec struct {
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	// Order sorts Home's tabs, lowest first.
	Order   int             `json:"order"`
	Widgets []widget.Widget `json:"widgets"`
}

func (s Spec) Key() string { return s.Identifier }

func (s Spec) Meta() configdoc.Meta {
	return configdoc.Meta{Title: s.Title, Description: s.Description, Icon: s.Icon}
}

func (s Spec) Stats() map[string]int { return map[string]int{"widgets": len(s.Widgets)} }

// Parse reads a dashboard document and checks everything that doesn't
// depend on other documents.
func Parse(source string) (Spec, []configdoc.Issue) {
	r := &configdoc.Reader{}
	doc := configdoc.ReadDocument(r, source, DocumentKind)
	spec := Spec{
		Identifier: doc.Name, Title: doc.Meta.Title, Description: doc.Meta.Description,
		Icon: doc.Meta.Icon, Widgets: []widget.Widget{},
	}
	if doc.Spec == nil {
		return spec, r.Issues
	}
	fields, ok := r.Fields(doc.Spec, "spec", "order", "widgets")
	if !ok {
		return spec, r.Issues
	}

	if n := fields["order"]; n != nil {
		if err := n.Decode(&spec.Order); err != nil {
			r.Errorf(n, "spec.order", "order must be a whole number")
		}
	}
	spec.Widgets = widget.Parse(r, fields["widgets"], "spec.widgets", widget.Scope{})
	if len(spec.Widgets) == 0 {
		r.Errorf(configdoc.OrNode(fields["widgets"], doc.Spec), "spec.widgets", "add the widgets, keyed by identifier")
	}

	return spec, r.Issues
}
