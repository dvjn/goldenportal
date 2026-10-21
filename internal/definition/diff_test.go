package definition

import (
	"reflect"
	"strings"
	"testing"
)

func setOf(t *testing.T, documents ...string) Set {
	t.Helper()
	return NewSet(mustParse(t, source(documents...)).Documents...)
}

func TestDiffListsAddedChangedAndRemovedDocuments(t *testing.T) {
	from := setOf(t, teamYAML, serviceYAML)
	service := strings.Replace(serviceYAML, "      - typescript\n", "      - typescript\n      - rust\n", 1)
	service = strings.Replace(service, "description: A deployable service\n", "", 1)
	to := setOf(t, service, gitlabYAML)

	changes, err := Diff(from, to)
	if err != nil {
		t.Fatal(err)
	}

	want := []Change{
		{
			Kind: KindBlueprint, Identifier: "service", Title: "Service", Action: Changed,
			Fields: []FieldChange{
				{Path: "properties.language.enum", Before: []byte(`["go","typescript"]`), After: []byte(`["go","typescript","rust"]`)},
				{Path: "description", Before: []byte(`"A deployable service"`)},
			},
		},
		{Kind: KindIntegration, Identifier: "gitlab", Title: "GitLab", Action: Added},
		{Kind: KindBlueprint, Identifier: "team", Title: "Team", Action: Removed},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("Diff() =\n%+v\nwant\n%+v", changes, want)
	}
}

func TestDiffComparesListsOfObjectsByField(t *testing.T) {
	from := setOf(t, teamYAML, serviceYAML, gitlabYAML)
	to := setOf(t, teamYAML, serviceYAML, strings.Replace(gitlabYAML, "title: .name", "title: .name_with_namespace", 1))

	changes, err := Diff(from, to)
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) != 1 || len(changes[0].Fields) != 1 || changes[0].Fields[0].Path != "mappings[0].title" {
		t.Fatalf("Diff() = %+v, want one change at mappings[0].title", changes)
	}
}

func TestDiffOfEqualSetsIsEmpty(t *testing.T) {
	changes, err := Diff(setOf(t, teamYAML, serviceYAML), setOf(t, serviceYAML, teamYAML))
	if err != nil || len(changes) != 0 {
		t.Fatalf("Diff() = %+v, %v, want no changes", changes, err)
	}
}

func TestEffectsOfListWhatHappensToEntities(t *testing.T) {
	team := strings.Replace(teamYAML, "properties:", "relations:\n  members:\n    title: Members\n    target: team\n    many: true\nproperties:", 1)
	from := setOf(t, team, serviceYAML, gitlabYAML)
	narrowed := strings.Replace(team, "    many: true\n", "", 1)
	service := strings.Replace(serviceYAML, "      - typescript\n", "", 1)
	service = strings.Replace(service, "  repositoryUrl:\n    title: Repository\n    type: string\n    format: url\n", "", 1)
	service = strings.Replace(service, "    target: team\n", "    target: service\n", 1)
	to := setOf(t, narrowed, service)

	effects := EffectsOf(from, to)

	language, _ := to.Documents[0].(*Blueprint).Properties.Get("language")
	want := Effects{
		RemovedIntegrations: []string{"gitlab"},
		RemovedProperties:   []Field{{Blueprint: "service", Name: "repositoryUrl"}},
		ChangedProperties:   []ChangedProperty{{Field: Field{Blueprint: "service", Name: "language"}, Property: language}},
		RemovedRelations:    []Field{{Blueprint: "service", Name: "owner"}},
		NarrowedRelations:   []Field{{Blueprint: "team", Name: "members"}},
	}
	if !reflect.DeepEqual(effects, want) {
		t.Fatalf("EffectsOf() =\n%+v\nwant\n%+v", effects, want)
	}
	if removed := EffectsOf(from, setOf(t, team)); !reflect.DeepEqual(removed.RemovedBlueprints, []string{"service"}) {
		t.Fatalf("RemovedBlueprints = %v, want service", removed.RemovedBlueprints)
	}
}

func TestCheckValuesFollowsTheProperties(t *testing.T) {
	service, _ := setOf(t, teamYAML, serviceYAML).Blueprint("service")

	problems := service.CheckValues(map[string]any{
		"language":      "cobol",
		"tier":          nil,
		"repositoryUrl": "not a url",
		"homepage":      "https://example.test",
	})

	if len(problems) != 3 || problems["language"] == "" || problems["repositoryUrl"] == "" || problems["homepage"] == "" {
		t.Fatalf("CheckValues() = %v, want language, repositoryUrl and homepage refused", problems)
	}
	if problems := service.CheckValues(map[string]any{"language": "go", "repositoryUrl": "https://gitlab.example.test/acme/api"}); len(problems) != 0 {
		t.Fatalf("CheckValues() = %v, want none", problems)
	}
}

func TestPropertyCheckByType(t *testing.T) {
	cases := []struct {
		property Property
		value    any
		valid    bool
	}{
		{Property{Type: TypeNumber}, 3.5, true},
		{Property{Type: TypeNumber}, "3", false},
		{Property{Type: TypeBoolean}, true, true},
		{Property{Type: TypeString, Format: FormatDate}, "2026-10-22", true},
		{Property{Type: TypeString, Format: FormatDate}, "22/10/2026", false},
		{Property{Type: TypeString, Format: FormatEmail}, "alice@example.test", true},
		{Property{Type: TypeArray, Enum: []string{"a", "b"}}, []any{"a", "b"}, true},
		{Property{Type: TypeArray, Enum: []string{"a", "b"}}, []any{"c"}, false},
		{Property{Type: TypeObject}, map[string]any{"a": 1.0}, true},
	}
	for _, tc := range cases {
		if err := tc.property.Check(tc.value); (err == nil) != tc.valid {
			t.Errorf("%+v.Check(%v) = %v, want valid %t", tc.property, tc.value, err, tc.valid)
		}
	}
}
