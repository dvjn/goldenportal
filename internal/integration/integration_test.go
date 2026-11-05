package integration

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

const inventoryYAML = `
kind: Integration
metadata:
  name: inventory
  title: Inventory
spec:
  connector:
    type: http
    config:
      resources:
        services:
          push:
            items: .body.services[]
            deleted: .query.deleted == "true"
  mappings:
    - resource: services
      blueprint: service
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
	if len(spec.Mappings) != 1 || spec.Mappings[0].Entity.Properties[0].ID != "tier" || spec.Icon != "globe" ||
		spec.Pulls || !spec.Receives || !spec.Checks {
		t.Fatalf("spec = %+v", spec)
	}
	portal := mustParse(t, portalYAML)
	if portal.SyncEvery().Minutes() != 5 || portal.Mappings[1].Resource != "groups" || !portal.Pulls || portal.Receives {
		t.Fatalf("portal = %+v", portal)
	}

	head := "kind: Integration\nmetadata: {name: a, title: A}\nspec:\n"
	push := head + "  connector:\n    type: http\n    config: {resources: {s: {push: {items: .body}}}}\n"
	pull := head + "  connector: {type: portal}\n"
	fetch := head + "  connector:\n    type: http\n    config: {resources: {s: {url: https://x.example.com}}}\n"
	tests := []struct{ name, source, want string }{
		{"own icon", "kind: Integration\nmetadata: {name: a, title: A, icon: plug}\nspec:\n  connector: {type: portal}", "icon comes from its connector"},
		{"connector", head + "  connector: {type: push}", "type must be one of portal, http"},
		{"config", head + "  connector: {type: portal, config: {url: x}}", "takes no config"},
		{"no mappings", push, "add a mapping for each blueprint"},
		{"no entity", push + "  mappings:\n    - {resource: s, blueprint: s}", "add the entity to make"},
		{"no identifier", push + "  mappings:\n    - resource: s\n      blueprint: s\n      entity: {title: .a}", "add a jq expression for identifier"},
		{"bad jq", push + "  mappings:\n    - resource: s\n      blueprint: s\n      entity: {identifier: .id |}", "identifier is not valid jq"},
		{"unquoted list", push + "  mappings:\n    - resource: s\n      blueprint: s\n      entity:\n        identifier: .id\n        properties:\n          x: [.a]", "quote this jq expression"},
		{"items on a mapping", push + "  mappings:\n    - resource: s\n      blueprint: s\n      items: .a\n      entity: {identifier: .id}", "items"},
		{"sync without pulling", push + "  sync: {interval: 5m}\n  mappings: []", "only integrations that pull sync"},
		{"short interval", pull + "  sync: {interval: 10s}\n  mappings: []", "interval must be at least 1m0s"},
		{"complete under interval", fetch + "  sync: {interval: 1h, complete: 5m}\n  mappings: []", "complete must be at least the interval"},
		{"complete without since", fetch + "  sync: {interval: 5m, complete: 1h}\n  mappings: []", "no resource url uses {{ $since }}"},
		{"complete of a connector that reads everything", pull + "  sync: {interval: 5m, complete: 1h}\n  mappings: []", "the portal connector reads everything on every sync, so every sync is complete and complete is ignored"},
		{"no resource", pull + "  mappings:\n    - blueprint: s\n      entity: {identifier: .id}", "add the resource to map"},
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

func mustMap(t *testing.T, spec Spec, items ...Item) []Entry {
	t.Helper()
	m, err := spec.mapper()
	if err != nil {
		t.Fatal(err)
	}

	return m.Map(items)
}

func TestMap(t *testing.T) {
	spec := mustParse(t, inventoryYAML)
	entries := mustMap(t, spec,
		Item{Resource: "services", Object: map[string]any{"id": float64(42), "name": "Indexer", "tier": "standard", "team": "search"}},
		Item{Resource: "services", Object: map[string]any{"id": "old", "archived": true}},
		Item{Resource: "services", Object: map[string]any{"id": "gone", "archived": true}, Deleted: true},
		Item{Resource: "other", Object: map[string]any{"id": "x"}},
	)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want the archived one filtered out, the deletion kept and the other resource unmapped", entries)
	}
	got := entries[0]
	if got.Blueprint != "service" || got.Entity.Identifier != "42" || got.Entity.Title != "Indexer" ||
		got.Entity.Properties["tier"] != "standard" || got.Entity.Properties["owner"] != "search" {
		t.Fatalf("entry = %+v", got)
	}
	if deleted := entries[1]; !deleted.Deleted || deleted.Entity.Identifier != "gone" || len(deleted.Entity.Properties) != 0 {
		t.Fatalf("deletion = %+v", deleted)
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

	pulled := mustParse(t, strings.Replace(inventoryYAML, "        services:\n          push:", "        services:\n          url: https://inventory.example.com\n          push:", 1))
	if changes := Diff(before, pulled); len(changes) != 2 || changes[1].Detail != "starts syncing on a schedule" || changes[1].Breaking {
		t.Fatalf("starts pulling: changes = %+v", changes)
	}
	pullOnly := mustParse(t, strings.Replace(portalYAML, "name: portal", "name: inventory", 1))
	if changes := Diff(before, pullOnly); !slices.ContainsFunc(changes, func(c configdoc.Change) bool {
		return c.Breaking && c.Detail == "stops taking pushes, so systems pushing to it are refused"
	}) {
		t.Fatalf("stops taking pushes: changes = %+v", changes)
	}
}

func TestMapResources(t *testing.T) {
	spec := mustParse(t, portalYAML)
	entries := mustMap(t, spec,
		Item{Resource: "users", Object: map[string]any{"email": "ada@example.com", "active": true, "groups": []any{"payments"}}},
		Item{Resource: "users", Object: map[string]any{"email": "gone@example.com", "active": false}},
		Item{Resource: "groups", Object: map[string]any{"name": "payments"}},
	)
	if len(entries) != 2 || entries[0].Entity.Identifier != "ada@example.com" || entries[1].Blueprint != "group" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestUsualMode(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	daily := mustParse(t, strings.Replace(httpYAML("https://tracker.example.com?since={{ $since }}"), "sync: {interval: 5m}", "sync: {interval: 5m, complete: 24h}", 1))
	published := now.Add(-48 * time.Hour)
	tests := []struct {
		name    string
		spec    Spec
		history syncHistory
		want    string
	}{
		{"always complete without a complete interval", mustParse(t, portalYAML), syncHistory{lastComplete: now.Add(-time.Minute), published: published}, ModeComplete},
		{"complete first", daily, syncHistory{published: published}, ModeComplete},
		{"incremental between", daily, syncHistory{lastComplete: now.Add(-time.Hour), published: published}, ModeIncremental},
		{"complete once the interval passes", daily, syncHistory{lastComplete: now.Add(-24 * time.Hour), published: published}, ModeComplete},
		{"complete after a publish", daily, syncHistory{lastComplete: now.Add(-time.Hour), published: now.Add(-time.Minute)}, ModeComplete},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.spec.usualMode(test.history, now); got != test.want {
				t.Fatalf("mode = %s, want %s", got, test.want)
			}
		})
	}
}
