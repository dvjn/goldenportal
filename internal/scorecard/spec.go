// Package scorecard rates entities against ordered tiers of jq checks, such
// as bronze, silver and gold, and keeps each entity's latest results.
package scorecard

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
)

// DocumentKind is how scorecard documents name their kind.
const DocumentKind = "Scorecard"

// Spec is a parsed scorecard.
type Spec struct {
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Blueprint   string `json:"blueprint"`
	// Filter narrows the entities scored, in the catalog's filter language.
	Filter *entity.Filter `json:"filter,omitempty"`
	// Tiers are a ladder, lowest first: an entity reaches a tier only by
	// meeting it and every tier below.
	Tiers []Tier `json:"tiers"`
	line  int
}

type Tier struct {
	Name     string      `json:"name"`
	Title    string      `json:"title"`
	Color    string      `json:"color,omitempty"`
	Criteria []Criterion `json:"criteria"`
}

// Criterion is one check, written in jq against the entity.
type Criterion struct {
	ID          string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	// Check must give true or false.
	Check string `json:"check"`
	// When, if set, skips the criterion for entities it's false for.
	When string `json:"when,omitempty"`
	// Fix tells people what to do when the check fails.
	Fix  string `json:"fix,omitempty"`
	line int
}

var namePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

func (s Spec) Key() string { return s.Identifier }

func (s Spec) Meta() configdoc.Meta {
	return configdoc.Meta{Title: s.Title, Description: s.Description, Icon: s.Icon}
}

func (s Spec) Stats() map[string]int {
	criteria := 0
	for _, tier := range s.Tiers {
		criteria += len(tier.Criteria)
	}

	return map[string]int{"tiers": len(s.Tiers), "criteria": criteria}
}

// Parse reads a scorecard document and checks everything that doesn't
// depend on its blueprint.
func Parse(source string) (Spec, []configdoc.Issue) {
	r := &configdoc.Reader{}
	doc := configdoc.ReadDocument(r, source, DocumentKind)
	spec := Spec{
		Identifier: doc.Name, Title: doc.Meta.Title, Description: doc.Meta.Description,
		Icon: doc.Meta.Icon, Tiers: []Tier{},
	}
	if doc.Spec == nil {
		return spec, r.Issues
	}
	fields, ok := r.Fields(doc.Spec, "spec", "blueprint", "filter", "tiers")
	if !ok {
		return spec, r.Issues
	}

	spec.Blueprint = r.Text(fields["blueprint"], "spec.blueprint")
	spec.line = configdoc.OrNode(fields["blueprint"], doc.Spec).Line
	if !configdoc.ValidIdentifier(spec.Blueprint) {
		r.Errorf(configdoc.OrNode(fields["blueprint"], doc.Spec), "spec.blueprint", "blueprint must name the blueprint to score")
	}
	if n := fields["filter"]; n != nil {
		var raw any
		if err := n.Decode(&raw); err != nil {
			r.Errorf(n, "spec.filter", "filter is not valid: %v", err)
		} else if f, err := toFilter(raw); err != nil {
			r.Errorf(n, "spec.filter", "%v", err)
		} else {
			spec.Filter = f
		}
	}

	ids := map[string]bool{}
	tierNames := map[string]bool{}
	r.List(fields["tiers"], "spec.tiers", func(i int, node *yaml.Node) {
		spec.Tiers = append(spec.Tiers, parseTier(r, fmt.Sprintf("spec.tiers[%d]", i), node, ids, tierNames))
	})
	if len(spec.Tiers) == 0 {
		r.Errorf(configdoc.OrNode(fields["tiers"], doc.Spec), "spec.tiers", "add the tiers, lowest first")
	}

	return spec, r.Issues
}

func parseTier(r *configdoc.Reader, path string, node *yaml.Node, ids, names map[string]bool) Tier {
	tier := Tier{Criteria: []Criterion{}}
	fields, ok := r.Fields(node, path, "name", "title", "color", "criteria")
	if !ok {
		return tier
	}
	tier.Name = r.Text(fields["name"], path+".name")
	switch {
	case !namePattern.MatchString(tier.Name):
		r.Errorf(configdoc.OrNode(fields["name"], node), path+".name",
			"name must start with a letter and use only letters, digits, - and _")
	case names[tier.Name]:
		r.Errorf(fields["name"], path+".name", "another tier is already named %q", tier.Name)
	}
	names[tier.Name] = true
	tier.Title = r.Text(fields["title"], path+".title")
	if tier.Title == "" {
		tier.Title = tier.Name
	}
	if n := fields["color"]; n != nil {
		tier.Color = r.Text(n, path+".color")
		if !slices.Contains(configdoc.Colors, tier.Color) {
			r.Errorf(n, path+".color", "color must be one of %s", strings.Join(configdoc.Colors, ", "))
		}
	}

	r.Each(fields["criteria"], path+".criteria", func(id string, key, value *yaml.Node) {
		criterionPath := path + ".criteria." + id
		c := Criterion{ID: id, Title: id, line: key.Line}
		switch {
		case !namePattern.MatchString(id):
			r.Errorf(key, criterionPath, "%q must start with a letter and use only letters, digits, - and _", id)
		case ids[id]:
			r.Errorf(key, criterionPath, "another criterion is already named %q; criteria are named across tiers", id)
		}
		ids[id] = true
		criterionFields, ok := r.Fields(value, criterionPath, "title", "description", "check", "when", "fix")
		if !ok {
			tier.Criteria = append(tier.Criteria, c)
			return
		}
		if n := criterionFields["title"]; n != nil {
			c.Title = r.Text(n, criterionPath+".title")
		}
		c.Description = r.Text(criterionFields["description"], criterionPath+".description")
		c.Fix = r.Text(criterionFields["fix"], criterionPath+".fix")
		c.Check = jq(r, criterionFields["check"], value, criterionPath+".check", true)
		c.When = jq(r, criterionFields["when"], value, criterionPath+".when", false)
		tier.Criteria = append(tier.Criteria, c)
	})
	if len(tier.Criteria) == 0 {
		r.Errorf(configdoc.OrNode(fields["criteria"], node), path+".criteria", "add the criteria entities must meet for this tier")
	}

	return tier
}

func jq(r *configdoc.Reader, node, parent *yaml.Node, path string, required bool) string {
	if node == nil {
		if required {
			r.Errorf(parent, path, "add a jq check that gives true or false, such as .properties.owner != null")
		}
		return ""
	}
	if node.Kind != yaml.ScalarNode {
		r.Errorf(node, path, "quote this jq expression, or write it as a block scalar with |-, since YAML reads it as a list or mapping")
		return ""
	}
	if err := blueprint.CompileExpression(node.Value); err != nil {
		r.Errorf(node, path, "this is not valid jq: %v", err)
	}

	return node.Value
}

// toFilter turns a filter written in YAML into the catalog's filter.
func toFilter(raw any) (*entity.Filter, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("filter is not valid: %w", err)
	}

	return entity.ParseFilter(string(encoded))
}

// Check checks the scorecard against its blueprint: an unpublished one is
// a warning, a filter it can't answer is an error.
func Check(ctx context.Context, db configdoc.DB, spec Spec) ([]configdoc.Issue, error) {
	published, err := blueprint.Published(ctx, db)
	if err != nil {
		return nil, err
	}
	bp, ok := published[spec.Blueprint]
	if !ok {
		return []configdoc.Issue{{
			Severity: configdoc.SeverityWarning, Path: "spec.blueprint", Line: spec.line,
			Message: fmt.Sprintf("no blueprint %q is published yet, so nothing is scored until it is", spec.Blueprint),
		}}, nil
	}
	if spec.Filter != nil {
		if err := entity.CheckFilter(bp, *spec.Filter); err != nil {
			return []configdoc.Issue{{Severity: configdoc.SeverityError, Path: "spec.filter", Message: err.Error()}}, nil
		}
	}

	return nil, nil
}
