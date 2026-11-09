package connection

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

const inventoryYAML = `
kind: Connection
metadata:
  identifier: inventory
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
`

const serviceYAML = `
kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    tier: {type: string}
    owner: {relation: team}
  sources:
    - connection: inventory
      resource: services
      filter: .archived | not
      entity:
        identifier: .id
        title: .name
        properties:
          tier: .tier
          owner: .team
`

const portalYAML = `
kind: Connection
metadata: {identifier: portal, title: Portal}
spec:
  connector: {type: portal}
  sync: {interval: 5m}
`

var portalBlueprintYAMLs = []string{`
kind: Blueprint
metadata: {identifier: user, title: User}
spec:
  properties:
    groups: {relation: group, many: true}
  sources:
    - connection: portal
      resource: users
      filter: .active
      entity:
        identifier: .email
        properties:
          groups: .groups
`, `
kind: Blueprint
metadata: {identifier: group, title: Group}
spec:
  sources:
    - connection: portal
      resource: groups
      entity:
        identifier: .name
`}

func mustParse(t *testing.T, source string) Spec {
	t.Helper()
	spec, issues := Parse(source)
	if configdoc.HasErrors(issues) {
		t.Fatalf("Parse() issues = %+v", issues)
	}

	return spec
}

func TestParse(t *testing.T) {
	spec := withSources(t, inventoryYAML, serviceYAML)
	if len(spec.sources) != 1 || spec.sources[0].Entity.Properties[0].ID != "tier" || spec.Icon != "globe" ||
		spec.Pulls || !spec.Receives || !spec.Checks {
		t.Fatalf("spec = %+v", spec)
	}
	portal := withSources(t, portalYAML, portalBlueprintYAMLs...)
	if portal.SyncEvery().Minutes() != 5 || portal.sources[1].Resource != "users" || !portal.Pulls || portal.Receives {
		t.Fatalf("portal = %+v", portal)
	}

	head := "kind: Connection\nmetadata: {identifier: a, title: A}\nspec:\n"
	push := head + "  connector:\n    type: http\n    config: {resources: {s: {push: {items: .body}}}}\n"
	pull := head + "  connector: {type: portal}\n"
	fetch := head + "  connector:\n    type: http\n    config: {resources: {s: {url: https://x.example.com}}}\n"
	tests := []struct{ name, source, want string }{
		{"own icon", "kind: Connection\nmetadata: {identifier: a, title: A, icon: plug}\nspec:\n  connector: {type: portal}", "icon comes from its connector"},
		{"connector", head + "  connector: {type: push}", "type must be one of portal, http"},
		{"config", head + "  connector: {type: portal, config: {url: x}}", "takes no config"},
		{"sync without pulling", push + "  sync: {interval: 5m}", "only connections that pull sync"},
		{"short interval", pull + "  sync: {interval: 10s}", "interval must be at least 1m"},
		{"compound interval", pull + "  sync: {interval: 1h30m}", "interval must be a duration, such as 5m or 1h"},
		{"complete under interval", fetch + "  sync: {interval: 1h, complete: 5m}", "complete must be at least the interval"},
		{"complete without since", fetch + "  sync: {interval: 5m, complete: 1h}", "no resource url uses {{ $since }}"},
		{"mappings", pull + "  mappings:\n    - {resource: users, blueprint: user, entity: {identifier: .email}}", "mappings moved to each blueprint's sources, such as sources on user"},
		{"complete of a connector that reads everything", pull + "  sync: {interval: 5m, complete: 1h}", "the portal connector reads everything on every sync, so every sync is complete and complete is ignored"},
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

func mustMap(t *testing.T, spec Spec, items ...Item) []Entry {
	t.Helper()
	m, err := spec.mapper()
	if err != nil {
		t.Fatal(err)
	}

	return m.Map(context.Background(), items)
}

func TestMap(t *testing.T) {
	spec := withSources(t, inventoryYAML, serviceYAML)
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

func TestMapStopsWhenCancelled(t *testing.T) {
	spec := withSources(t, inventoryYAML, serviceYAML)
	m, err := spec.mapper()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	entries := m.Map(ctx, []Item{{Resource: "services", Object: map[string]any{"id": float64(1), "name": "A"}}})
	if len(entries) != 1 || len(entries[0].Problems) == 0 {
		t.Fatalf("entries = %+v, want the item reported as a problem, not mapped", entries)
	}
}

func TestDiff(t *testing.T) {
	before := mustParse(t, inventoryYAML)
	if changes := Diff(before, before); len(changes) != 0 {
		t.Fatalf("no change: %+v", changes)
	}

	after := mustParse(t, strings.Replace(inventoryYAML, `.query.deleted == "true"`, ".body.deleted", 1))
	changes := Diff(before, after)
	if len(changes) != 1 || changes[0].Detail != "changes how inventory reads services; the next sync re-reads them" || changes[0].Breaking {
		t.Fatalf("changes = %+v", changes)
	}

	gitlab := strings.Split(modesYAML, "---\n")[0]
	wider := Diff(mustParse(t, gitlab), mustParse(t, strings.Replace(gitlab, "groups: [acme]", "groups: [acme, beta]", 1)))
	if len(wider) != 1 || wider[0].Detail != "changes what gitlab reads; the next sync re-reads everything it reads, completely" {
		t.Fatalf("gitlab config: changes = %+v", wider)
	}

	pulled := mustParse(t, strings.Replace(inventoryYAML, "        services:\n          push:", "        services:\n          url: https://inventory.example.com\n          push:", 1))
	if changes := Diff(before, pulled); len(changes) != 2 || changes[0].Detail != "starts syncing on a schedule" || changes[0].Breaking {
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
	spec := withSources(t, portalYAML, portalBlueprintYAMLs...)
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
	changes := readChanges{complete: now.Add(-48 * time.Hour)}
	tests := []struct {
		name    string
		spec    Spec
		history syncHistory
		want    string
	}{
		{"always complete without a complete interval", mustParse(t, portalYAML), syncHistory{lastComplete: now.Add(-time.Minute), changes: changes}, ModeComplete},
		{"complete first", daily, syncHistory{changes: changes}, ModeComplete},
		{"incremental between", daily, syncHistory{lastComplete: now.Add(-time.Hour), changes: changes}, ModeIncremental},
		{"complete once the interval passes", daily, syncHistory{lastComplete: now.Add(-24 * time.Hour), changes: changes}, ModeComplete},
		{"complete after a publish that reads a reconcile source differently", daily, syncHistory{lastComplete: now.Add(-time.Hour), changes: readChanges{complete: now.Add(-time.Minute)}}, ModeComplete},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.spec.usualMode(test.history, now); got != test.want {
				t.Fatalf("mode = %s, want %s", got, test.want)
			}
		})
	}
}
