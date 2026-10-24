package integration

import (
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

const inventoryYAML = `
identifier: inventory
title: Inventory
connector: push
mappings:
  - blueprint: service
    items: .services[]
    filter: .archived | not
    identifier: .id
    title: .name
    properties:
      tier: .tier
      owner: .team
`

func mustParse(t *testing.T, source string) Spec {
	t.Helper()
	spec, issues := Parse(source)
	if configdoc.HasErrors(issues) {
		t.Fatalf("Parse() issues = %+v", issues)
	}

	return spec
}

func TestParse(t *testing.T) {
	spec := mustParse(t, inventoryYAML)
	if len(spec.Mappings) != 1 || spec.Mappings[0].Properties[0].ID != "tier" {
		t.Fatalf("spec = %+v", spec)
	}

	tests := []struct{ name, source, want string }{
		{"connector", "identifier: a\ntitle: A\nconnector: ftp", "connector must be one of push"},
		{"no mappings", "identifier: a\ntitle: A\nconnector: push", "add a mapping for each blueprint"},
		{"no identifier", "identifier: a\ntitle: A\nconnector: push\nmappings:\n  - blueprint: s", "add a jq expression for identifier"},
		{"bad jq", "identifier: a\ntitle: A\nconnector: push\nmappings:\n  - blueprint: s\n    identifier: .id |", "identifier is not valid jq"},
		{"bad field", "identifier: a\ntitle: A\nconnector: push\nmappings:\n  - blueprint: s\n    identifier: .id\n    properties:\n      x: '.['", "x is not valid jq"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := Parse(test.source)
			var messages []string
			for _, issue := range issues {
				messages = append(messages, issue.Message)
			}
			if !strings.Contains(strings.Join(messages, "\n"), test.want) {
				t.Fatalf("issues = %v, want %q", messages, test.want)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	spec := mustParse(t, inventoryYAML)
	if issues := Check(spec, nil); len(issues) != 1 || issues[0].Severity != configdoc.SeverityWarning {
		t.Fatalf("unpublished blueprint: issues = %+v", issues)
	}

	service, _ := blueprint.Parse("identifier: service\ntitle: Service\nproperties:\n  owner: {relation: team}\n  tier: {type: string, compute: '\"x\"'}")
	issues := Check(spec, map[string]blueprint.Spec{"service": service})
	if len(issues) != 1 || issues[0].Path != "mappings[0].properties.tier" {
		t.Fatalf("computed property: issues = %+v", issues)
	}
}

func TestMap(t *testing.T) {
	spec := mustParse(t, inventoryYAML)
	items, err := spec.Map(map[string]any{"services": []any{
		map[string]any{"id": float64(42), "name": "Indexer", "tier": "standard", "team": "search"},
		map[string]any{"id": "old", "archived": true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v, want the archived one filtered out", items)
	}
	got := items[0]
	if got.Blueprint != "service" || got.Entity.Identifier != "42" || got.Entity.Title != "Indexer" ||
		got.Entity.Properties["tier"] != "standard" || got.Entity.Properties["owner"] != "search" {
		t.Fatalf("item = %+v", got)
	}
}

func TestDiff(t *testing.T) {
	before := mustParse(t, inventoryYAML)
	if changes := Diff(before, before); len(changes) != 0 {
		t.Fatalf("no change: %+v", changes)
	}

	after := mustParse(t, strings.Replace(inventoryYAML, "tier: .tier", "tier: .level", 1))
	changes := Diff(before, after)
	if len(changes) != 1 || changes[0].Change != configdoc.ChangeChanged || changes[0].Breaking {
		t.Fatalf("changes = %+v", changes)
	}

}
