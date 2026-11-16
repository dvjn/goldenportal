package blueprint

import (
	"slices"
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

const serviceYAML = `
kind: Blueprint
metadata:
  identifier: service
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
      lookup: owner.title
    popular:
      type: boolean
      compute: .stars > 100
`

const teamYAML = `
kind: Blueprint
metadata:
  identifier: team
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
metadata: {identifier: s, title: S}
spec:
  properties:
    tier:
      type: string
      enum: [critical, standard]
      display: {as: badge, colors: {critical: red}, titles: {critical: Business Critical}, filter: false}
    coverage:
      type: number
      maximum: 100
      display: {as: progress, unit: "%"}
    owner:
      relation: team
      display: {hidden: true}
  catalog:
    columns: [tier, updatedAt]
    tabs: {}
`)
	if !slices.Equal(spec.Catalog.Columns, []string{"tier", "updatedAt"}) {
		t.Errorf("columns = %v", spec.Catalog.Columns)
	}
	tier, coverage, owner := spec.Properties[0].Display, spec.Properties[1].Display, spec.Properties[2].Display
	if tier.As != "badge" || tier.Colors["critical"] != "red" || *tier.Filter {
		t.Errorf("tier display = %+v", tier)
	}
	if tier.Titles["critical"] != "Business Critical" {
		t.Errorf("tier titles = %v", tier.Titles)
	}
	if coverage.As != "progress" || coverage.Unit != "%" {
		t.Errorf("coverage display = %+v", coverage)
	}
	if !owner.Hidden {
		t.Errorf("owner display = %+v", owner)
	}
}

func TestParsePages(t *testing.T) {
	source := `
kind: Blueprint
metadata: {identifier: service, title: Service}
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
    queries:
      recentDeployments: {blueprint: deployment, filter: {property: service, op: eq, value: $entity}}
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
            query: recentDeployments
`
	spec := mustParse(t, source)
	if len(spec.Entity.Queries) != 1 || spec.Entity.Tabs[1].Widgets[0].Query.Blueprint != "deployment" {
		t.Errorf("entity queries = %+v", spec.Entity)
	}
	if len(spec.Catalog.Tabs) != 1 || spec.Catalog.Tabs[0].Widgets[0].Query.Blueprint != "service" {
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

func TestParseTabWhen(t *testing.T) {
	head := `
kind: Blueprint
metadata: {identifier: job, title: Job}
spec:
  properties:
    appliesTo: {type: array, items: {type: string}}
`
	tab := "        when: {property: appliesTo, op: exists}\n        widgets: {n: {type: stat, title: N, value: {}}}\n"
	spec := mustParse(t, head+"  entity:\n    tabs:\n      adoption:\n"+tab)
	if when := spec.Entity.Tabs[0].When; when == nil || when.Property != "appliesTo" || when.Op != "exists" {
		t.Errorf("when = %+v", when)
	}

	tests := []struct{ name, page, path, message string }{
		{"catalog tabs show no entity", "  catalog:\n    tabs:\n      adoption:\n", "spec.catalog.tabs.adoption.when", "only entity pages' tabs take it"},
		{"the overview always shows", "  entity:\n    tabs:\n      overview:\n", "spec.entity.tabs.overview.when", "always shown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, issues := Parse(head + tt.page + tab)
			if !HasErrors(issues) || issues[0].Path != tt.path || !strings.Contains(issues[0].Message, tt.message) {
				t.Errorf("issues = %+v", issues)
			}
		})
	}
}

func TestParseEntityLayout(t *testing.T) {
	source := `
kind: Blueprint
metadata: {identifier: service, title: Service}
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
	spec := mustParse(t, `{"kind": "Blueprint", "metadata": {"identifier": "team", "title": "Team"},
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
		{"wrong kind", "kind: Connection\nmetadata: {identifier: s, title: S}\nspec: {}", "kind", "takes kind: Blueprint", 1},
		{"no metadata", "kind: Blueprint\nspec: {}", "metadata", "add metadata", 1},
		{"bad name", "kind: Blueprint\nmetadata: {identifier: Service, title: S}\nspec: {}", "metadata.identifier", "lowercase letter", 2},
		{"missing title", "kind: Blueprint\nmetadata:\n  identifier: service\nspec: {}", "metadata.title", "add a title", 3},
		{"api version", "apiVersion: v1\nkind: Blueprint\nmetadata: {identifier: s, title: S}\nspec: {}", "apiVersion", `unknown field "apiVersion"`, 1},
		{"unknown spec field", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  owner: x", "spec.owner", `unknown field "owner"`, 4},
		{"bad type", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a:\n      type: text", "spec.properties.a.type", "type must be one of", 6},
		{"enum type", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a:\n      type: integer\n      enum: [1, 2.5]", "spec.properties.a.enum", `"2.5" is not of type integer`, 7},
		{"pattern", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a:\n      type: string\n      pattern: '('", "spec.properties.a.pattern", "not a valid regular expression", 7},
		{"bounds", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a:\n      type: number\n      minimum: 5\n      maximum: 1", "spec.properties.a.minimum", "more than maximum", 7},
		{"format on number", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a:\n      type: number\n      format: uri", "spec.properties.a.format", "only string values", 7},
		{"many on a value", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a:\n      type: string\n      many: true", "spec.properties.a.many", "only relations can be many", 7},
		{"type on a relation", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a:\n      relation: t\n      type: string", "spec.properties.a.type", "relations can't have type", 7},
		{"bad field name", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    1a:\n      type: string", "spec.properties.1a", "must start with a letter", 5},
		{"bad target", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a:\n      relation: Team", "spec.properties.a.relation", "relation must name a blueprint", 6},
		{"long lookup", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    m:\n      lookup: a.b.c.d.e", "spec.properties.m.lookup", "1 to 3 relations", 6},
		{"bad jq", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    c:\n      type: string\n      compute: .a |", "spec.properties.c.compute", "not valid jq", 7},
		{"lookup and compute", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    c:\n      lookup: a.b\n      compute: .a", "spec.properties.c", "not both", 7},
		{"constraint on derived", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    c:\n      type: string\n      compute: .a\n      required: true", "spec.properties.c.required", "only applies to stored", 8},
		{"display as", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a:\n      type: boolean\n      display: {as: progress}", "spec.properties.a.display.as", "boolean properties show as badge, check", 7},
		{"display unit", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a:\n      type: string\n      display: {unit: ms}", "spec.properties.a.display.unit", "only numbers have a unit", 7},
		{"display color", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a:\n      type: string\n      enum: [x]\n      display:\n        colors: {x: navy}", "spec.properties.a.display.colors.x", "color must be one of", 9},
		{"display color value", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a:\n      type: string\n      enum: [x]\n      display:\n        colors: {y: red}", "spec.properties.a.display.colors.y", "isn't one of the property's enum values", 9},
		{"display title value", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a:\n      type: string\n      enum: [x]\n      display:\n        titles: {y: Why}", "spec.properties.a.display.titles.y", "isn't one of the property's enum values", 9},
		{"compute type", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    c:\n      compute: .a", "spec.properties.c.type", "type must be one of", 5},
		{"unknown header property", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a: {type: string}\n  entity:\n    header: {subtitle: b}", "spec.entity.header.subtitle", `s has no property "b"`, 7},
		{"link without uri", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a: {type: string}\n  entity:\n    header: {link: a}", "spec.entity.header.link", "format: uri", 7},
		{"section without title", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a: {type: string}\n  entity:\n    sections:\n      - properties: [a]", "spec.entity.sections[0].title", "add a title", 8},
		{"property in two sections", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    a: {type: string}\n  entity:\n    sections:\n      - {title: One, properties: [a]}\n      - {title: Two, properties: [a]}", "spec.entity.sections[1].properties[0]", "already in a section", 9},
		{"catalog header", "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  catalog:\n    header: {}", "spec.catalog.header", `unknown field "header"`, 5},
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

	broken := mustParse(t, strings.Replace(serviceYAML, "owner.title", "owner.missing", 1))
	if issues := Check(broken, map[string]Spec{"team": team}); !HasErrors(issues) {
		t.Fatalf("missing property: issues = %+v", issues)
	}
	many := mustParse(t, strings.Replace(serviceYAML, "owner.title", "dependsOn.title", 1))
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
		{"remove derived", "    ownerName:\n      lookup: owner.title\n", "", "properties", ChangeRemoved, false},
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

func TestParseSymbols(t *testing.T) {
	base := "kind: Blueprint\nmetadata: {identifier: service, title: Service}\nspec:\n  properties:\n"
	tests := []struct{ name, properties, path, message string }{
		{"reserved title", "    title: {type: string}", "spec.properties.title", "built-in field every entity has"},
		{"reserved updatedAt", "    updatedAt: {type: string}", "spec.properties.updatedAt", "built-in field"},
		{"lookup with $", "    owner: {relation: team}\n    ownerName: {lookup: owner.$title}", "spec.properties.ownerName.lookup", "write owner.title"},
		{"scorecard needs a name", "    readiness: {type: scorecard}", "spec.properties.readiness.scorecard", "name the scorecard"},
		{"scorecard type", "    readiness: {type: string, scorecard: r}", "spec.properties.readiness.type", "type: scorecard"},
		{"scorecard constraints", "    readiness: {scorecard: r, required: true}", "spec.properties.readiness.required", "the scorecard works the value out"},
		{"scorecard display", "    readiness: {scorecard: r, display: {as: text}}", "spec.properties.readiness.display.as", "show as badge"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := Parse(base + test.properties)
			for _, issue := range issues {
				if issue.Path == test.path && strings.Contains(issue.Message, test.message) {
					return
				}
			}
			t.Fatalf("issues = %+v, want %s: %s", issues, test.path, test.message)
		})
	}

	spec := mustParse(t, base+"    readiness: {type: scorecard, scorecard: service-readiness, display: {as: badge}}")
	prop, _ := spec.Property("readiness")
	if !prop.IsScorecard() || !prop.Derived() || prop.Type != "scorecard" || prop.Scorecard != "service-readiness" {
		t.Fatalf("readiness = %+v", prop)
	}
	if _, _, problems := spec.ValidateEntity(Entity{Identifier: "a", Properties: map[string]any{"readiness": "gold"}}); len(problems) == 0 {
		t.Fatal("a connection wrote a scorecard's result")
	}
}

func TestCheckScorecards(t *testing.T) {
	spec := mustParse(t, "kind: Blueprint\nmetadata: {identifier: service, title: Service}\nspec:\n  properties:\n    readiness: {type: scorecard, scorecard: readiness}")
	for _, test := range []struct {
		ladders map[string]Ladder
		want    string
	}{
		{map[string]Ladder{"readiness": {Blueprint: "service"}}, ""},
		{map[string]Ladder{}, SeverityWarning},
		{map[string]Ladder{"readiness": {Blueprint: "team"}}, SeverityError},
	} {
		issues := CheckScorecards(spec, test.ladders)
		got := ""
		if len(issues) > 0 {
			got = issues[0].Severity
		}
		if got != test.want {
			t.Errorf("ladders %v: issues = %+v, want %q", test.ladders, issues, test.want)
		}
	}
	if names := (Ladder{Tiers: []Rung{{Name: "bronze"}, {Name: "gold"}}}).Names(); strings.Join(names, ",") != ",bronze,gold" {
		t.Errorf("Names() = %q", names)
	}
}

func TestCatalogColumnsAreKnownFields(t *testing.T) {
	head := "kind: Blueprint\nmetadata: {identifier: s, title: S}\nspec:\n  properties:\n    tier: {type: string}\n  catalog:\n    tabs: {}\n    columns: "
	for columns, want := range map[string]string{
		"[tier, tier]": "tier is already a column",
		"[title]":      "the title column already shows the title",
		"[nope]":       `s has no property "nope"`,
	} {
		_, issues := Parse(head + columns)
		if !slices.ContainsFunc(issues, func(issue configdoc.Issue) bool { return strings.Contains(issue.Message, want) }) {
			t.Errorf("columns %s: issues = %+v, want %q", columns, issues, want)
		}
	}
}

const pipelineYAML = `
kind: Blueprint
metadata: {identifier: pipeline, title: Pipeline}
spec:
  properties:
    status: {type: string}
    project: {relation: project}
    slow: {type: boolean, compute: .status == "slow"}
  sources:
    - connection: gitlab
      resource: pipelines
      entity:
        identifier: .id | tostring
        title: .ref
        properties:
          status: .status
          project: ._project
`

func TestParseSources(t *testing.T) {
	spec := mustParse(t, pipelineYAML)
	if len(spec.Sources) != 1 {
		t.Fatalf("sources = %+v", spec.Sources)
	}
	s := spec.Sources[0]
	if s.Connection != "gitlab" || s.Resource != "pipelines" || s.Mode != SourceReconcile || s.Upsert() ||
		s.Entity.Identifier != ".id | tostring" || len(s.Entity.Properties) != 2 {
		t.Fatalf("source = %+v", s)
	}
	if got := spec.Stats()["sources"]; got != 1 {
		t.Fatalf("stats sources = %d", got)
	}
	if spec := mustParse(t, teamYAML); spec.Sources == nil || len(spec.Sources) != 0 {
		t.Fatalf("no sources: %+v", spec.Sources)
	}

	two := strings.Replace(pipelineYAML, "  sources:\n", `  sources:
    - connection: tracker
      resource: runs
      mode: upsert
      entity: {identifier: .id}
`, 1)
	if spec := mustParse(t, two); len(spec.Sources) != 2 {
		t.Fatalf("two connections with their own modes: %+v", spec.Sources)
	}
}

func TestParseSourceProblems(t *testing.T) {
	tests := []struct {
		name, from, to, path, message string
	}{
		{"missing connection", "    - connection: gitlab\n      resource", "    - resource", "spec.sources[0].connection", "connection must name a connection"},
		{"missing resource", "      resource: pipelines\n", "", "spec.sources[0].resource", "add the resource"},
		{"bad mode", "      resource: pipelines\n", "      resource: pipelines\n      mode: merge\n", "spec.sources[0].mode", "mode must be reconcile or upsert"},
		{"bad jq", "title: .ref", "title: .ref |", "spec.sources[0].entity.title", "not valid jq"},
		{"bad property jq", "status: .status", "status: .status |", "spec.sources[0].entity.properties.status", "not valid jq"},
		{"yaml list as jq", "title: .ref", "title: [.ref]", "spec.sources[0].entity.title", "quote this jq expression"},
		{"no identifier", "        identifier: .id | tostring\n", "", "spec.sources[0].entity.identifier", "add a jq expression for identifier"},
		{"unknown property", "status: .status", "state: .status", "spec.sources[0].entity.properties.state", `pipeline has no stored property "state" to write`},
		{"derived property", "status: .status", "slow: .status", "spec.sources[0].entity.properties.slow", `pipeline has no stored property "slow" to write`},
		{
			"retention with reconcile", "  sources:\n", "  retention: {by: started, keep: 30d}\n  sources:\n",
			"spec.sources[0].mode", "pipeline has retention, so its sources must use mode: upsert",
		},
		{
			"mixed modes", "  sources:\n", "  sources:\n    - {connection: gitlab, resource: jobs, mode: upsert, entity: {identifier: .id}}\n",
			"spec.sources[1].mode", "another source from gitlab uses mode: upsert, and a blueprint's sources from one connection share a mode",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := strings.Replace(pipelineYAML, test.from, test.to, 1)
			if test.name == "retention with reconcile" {
				source = strings.Replace(source, "status: {type: string}", "status: {type: string}\n    started: {type: string, format: date-time}", 1)
			}
			spec, issues := Parse(source)
			if len(spec.Sources) == 0 {
				t.Fatalf("sources dropped: %+v", issues)
			}
			for _, issue := range issues {
				if issue.Path == test.path && strings.Contains(issue.Message, test.message) {
					if issue.Line == 0 {
						t.Errorf("issue has no line: %+v", issue)
					}
					return
				}
			}
			t.Fatalf("issues = %+v, want %s: %s", issues, test.path, test.message)
		})
	}
}

func TestSameSource(t *testing.T) {
	a := mustParse(t, pipelineYAML).Sources[0]
	reordered := strings.Replace(pipelineYAML, "          status: .status\n          project: ._project\n",
		"          project: ._project\n          status: .status\n", 1)
	b := mustParse(t, "\n\n"+reordered).Sources[0]
	if !SameSource(a, b) {
		t.Fatalf("reordered properties on other lines differ: %+v %+v", a, b)
	}
	b.Filter = ".ref == \"main\""
	if SameSource(a, b) {
		t.Fatal("a filter makes no difference")
	}
}

func TestCheckSourceReadsGP(t *testing.T) {
	const property, identifier, title = "spec.sources[0].entity.properties.project", "spec.sources[0].entity.identifier", "spec.sources[0].entity.title"
	for _, test := range []struct {
		field, expression, path string
		line                    int
		want                    string
	}{
		{"project", `._gp.project`, property, 17, "._gp.project is now ._project"},
		{"project", `._gp["project"]`, property, 17, `._gp["project"] is now ._project`},
		{"project", `.["_gp"].project`, property, 17, `.["_gp"].project is now ._project`},
		{"project", `._gp?.project`, property, 17, "._gp?.project is now ._project?"},
		{"project", `"\(._gp.project)!"`, property, 17, "._gp.project is now ._project"},
		{"identifier", `._gp.project + "#" + (._gp.project | ascii_downcase)`, identifier, 13, "._gp.project is now ._project"},
		{"project", `._gp | .project`, property, 17, "_gp is gone: connectors add each field to the item itself, so ._gp.<name> is now ._<name>"},
		{"title", `._gp.project + (._gp | keys | join(","))`, title, 14, "._gp.project is now ._project; _gp is gone"},
	} {
		issues := checkSourceReads(mustParse(t, withSourceExpression(test.field, test.expression)))
		if len(issues) != 1 || issues[0].Severity != SeverityError || issues[0].Path != test.path || issues[0].Line != test.line ||
			!strings.HasPrefix(issues[0].Message, test.want) {
			t.Errorf("%s: issues = %+v, want %s at line %d: %s", test.expression, issues, test.path, test.line, test.want)
		}
	}
	// Reads of _gp further into the item, and _gp in strings, are fine.
	for _, expression := range []string{
		`._project`, `.labels["_gp"]["x"]`, `.x._gp.y`, `.x | .labels.["_gp"]`, `.name | test("_gp")`, `"\(.id) _gp"`, `"._gp.project"`,
		`._gpx.project`, `.id # ._gp.project`,
	} {
		if issues := checkSourceReads(mustParse(t, withSourceExpression("project", expression))); len(issues) != 0 {
			t.Errorf("%s: issues = %+v", expression, issues)
		}
	}
}

// withSourceExpression is pipelineYAML with one of its source's expressions
// replaced.
func withSourceExpression(field, expression string) string {
	quoted := "'" + expression + "'"
	if strings.Contains(expression, "#") {
		quoted = "|-\n            " + expression
	}
	switch field {
	case "identifier":
		return strings.Replace(pipelineYAML, "identifier: .id | tostring\n", "identifier: "+quoted+"\n", 1)
	case "title":
		return strings.Replace(pipelineYAML, "title: .ref\n", "title: "+quoted+"\n", 1)
	}

	return strings.Replace(pipelineYAML, "project: ._project\n", "project: "+quoted+"\n", 1)
}

func TestDiffSources(t *testing.T) {
	before := mustParse(t, pipelineYAML)
	upsert := strings.Replace(pipelineYAML, "      resource: pipelines\n", "      resource: pipelines\n      mode: upsert\n", 1)
	added := strings.Replace(pipelineYAML, "  sources:\n", "  sources:\n    - {connection: gitlab, resource: jobs, entity: {identifier: .id}}\n", 1)

	tests := []struct {
		name       string
		from, to   Spec
		change, id string
		detail     string
	}{
		{"added reconcile", before, mustParse(t, added), ChangeAdded, "gitlab.jobs", "gitlab starts writing pipeline from jobs; the next gitlab sync is complete"},
		{"changed upsert", before, mustParse(t, upsert), ChangeChanged, "gitlab.pipelines", "changes how gitlab writes pipeline from pipelines; the next gitlab sync re-reads pipelines"},
		{"removed", mustParse(t, added), before, ChangeRemoved, "gitlab.jobs", "gitlab stops writing pipeline from jobs; the ones it wrote stay"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changes := Diff(test.from, test.to)
			if len(changes) != 1 {
				t.Fatalf("changes = %+v, want one", changes)
			}
			got := changes[0]
			if got.Section != "sources" || got.Change != test.change || got.ID != test.id || got.Detail != test.detail || got.Breaking {
				t.Fatalf("change = %+v", got)
			}
		})
	}
}
