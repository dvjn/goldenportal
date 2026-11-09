package connection

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

const modesYAML = `kind: Connection
metadata: {identifier: gitlab, title: GitLab}
spec:
  connector:
    type: gitlab
    config: {token: {env: GP_SECRET_GITLAB_TOKEN}, groups: [acme]}
---
kind: Blueprint
metadata: {identifier: repository, title: Repository}
spec:
  sources:
    - {connection: gitlab, resource: projects, entity: {identifier: .path_with_namespace}}
---
kind: Blueprint
metadata: {identifier: pipeline, title: Pipeline}
spec:
  properties:
    startedAt: {type: string, format: date-time}
  sources:
    - {connection: gitlab, resource: pipelines, mode: upsert, entity: {identifier: .id, properties: {startedAt: .created_at}}}
---
` + jobBlueprintYAML + `---
kind: Blueprint
metadata: {identifier: deployment, title: Deployment}
spec:
  sources:
    - {connection: gitlab, resource: deployments, mode: upsert, entity: {identifier: .id}}
---
kind: Blueprint
metadata: {identifier: release, title: Release}
spec:
  sources:
    - {connection: gitlab, resource: deployments, entity: {identifier: .id}}
`

const jobBlueprintYAML = `kind: Blueprint
metadata: {identifier: job, title: Job}
spec:
  sources:
    - {connection: gitlab, resource: jobs, mode: upsert, entity: {identifier: .id}}
`

// TestSyncCursors checks a complete sync reads in full what a reconcile
// source reads, and everything else from where the last sync left off:
// upsert sources, first, back only as far as their blueprints keep rows,
// when all of them have retention.
func TestSyncCursors(t *testing.T) {
	spec := sourced(t, modesYAML)
	day := 24 * time.Hour
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	keep := func(identifier, retention string) blueprint.Spec {
		bp, _ := blueprint.Parse("kind: Blueprint\nmetadata: {identifier: " + identifier + ", title: X}\nspec:\n  properties:\n" +
			"    startedAt: {type: string, format: date-time}\n    env: {type: string}\n" + retention)
		return bp
	}
	published := map[string]blueprint.Spec{
		"pipeline": keep("pipeline", "  retention: {by: startedAt, keep: 30d, rules: [{filter: {property: env, op: eq, value: prod}, keep: 90d}]}\n"),
		"job":      keep("job", ""),
	}
	read := now.Add(-time.Hour)
	stored := map[string]map[string]time.Time{gitlabPipelines: {"1": read}, gitlabProjects: {"1": read}}
	resources := []string{gitlabProjects, gitlabPipelines, gitlabJobs, gitlabDeployments}

	complete := spec.syncCursors(ModeComplete, resources, stored, published, nil, readChanges{}, now)
	if _, ok := complete[gitlabProjects]; ok {
		t.Error("a complete sync reads from a cursor what a reconcile source reads")
	}
	if _, ok := complete[gitlabDeployments]; ok {
		t.Error("a complete sync reads from a cursor what an upsert and a reconcile source both read")
	}
	pipelines := PullRequest{Cursors: complete}
	if since, first := pipelines.From(gitlabPipelines, "1"); !since.Equal(read.Add(-cursorOverlap)) || first {
		t.Errorf("pipelines in project 1 from %s, first %v; want a minute before the last sync", since, first)
	}
	if since, first := pipelines.From(gitlabPipelines, "2"); !since.Equal(now.Add(-90*day)) || !first {
		t.Errorf("pipelines in project 2 from %s, first %v; want as far back as the longest rule keeps them", since, first)
	}
	if since, _ := pipelines.From(gitlabJobs, "1"); !since.IsZero() {
		t.Errorf("jobs from %s, want in full, since their blueprint keeps rows for ever", since)
	}

	// Published to keep pipelines longer since the last sync read them, so
	// they're read again as far back as kept.
	again := PullRequest{Cursors: spec.syncCursors(ModeComplete, resources, stored, published, map[string]time.Time{"pipeline": read.Add(time.Minute)}, readChanges{}, now)}
	if since, first := again.From(gitlabPipelines, "1"); !since.Equal(now.Add(-90*day)) || !first {
		t.Errorf("pipelines kept longer since read from %s, first %v; want as far back as kept", since, first)
	}
	earlier := PullRequest{Cursors: spec.syncCursors(ModeComplete, resources, stored, published, map[string]time.Time{"pipeline": read.Add(-time.Minute)}, readChanges{}, now)}
	if since, _ := earlier.From(gitlabPipelines, "1"); !since.Equal(read.Add(-cursorOverlap)) {
		t.Errorf("pipelines kept longer before the last sync read from %s, want its cursor", since)
	}

	incremental := PullRequest{Cursors: spec.syncCursors(ModeIncremental, resources, stored, published, nil, readChanges{}, now)}
	if since, _ := incremental.From(gitlabProjects, "1"); !since.Equal(read.Add(-cursorOverlap)) {
		t.Errorf("an incremental sync reads projects from %s, want a minute before the last sync", since)
	}
	if since, _ := incremental.From(gitlabProjects, "2"); !since.IsZero() {
		t.Errorf("an incremental sync reads projects of a new scope from %s, want in full", since)
	}
}

// TestReadLog checks a sync advances the cursor of each scope it read
// without failing, and says the earliest it read each resource from, but
// for what it read in full somewhere.
func TestReadLog(t *testing.T) {
	read := newReadLog()
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	request := PullRequest{read: read, Cursors: map[string]Cursor{
		gitlabPipelines: {Scopes: map[string]time.Time{"1": at, "2": at.Add(time.Hour)}},
		gitlabJobs:      {Scopes: map[string]time.Time{"1": at}},
	}}
	for _, scope := range []string{"1", "2", "3"} {
		request.From(gitlabPipelines, scope)
	}
	request.From(gitlabJobs, "1")
	request.From(gitlabMergeRequests, "1")
	request.From(gitlabMergeRequests, "2")
	report := Report{
		ScopeFailures: []ScopeFailure{{Resource: gitlabPipelines, Scope: "2"}},
		Failed:        map[string]error{gitlabMergeRequests: errors.New("refused")},
	}
	var keys []string
	for _, k := range read.readWithout(report) {
		keys = append(keys, k.resource+"/"+k.scope)
	}
	if got := strings.Join(keys, ","); got != "jobs/1,pipelines/1,pipelines/3" {
		t.Errorf("advanced %s", got)
	}
	since := read.readSince()
	if len(since) != 1 || !since[gitlabJobs].Equal(at) {
		t.Errorf("read since %v, want jobs only, as project 3's pipelines were read in full", since)
	}
}
