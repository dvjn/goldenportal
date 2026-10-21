package definition

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

const teamYAML = `kind: Blueprint
identifier: team
title: Team
icon: users
properties:
  email:
    title: Email
    type: string
    format: email
`

const serviceYAML = `kind: Blueprint
identifier: service
title: Service
description: A deployable service
icon: server
properties:
  language:
    title: Language
    type: string
    enum:
      - go
      - typescript
  tier:
    title: Tier
    type: string
    enum:
      - "1"
      - "2"
  repositoryUrl:
    title: Repository
    type: string
    format: url
relations:
  owner:
    title: Owner
    target: team
`

const gitlabYAML = `kind: Integration
identifier: gitlab
title: GitLab
connector: gitlab
interval: 1h
config:
  url: https://gitlab.example.test
secrets:
  token:
    secretRef: GITLAB_TOKEN
mappings:
  - kind: project
    blueprint: service
    identifier: .path
    title: .name
    properties:
      repositoryUrl: .web_url
    relations:
      owner: .namespace.path
`

func source(documents ...string) []byte {
	return []byte(strings.Join(documents, "---\n"))
}

func mustParse(t *testing.T, text []byte) Source {
	t.Helper()
	parsed, problems := Parse(text)
	if len(problems) > 0 {
		t.Fatalf("Parse() problems = %v", problems)
	}

	return parsed
}

func TestParseReadsEveryDocumentKeepingTheKeyOrder(t *testing.T) {
	parsed := mustParse(t, source(teamYAML, serviceYAML, gitlabYAML))

	if len(parsed.Documents) != 3 {
		t.Fatalf("documents = %d, want 3", len(parsed.Documents))
	}
	service := parsed.Documents[1].(*Blueprint)
	if keys := service.Properties.Keys(); !slices.Equal(keys, []string{"language", "tier", "repositoryUrl"}) {
		t.Fatalf("property keys = %v, want the written order", keys)
	}
	if relation, _ := service.Relations.Get("owner"); relation.Target != "team" || relation.Many {
		t.Fatalf("owner relation = %+v", relation)
	}
	gitlab := parsed.Documents[2].(*Integration)
	if secret, _ := gitlab.Secrets.Get("token"); secret.SecretRef != "GITLAB_TOKEN" {
		t.Fatalf("secrets = %+v", gitlab.Secrets)
	}
	if problems := NewSet(parsed.Documents...).Check(); len(problems) > 0 {
		t.Fatalf("Check() = %v, want no problems", problems)
	}
}

func TestParseAcceptsJSON(t *testing.T) {
	parsed := mustParse(t, []byte(`{"kind":"Blueprint","identifier":"team","title":"Team","properties":{"b":{"title":"B","type":"number"},"a":{"title":"A","type":"boolean"}}}`))

	if keys := parsed.Documents[0].(*Blueprint).Properties.Keys(); !slices.Equal(keys, []string{"b", "a"}) {
		t.Fatalf("keys = %v", keys)
	}
}

func TestParseReportsSchemaProblemsAtTheirLine(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		line    int
		path    string
		message string
	}{
		{
			name:    "unknown property type",
			text:    "kind: Blueprint\nidentifier: team\ntitle: Team\nproperties:\n  size:\n    title: Size\n    type: integer\n",
			line:    7,
			path:    "properties.size.type",
			message: "value must be one of",
		},
		{
			name:    "enum value that is a number",
			text:    "kind: Blueprint\nidentifier: team\ntitle: Team\nproperties:\n  tier:\n    title: Tier\n    type: string\n    enum:\n      - 1\n",
			line:    9,
			path:    "properties.tier.enum[0]",
			message: "got number, want string",
		},
		{
			name:    "missing title",
			text:    "kind: Blueprint\nidentifier: team\n",
			line:    1,
			message: "missing property 'title'",
		},
		{
			name:    "invalid property key",
			text:    "kind: Blueprint\nidentifier: team\ntitle: Team\nproperties:\n  repository-url:\n    title: Repository\n    type: string\n",
			line:    4,
			path:    "properties",
			message: "repository-url",
		},
		{
			name:    "unknown kind",
			text:    "kind: Dashboard\nidentifier: home\ntitle: Home\n",
			line:    1,
			path:    "kind",
			message: "value must be one of",
		},
		{
			name:    "literal secret reference",
			text:    "kind: Integration\nidentifier: gitlab\ntitle: GitLab\nconnector: gitlab\nsecrets:\n  token:\n    secretRef: glpat-1234\n",
			line:    7,
			path:    "secrets.token.secretRef",
			message: "does not match pattern",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, problems := Parse([]byte(tc.text))

			if !slices.ContainsFunc(problems, func(problem Problem) bool {
				return problem.Line == tc.line && problem.Path == tc.path && strings.Contains(problem.Message, tc.message) && problem.Document == 1
			}) {
				t.Fatalf("problems = %+v, want %q at line %d path %q", problems, tc.message, tc.line, tc.path)
			}
		})
	}
}

func TestParseReportsYAMLSyntaxErrorsAtTheirLine(t *testing.T) {
	_, problems := Parse([]byte("kind: Blueprint\nidentifier: [team\ntitle: Team\n"))

	if len(problems) != 1 || problems[0].Line == 0 {
		t.Fatalf("problems = %+v, want one syntax problem with a line", problems)
	}
}

func TestParseRefusesADocumentDefinedTwice(t *testing.T) {
	_, problems := Parse(source(teamYAML, teamYAML))

	if len(problems) != 1 || problems[0].Document != 2 || !strings.Contains(problems[0].Message, "twice") {
		t.Fatalf("problems = %+v", problems)
	}
}

func TestSchemaPicksTheKindsSchema(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(SchemaJSON(), &schema); err != nil {
		t.Fatal(err)
	}
	definitions, _ := schema["$defs"].(map[string]any)
	for _, kind := range []string{"Blueprint", "Integration", "MapOfProperty", "Mapping"} {
		if _, ok := definitions[kind]; !ok {
			t.Errorf("$defs has no %s: %v", kind, definitions)
		}
	}
	if all, _ := schema["allOf"].([]any); len(all) != len(Kinds()) {
		t.Errorf("allOf = %v, want one branch per kind", all)
	}
}
