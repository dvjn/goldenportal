package connection

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

const twoURLsYAML = `kind: Connection
metadata: {identifier: tracker, title: Tracker}
spec:
  connector:
    type: http
    config:
      resources:
        services: {url: "https://tracker.example.com/services?since={{ $since }}"}
        teams: {url: https://tracker.example.com/teams}
---
kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    owner: {relation: team}
    tier: {type: string}
  sources:
    - {connection: tracker, resource: services, mode: upsert, entity: {identifier: .id, properties: {owner: .team, tier: .tier}}}
---
kind: Blueprint
metadata: {identifier: team, title: Team}
spec:
  sources:
    - {connection: tracker, resource: teams, entity: {identifier: .id}}
`

// TestReadChanges checks a publish resets the cursors only of the resources
// it reads differently, and forces a complete sync only when it reads
// differently what a reconcile source reads. Edits to the blueprints after
// the connection publish as their next versions.
func TestReadChanges(t *testing.T) {
	first := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	at := first.Add(time.Hour)
	signed := httpYAML("https://tracker.example.com")
	jobs := "---\n" + jobBlueprintYAML
	for _, test := range []struct {
		name, before, old, new string
		reset                  []string
		complete               bool
	}{
		{"a schedule", modesYAML, "groups: [acme]}\n", "groups: [acme]}\n  sync: {interval: 1h, complete: 24h}\n", nil, false},
		{"a title and description", modesYAML, "title: GitLab}", "title: Git, description: Code}", nil, false},
		{"an upsert source changed", modesYAML, "startedAt: .created_at", "startedAt: .started_at", []string{gitlabPipelines}, false},
		{"a reconcile source changed", modesYAML, "{identifier: .path_with_namespace}", "{identifier: .path_with_namespace, title: .name}", []string{gitlabProjects}, true},
		{"a source's filter", modesYAML, "resource: jobs, mode", "resource: jobs, filter: '.ref == \"main\"', mode", []string{gitlabJobs}, false},
		{"a source added", modesYAML, jobs, jobs + strings.ReplaceAll(jobs, "job", "merge_request"), []string{gitlabMergeRequests}, false},
		{"a source removed", modesYAML, jobs, "", nil, false},
		{"the connector config", modesYAML, "groups: [acme]", "groups: [acme, beta]", []string{gitlabProjects, gitlabPipelines, gitlabJobs, gitlabDeployments}, true},
		{"properties reordered", twoURLsYAML, "owner: .team, tier: .tier", "tier: .tier, owner: .team", nil, false},
		{"an upsert resource's url", twoURLsYAML, "/services?", "/services/all?", []string{"services"}, false},
		{"a reconcile resource's url", twoURLsYAML, "/teams", "/teams/all", []string{"teams"}, true},
		{"how pushes are signed", signed, "prefix: sha256=", "prefix: v1=", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			edited := strings.Replace(test.before, test.old, test.new, 1)
			if edited == test.before {
				t.Fatalf("%q isn't in the document", test.old)
			}
			before, after := sourced(t, test.before), sourced(t, edited)
			var changes readChanges
			publishAll(&changes, nil, before, first)
			publishAll(&changes, &before, after, at)
			var reset []string
			for _, resource := range append(after.mappedResources(), before.mappedResources()...) {
				if changes.since(resource).Equal(at) && !slices.Contains(reset, resource) {
					reset = append(reset, resource)
				}
			}
			slices.Sort(reset)
			want := slices.Sorted(slices.Values(test.reset))
			if !slices.Equal(reset, want) {
				t.Errorf("reset %v, want %v", reset, want)
			}
			if complete := changes.complete.Equal(at); complete != test.complete {
				t.Errorf("forces a complete sync %v, want %v", complete, test.complete)
			}
		})
	}
}

// TestReadChangesKeepCursors checks a publish that reads nothing differently
// leaves a sync reading from where the last left off, and one that does
// reads only what it changed afresh.
func TestReadChangesKeepCursors(t *testing.T) {
	before := sourced(t, modesYAML)
	after := sourced(t, strings.Replace(modesYAML, "startedAt: .created_at", "startedAt: .started_at", 1))
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	read := now.Add(-2 * time.Hour)
	var changes readChanges
	publishAll(&changes, nil, before, now.Add(-3*time.Hour))
	publishAll(&changes, &before, before, now.Add(-time.Hour))
	publishAll(&changes, &before, after, now.Add(-time.Hour))
	stored := map[string]map[string]time.Time{gitlabPipelines: {"1": read}, gitlabJobs: {"1": read}}
	request := PullRequest{Cursors: after.syncCursors(ModeIncremental, []string{gitlabPipelines, gitlabJobs}, stored, nil, nil, changes, now)}
	if since, _ := request.From(gitlabJobs, "1"); !since.Equal(read.Add(-cursorOverlap)) {
		t.Errorf("jobs from %s, want from their cursor", since)
	}
	if since, _ := request.From(gitlabPipelines, "1"); !since.IsZero() {
		t.Errorf("pipelines from %s, want in full, as their source changed since they were read", since)
	}
}

// publishAll notes next published at at over previous, the connection and
// then each blueprint whose sources it reads, as next versions of both.
func publishAll(changes *readChanges, previous *Spec, next Spec, at time.Time) {
	changes.publish(previous, next, at)
	bySource := func(spec *Spec) map[string][]blueprint.Source {
		sources := map[string][]blueprint.Source{}
		if spec != nil {
			for _, m := range spec.sources {
				sources[m.Blueprint] = append(sources[m.Blueprint], m.Source)
			}
		}
		return sources
	}
	was := bySource(previous)
	for id, sources := range bySource(&next) {
		changes.publishSources(was[id], sources, at)
	}
}
