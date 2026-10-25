package blueprint

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidateEntity(t *testing.T) {
	spec := mustParse(t, serviceYAML)

	valid := Entity{
		Identifier: "group/payments-api",
		Properties: map[string]any{
			"language": "go", "stars": float64(120), "tags": []any{"pci"}, "ignored": nil,
			"owner": "payments", "dependsOn": []any{"ledger", "ledger", ""},
		},
	}
	out, problems := spec.ValidateEntity(valid)
	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
	if out.Title != "group/payments-api" {
		t.Errorf("title = %q, want the identifier", out.Title)
	}
	if _, ok := out.Properties["ignored"]; ok {
		t.Error("null property kept")
	}
	if got := out.Relations["dependsOn"]; !reflect.DeepEqual(got, []string{"ledger"}) {
		t.Errorf("dependsOn = %#v, want de-duplicated targets among the relations", got)
	}
	if _, ok := out.Properties["owner"]; ok {
		t.Error("owner should move to the relations")
	}

	tests := []struct {
		name   string
		entity Entity
		want   string
	}{
		{"missing identifier", Entity{Properties: map[string]any{"language": "go", "owner": "t"}}, "identifier is missing"},
		{"required property", Entity{Identifier: "a", Properties: map[string]any{"owner": "t"}}, "properties.language is required"},
		{"required relation", Entity{Identifier: "a", Properties: map[string]any{"language": "go"}}, "properties.owner is required"},
		{"unknown property", Entity{Identifier: "a", Properties: map[string]any{"language": "go", "x": 1.0, "owner": "t"}}, `no property "x"`},
		{"enum", Entity{Identifier: "a", Properties: map[string]any{"language": "rust", "owner": "t"}}, "properties.language must be one of"},
		{"integer", Entity{Identifier: "a", Properties: map[string]any{"language": "go", "stars": 1.5, "owner": "t"}}, "properties.stars must be a whole number"},
		{"minimum", Entity{Identifier: "a", Properties: map[string]any{"language": "go", "stars": -1.0, "owner": "t"}}, "properties.stars is less than 0"},
		{"derived", Entity{Identifier: "a", Properties: map[string]any{"language": "go", "popular": true, "owner": "t"}}, "properties.popular is worked out by the portal"},
		{"array items", Entity{Identifier: "a", Properties: map[string]any{"language": "go", "tags": []any{1.0}, "owner": "t"}}, "properties.tags item 1 must be a string"},
		{"single relation", Entity{Identifier: "a", Properties: map[string]any{"language": "go", "owner": []any{"t"}}}, "properties.owner must be a team identifier"},
		{"many relation", Entity{Identifier: "a", Properties: map[string]any{"language": "go", "owner": "t", "dependsOn": "x"}}, "properties.dependsOn must be a list"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, problems := spec.ValidateEntity(test.entity)
			if !strings.Contains(strings.Join(problems, "\n"), test.want) {
				t.Fatalf("problems = %v, want %q", problems, test.want)
			}
		})
	}
}

func TestValidateEntityFormats(t *testing.T) {
	spec := mustParse(t, `
kind: Blueprint
metadata: {name: thing, title: Thing}
spec:
  properties:
    site: {type: string, format: uri}
    born: {type: string, format: date}
    code: {type: string, pattern: '^[A-Z]{3}$', maxLength: 3}
`)
	_, problems := spec.ValidateEntity(Entity{Identifier: "a", Properties: map[string]any{
		"site": "example.com", "born": "25/10/2026", "code": "abcd",
	}})
	got := strings.Join(problems, "\n")
	for _, want := range []string{"site must be an absolute URI", "born must be a date", "code doesn't match", "code is longer than 3"} {
		if !strings.Contains(got, want) {
			t.Errorf("problems = %v, want %q", problems, want)
		}
	}
}

func TestCompute(t *testing.T) {
	spec := mustParse(t, serviceYAML+`    owners:
      type: number
      compute: .properties.dependsOn | length
    broken:
      type: string
      compute: .properties.stars
`)
	values, problems := spec.Compute(Entity{
		Identifier: "a",
		Properties: map[string]any{"stars": float64(150)},
		Relations:  map[string]any{"dependsOn": []string{"x", "y"}},
	})

	if values["popular"] != true || values["owners"] != float64(2) {
		t.Errorf("values = %#v", values)
	}
	if _, ok := values["broken"]; ok || len(problems) != 1 || !strings.Contains(problems[0], "broken must be a string") {
		t.Errorf("broken = %v, problems = %v", values["broken"], problems)
	}
}

func TestEntityWidgetVariables(t *testing.T) {
	source := `kind: Blueprint
metadata: {name: service, title: Service}
spec:
  properties:
    owner: {relation: team}
    coverage: {type: number}
  entity:
    tabs:
      overview:
        widgets:
          about: {type: markdown, text: 'About {{ $entity.title }} ({{ $entity.blueprint }}), owned by {{ $entity.owner }}, as of {{ $today-7d }}.'}
          peers: {type: stat, title: Peers, filter: {property: coverage, op: gte, value: $entity.coverage}}
`
	_, issues := Parse(source)
	if HasErrors(issues) {
		t.Fatalf("valid variables: %+v", issues)
	}
	for _, invalid := range []string{
		strings.Replace(source, "$entity.owner", "$entity.owenr", 1),
		strings.Replace(source, "$entity.coverage", "$entity.coveage", 1),
		strings.Replace(source, "  entity:", "  catalog:", 1),
		strings.Replace(source, "$today-7d", "$today-7w", 1),
	} {
		_, issues := Parse(invalid)
		if !HasErrors(issues) {
			t.Fatalf("accepted invalid variable: %s", invalid)
		}
		for _, issue := range issues {
			if issue.Line == 0 {
				t.Errorf("missing line: %+v", issue)
			}
		}
	}
}
