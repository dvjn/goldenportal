package blueprint

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

// Header picks the properties an entity page shows beside its title.
type Header struct {
	// Subtitle is shown under the title, on entity pages and in the catalog.
	Subtitle string `json:"subtitle,omitempty"`
	// Badges are shown as chips beside the title.
	Badges []string `json:"badges,omitempty"`
	// Link is an http(s) URL property the page offers to open.
	Link string `json:"link,omitempty"`
}

// Section groups properties under a title on the entity page's overview.
// Properties no section lists follow the sections.
type Section struct {
	Title      string   `json:"title"`
	Properties []string `json:"properties"`
}

func (p *parser) header(node *yaml.Node, path string, spec Spec) *Header {
	fields, ok := p.Fields(node, path, "subtitle", "badges", "link")
	if !ok {
		return nil
	}
	h := &Header{}
	if n := fields["subtitle"]; n != nil {
		h.Subtitle = p.reference(n, path+".subtitle", spec)
	}
	p.List(fields["badges"], path+".badges", func(i int, item *yaml.Node) {
		if name := p.reference(item, fmt.Sprintf("%s.badges[%d]", path, i), spec); name != "" {
			h.Badges = append(h.Badges, name)
		}
	})
	if n := fields["link"]; n != nil {
		h.Link = p.reference(n, path+".link", spec)
		if prop, ok := spec.Property(h.Link); ok && prop.Format != "uri" {
			p.Errorf(n, path+".link", "link must name a property with format: uri")
		}
	}

	return h
}

func (p *parser) sections(node *yaml.Node, path string, spec Spec) []Section {
	sections := []Section{}
	placed := map[string]bool{}
	p.List(node, path, func(i int, item *yaml.Node) {
		at := fmt.Sprintf("%s[%d]", path, i)
		fields, ok := p.Fields(item, at, "title", "properties")
		if !ok {
			return
		}
		section := Section{Title: p.Text(fields["title"], at+".title"), Properties: []string{}}
		if section.Title == "" {
			p.Errorf(item, at+".title", "add a title")
		}
		p.List(fields["properties"], at+".properties", func(j int, n *yaml.Node) {
			name := p.reference(n, fmt.Sprintf("%s.properties[%d]", at, j), spec)
			if name == "" {
				return
			}
			if placed[name] {
				p.Errorf(n, fmt.Sprintf("%s.properties[%d]", at, j), "%s is already in a section", name)
				return
			}
			placed[name] = true
			section.Properties = append(section.Properties, name)
		})
		sections = append(sections, section)
	})

	return sections
}

// reference reads the name of one of the blueprint's properties.
func (p *parser) reference(node *yaml.Node, path string, spec Spec) string {
	name := p.Text(node, path)
	if name == "" {
		return ""
	}
	if _, ok := spec.Property(name); !ok {
		p.Errorf(node, path, "%s has no property %q", spec.Identifier, name)
		return ""
	}

	return name
}
