package integration

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dvjn/goldport/internal/catalog"
	"github.com/dvjn/goldport/internal/definition"
)

const (
	teamYAML = `kind: Blueprint
identifier: team
title: Team
`
	serviceYAML = `kind: Blueprint
identifier: service
title: Service
properties:
  language:
    title: Language
    type: string
  tier:
    title: Tier
    type: string
    enum:
      - "1"
      - "2"
  lifecycle:
    title: Lifecycle
    type: string
relations:
  owner:
    title: Owner
    target: team
  dependsOn:
    title: Depends on
    target: service
    many: true
`
	ciYAML = `kind: Integration
identifier: ci
title: CI
connector: push
mappings:
  - kind: service
    blueprint: service
    identifier: .name
    title: .display_name // .name
    properties:
      language: .language | values | ascii_downcase
      tier: .tier | values | tostring
      lifecycle: '"production"'
    relations:
      owner: .team
      dependsOn: .uses
  - kind: team
    blueprint: team
`
)

func testSet(t *testing.T, documents ...string) definition.Set {
	t.Helper()
	source, problems := definition.Parse([]byte(strings.Join(documents, "---\n")))
	if len(problems) > 0 {
		t.Fatalf("Parse() = %v", problems)
	}
	set := definition.NewSet(source.Documents...)
	if problems := set.Check(); len(problems) > 0 {
		t.Fatalf("Check() = %v", problems)
	}

	return set
}

func acmeSet(t *testing.T) definition.Set {
	return testSet(t, teamYAML, serviceYAML, ciYAML)
}

func testMapper(t *testing.T, set definition.Set, kind string) *Mapper {
	t.Helper()
	integration, _ := set.Integration("ci")
	mapping, ok := integration.Mapping(kind)
	if !ok {
		t.Fatalf("no mapping for %s", kind)
	}
	mapper, err := NewMapper(set, mapping)
	if err != nil {
		t.Fatal(err)
	}

	return mapper
}

func records(t *testing.T, text string) []any {
	t.Helper()
	var decoded []any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatal(err)
	}

	return decoded
}

func link(source, relation, blueprint, target string) catalog.Link {
	return catalog.Link{
		Relation: relation,
		Source:   catalog.EntityName{Blueprint: "service", Identifier: source, Title: map[string]string{"payments-api": "Payments API"}[source]},
		Target:   catalog.EntityName{Blueprint: blueprint, Identifier: target},
	}
}

func TestMapperRunsTheMappingsExpressions(t *testing.T) {
	mapper := testMapper(t, acmeSet(t), "service")

	entities, problems := mapper.Map(context.Background(), records(t, `[
		{"name": "payments-api", "display_name": "Payments API", "language": "go", "tier": 1, "team": "payments", "uses": ["ledger", null, "ledger"]}
	]`))

	want := []catalog.OwnedEntity{{
		Entity: catalog.Entity{
			Blueprint:  "service",
			Identifier: "payments-api",
			Title:      "Payments API",
			Properties: map[string]any{"language": "go", "tier": "1", "lifecycle": "production"},
			Links: []catalog.Link{
				link("payments-api", "dependsOn", "service", "ledger"),
				link("payments-api", "owner", "team", "payments"),
			},
		},
		Relations: []string{"dependsOn", "owner"},
	}}
	if len(problems) > 0 || !reflect.DeepEqual(entities, want) {
		t.Fatalf("Map() = %+v, %v, want %+v", entities, problems, want)
	}
}

func TestMapperFallsBackToTheIdentifierAsTitleAndDropsNullValues(t *testing.T) {
	mapper := testMapper(t, acmeSet(t), "service")

	entities, problems := mapper.Map(context.Background(), records(t, `[{"name": "ledger"}]`))

	if len(problems) > 0 || len(entities) != 1 {
		t.Fatalf("Map() = %+v, %v", entities, problems)
	}
	ledger := entities[0]
	if ledger.Title != "ledger" || !reflect.DeepEqual(ledger.Properties, map[string]any{"lifecycle": "production"}) ||
		len(ledger.Links) != 0 || !reflect.DeepEqual(ledger.Relations, []string{"dependsOn", "owner"}) {
		t.Fatalf("ledger = %+v, want the identifier as title and both relations cleared", ledger)
	}
}

func TestMapperReadsCanonicalRecordsAndReplacesOnlyTheRelationsTheyName(t *testing.T) {
	set := testSet(t, teamYAML, serviceYAML, strings.Replace(ciYAML, "  - kind: team\n    blueprint: team\n", "", 1),
		"kind: Integration\nidentifier: deploys\ntitle: Deploys\nconnector: push\nmappings:\n  - kind: service\n    blueprint: service\n")
	integration, _ := set.Integration("deploys")
	mapping, _ := integration.Mapping("service")
	mapper, err := NewMapper(set, mapping)
	if err != nil {
		t.Fatal(err)
	}

	entities, problems := mapper.Map(context.Background(), records(t, `[
		{"identifier": "payments-api", "title": "Payments API", "properties": {"tier": "2"}, "relations": {"owner": "payments"}}
	]`))

	want := []catalog.OwnedEntity{{
		Entity: catalog.Entity{
			Blueprint: "service", Identifier: "payments-api", Title: "Payments API",
			Properties: map[string]any{"tier": "2"},
			Links:      []catalog.Link{link("payments-api", "owner", "team", "payments")},
		},
		Relations: []string{"owner"},
	}}
	if len(problems) > 0 || !reflect.DeepEqual(entities, want) {
		t.Fatalf("Map() = %+v, %v, want %+v", entities, problems, want)
	}
}

func TestMapperReportsWhatTheBlueprintRefuses(t *testing.T) {
	set := acmeSet(t)
	cases := []struct {
		name   string
		kind   string
		record string
		want   Problem
	}{
		{
			name:   "identifier that is not a string",
			kind:   "service",
			record: `{"name": 42}`,
			want:   Problem{Kind: "service", Field: "identifier", Message: "not 42"},
		},
		{
			name:   "value outside the enum",
			kind:   "service",
			record: `{"name": "ledger", "tier": 5}`,
			want:   Problem{Kind: "service", Identifier: "ledger", Field: "properties.tier", Message: "must be one of"},
		},
		{
			name:   "two targets for a relation to one",
			kind:   "service",
			record: `{"name": "ledger", "team": ["payments", "platform"]}`,
			want:   Problem{Kind: "service", Identifier: "ledger", Field: "relations.owner", Message: "Owner links to one Team, not 2"},
		},
		{
			name:   "jq expression that fails",
			kind:   "service",
			record: `{"name": "ledger", "language": 5}`,
			want:   Problem{Kind: "service", Identifier: "ledger", Field: "properties.language", Message: "the jq expression failed"},
		},
		{
			name:   "target that is not a string",
			kind:   "service",
			record: `{"name": "ledger", "uses": 7}`,
			want:   Problem{Kind: "service", Identifier: "ledger", Field: "relations.dependsOn", Message: "give target identifiers as strings, not 7"},
		},
		{
			name:   "title that is not a string",
			kind:   "service",
			record: `{"name": "ledger", "display_name": {"text": "Ledger"}}`,
			want:   Problem{Kind: "service", Identifier: "ledger", Field: "title", Message: `not {"text":"Ledger"}`},
		},
		{
			name:   "unknown relation in a canonical record",
			kind:   "team",
			record: `{"identifier": "payments", "relations": {"lead": "ledger"}}`,
			want:   Problem{Kind: "team", Identifier: "payments", Field: "relations.lead", Message: "lead is not a relation of Team"},
		},
		{
			name:   "canonical record that is not an object",
			kind:   "team",
			record: `"payments"`,
			want:   Problem{Kind: "team", Field: "record", Message: "give an object"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mapper := testMapper(t, set, tc.kind)

			entities, problems := mapper.Map(context.Background(), records(t, "["+tc.record+"]"))

			found := false
			for _, problem := range problems {
				found = found || problem.Field == tc.want.Field && problem.Identifier == tc.want.Identifier &&
					strings.Contains(problem.Message, tc.want.Message)
			}
			if len(entities) != 0 || !found {
				t.Fatalf("Map() = %+v, %+v, want no entity and %+v", entities, problems, tc.want)
			}
		})
	}
}

func TestMapperRefusesADuplicateIdentifier(t *testing.T) {
	mapper := testMapper(t, acmeSet(t), "team")

	entities, problems := mapper.Map(context.Background(), records(t, `[{"identifier": "payments"}, {"identifier": "payments"}]`))

	want := []Problem{{Kind: "team", Index: 1, Identifier: "payments", Field: "identifier", Message: "another record already has this identifier"}}
	if len(entities) != 1 || !reflect.DeepEqual(problems, want) {
		t.Fatalf("Map() = %+v, %+v, want one entity and %+v", entities, problems, want)
	}
	if got := problems[0].String(); got != "team record 2 (payments): identifier: another record already has this identifier" {
		t.Fatalf("String() = %q", got)
	}
}
