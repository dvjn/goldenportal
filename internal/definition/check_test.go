package definition

import (
	"slices"
	"strings"
	"testing"
)

func problemsOf(t *testing.T, text []byte) []Problem {
	t.Helper()
	parsed := mustParse(t, text)

	return parsed.Locate(NewSet(parsed.Documents...).Check())
}

func TestCheckFindsWhatTheSchemaCannotSee(t *testing.T) {
	cases := []struct {
		name    string
		text    []byte
		line    int
		path    string
		message string
	}{
		{
			name:    "relation to an unknown blueprint",
			text:    source(serviceYAML),
			line:    26,
			path:    "relations.owner.target",
			message: `no blueprint has the identifier "team"`,
		},
		{
			name:    "relation named like a property",
			text:    source(teamYAML, "kind: Blueprint\nidentifier: service\ntitle: Service\nproperties:\n  owner:\n    title: Owner\n    type: string\nrelations:\n  owner:\n    title: Owner\n    target: team\n"),
			line:    19,
			path:    "relations.owner",
			message: "already a property",
		},
		{
			name:    "enum on a number",
			text:    []byte("kind: Blueprint\nidentifier: team\ntitle: Team\nproperties:\n  size:\n    title: Size\n    type: number\n    enum:\n      - small\n"),
			line:    5,
			path:    "properties.size",
			message: "apply only to string and array",
		},
		{
			name:    "display column that is not a property",
			text:    []byte("kind: Blueprint\nidentifier: team\ntitle: Team\ndisplay:\n  columns:\n    - email\n"),
			line:    6,
			path:    "display.columns[0]",
			message: `"email" is not a property`,
		},
		{
			name:    "integration named manual",
			text:    []byte("kind: Integration\nidentifier: manual\ntitle: Manual\nconnector: push\n"),
			line:    2,
			path:    "identifier",
			message: "built-in integration",
		},
		{
			name:    "interval on a push integration",
			text:    []byte("kind: Integration\nidentifier: ci\ntitle: CI\nconnector: push\ninterval: 1h\n"),
			line:    5,
			path:    "interval",
			message: "has no interval",
		},
		{
			name:    "interval shorter than a minute",
			text:    []byte("kind: Integration\nidentifier: gitlab\ntitle: GitLab\nconnector: gitlab\ninterval: 10s\n"),
			line:    5,
			path:    "interval",
			message: "at least 1m",
		},
		{
			name:    "literal secret in the config",
			text:    []byte("kind: Integration\nidentifier: gitlab\ntitle: GitLab\nconnector: gitlab\nconfig:\n  url: https://gitlab.example.test\n  apiToken: glpat-1234\n"),
			line:    7,
			path:    "config.apiToken",
			message: "looks like a secret",
		},
		{
			name:    "invalid jq",
			text:    source(teamYAML, serviceYAML, strings.Replace(gitlabYAML, "identifier: .path", "identifier: .path |", 1)),
			line:    51,
			path:    "mappings[0].identifier",
			message: "not a valid jq expression",
		},
		{
			name:    "mapping of an unknown property",
			text:    source(teamYAML, serviceYAML, strings.Replace(gitlabYAML, "repositoryUrl: .web_url", "homepage: .web_url", 1)),
			line:    54,
			path:    "mappings[0].properties.homepage",
			message: `"homepage" is not a property of blueprint service`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := problemsOf(t, tc.text)

			if !slices.ContainsFunc(problems, func(problem Problem) bool {
				return problem.Line == tc.line && problem.Path == tc.path && strings.Contains(problem.Message, tc.message)
			}) {
				t.Fatalf("problems = %+v, want %q at line %d path %q", problems, tc.message, tc.line, tc.path)
			}
		})
	}
}

func TestCheckGivesEachMappingAKindItsConnectorReceives(t *testing.T) {
	const push = "kind: Integration\nidentifier: ci\ntitle: CI\nconnector: push\nmappings:\n"
	cases := []struct {
		name     string
		mappings string
		path     string
		message  string
	}{
		{
			name:     "kind a pull connector does not read",
			mappings: "kind: Integration\nidentifier: gitlab\ntitle: GitLab\nconnector: gitlab\nmappings:\n  - kind: issue\n    blueprint: team\n    identifier: .id\n",
			path:     "mappings[0].kind",
			message:  `the gitlab connector reads group, project, pipeline, not "issue"`,
		},
		{
			name:     "pull mapping without an identifier",
			mappings: "kind: Integration\nidentifier: sonar\ntitle: Sonar\nconnector: sonarqube\nmappings:\n  - kind: project\n    blueprint: team\n",
			path:     "mappings[0]",
			message:  "a pull connector reads records that are not entities yet",
		},
		{
			name:     "push mapping with a title but no identifier",
			mappings: push + "  - kind: team\n    blueprint: team\n    title: .name\n",
			path:     "mappings[0]",
			message:  "leave out every expression to receive entities as they are",
		},
		{
			name:     "push kind mapped twice",
			mappings: push + "  - kind: team\n    blueprint: team\n  - kind: team\n    blueprint: service\n",
			path:     "mappings[1].kind",
			message:  `another mapping already receives kind "team"`,
		},
		{
			name:     "push blueprint written from two kinds",
			mappings: push + "  - kind: team\n    blueprint: team\n  - kind: group\n    blueprint: team\n",
			path:     "mappings[1].blueprint",
			message:  `another mapping already writes blueprint "team"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := problemsOf(t, source(teamYAML, "kind: Blueprint\nidentifier: service\ntitle: Service\n", tc.mappings))

			if !slices.ContainsFunc(problems, func(problem Problem) bool {
				return problem.Path == tc.path && strings.Contains(problem.Message, tc.message)
			}) {
				t.Fatalf("problems = %+v, want %q at path %q", problems, tc.message, tc.path)
			}
		})
	}
}

func TestCheckAcceptsAPushMappingWithoutExpressions(t *testing.T) {
	ci := "kind: Integration\nidentifier: ci\ntitle: CI\nconnector: push\nmappings:\n  - kind: team\n    blueprint: team\n"
	set := NewSet(mustParse(t, source(teamYAML, ci)).Documents...)

	if problems := set.Check(); len(problems) > 0 {
		t.Fatalf("problems = %v, want none", problems)
	}
	integration, _ := set.Integration("ci")
	if mapping, ok := integration.Mapping("team"); !ok || !mapping.Canonical() {
		t.Fatalf("Mapping(team) = %+v, %t, want the canonical team mapping", mapping, ok)
	}
	if _, ok := integration.Mapping("service"); ok {
		t.Fatal("Mapping(service) found a mapping, want none")
	}
}

func TestCheckAllowsRelationCycles(t *testing.T) {
	team := strings.Replace(teamYAML, "properties:", "relations:\n  parent:\n    title: Parent team\n    target: team\n  lead:\n    title: Lead service\n    target: service\nproperties:", 1)

	if problems := problemsOf(t, source(team, serviceYAML)); len(problems) > 0 {
		t.Fatalf("problems = %v, want none", problems)
	}
}

func TestCheckReportsAProblemOutsideTheSourceWithoutALine(t *testing.T) {
	current := NewSet(mustParse(t, source(teamYAML, serviceYAML)).Documents...)
	parsed := mustParse(t, []byte("kind: Blueprint\nidentifier: group\ntitle: Group\n"))
	desired := current.Merge(parsed.Documents, []Ref{{Kind: KindBlueprint, Identifier: "team"}})

	problems := parsed.Locate(desired.Check())

	if len(problems) != 1 || problems[0].Identifier != "service" || problems[0].Line != 0 || problems[0].Document != 0 {
		t.Fatalf("problems = %+v, want the service relation without a line", problems)
	}
}

func TestYAMLWritesBlockStyleInTheDocumentsOrder(t *testing.T) {
	set := NewSet(mustParse(t, source(teamYAML, serviceYAML)).Documents...)

	text, err := set.YAML()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(string(text), serviceYAML+"---\n"+teamYAML) {
		t.Fatalf("YAML() =\n%s\nwant the blueprints by identifier, keys as written", text)
	}
	again := mustParse(t, text)
	if roundTrip, _ := NewSet(again.Documents...).YAML(); string(roundTrip) != string(text) {
		t.Fatalf("round trip =\n%s\nwant\n%s", roundTrip, text)
	}
	if !strings.Contains(string(text), "    enum:\n      - go\n") || !strings.Contains(string(text), `- "1"`) {
		t.Fatalf("YAML() =\n%s\nwant indented lists with quoted numeric strings", text)
	}
}
