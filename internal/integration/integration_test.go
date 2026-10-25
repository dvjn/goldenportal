package integration

import (
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

const inventoryYAML = `
kind: Integration
metadata:
  name: inventory
  title: Inventory
spec:
  connector: {type: push}
  mappings:
    - blueprint: service
      items: .services[]
      filter: .archived | not
      entity:
        identifier: .id
        title: .name
        properties:
          tier: .tier
          owner: .team
`

const portalYAML = `
kind: Integration
metadata: {name: portal, title: Portal}
spec:
  connector: {type: portal}
  sync: {interval: 5m}
  mappings:
    - resource: users
      blueprint: user
      filter: .active
      entity:
        identifier: .email
        properties:
          groups: .groups
    - resource: groups
      blueprint: group
      entity:
        identifier: .name
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
	if len(spec.Mappings) != 1 || spec.Mappings[0].Entity.Properties[0].ID != "tier" || spec.Connector.Type != "push" {
		t.Fatalf("spec = %+v", spec)
	}
	portal := mustParse(t, portalYAML)
	if portal.SyncEvery().Minutes() != 5 || portal.Mappings[1].Resource != "groups" {
		t.Fatalf("portal = %+v", portal)
	}

	head := "kind: Integration\nmetadata: {name: a, title: A}\nspec:\n"
	push := head + "  connector: {type: push}\n"
	pull := head + "  connector: {type: portal}\n"
	tests := []struct{ name, source, want string }{
		{"connector", head + "  connector: {type: ftp}", "type must be one of push, portal"},
		{"config", head + "  connector: {type: push, config: {url: x}}", "takes no config"},
		{"no mappings", push, "add a mapping for each blueprint"},
		{"no entity", push + "  mappings:\n    - blueprint: s", "add the entity to make"},
		{"no identifier", push + "  mappings:\n    - blueprint: s\n      entity: {title: .a}", "add a jq expression for identifier"},
		{"bad jq", push + "  mappings:\n    - blueprint: s\n      entity: {identifier: .id |}", "identifier is not valid jq"},
		{"unquoted list", push + "  mappings:\n    - blueprint: s\n      entity:\n        identifier: .id\n        properties:\n          x: [.a]", "quote this jq expression"},
		{"sync on push", push + "  sync: {interval: 5m}\n  mappings: []", "push integrations don't sync"},
		{"short interval", pull + "  sync: {interval: 10s}\n  mappings: []", "at least 1m0s"},
		{"resource on push", push + "  mappings:\n    - resource: users\n      blueprint: s\n      entity: {identifier: .id}", "have no resources"},
		{"no resource on pull", pull + "  mappings:\n    - blueprint: s\n      entity: {identifier: .id}", "add the resource to map"},
		{"unknown resource", pull + "  mappings:\n    - resource: tokens\n      blueprint: s\n      entity: {identifier: .id}", "resource must be one of users"},
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

	service, _ := blueprint.Parse("kind: Blueprint\nmetadata: {name: service, title: Service}\nspec:\n  properties:\n    owner: {relation: team}\n    tier: {type: string, compute: '\"x\"'}")
	issues := Check(spec, map[string]blueprint.Spec{"service": service})
	if len(issues) != 1 || issues[0].Path != "spec.mappings[0].entity.properties.tier" {
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

func TestMapResources(t *testing.T) {
	spec := mustParse(t, portalYAML)
	items, err := spec.Map(map[string]any{
		"users": []any{
			map[string]any{"email": "ada@example.com", "active": true, "groups": []any{"payments"}},
			map[string]any{"email": "gone@example.com", "active": false},
		},
		"groups": []any{map[string]any{"name": "payments"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Entity.Identifier != "ada@example.com" || items[1].Blueprint != "group" {
		t.Fatalf("items = %+v", items)
	}
}
