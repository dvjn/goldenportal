package blueprint

import (
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

const serviceYAML = `
kind: Blueprint
metadata:
  name: service
  title: Service
  description: A deployable unit of software.
  icon: server
spec:
  properties:
    language:
      title: Language
      type: string
      enum: [go, python]
      required: true
    stars:
      type: integer
      minimum: 0
    tags:
      type: array
      items:
        type: string
    owner:
      title: Owner
      relation: team
      required: true
    dependsOn:
      relation: service
      many: true
    ownerName:
      lookup: owner.$title
    popular:
      type: boolean
      compute: .properties.stars > 100
`

const teamYAML = `
kind: Blueprint
metadata:
  name: team
  title: Team
spec:
  properties:
    slack:
      type: string
      format: uri
`

func mustParse(t *testing.T, source string) Spec {
	t.Helper()
	spec, issues := Parse(source)
	if HasErrors(issues) {
		t.Fatalf("Parse() issues = %+v", issues)
	}

	return spec
}

func TestParseKeepsOrderAndDefaults(t *testing.T) {
	spec := mustParse(t, serviceYAML)

	if spec.Identifier != "service" || spec.Title != "Service" || spec.Icon != "server" {
		t.Fatalf("metadata = %q %q %q", spec.Identifier, spec.Title, spec.Icon)
	}
	var ids []string
	for _, prop := range spec.Properties {
		ids = append(ids, prop.ID)
	}
	if got := strings.Join(ids, ","); got != "language,stars,tags,owner,dependsOn,ownerName,popular" {
		t.Fatalf("property order = %s", got)
	}
	if spec.Properties[1].Title != "stars" {
		t.Errorf("default title = %q, want the identifier", spec.Properties[1].Title)
	}
	if spec.Properties[0].Enum[1] != "python" {
		t.Errorf("enum = %v", spec.Properties[0].Enum)
	}
	owner, dependsOn := spec.Properties[3], spec.Properties[4]
	if owner.Type != "relation" || owner.Relation != "team" || !dependsOn.Many || owner.Derived() {
		t.Errorf("relations = %+v %+v", owner, dependsOn)
	}
	if got := spec.Stats(); got["properties"] != 5 || got["relations"] != 2 {
		t.Errorf("stats = %v", got)
	}
}

func TestParseDisplay(t *testing.T) {
	spec := mustParse(t, `
kind: Blueprint
metadata: {name: s, title: S}
spec:
  properties:
    tier:
      type: string
      enum: [critical, standard]
      display: {as: badge, colors: {critical: red}, column: true, filter: false}
    coverage:
      type: number
      maximum: 100
      display: {as: progress, unit: "%"}
    owner:
      relation: team
      display: {hidden: true}
`)
	tier, coverage, owner := spec.Properties[0].Display, spec.Properties[1].Display, spec.Properties[2].Display
	if tier.As != "badge" || tier.Colors["critical"] != "red" || !*tier.Column || *tier.Filter {
		t.Errorf("tier display = %+v", tier)
	}
	if coverage.As != "progress" || coverage.Unit != "%" || coverage.Column != nil {
		t.Errorf("coverage display = %+v", coverage)
	}
	if !owner.Hidden {
		t.Errorf("owner display = %+v", owner)
	}
}

func TestParsePages(t *testing.T) {
	source := `
kind: Blueprint
metadata: {name: service, title: Service}
spec:
  properties:
    tier: {type: string}
  catalog:
    tabs:
      overview:
        title: Overview
        widgets:
          byTier: {type: donut, title: By Tier, groupBy: tier}
  entity:
    tabs:
      overview:
        widgets:
          about: {type: properties, title: About}
      deployments:
        title: Deployments
        widgets:
          recent:
            type: table
            title: Recent
            blueprint: deployment
            filter: {property: service, op: eq, value: $entity}
`
	spec := mustParse(t, source)
	if len(spec.Catalog.Tabs) != 1 || spec.Catalog.Tabs[0].Widgets[0].Blueprint != "service" {
		t.Errorf("catalog = %+v", spec.Catalog)
	}
	if len(spec.Entity.Tabs) != 2 || spec.Entity.Tabs[1].Widgets[0].Path != "spec.entity.tabs.deployments.widgets.recent" {
		t.Errorf("entity = %+v", spec.Entity)
	}

	next := mustParse(t, strings.Replace(source, "title: Recent", "title: Latest", 1))
	changes := Diff(spec, next)
	if len(changes) != 1 || changes[0].Section != "entity" || changes[0].ID != "deployments" || configdoc.Breaking(changes) {
		t.Errorf("Diff() = %+v", changes)
	}

	_, issues := Parse(strings.Replace(source, "      deployments:", "      relations:", 1))
	if !HasErrors(issues) || issues[0].Path != "spec.entity.tabs.relations" {
		t.Errorf("reserved tab issues = %+v", issues)
	}
}

func TestParseEntityLayout(t *testing.T) {
	source := `
kind: Blueprint
metadata: {name: service, title: Service}
spec:
  properties:
    summary: {type: string}
    lifecycle: {type: string, enum: [production, retired]}
    url: {type: string, format: uri}
    owner: {relation: team, display: {empty: No owner}}
    oncall: {type: string}
  entity:
    header: {subtitle: summary, badges: [lifecycle], link: url}
    sections:
      - title: Ownership
        properties: [owner, oncall]
`
	spec := mustParse(t, source)
	header := spec.Entity.Header
	if header.Subtitle != "summary" || len(header.Badges) != 1 || header.Badges[0] != "lifecycle" || header.Link != "url" {
		t.Errorf("header = %+v", header)
	}
	if len(spec.Entity.Sections) != 1 || spec.Entity.Sections[0].Title != "Ownership" || len(spec.Entity.Sections[0].Properties) != 2 {
		t.Errorf("sections = %+v", spec.Entity.Sections)
	}
	if spec.Properties[3].Display.Empty != "No owner" {
		t.Errorf("owner display = %+v", spec.Properties[3].Display)
	}

	next := mustParse(t, strings.Replace(source, "badges: [lifecycle]", "badges: [lifecycle, oncall]", 1))
	changes := Diff(spec, next)
	if len(changes) != 1 || changes[0].Section != "entity" || configdoc.Breaking(changes) {
		t.Errorf("Diff() = %+v", changes)
	}
}

func TestParseAcceptsJSON(t *testing.T) {
	spec := mustParse(t, `{"kind": "Blueprint", "metadata": {"name": "team", "title": "Team"},
		"spec": {"properties": {"slack": {"type": "string"}}}}`)
	if len(spec.Properties) != 1 {
		t.Fatalf("properties = %+v", spec.Properties)
	}
}

func TestParseReportsProblemsWithLines(t *testing.T) {
	tests := []struct {
		name, source, path, message string
		line                        int
	}{
		{"not yaml", "kind: [", "", "did not find expected node content", 1},
		{"empty", "", "", "the document is empty", 0},
		{"two documents", "kind: Blueprint\n---\nkind: Blueprint", "", "more than one document", 2},
		{"wrong kind", "kind: Integration\nmetadata: {name: s, title: S}\nspec: {}", "kind", "takes kind: Blueprint", 1},
		{"no metadata", "kind: Blueprint\nspec: {}", "metadata", "add metadata", 1},
		{"bad name", "kind: Blueprint\nmetadata: {name: Service, title: S}\nspec: {}", "metadata.name", "lowercase letter", 2},
		{"missing title", "kind: Blueprint\nmetadata:\n  name: service\nspec: {}", "metadata.title", "add a title", 3},
		{"api version", "apiVersion: v1\nkind: Blueprint\nmetadata: {name: s, title: S}\nspec: {}", "apiVersion", `unknown field "apiVersion"`, 1},
		{"unknown spec field", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  owner: x", "spec.owner", `unknown field "owner"`, 4},
		{"bad type", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a:\n      type: text", "spec.properties.a.type", "type must be one of", 6},
		{"enum type", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a:\n      type: integer\n      enum: [1, 2.5]", "spec.properties.a.enum", `"2.5" is not of type integer`, 7},
		{"pattern", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a:\n      type: string\n      pattern: '('", "spec.properties.a.pattern", "not a valid regular expression", 7},
		{"bounds", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a:\n      type: number\n      minimum: 5\n      maximum: 1", "spec.properties.a.minimum", "more than maximum", 7},
		{"format on number", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a:\n      type: number\n      format: uri", "spec.properties.a.format", "only string values", 7},
		{"many on a value", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a:\n      type: string\n      many: true", "spec.properties.a.many", "only relations can be many", 7},
		{"type on a relation", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a:\n      relation: t\n      type: string", "spec.properties.a.type", "relations can't have type", 7},
		{"bad field name", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    1a:\n      type: string", "spec.properties.1a", "must start with a letter", 5},
		{"bad target", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a:\n      relation: Team", "spec.properties.a.relation", "relation must name a blueprint", 6},
		{"long lookup", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    m:\n      lookup: a.b.c.d.e", "spec.properties.m.lookup", "1 to 3 relations", 6},
		{"bad jq", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    c:\n      type: string\n      compute: .a |", "spec.properties.c.compute", "not valid jq", 7},
		{"lookup and compute", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    c:\n      lookup: a.b\n      compute: .a", "spec.properties.c", "not both", 7},
		{"constraint on derived", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    c:\n      type: string\n      compute: .a\n      required: true", "spec.properties.c.required", "only applies to stored", 8},
		{"display as", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a:\n      type: boolean\n      display: {as: progress}", "spec.properties.a.display.as", "boolean properties show as badge, check", 7},
		{"display unit", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a:\n      type: string\n      display: {unit: ms}", "spec.properties.a.display.unit", "only numbers have a unit", 7},
		{"display color", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a:\n      type: string\n      enum: [x]\n      display:\n        colors: {x: navy}", "spec.properties.a.display.colors.x", "color must be one of", 9},
		{"display color value", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a:\n      type: string\n      enum: [x]\n      display:\n        colors: {y: red}", "spec.properties.a.display.colors.y", "isn't one of the property's enum values", 9},
		{"compute type", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    c:\n      compute: .a", "spec.properties.c.type", "type must be one of", 5},
		{"unknown header property", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a: {type: string}\n  entity:\n    header: {subtitle: b}", "spec.entity.header.subtitle", `s has no property "b"`, 7},
		{"link without uri", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a: {type: string}\n  entity:\n    header: {link: a}", "spec.entity.header.link", "format: uri", 7},
		{"section without title", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a: {type: string}\n  entity:\n    sections:\n      - properties: [a]", "spec.entity.sections[0].title", "add a title", 8},
		{"property in two sections", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  properties:\n    a: {type: string}\n  entity:\n    sections:\n      - {title: One, properties: [a]}\n      - {title: Two, properties: [a]}", "spec.entity.sections[1].properties[0]", "already in a section", 9},
		{"catalog header", "kind: Blueprint\nmetadata: {name: s, title: S}\nspec:\n  catalog:\n    header: {}", "spec.catalog.header", `unknown field "header"`, 5},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := Parse(test.source)
			for _, issue := range issues {
				if issue.Path == test.path && strings.Contains(issue.Message, test.message) {
					if issue.Line != test.line {
						t.Errorf("line = %d, want %d", issue.Line, test.line)
					}
					return
				}
			}
			t.Fatalf("issues = %+v, want %s: %s", issues, test.path, test.message)
		})
	}
}

func TestCheckRelationsAndLookups(t *testing.T) {
	service := mustParse(t, serviceYAML)

	issues := Check(service, map[string]Spec{})
	if HasErrors(issues) || len(issues) != 2 {
		t.Fatalf("without team: issues = %+v, want two warnings", issues)
	}

	team := mustParse(t, teamYAML)
	if issues := Check(service, map[string]Spec{"team": team}); len(issues) != 0 {
		t.Fatalf("with team: issues = %+v", issues)
	}

	broken := mustParse(t, strings.Replace(serviceYAML, "owner.$title", "owner.missing", 1))
	if issues := Check(broken, map[string]Spec{"team": team}); !HasErrors(issues) {
		t.Fatalf("missing property: issues = %+v", issues)
	}
	many := mustParse(t, strings.Replace(serviceYAML, "owner.$title", "dependsOn.$title", 1))
	if issues := Check(many, nil); !HasErrors(issues) {
		t.Fatalf("many relation: issues = %+v", issues)
	}
}

func TestDiff(t *testing.T) {
	before := mustParse(t, serviceYAML)

	if changes := Diff(before, before); len(changes) != 0 {
		t.Fatalf("no change: %+v", changes)
	}

	tests := []struct {
		name, from, to, section, change string
		breaking                        bool
	}{
		{"title only", "title: Service", "title: Services", "blueprint", ChangeChanged, false},
		{"remove property", "    stars:\n      type: integer\n      minimum: 0\n", "", "properties", ChangeRemoved, true},
		{"type change", "type: integer", "type: number", "properties", ChangeChanged, true},
		{"loosen bound", "minimum: 0", "minimum: -1", "properties", ChangeChanged, false},
		{"narrow enum", "enum: [go, python]", "enum: [go]", "properties", ChangeChanged, true},
		{"widen enum", "enum: [go, python]", "enum: [go, python, rust]", "properties", ChangeChanged, false},
		{"add optional", "    tags:\n", "    extra:\n      type: string\n    tags:\n", "properties", ChangeAdded, false},
		{"add required", "    tags:\n", "    extra:\n      type: string\n      required: true\n    tags:\n", "properties", ChangeAdded, true},
		{"retarget", "relation: team", "relation: group", "properties", ChangeChanged, true},
		{"many to one", "      many: true\n", "", "properties", ChangeChanged, true},
		{"jq change", "> 100", "> 10", "properties", ChangeChanged, false},
		{"remove derived", "    ownerName:\n      lookup: owner.$title\n", "", "properties", ChangeRemoved, false},
		{"stored to derived", "      minimum: 0\n", "      compute: '1'\n", "properties", ChangeChanged, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			after := mustParse(t, strings.Replace(serviceYAML, test.from, test.to, 1))
			changes := Diff(before, after)
			if len(changes) != 1 {
				t.Fatalf("changes = %+v, want one", changes)
			}
			got := changes[0]
			if got.Section != test.section || got.Change != test.change || got.Breaking != test.breaking {
				t.Fatalf("change = %+v, want %s %s breaking=%v", got, test.section, test.change, test.breaking)
			}
		})
	}
}
