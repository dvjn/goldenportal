package blueprint

import (
	"strings"
	"testing"
)

const serviceYAML = `
identifier: service
title: Service
description: A deployable unit of software.
icon: server
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
identifier: team
title: Team
properties:
  slack:
    type: string
    format: url
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

func TestParseAcceptsJSON(t *testing.T) {
	spec := mustParse(t, `{"identifier": "team", "title": "Team", "properties": {"slack": {"type": "string"}}}`)
	if len(spec.Properties) != 1 {
		t.Fatalf("properties = %+v", spec.Properties)
	}
}

func TestParseReportsProblemsWithLines(t *testing.T) {
	tests := []struct {
		name, source, path, message string
		line                        int
	}{
		{"not yaml", "identifier: [", "", "did not find expected node content", 1},
		{"empty", "", "", "the document is empty", 0},
		{"bad identifier", "identifier: Service\ntitle: S", "identifier", "lowercase letter", 1},
		{"missing title", "identifier: service", "title", "add a title", 1},
		{"unknown field", "identifier: s\ntitle: S\nowner: x", "owner", `unknown field "owner"`, 3},
		{"bad type", "identifier: s\ntitle: S\nproperties:\n  a:\n    type: text", "properties.a.type", "type must be one of", 5},
		{"enum type", "identifier: s\ntitle: S\nproperties:\n  a:\n    type: integer\n    enum: [1, 2.5]", "properties.a.enum", `"2.5" is not of type integer`, 6},
		{"pattern", "identifier: s\ntitle: S\nproperties:\n  a:\n    type: string\n    pattern: '('", "properties.a.pattern", "not a valid regular expression", 6},
		{"bounds", "identifier: s\ntitle: S\nproperties:\n  a:\n    type: number\n    minimum: 5\n    maximum: 1", "properties.a.minimum", "more than maximum", 6},
		{"format on number", "identifier: s\ntitle: S\nproperties:\n  a:\n    type: number\n    format: url", "properties.a.format", "only string values", 6},
		{"many on a value", "identifier: s\ntitle: S\nproperties:\n  a:\n    type: string\n    many: true", "properties.a.many", "only relations can be many", 6},
		{"type on a relation", "identifier: s\ntitle: S\nproperties:\n  a:\n    relation: t\n    type: string", "properties.a.type", "relations can't have type", 6},
		{"bad field name", "identifier: s\ntitle: S\nproperties:\n  1a:\n    type: string", "properties.1a", "must start with a letter", 4},
		{"bad target", "identifier: s\ntitle: S\nproperties:\n  a:\n    relation: Team", "properties.a.relation", "relation must name a blueprint", 5},
		{"long lookup", "identifier: s\ntitle: S\nproperties:\n  m:\n    lookup: a.b.c.d.e", "properties.m.lookup", "1 to 3 relations", 5},
		{"bad jq", "identifier: s\ntitle: S\nproperties:\n  c:\n    type: string\n    compute: .a |", "properties.c.compute", "not valid jq", 6},
		{"lookup and compute", "identifier: s\ntitle: S\nproperties:\n  c:\n    lookup: a.b\n    compute: .a", "properties.c", "not both", 6},
		{"constraint on derived", "identifier: s\ntitle: S\nproperties:\n  c:\n    type: string\n    compute: .a\n    required: true", "properties.c.required", "only applies to stored", 7},
		{"compute type", "identifier: s\ntitle: S\nproperties:\n  c:\n    compute: .a", "properties.c.type", "type must be one of", 4},
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
		{"remove property", "  stars:\n    type: integer\n    minimum: 0\n", "", "properties", ChangeRemoved, true},
		{"type change", "type: integer", "type: number", "properties", ChangeChanged, true},
		{"loosen bound", "minimum: 0", "minimum: -1", "properties", ChangeChanged, false},
		{"narrow enum", "enum: [go, python]", "enum: [go]", "properties", ChangeChanged, true},
		{"widen enum", "enum: [go, python]", "enum: [go, python, rust]", "properties", ChangeChanged, false},
		{"add optional", "  tags:\n", "  extra:\n    type: string\n  tags:\n", "properties", ChangeAdded, false},
		{"add required", "  tags:\n", "  extra:\n    type: string\n    required: true\n  tags:\n", "properties", ChangeAdded, true},
		{"retarget", "relation: team", "relation: group", "properties", ChangeChanged, true},
		{"many to one", "    many: true\n", "", "properties", ChangeChanged, true},
		{"jq change", "> 100", "> 10", "properties", ChangeChanged, false},
		{"remove derived", "  ownerName:\n    lookup: owner.$title\n", "", "properties", ChangeRemoved, false},
		{"stored to derived", "    minimum: 0\n", "    compute: '1'\n", "properties", ChangeChanged, true},
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
