package connection

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

const sourcedYAML = `kind: Connection
metadata: {identifier: tracker, title: Tracker}
spec:
  connector:
    type: http
    config:
      resources:
        services: {url: https://tracker.example.com/services}
        teams: {url: https://tracker.example.com/teams}
`

const serviceSourcesYAML = `kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    tier: {type: string}
  sources:
    - connection: tracker
      resource: services
      mode: upsert
      entity: {identifier: .id, title: .name, properties: {tier: .tier}}
    - connection: other
      resource: teams
      entity: {identifier: .id}
`

const teamSourcesYAML = `kind: Blueprint
metadata: {identifier: team, title: Team}
spec:
  sources:
    - connection: tracker
      resource: teams
      entity: {identifier: .id}
`

// TestSourcesRead checks a connection reads what the
// blueprints' sources naming it map, and only those.
func TestSourcesRead(t *testing.T) {
	spec := withSources(t, sourcedYAML, serviceSourcesYAML, teamSourcesYAML)
	if got := spec.mappedResources(); !slices.Equal(got, []string{"services", "teams"}) {
		t.Fatalf("mappedResources() = %v", got)
	}
	m, err := spec.mapper()
	if err != nil {
		t.Fatal(err)
	}
	entries := m.Map(context.Background(), []Item{
		{Resource: "services", Object: map[string]any{"id": "api", "name": "API", "tier": "1"}},
		{Resource: "teams", Object: map[string]any{"id": "core"}},
	})
	if len(entries) != 2 || entries[0].Blueprint != "service" || entries[0].Entity.Properties["tier"] != "1" ||
		entries[1].Blueprint != "team" {
		t.Fatalf("entries = %+v", entries)
	}
	if got := spec.reconcilable([]string{"services", "teams"}); !slices.Equal(got, []string{"team"}) {
		t.Errorf("reconcilable() = %v, want only the reconcile source's blueprint", got)
	}
	if !spec.reconciles("teams") || spec.reconciles("services") {
		t.Error("reconciles() should follow the sources' modes")
	}
}

// TestPublishSources checks a blueprint version resets only the resources
// whose sources from the connection it adds or changes, and forces a
// complete sync only for a reconcile one.
func TestPublishSources(t *testing.T) {
	first := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	at := first.Add(time.Hour)
	upsert := blueprint.Source{
		Connection: "tracker", Resource: "services", Mode: blueprint.SourceUpsert,
		Entity: blueprint.SourceEntity{Identifier: ".id"},
	}
	reconcile := blueprint.Source{
		Connection: "tracker", Resource: "teams", Mode: blueprint.SourceReconcile,
		Entity: blueprint.SourceEntity{Identifier: ".id"},
	}
	changed := upsert
	changed.Filter = ".active"
	for _, test := range []struct {
		name           string
		previous, next []blueprint.Source
		reset          []string
		complete       bool
	}{
		{"first version", nil, []blueprint.Source{upsert, reconcile}, []string{"services", "teams"}, true},
		{"unchanged", []blueprint.Source{upsert}, []blueprint.Source{upsert}, nil, false},
		{"an upsert source changed", []blueprint.Source{upsert}, []blueprint.Source{changed}, []string{"services"}, false},
		{"a reconcile source added", []blueprint.Source{upsert}, []blueprint.Source{upsert, reconcile}, []string{"teams"}, true},
		{"a source removed", []blueprint.Source{upsert, reconcile}, []blueprint.Source{upsert}, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			changes := readChanges{all: first, complete: first}
			changes.publishSources(test.previous, test.next, at)
			var reset []string
			for _, resource := range []string{"services", "teams"} {
				if changes.since(resource).Equal(at) {
					reset = append(reset, resource)
				}
			}
			if !slices.Equal(reset, test.reset) {
				t.Errorf("reset %v, want %v", reset, test.reset)
			}
			if complete := changes.complete.Equal(at); complete != test.complete {
				t.Errorf("forces a complete sync %v, want %v", complete, test.complete)
			}
		})
	}
}

// TestCheckSourcesOfConnection checks a connection whose connector stops
// offering what a blueprint's source reads is refused.
func TestCheckSourcesOfConnection(t *testing.T) {
	spec := mustParse(t, sourcedYAML)
	team, _ := blueprint.Parse(teamSourcesYAML)
	if issues := checkSources(spec, map[string]blueprint.Spec{"team": team}); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
	team.Sources[0].Resource = "people"
	issues := checkSources(spec, map[string]blueprint.Spec{"team": team})
	if len(issues) != 1 || issues[0].Message != "team's sources read people, which the http connector doesn't offer" {
		t.Fatalf("issues = %+v", issues)
	}
}
