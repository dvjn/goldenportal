// Package scorecard rates entities against ordered tiers of checks, such as
// bronze, silver and gold, and keeps each entity's latest results.
package scorecard

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// DocumentKind is how scorecard documents name their kind.
const DocumentKind = "Scorecard"

// Spec is a parsed scorecard.
type Spec struct {
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
	Blueprint   string `json:"blueprint"`
	// Filter narrows the entities scored, in the catalog's filter language.
	Filter *entity.Filter `json:"filter,omitempty"`
	// Tiers are a ladder, lowest first: an entity reaches a tier only by
	// meeting it and every tier below.
	Tiers []Tier `json:"tiers"`
	line  int
	// source is the document the spec was read from, for checking what
	// publishing it would break.
	source string
}

type Tier struct {
	Name     string      `json:"name"`
	Title    string      `json:"title"`
	Color    string      `json:"color,omitempty"`
	Criteria []Criterion `json:"criteria"`
}

// Criterion is one check, a filter on the blueprint's entities: those it
// matches pass.
type Criterion struct {
	ID          string        `json:"identifier"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Check       entity.Filter `json:"check"`
	// When, if set, skips the criterion for entities it doesn't match.
	When *entity.Filter `json:"when,omitempty"`
	// Fix tells people what to do when the check fails.
	Fix                 string `json:"fix,omitempty"`
	checkLine, whenLine int
}

// The keys each mapping of a scorecard takes, exported so the documents'
// JSON Schema can be checked against them.
var (
	SpecFields      = []string{"blueprint", "filter", "tiers"}
	TierFields      = []string{"name", "title", "color", "criteria"}
	CriterionFields = []string{"title", "description", "check", "when", "fix"}
)

var namePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`)

func (s Spec) Key() string { return s.Identifier }

func (s Spec) Meta() configdoc.Meta {
	return configdoc.Meta{Title: s.Title, Description: s.Description, Icon: s.Icon, Color: s.Color}
}

func (s Spec) Stats() map[string]int {
	criteria := 0
	for _, tier := range s.Tiers {
		criteria += len(tier.Criteria)
	}

	return map[string]int{"tiers": len(s.Tiers), "criteria": criteria}
}

// Ladders are the scorecards as blueprints and filters see them.
func Ladders(specs map[string]Spec) map[string]blueprint.Ladder {
	ladders := make(map[string]blueprint.Ladder, len(specs))
	for identifier, spec := range specs {
		ladder := blueprint.Ladder{Title: spec.Title, Blueprint: spec.Blueprint}
		for _, tier := range spec.Tiers {
			rung := blueprint.Rung{Name: tier.Name, Title: tier.Title, Color: tier.Color, Criteria: map[string]any{}}
			for _, criterion := range tier.Criteria {
				rung.Criteria[criterion.ID] = nil
			}
			ladder.Tiers = append(ladder.Tiers, rung)
		}
		ladders[identifier] = ladder
	}

	return ladders
}

// Facts name the blueprint a scorecard grades, so its blueprint's pages
// can list it.
func (s Spec) Facts() map[string]any { return map[string]any{"blueprint": s.Blueprint} }

// Parse reads a scorecard document and checks everything that doesn't
// depend on its blueprint.
func Parse(source string) (Spec, []configdoc.Issue) {
	r := &configdoc.Reader{}
	doc := configdoc.ReadDocument(r, source, DocumentKind)
	spec := Spec{
		Identifier: doc.Identifier, Title: doc.Meta.Title, Description: doc.Meta.Description,
		Icon: doc.Meta.Icon, Color: doc.Meta.Color, Tiers: []Tier{}, source: source,
	}
	if doc.Spec == nil {
		return spec, r.Issues
	}
	fields, ok := r.Fields(doc.Spec, "spec", SpecFields...)
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
		spec.Tiers = append(spec.Tiers, parseTier(r, fmt.Sprintf("spec.tiers[%d]", i), node, spec.Blueprint, ids, tierNames))
	})
	if len(spec.Tiers) == 0 {
		r.Errorf(configdoc.OrNode(fields["tiers"], doc.Spec), "spec.tiers", "add the tiers, lowest first")
	}

	return spec, r.Issues
}

func parseTier(r *configdoc.Reader, path string, node *yaml.Node, graded string, ids, names map[string]bool) Tier {
	tier := Tier{Criteria: []Criterion{}}
	fields, ok := r.Fields(node, path, TierFields...)
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
		c := Criterion{ID: id, Title: id, checkLine: key.Line}
		switch {
		case !namePattern.MatchString(id):
			r.Errorf(key, criterionPath, "%q must start with a letter and use only letters, digits, - and _", id)
		case ids[id]:
			r.Errorf(key, criterionPath, "another criterion is already named %q; criteria are named across tiers", id)
		}
		ids[id] = true
		criterionFields, ok := r.Fields(value, criterionPath, CriterionFields...)
		if !ok {
			tier.Criteria = append(tier.Criteria, c)
			return
		}
		if n := criterionFields["title"]; n != nil {
			c.Title = r.Text(n, criterionPath+".title")
		}
		c.Description = r.Text(criterionFields["description"], criterionPath+".description")
		c.Fix = r.Text(criterionFields["fix"], criterionPath+".fix")
		if n := criterionFields["check"]; n == nil {
			r.Errorf(value, criterionPath+".check", "add a check, a filter such as %s", checkExample)
		} else if f := readCondition(r, n, criterionPath+".check", "a check is", "the check", graded); f != nil {
			c.Check, c.checkLine = *f, n.Line
		}
		if n := criterionFields["when"]; n != nil {
			c.When, c.whenLine = readCondition(r, n, criterionPath+".when", "when is", "when", graded), n.Line
		}
		tier.Criteria = append(tier.Criteria, c)
	})
	if len(tier.Criteria) == 0 {
		r.Errorf(configdoc.OrNode(fields["criteria"], node), path+".criteria", "add the criteria entities must meet for this tier")
	}

	return tier
}

const checkExample = "{property: hasReadme, op: eq, value: true}"

// readCondition reads a check or when, a filter. jq, written as text, is
// pointed at a computed property on graded, the blueprint scored.
func readCondition(r *configdoc.Reader, node *yaml.Node, path, isA, reader, graded string) *entity.Filter {
	switch {
	case node.Kind == yaml.ScalarNode && node.Tag == "!!str":
		r.Errorf(node, path, "%s a filter, as the catalog writes them, such as %s; jq over one entity belongs in a computed property on %s, which %s reads",
			isA, checkExample, graded, reader)
		return nil
	case node.Kind != yaml.MappingNode:
		r.Errorf(node, path, "%s a filter, a mapping such as %s", isA, checkExample)
		return nil
	case len(node.Content) == 0:
		r.Errorf(node, path, "%s a filter and needs a condition, such as %s", isA, checkExample)
		return nil
	}
	var raw any
	if err := node.Decode(&raw); err != nil {
		r.Errorf(node, path, "this is not a valid filter: %v", err)
		return nil
	}
	f, err := toFilter(raw)
	if err != nil {
		r.Errorf(node, path, "%v", err)
		return nil
	}

	return f
}

// toFilter turns a filter written in YAML into the catalog's filter.
func toFilter(raw any) (*entity.Filter, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("filter is not valid: %w", err)
	}

	return entity.ParseFilter(string(encoded))
}

// condition is one of a scorecard's filters, where it's written.
type condition struct {
	path   string
	line   int
	filter entity.Filter
}

// conditions are the scorecard's filter, and each criterion's check and when.
func (s Spec) conditions() []condition {
	var out []condition
	if s.Filter != nil {
		out = append(out, condition{"spec.filter", 0, *s.Filter})
	}
	for i, tier := range s.Tiers {
		for _, c := range tier.Criteria {
			path := fmt.Sprintf("spec.tiers[%d].criteria.%s", i, c.ID)
			out = append(out, condition{path + ".check", c.checkLine, c.Check})
			if c.When != nil {
				out = append(out, condition{path + ".when", c.whenLine, *c.When})
			}
		}
	}

	return out
}

// readers are the kinds of document whose paths and filters can read a
// scorecard's results.
var readers = []string{blueprint.DocumentKind, "Dashboard", "Action", "Workflow", DocumentKind}

// breaking says what publishing source as the scorecard identifier, or
// deleting it with configdoc.DeletedSource, would break in the published
// documents that read its results: a path or filter on a check, tier or
// scorecard that would no longer be there, or on a blueprint it no longer
// grades.
func breaking(ctx context.Context, db configdoc.DB, identifier, source string) ([]string, error) {
	return configdoc.BreakingReaders(ctx, db, DocumentKind, identifier, source, readers...)
}

// deletable refuses to delete a scorecard whose results published
// documents read.
func deletable(ctx context.Context, db configdoc.DB, identifier string) error {
	broken, err := breaking(ctx, db, identifier, configdoc.DeletedSource)
	if err != nil || len(broken) == 0 {
		return err
	}

	return httpx.Conflict("these read its results, so change them first: " + strings.Join(broken, "; "))
}

// Check checks the scorecard against its blueprint: an unpublished one is
// a warning. A filter, check or when it can't answer is an error, as is
// what publishing it would break in the documents that read its results.
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
	ladders, err := blueprint.Ladders(ctx, db)
	if err != nil {
		return nil, err
	}
	var issues []configdoc.Issue
	for _, c := range spec.conditions() {
		if err := entity.CheckFilter(entity.Schema{Blueprint: bp, Blueprints: published, Ladders: ladders}, c.filter); err != nil {
			issues = append(issues, configdoc.Issue{Severity: configdoc.SeverityError, Path: c.path, Line: c.line, Message: err.Error()})
		}
	}
	if configdoc.Rechecking(ctx) || spec.source == "" {
		return issues, nil
	}
	broken, err := breaking(ctx, db, spec.Identifier, spec.source)
	if err != nil {
		return nil, err
	}
	for _, problem := range broken {
		issues = append(issues, configdoc.Issue{
			Severity: configdoc.SeverityError, Path: "spec", Line: spec.line,
			Message: "this change breaks " + problem + "; change it first",
		})
	}

	return issues, nil
}

// CheckGraders checks publishing next leaves every published scorecard's
// filters answerable, whether they grade it or follow a relation to it. A
// filter only next breaks is an error on next, so the scorecard is fixed
// first; one already broken is the scorecard's own to report.
func CheckGraders(ctx context.Context, db configdoc.DB, next blueprint.Spec) ([]configdoc.Issue, error) {
	before, err := blueprint.Published(ctx, db)
	if err != nil {
		return nil, err
	}
	after := maps.Clone(before)
	after[next.Identifier] = next
	specs, err := Published(ctx, db)
	if err != nil {
		return nil, err
	}
	ladders := Ladders(specs)

	var issues []configdoc.Issue
	for _, identifier := range slices.Sorted(maps.Keys(specs)) {
		spec := specs[identifier]
		graded, ok := after[spec.Blueprint]
		if !ok {
			continue
		}
		for _, c := range spec.conditions() {
			err := entity.CheckFilter(entity.Schema{Blueprint: graded, Blueprints: after, Ladders: ladders}, c.filter)
			if err == nil {
				continue
			}
			if was, ok := before[spec.Blueprint]; ok && entity.CheckFilter(entity.Schema{Blueprint: was, Blueprints: before, Ladders: ladders}, c.filter) != nil {
				continue
			}
			issues = append(issues, configdoc.Issue{
				Severity: configdoc.SeverityError, Path: "spec.properties",
				Message: fmt.Sprintf("scorecard %s reads this blueprint at %s, which this change breaks: %v; change the scorecard first",
					identifier, c.path, err),
			})
		}
	}

	return issues, nil
}
