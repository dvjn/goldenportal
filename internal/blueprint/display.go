package blueprint

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Display is how the portal shows a property's values, in tables, on entity
// pages and in widgets. Every field is optional; the type picks the rest.
type Display struct {
	// As picks how a value looks, from the ones its type allows. Progress
	// runs from the property's minimum to its maximum, or 0 to 100.
	As string `json:"as,omitempty"`
	// Unit follows numbers, such as % or ms.
	Unit string `json:"unit,omitempty"`
	// Colors color badges by value.
	Colors map[string]string `json:"colors,omitempty"`
	// Column shows the property in the catalog table until someone picks columns.
	Column *bool `json:"column,omitempty"`
	// Filter offers the property's values as a catalog filter.
	Filter *bool `json:"filter,omitempty"`
	// Hidden leaves the property off entity pages; it's still in the data.
	Hidden bool `json:"hidden,omitempty"`
}

// displays lists how each type can look; the first is the default.
var displays = map[string][]string{
	"string":   {"text", "badge", "code", "link", "date", "relative"},
	"number":   {"number", "progress", "badge"},
	"integer":  {"number", "progress", "badge"},
	"boolean":  {"check", "badge"},
	"array":    {"tags", "list"},
	"object":   {"json"},
	"relation": {"link"},
}

func (p *parser) display(node *yaml.Node, path string, prop Property) *Display {
	d := &Display{}
	fields, ok := p.Fields(node, path, "as", "unit", "colors", "column", "filter", "hidden")
	if !ok {
		return d
	}

	if n := fields["as"]; n != nil {
		d.As = p.Text(n, path+".as")
		allowed, known := displays[prop.Type]
		// A lookup without a type takes its target's, which isn't known here.
		if !known && prop.Lookup != "" {
			allowed = slices.Concat(displays["string"], displays["number"], displays["boolean"], displays["array"])
		}
		if !slices.Contains(allowed, d.As) {
			p.Errorf(n, path+".as", "%s properties show as %s", orType(prop.Type), strings.Join(slices.Compact(slices.Sorted(slices.Values(allowed))), ", "))
		}
	}
	if n := fields["unit"]; n != nil {
		d.Unit = p.Text(n, path+".unit")
		if prop.Type != "number" && prop.Type != "integer" && prop.Lookup == "" {
			p.Errorf(n, path+".unit", "only numbers have a unit")
		}
	}
	p.Each(fields["colors"], path+".colors", func(value string, key, color *yaml.Node) {
		if d.Colors == nil {
			d.Colors = map[string]string{}
		}
		name := p.Text(color, path+".colors."+value)
		if !slices.Contains(configdoc.Colors, name) {
			p.Errorf(color, path+".colors."+value, "color must be one of %s", strings.Join(configdoc.Colors, ", "))
		}
		if prop.Enum != nil && !slices.ContainsFunc(prop.Enum, func(v any) bool { return fmt.Sprint(v) == value }) {
			p.Errorf(key, path+".colors."+value, "%q isn't one of the property's enum values", value)
		}
		d.Colors[value] = name
	})
	if n := fields["column"]; n != nil {
		column := p.Bool(n, path+".column")
		d.Column = &column
	}
	if n := fields["filter"]; n != nil {
		filter := p.Bool(n, path+".filter")
		d.Filter = &filter
		if filter && (prop.Lookup != "" || prop.Type == "object") {
			p.Errorf(n, path+".filter", "lookups and objects can't be filtered")
		}
	}
	d.Hidden = p.Bool(fields["hidden"], path+".hidden")

	return d
}

func orType(t string) string {
	if t == "" {
		return "these"
	}

	return t
}
