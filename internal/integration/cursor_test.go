package integration

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

const modesYAML = `kind: Integration
metadata: {identifier: gitlab, title: GitLab}
spec:
  connector:
    type: gitlab
    config: {token: {env: GP_SECRET_GITLAB_TOKEN}, groups: [acme]}
  mappings:
    - resource: projects
      blueprint: repository
      entity: {identifier: .path_with_namespace}
    - resource: pipelines
      blueprint: pipeline
      mode: upsert
      entity: {identifier: .id, properties: {startedAt: .created_at}}
    - resource: jobs
      blueprint: job
      mode: upsert
      entity: {identifier: .id}
    - resource: deployments
      blueprint: deployment
      mode: upsert
      entity: {identifier: .id}
    - resource: deployments
      blueprint: release
      entity: {identifier: .id}
`

func TestMappingModes(t *testing.T) {
	spec := mustParse(t, modesYAML)
	if spec.Mappings[0].Mode != MappingReconcile || spec.Mappings[0].Upsert() || !spec.Mappings[1].Upsert() {
		t.Fatalf("modes = %s, %s; want reconcile by default", spec.Mappings[0].Mode, spec.Mappings[1].Mode)
	}
	for _, test := range []struct{ name, source, want string }{
		{"unknown mode", strings.Replace(modesYAML, "mode: upsert", "mode: append", 1), "mode must be reconcile or upsert"},
		{
			"mixed modes", modesYAML + "    - resource: jobs\n      blueprint: pipeline\n      entity: {identifier: .id}\n",
			"another mapping into pipeline uses mode: upsert, and an integration's mappings into one blueprint share a mode",
		},
	} {
		_, issues := Parse(test.source)
		if len(issues) != 1 || !strings.Contains(issues[0].Message, test.want) {
			t.Errorf("%s: issues = %+v, want %q", test.name, issues, test.want)
		}
	}

	retained := func(identifier string) blueprint.Spec {
		bp, issues := blueprint.Parse("kind: Blueprint\nmetadata: {identifier: " + identifier + ", title: X}\nspec:\n  properties:\n" +
			"    startedAt: {type: string, format: date-time}\n  retention: {by: startedAt, keep: 30d}\n")
		if len(issues) > 0 {
			t.Fatal(issues)
		}
		return bp
	}
	published := map[string]blueprint.Spec{"pipeline": retained("pipeline"), "repository": retained("repository")}
	issues := slices.DeleteFunc(Check(spec, published), func(i configdoc.Issue) bool { return i.Severity == configdoc.SeverityWarning })
	if len(issues) != 1 || issues[0].Path != "spec.mappings[0].mode" || issues[0].Message != "repository has retention, so its mappings must use mode: upsert" {
		t.Errorf("issues = %+v, want only the reconcile mapping into a blueprint with retention refused", issues)
	}
}

// TestSyncCursors checks a complete sync reads in full what a reconcile
// mapping reads, and everything else from where the last sync left off:
// upsert mappings, first, back only as far as their blueprints keep rows,
// when all of them have retention.
func TestSyncCursors(t *testing.T) {
	spec := mustParse(t, modesYAML)
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
		t.Error("a complete sync reads from a cursor what a reconcile mapping reads")
	}
	if _, ok := complete[gitlabDeployments]; ok {
		t.Error("a complete sync reads from a cursor what an upsert and a reconcile mapping both read")
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
