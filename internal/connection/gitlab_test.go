package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

// gitlabYAML reads the acme group and the octo/tools project; the sources
// in gitlabBlueprints map each resource.
const gitlabYAML = `
kind: Connection
metadata: {identifier: gitlab, title: GitLab}
spec:
  connector:
    type: gitlab
    config:
      url: https://gitlab.example.com
      token: {env: GP_SECRET_GITLAB_TOKEN}
      groups: [acme]
      projects: [octo/tools]
  sync: {interval: 1h}
`

// gitlabBlueprints are blueprints with a source for each resource, from
// the groups and members ones only when withGroups.
func gitlabBlueprints(withGroups bool) []string {
	var out []string
	for _, b := range []struct{ resource, blueprint, entity, properties string }{
		{"groups", "group", "{identifier: .full_path}", ""},
		{"projects", "project", `
        identifier: .path_with_namespace
        properties:
          ci: ._gp.latestPipeline.status
          protected: ._gp.defaultBranch.protected
          ciValid: ._gp.ciConfig.valid`, "ci protected ciValid"},
		{"tags", "tag", `{identifier: ._gp.project + "@" + .name}`, ""},
		{"merge_requests", "merge-request", `{identifier: ._gp.project + "!" + (.iid | tostring)}`, ""},
		{"issues", "issue", `{identifier: ._gp.project + "#" + (.iid | tostring)}`, ""},
		{"releases", "release", `{identifier: ._gp.project + "@" + .tag_name}`, ""},
		{"pipelines", "pipeline", "{identifier: .id}", ""},
		{"jobs", "job", "{identifier: .id}", ""},
		{"job_attempts", "job-attempt", "{identifier: .id}", ""},
		{"runners", "runner", "{identifier: .id}", ""},
		{"deployments", "deployment", "{identifier: .id}", ""},
		{"environments", "environment", `{identifier: ._gp.project + "/" + .name}`, ""},
		{"members", "member", `{identifier: ._gp.group + "/" + .username}`, ""},
	} {
		if !withGroups && (b.resource == "groups" || b.resource == "members") {
			continue
		}
		properties := "  properties: {}\n"
		if b.properties != "" {
			properties = "  properties:\n"
			for _, p := range strings.Fields(b.properties) {
				properties += "    " + p + ": {type: string}\n"
			}
		}
		out = append(out, "kind: Blueprint\nmetadata: {identifier: "+b.blueprint+", title: X}\nspec:\n"+properties+
			"  sources:\n    - connection: gitlab\n      resource: "+b.resource+"\n      entity: "+b.entity+"\n")
	}

	return out
}

// gitlabProjectYAML reads one project on its own.
func gitlabProjectYAML(project string) string {
	return strings.Replace(strings.Replace(gitlabYAML, "      groups: [acme]\n", "", 1), "[octo/tools]", "["+project+"]", 1)
}

// gitlabSpec parses a connection that reads the sources of the blueprints,
// gitlabBlueprints when none are given.
func gitlabSpec(t *testing.T, source, instance string, blueprints ...string) (Spec, *GitLabConfig) {
	t.Helper()
	if len(blueprints) == 0 {
		blueprints = gitlabBlueprints(strings.Contains(source, "groups: ["))
	}
	spec := withSources(t, strings.ReplaceAll(source, "https://gitlab.example.com", instance), blueprints...)
	config, ok := spec.config.(*GitLabConfig)
	if !ok {
		t.Fatalf("config = %T, want the gitlab connector's", spec.config)
	}

	return spec, config
}

// gitlabMapped maps items all at once, as a push does, and renders each
// entry as its blueprint and identifier, marking deletions and problems.
func gitlabMapped(t *testing.T, spec Spec, items []Item) ([]Entry, string) {
	t.Helper()
	entries := mustMap(t, spec, items...)
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Blueprint + " " + e.Entity.Identifier
		if e.Deleted {
			out[i] += " deleted"
		}
		if len(e.Problems) > 0 {
			out[i] += fmt.Sprint(e.Problems)
		}
	}

	return entries, strings.Join(out, ", ")
}

func pullGitLab(t *testing.T, config *GitLabConfig, resources ...string) (map[string][]map[string]any, map[string]error) {
	t.Helper()
	return pullGitLabAs(t, config, PullRequest{Resources: resources})
}

func pullGitLabAs(t *testing.T, config *GitLabConfig, request PullRequest) (map[string][]map[string]any, map[string]error) {
	t.Helper()
	if len(request.Resources) == 0 {
		request.Resources = gitlabResources
	}
	if request.Warn == nil {
		request.Warn = func(string) {}
	}
	pulled := map[string][]map[string]any{}
	report, err := config.Pull(t.Context(), nil, request, func(resource string, items []Item) error {
		for _, item := range items {
			if item.Resource != resource || item.Deleted {
				t.Errorf("emitted %+v as %s", item, resource)
			}
			pulled[resource] = append(pulled[resource], item.Object.(map[string]any))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return pulled, failedByResource(report, request.Resources)
}

// failedByResource is each resource's failures, whole or in a scope, as a
// run reports them, with a failed listing of scopes failing every resource
// read from scopes.
func failedByResource(report Report, resources []string) map[string]error {
	errs := map[string][]error{}
	for resource, err := range report.Failed {
		errs[resource] = append(errs[resource], err)
	}
	for _, f := range report.ScopeFailures {
		errs[f.Resource] = append(errs[f.Resource], f.Err)
	}
	for _, err := range report.Unlisted {
		for _, resource := range resources {
			if slices.Contains(gitlabProjectResources, resource) {
				errs[resource] = append(errs[resource], err)
			}
		}
	}
	failed := map[string]error{}
	for resource, list := range errs {
		failed[resource] = list[0]
		if len(list) > 1 {
			failed[resource] = fmt.Errorf("%w, and %d more failed", list[0], len(list)-1)
		}
	}

	return failed
}

func gitlabSorted(list string) string {
	values := strings.Split(list, ",")
	slices.Sort(values)
	return strings.Join(values, ",")
}

// gitlabField lists a field of each object, as text, sorted, since projects
// are read a few at a time.
func gitlabField(objects []map[string]any, path ...string) string {
	var out []string
	for _, o := range objects {
		value := valueAt(o, path...)
		if n, ok := value.(float64); ok {
			value = idOf(n)
		}
		text, _ := value.(string)
		out = append(out, text)
	}
	slices.Sort(out)

	return strings.Join(out, ",")
}

const gitlabIngestURL = "https://portal.example.com/api/ingest/gitlab"

// receiveGitLab hands config a webhook event with the secret token,
// asking for every resource it receives.
func receiveGitLab(t *testing.T, config *GitLabConfig, event, body string) ([]Item, error) {
	t.Helper()
	header := http.Header{"X-Gitlab-Token": {"webhook-secret"}}
	if event != "" {
		header.Set("X-Gitlab-Event", event)
	}
	return config.Receive(t.Context(), Request{Resources: config.Receives(), Header: header, Body: []byte(body), WebhookSecret: "webhook-secret"}, false)
}

func shortenGitLabWaits(t *testing.T) {
	t.Helper()
	shortenRetries(t)
	clear(apiBuckets.buckets)
	t.Cleanup(func() {
		apiBuckets.mu.Lock()
		clear(apiBuckets.buckets)
		apiBuckets.mu.Unlock()
	})
}

func TestParseGitLabConfig(t *testing.T) {
	spec, config := gitlabSpec(t, gitlabYAML, "https://gitlab.example.com")
	if !spec.Pulls || !spec.Receives || !spec.Events || !spec.Checks || config.ExcludeArchived ||
		config.apiURL() != "https://gitlab.example.com/api/v4" {
		t.Fatalf("spec = %+v, config = %+v", spec, config)
	}
	for _, resource := range []string{gitlabGroups, gitlabMembers} {
		if slices.Contains(config.Receives(), resource) {
			t.Errorf("%s have no project webhook events, so can't be received", resource)
		}
	}
	_, defaults := gitlabSpec(t, strings.Replace(gitlabYAML, "      url: https://gitlab.example.com\n", "", 1), "")
	if defaults.apiURL() != "https://gitlab.com/api/v4" {
		t.Errorf("API = %s, want GitLab.com's", defaults.apiURL())
	}
	_, relative := gitlabSpec(t, strings.Replace(gitlabYAML, "https://gitlab.example.com", "https://example.com/gitlab/", 1), "")
	if relative.apiURL() != "https://example.com/gitlab/api/v4" {
		t.Errorf("API = %s, want it under the instance's relative URL", relative.apiURL())
	}

	head := "kind: Connection\nmetadata: {identifier: a, title: A}\nspec:\n  connector:\n    type: gitlab\n    config:\n"
	token := "      token: {env: GP_SECRET_T}\n"
	tests := []struct{ name, source, want string }{
		{"no token", "      groups: [acme]\n", "add a token, as {env: GP_SECRET_...}"},
		{"inline token", "      token: glpat-secret\n      groups: [acme]\n", "write the secret as {env: GP_SECRET_...}"},
		{"other env", "      token: {env: DB_PASSWORD}\n      groups: [acme]\n", "starting with GP_SECRET_"},
		{"nothing to read", token, "add the groups or projects to read"},
		{"project without a namespace", token + "      projects: [api]\n", `"api" isn't a project's full path`},
		{"empty part", token + "      groups: [acme//platform]\n", `"acme//platform" isn't a group's full path`},
		{"spaces", token + "      projects: [acme/my api]\n", `isn't a project's full path`},
		{"twice", token + "      groups: [acme, ACME]\n", "ACME is listed twice"},
		{"bad url", token + "      groups: [acme]\n      url: ftp://gitlab.example.com\n", "url must be the instance's http or https address"},
		{"url with credentials", token + "      groups: [acme]\n      url: https://u:p@gitlab.example.com\n", "url must be the instance's http or https address"},
		{"nested groups", token + "      groups: [acme, acme/platform]\n", "acme/platform is already read with the acme group, which includes its subgroups"},
		{"archived in words", token + "      groups: [acme]\n      excludeArchived: yes please\n", "excludeArchived must be true or false"},
		{"historyDays", token + "      groups: [acme]\n      historyDays: 30\n", "historyDays is gone: syncs read what changed since the last one, and each blueprint's retention says how long its rows are kept"},
		{"history", token + "      groups: [acme]\n      history: 30d\n", "history is gone"},
		{"unknown field", token + "      groups: [acme]\n      group: acme\n", `unknown field "group"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := Parse(head + test.source)
			var messages []string
			for _, issue := range issues {
				messages = append(messages, issue.Message)
			}
			if !strings.Contains(strings.Join(messages, "\n"), test.want) {
				t.Fatalf("issues = %v, want %q", messages, test.want)
			}
		})
	}

	warnings := []struct{ config, want string }{
		{"      groups: [acme]\n      url: https://example.com/gitlab\n      excludeArchived: true\n", ""},
		{"      groups: [acme]\n      projects: [acme/platform/web]\n", "acme/platform/web is already read with the acme group"},
	}
	for _, w := range warnings {
		spec, issues := Parse(head + token + w.config)
		switch {
		case spec.config == nil || configdoc.HasErrors(issues):
			t.Errorf("%s: issues = %+v", w.config, issues)
		case w.want == "" && len(issues) != 0:
			t.Errorf("%s: issues = %+v, want none", w.config, issues)
		case w.want != "" && (len(issues) != 1 || issues[0].Severity != "warning" || !strings.Contains(issues[0].Message, w.want)):
			t.Errorf("%s: issues = %+v, want a warning that %s", w.config, issues, w.want)
		}
	}
}

func TestGitLabPull(t *testing.T) {
	f := newGitLabFixture(t)
	f.noCI = "3"
	spec, config := gitlabSpec(t, gitlabYAML, f.url())

	pulled, failed := pullGitLab(t, config)
	if len(failed) != 0 {
		t.Fatalf("failed = %v", failed)
	}
	checks := []struct {
		resource string
		path     []string
		want     string
	}{
		{gitlabGroups, []string{"full_path"}, "acme,acme/platform"},
		{gitlabProjects, []string{"path_with_namespace"}, "acme/api,acme/platform/web,octo/tools"},
		{gitlabProjects, []string{parentKey, "latestPipeline", "id"}, "13,13,13"},
		{gitlabMembers, []string{parentKey, "group"}, "acme,acme,acme/platform"},
		{gitlabMembers, []string{"username"}, "ada,grace,linus"},
		// Without cursors, everything, however long ago it changed.
		{gitlabMergeRequests, []string{"iid"}, "4,3,2,1,4,3,2,1,4,3,2,1"},
		// acme/platform/web has issues turned off.
		{gitlabIssues, []string{"iid"}, "9,8,7,6,9,8,7,6"},
		// Child pipelines too, which GitLab lists only when asked for them.
		{gitlabPipelines, []string{"id"}, "14,13,12,11,14,13,12,11,14,13,12,11"},
		// Each job's latest attempt, and every attempt, retried ones too.
		{gitlabJobs, []string{"id"}, "140,132,130,120,110,140,132,130,120,110,140,132,130,120,110"},
		{gitlabJobAttempts, []string{"id"}, "140,132,131,130,120,110,140,132,131,130,120,110,140,132,131,130,120,110"},
		{gitlabDeployments, []string{"id"}, "22,21,22,21,22,21"},
		{gitlabEnvironments, []string{parentKey, "lastDeployment", "id"}, "22,21,22,21,22,21"},
		{gitlabReleases, []string{parentKey, "project"}, "acme/api,acme/api,acme/platform/web,acme/platform/web,octo/tools,octo/tools"},
		{gitlabProjects, []string{parentKey, "defaultBranch", "commit", "id"}, "ma1n,ma1n,ma1n"},
	}
	for _, c := range checks {
		if got := gitlabField(pulled[c.resource], c.path...); got != gitlabSorted(c.want) {
			t.Errorf("%s %v = %s, want %s", c.resource, c.path, got, c.want)
		}
	}
	for _, project := range pulled[gitlabProjects] {
		if _, ok := project["cicd_catalog_enabled"]; ok || project["permissions"] != nil || len(project) != len(pulled[gitlabProjects][0]) {
			t.Errorf("project = %v, want every project alike, whether listed on its own or in a group", project)
		}
	}
	for _, group := range pulled[gitlabGroups] {
		if group["runners_token"] != nil || group["shared_with_groups"] != nil || len(group) != len(pulled[gitlabGroups][1]) {
			t.Errorf("group = %v, want it as GitLab lists groups, as its subgroup is", group)
		}
	}
	// Each project carries its open vulnerabilities by severity and its
	// oldest critical and high, or nothing when the token can't see them.
	for _, project := range pulled[gitlabProjects] {
		const none = `{"critical":0,"high":0,"low":0,"medium":0}`
		// What a project carries goes through sources' jq, which takes only
		// what JSON decodes to.
		if bad := nonJSON(project); bad != "" {
			t.Errorf("%s carries %s, which jq can't read", project["path_with_namespace"], bad)
		}
		got, _ := json.Marshal(valueAt(project, parentKey, "vulnerabilities"))
		want := "null"
		switch project["path_with_namespace"] {
		case "acme/api":
			want = `{"byScanner":{"containerScanning":` + none + `,"dast":` + none + `,"dependencyScanning":` + none +
				`,"sast":{"critical":2,"high":1,"low":0,"medium":5},"secretDetection":` + none + `},` +
				`"critical":2,"high":1,"low":0,"medium":5,"oldestCriticalAt":"` + gitlabT0 + `","oldestHighAt":null}`
		case "acme/platform/web":
			want = `{"byScanner":{"containerScanning":` + none + `,"dast":` + none + `,"dependencyScanning":` + none + `,"sast":` + none +
				`,"secretDetection":` + none + `},"critical":0,"high":0,"low":0,"medium":0,"oldestCriticalAt":null,"oldestHighAt":null}`
		}
		if string(got) != want {
			t.Errorf("%s has vulnerabilities %s, want %s", project["path_with_namespace"], got, want)
		}
		// Its dependencies by license, where the token can read them.
		deps, _ := json.Marshal(valueAt(project, parentKey, "dependencies"))
		wantDeps := "null"
		if project["path_with_namespace"] == "acme/api" {
			wantDeps = `{"count":4,"licenses":{"EPL-2.0 OR GPL-2.0-only":1,"GPL-3.0-only":1,"MIT":1,"unknown":1},"malware":1,"osPackages":1}`
		}
		if string(deps) != wantDeps {
			t.Errorf("%s has dependencies %s, want %s", project["path_with_namespace"], deps, wantDeps)
		}
	}
	// acme/platform turns its check on for acme/platform/web, though the
	// project's own setting is off; octo/tools turns its other one on.
	for _, project := range pulled[gitlabProjects] {
		path := project["path_with_namespace"]
		checks := map[string]any{"pipelineMustSucceed": path == "acme/platform/web", "discussionsMustBeResolved": path == "octo/tools"}
		if got := valueAt(project, parentKey, "mergeChecks"); !reflect.DeepEqual(got, checks) {
			t.Errorf("%s has merge checks %v, want %v", path, got, checks)
		}
	}
	for _, project := range pulled[gitlabProjects] {
		// octo/tools has no CI configuration file, so no CI config.
		config, _ := valueAt(project, parentKey, "ciConfig").(map[string]any)
		if (config == nil) != (project["path_with_namespace"] == "octo/tools") {
			t.Errorf("%s has CI config %v", project["path_with_namespace"], config)
		}
		// The root's files and the .gitlab directory's, without directories.
		if files := valueAt(project, parentKey, "files"); !reflect.DeepEqual(files, []any{"README.md", ".gitlab/CODEOWNERS"}) {
			t.Errorf("%s has files %v, want README.md and .gitlab/CODEOWNERS", project["path_with_namespace"], files)
		}
		if config == nil {
			continue
		}
		jobs, _ := config["jobs"].([]any)
		if job, _ := jobs[0].(map[string]any); job["script"] != nil || job["before_script"] != nil || job["name"] != "build" || config["merged_yaml"] != nil {
			t.Errorf("ci config = %v, want its jobs without their scripts", config)
		}
	}
	// Pipelines as GitLab lists them, with their times from GraphQL, a page
	// for each kind of pipeline in each project.
	for _, pipeline := range pulled[gitlabPipelines] {
		want := gitlabFixturePipelineTimes(pipeline)
		want["started_at"], want["finished_at"] = gitlabSecond(want["started_at"]), gitlabSecond(want["finished_at"])
		want["user"] = map[string]any{"id": float64(1), "username": "ada", "name": "Ada"}
		for field, value := range want {
			if !reflect.DeepEqual(pipeline[field], value) {
				t.Errorf("pipeline %v has %s %v, want %v", pipeline["id"], field, pipeline[field], value)
			}
		}
		if pipeline["detailed_status"] != nil || nonJSON(pipeline) != "" {
			t.Errorf("pipeline = %v, want it as GitLab lists it, with its times as JSON", pipeline)
		}
	}
	if n := len(f.sent(http.MethodPost, "/api/graphql")); n != 3*(1+2+1) {
		t.Errorf("asked GraphQL %d times, want once a project for vulnerabilities, and for each page of each kind of pipeline", n)
	}
	if n := len(slices.DeleteFunc(f.asked(), func(r string) bool { return !regexp.MustCompile(`/pipelines/\d+\?`).MatchString(r) })); n != 0 {
		t.Errorf("read %d pipelines on their own, want their times from GraphQL", n)
	}
	if slices.ContainsFunc(f.asked(), func(r string) bool { return strings.Contains(r, "/projects/2/issues") }) {
		t.Error("read the issues of a project with them turned off")
	}
	if latest := f.sent(http.MethodGet, "/projects/1/pipelines"); !slices.Contains(latest, "order_by=id&per_page=1&ref=main&sort=desc") ||
		len(f.sent(http.MethodGet, "/projects/1/pipelines/latest")) != 0 {
		t.Errorf("pipelines were asked for with %v, want the default branch's latest from their list, which a Reporter may read", latest)
	}
	if pages := f.sent(http.MethodGet, "/projects/1/repository/tags"); len(pages) != 2 {
		t.Errorf("tags were read in %d pages, want 2 by their Link headers", len(pages))
	}
	if q := f.sent(http.MethodGet, "/groups/10/projects"); len(q) == 0 || !strings.Contains(q[0], "include_subgroups=true") || !strings.Contains(q[0], "with_shared=false") {
		t.Errorf("the group's projects were asked for with %v, want its subgroups' and not those shared with it", q)
	}

	api := pulled[gitlabProjects][slices.IndexFunc(pulled[gitlabProjects], func(p map[string]any) bool { return p["path_with_namespace"] == "acme/api" })]
	entries := mustMap(t, spec, Item{Resource: gitlabProjects, Object: api})
	if len(entries) != 1 || entries[0].Entity.Identifier != "acme/api" || entries[0].Entity.Properties["ci"] != "success" ||
		entries[0].Entity.Properties["protected"] != true || entries[0].Entity.Properties["ciValid"] != true {
		t.Errorf("project entry = %+v", entries)
	}
}

// TestGitLabPullFromCursors checks a sync reads what piles up from where
// the request says in each project: what changed since the last sync; on a
// first sync that reads only as far back as rows are kept, the open merge
// requests and issues too; and jobs only of pipelines changed since jobs
// were read.
func TestGitLabPullFromCursors(t *testing.T) {
	resources := []string{gitlabMergeRequests, gitlabIssues, gitlabPipelines, gitlabJobs, gitlabJobAttempts, gitlabDeployments}
	minute := func(at string) time.Time {
		t, _ := time.Parse(time.RFC3339, at)
		return t.Add(-time.Minute)
	}
	every := func(c Cursor) map[string]Cursor {
		out := map[string]Cursor{}
		for _, resource := range resources {
			out[resource] = c
		}
		return out
	}
	tests := []struct {
		name    string
		cursors map[string]Cursor
		want    map[string]string
	}{
		{"since the last sync", every(Cursor{Scopes: map[string]time.Time{"1": minute(gitlabT3)}}), map[string]string{
			gitlabMergeRequests: "3", gitlabIssues: "9", gitlabPipelines: "14,13", gitlabJobs: "140,132,130", gitlabJobAttempts: "140,132,131,130",
			gitlabDeployments: "22",
		}},
		{"first, as far back as rows are kept", every(Cursor{Floor: minute(gitlabT2)}), map[string]string{
			gitlabMergeRequests: "4,3,2", gitlabIssues: "9,6,8", gitlabPipelines: "14,13,12", gitlabJobs: "140,132,130,120",
			gitlabJobAttempts: "140,132,131,130,120", gitlabDeployments: "22",
		}},
		{"jobs since the last sync, pipelines in full", map[string]Cursor{
			gitlabJobs: {Scopes: map[string]time.Time{"1": minute(gitlabT3)}}, gitlabJobAttempts: {Scopes: map[string]time.Time{"1": minute(gitlabT3)}},
		}, map[string]string{
			gitlabPipelines: "14,13,12,11", gitlabJobs: "140,132,130", gitlabJobAttempts: "140,132,131,130",
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newGitLabFixture(t)
			_, config := gitlabSpec(t, gitlabProjectYAML("acme/api"), f.url())
			request := PullRequest{Resources: resources, Cursors: test.cursors, read: newReadLog()}
			pulled, failed := pullGitLabAs(t, config, request)
			if len(failed) != 0 {
				t.Fatalf("failed = %v", failed)
			}
			for resource, want := range test.want {
				if got := gitlabField(pulled[resource], "id"); got != gitlabSorted(want) {
					t.Errorf("%s = %s, want %s", resource, got, want)
				}
			}
			if q := f.sent(http.MethodGet, "/projects/1/deployments"); test.cursors[gitlabDeployments].Scopes != nil &&
				(len(q) != 1 || !strings.Contains(q[0], "order_by=updated_at") || !strings.Contains(q[0], "updated_after=")) {
				t.Errorf("deployments were asked for with %v, want what changed since, by updated_at", q)
			}
		})
	}
}

// TestGitLabPullScopesItems checks what's read from a project carries the
// project's ID as its scope, and what's read from groups none, each with
// when GitLab answered, and that the report lists the projects read and
// fails a project's resource in that project alone.
func TestGitLabPullScopesItems(t *testing.T) {
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabYAML, f.url())
	f.deny("/projects/2/deployments")
	ids := map[string]string{}
	var items []Item
	before := time.Now().Add(-time.Minute)
	report, err := config.Pull(t.Context(), nil, PullRequest{Resources: gitlabResources, Warn: func(string) {}}, func(_ string, page []Item) error {
		items = append(items, page...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		object := item.Object.(map[string]any)
		if item.Resource == gitlabProjects {
			path, _ := object["path_with_namespace"].(string)
			ids[path] = idOf(object["id"])
		}
	}
	for _, item := range items {
		object := item.Object.(map[string]any)
		want := ""
		switch {
		case item.Resource == gitlabProjects:
			want = idOf(object["id"])
		// Several projects may share a runner, which is in none of them.
		case item.Resource == gitlabRunners:
		case !slices.Contains(gitlabGroupResources, item.Resource):
			path, _ := valueAt(object, parentKey, "project").(string)
			want = ids[path]
		}
		if item.Scope != want || want == "" && !slices.Contains(append([]string{gitlabRunners}, gitlabGroupResources...), item.Resource) {
			t.Errorf("%s %v has scope %q, want %q", item.Resource, object["id"], item.Scope, want)
		}
		if item.SourceAt.Before(before) || item.SourceAt.After(time.Now()) {
			t.Errorf("%s %v was answered at %v", item.Resource, object["id"], item.SourceAt)
		}
	}
	slices.Sort(report.Scopes)
	// Environments read their last deployment, so they fail with them.
	refused := slices.ContainsFunc(report.ScopeFailures, func(f ScopeFailure) bool { return f.Scope != "2" || !f.Refused })
	if strings.Join(report.Scopes, ",") != "1,2,3" || len(report.Failed) != 0 || len(report.ScopeFailures) != 2 || refused {
		t.Errorf("report = %+v", report)
	}
}

// TestGitLabPullFailsAPartialView checks a group the token sees only part
// of fails what it holds, and lists none of its projects, so a complete
// sync deletes none of it.
func TestGitLabPullFailsAPartialView(t *testing.T) {
	project := gitlabProjectYAML("octo/tools")
	tests := []struct {
		name string
		edit func(f *gitlabFixture)
		yaml string
		want string
	}{
		{"a member", nil, gitlabYAML, ""},
		{"not a member", func(f *gitlabFixture) { f.access = 0 }, gitlabYAML, "the token's user isn't a member of the group acme with at least the Reporter role"},
		{"a guest", func(f *gitlabFixture) { f.access = 10 }, gitlabYAML, "the token's user isn't a member of the group acme with at least the Reporter role"},
		// A guest of a public project reads it, without its confidential
		// issues.
		{"a guest of a project", func(f *gitlabFixture) { f.access = 10 }, project, "the token's user isn't a member of the project octo/tools with at least the Reporter role"},
		{"an administrator", func(f *gitlabFixture) { f.access, f.admin = 0, true }, gitlabYAML, ""},
		{"an administrator in Admin Mode", func(f *gitlabFixture) { f.access, f.admin, f.adminMode = 0, true, true }, gitlabYAML, "give the token the admin_mode scope"},
		{"an administrator's token for Admin Mode", func(f *gitlabFixture) {
			f.access, f.admin, f.adminMode, f.scopes = 0, true, true, []string{"api", "admin_mode"}
		}, gitlabYAML, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newGitLabFixture(t)
			if test.edit != nil {
				test.edit(f)
			}
			_, config := gitlabSpec(t, test.yaml, f.url())
			resources := []string{gitlabProjects, gitlabTags}
			if len(config.Groups) > 0 {
				resources = append(resources, gitlabGroups)
			}
			_, failed := pullGitLab(t, config, resources...)
			for _, resource := range resources {
				err := failed[resource]
				if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
					t.Errorf("%s failed with %v, want %q", resource, err, test.want)
				}
			}
		})
	}
}

// TestGitLabPullReadsWhatGitLabHas checks a listed group or project GitLab
// has moved is read where it is now, with a warning to fix the config; one
// it has no more fails what it would have fed, with a warning, since a
// token that lost access gets the same 404 and nothing should be deleted;
// and what's deleted while a sync reads it is skipped.
func TestGitLabPullReadsWhatGitLabHas(t *testing.T) {
	tests := []struct {
		name, yaml, projects, warning, failure string
	}{
		{"a moved group", strings.Replace(gitlabYAML, "groups: [acme]", "groups: [old-acme]", 1), "acme/api,acme/platform/web,octo/tools", "GitLab has moved the group old-acme to acme, where it's read", ""},
		{"a missing group", strings.Replace(gitlabYAML, "groups: [acme]", "groups: [nobody]", 1), "octo/tools", "GitLab has no group nobody that the token can see", "GitLab has no group nobody"},
		{"a moved project", gitlabProjectYAML("octo/old-tools"), "octo/tools", "GitLab has moved the project octo/old-tools to octo/tools, where it's read", ""},
		{"a missing project", gitlabProjectYAML("octo/missing"), "", "GitLab has no project octo/missing that the token can see", "GitLab has no project octo/missing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newGitLabFixture(t)
			_, config := gitlabSpec(t, test.yaml, f.url())
			var warnings []string
			resources := []string{gitlabProjects, gitlabTags}
			pulled, failed := pullGitLabAs(t, config, PullRequest{Resources: resources, Warn: func(w string) { warnings = append(warnings, w) }})
			if gitlabField(pulled[gitlabProjects], "path_with_namespace") != test.projects || len(warnings) != 1 || !strings.HasPrefix(warnings[0], test.warning) {
				t.Errorf("projects = %s, warnings = %v", gitlabField(pulled[gitlabProjects], "path_with_namespace"), warnings)
			}
			for _, resource := range resources {
				err := failed[resource]
				if test.failure == "" && err != nil || test.failure != "" && (err == nil || !strings.Contains(err.Error(), test.failure)) {
					t.Errorf("%s failed with %v, want %q", resource, err, test.failure)
				}
			}
		})
	}

	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabProjectYAML("acme/api"), f.url())
	f.answer("/projects/1/pipelines/13/jobs", gitlabNotFound)
	pulled, failed := pullGitLab(t, config, gitlabPipelines, gitlabJobs, gitlabTags)
	if len(failed) != 0 || gitlabField(pulled[gitlabPipelines], "id") != "11,12,13,14" || gitlabField(pulled[gitlabJobs], "id") != "110,120,140" ||
		len(pulled[gitlabTags]) != 3 {
		t.Errorf("pulled = %v, failed = %v, want what was deleted since it was listed skipped", pulled, failed)
	}
	// A 404 on part of a project is the project gone only if it is.
	f.answer("/projects/1/releases", gitlabNotFound, gitlabNotFound)
	f.answer("/projects/1", gitlabNotFound)
	if _, failed := pullGitLab(t, config, gitlabReleases); len(failed) != 0 {
		t.Errorf("failed = %v, want a project gone since it was listed skipped", failed)
	}
	if _, failed := pullGitLab(t, config, gitlabReleases); failed[gitlabReleases] == nil {
		t.Error("a 404 on part of a project that's still there didn't fail")
	}
}

// TestGitLabPullExcludesArchived checks excludeArchived leaves archived
// projects out with all that's in them, listed or in a group, saying so of
// one listed.
func TestGitLabPullExcludesArchived(t *testing.T) {
	for _, archived := range []string{"2", "3"} {
		f := newGitLabFixture(t)
		f.archived = archived
		_, config := gitlabSpec(t, strings.Replace(gitlabYAML, "      groups:", "      excludeArchived: true\n      groups:", 1), f.url())
		var warnings []string
		pulled, failed := pullGitLabAs(t, config, PullRequest{
			Resources: []string{gitlabProjects, gitlabTags}, Warn: func(w string) { warnings = append(warnings, w) },
		})
		want := map[string]string{"2": "acme/api,octo/tools", "3": "acme/api,acme/platform/web"}[archived]
		if len(failed) != 0 || gitlabField(pulled[gitlabProjects], "path_with_namespace") != want || len(pulled[gitlabTags]) != 6 {
			t.Errorf("archived %s: projects = %s, branches = %d, failed = %v", archived, gitlabField(pulled[gitlabProjects], "path_with_namespace"), len(pulled[gitlabTags]), failed)
		}
		if q := f.sent(http.MethodGet, "/groups/10/projects"); len(q) == 0 || !strings.Contains(q[0], "archived=false") {
			t.Errorf("the group's projects were asked for with %v, want archived ones left out", q)
		}
		items, err := receiveGitLab(t, config, "Push Hook", `{"object_kind": "push", "ref": "refs/heads/main", "after": "ma1n", "project": {"id": `+archived+`, "default_branch": "main", "path_with_namespace": "`+
			map[string]string{"2": "acme/platform/web", "3": "octo/tools"}[archived]+`"}}`)
		if err != nil || len(items) != 0 {
			t.Errorf("archived %s: an event wrote %v, %v", archived, items, err)
		}
		if listed := archived == "3"; listed != (len(warnings) == 1 && strings.HasPrefix(warnings[0], "The project octo/tools is archived, so it's left out")) {
			t.Errorf("archived %s: warnings = %v, want one only of a listed project", archived, warnings)
		}
	}
}

// TestGitLabEnabled checks a project with CI/CD off isn't asked for its
// deployments, which GitLab forbids, though its environments are on.
func TestGitLabEnabled(t *testing.T) {
	project := gitlabFixtureProject("1")
	project["builds_access_level"] = "disabled"
	for _, resource := range []string{gitlabDeployments, gitlabEnvironments, gitlabPipelines} {
		if gitlabEnabled(project, resource) {
			t.Errorf("%s is read from a project with CI/CD off", resource)
		}
	}
	if !gitlabEnabled(project, gitlabTags) {
		t.Error("tags aren't read from a project with CI/CD off")
	}
}

// TestGitLabSameFlags checks a setting an older GitLab doesn't show
// doesn't count as one to change.
func TestGitLabSameFlags(t *testing.T) {
	flags := map[string]bool{"push_events": true, "releases_events": false}
	if !gitlabSameFlags(map[string]any{"push_events": true, "releases_events": nil}, flags) ||
		!gitlabSameFlags(map[string]any{"push_events": true}, flags) ||
		gitlabSameFlags(map[string]any{"push_events": false}, flags) {
		t.Error("gitlabSameFlags compares settings GitLab doesn't show")
	}
}

// TestGitLabTransport checks an answer that breaks off is asked for again,
// and that a 429 of one endpoint's own limit holds back only that request,
// while one of the token's holds back them all.
func TestGitLabTransport(t *testing.T) {
	shortenGitLabWaits(t)
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabProjectYAML("acme/api"), f.url())
	f.answer("/projects/1/repository/tags", func(w http.ResponseWriter) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte(`[{"name":`))
	})
	f.answer("/projects/1/ci/lint", func(w http.ResponseWriter) {
		w.Header().Set("RateLimit-Name", "ci_lint")
		gitlabTooMany(w)
	})
	pulled, failed := pullGitLab(t, config, gitlabTags, gitlabProjects)
	if len(failed) != 0 || len(pulled[gitlabTags]) != 3 || valueAt(pulled[gitlabProjects][0], parentKey, "ciConfig") == nil {
		t.Fatalf("pulled = %v, failed = %v", pulled, failed)
	}
	if got := len(f.sent(http.MethodGet, "/projects/1/repository/tags")); got != 3 {
		t.Errorf("tags were asked for %d times, want the page that broke off again, then the next", got)
	}
	client, _ := newGitLabClient(config)
	cred, _ := client.authorize(t.Context(), "")
	if b := apiBuckets.get(cred.bucket); !b.resetAt.IsZero() {
		t.Errorf("CI lint's own limit held back the token until %s", b.resetAt)
	}
}

func TestGitLabPullRateLimits(t *testing.T) {
	only := []string{gitlabProjects, gitlabMergeRequests}

	t.Run("recovers", func(t *testing.T) {
		shortenGitLabWaits(t)
		f := newGitLabFixture(t)
		_, config := gitlabSpec(t, gitlabYAML, f.url())
		f.answer("/groups/10/projects", gitlabTooMany, gitlabServerError)
		f.answer("/projects/1/merge_requests", gitlabTooMany)
		pulled, failed := pullGitLab(t, config, only...)
		if len(failed) != 0 || len(pulled[gitlabMergeRequests]) != 12 {
			t.Fatalf("pulled = %v, failed = %v", pulled, failed)
		}
	})

	t.Run("stops once spent for long", func(t *testing.T) {
		shortenGitLabWaits(t)
		f := newGitLabFixture(t)
		_, config := gitlabSpec(t, gitlabProjectYAML("acme/api"), f.url())
		f.answer("/projects/1/merge_requests", gitlabSpent(time.Now().Add(time.Hour)))
		_, failed := pullGitLab(t, config, gitlabProjects, gitlabMergeRequests, gitlabReleases)
		var limited rateLimitError
		if !errors.As(failed[gitlabMergeRequests], &limited) || !errors.As(failed[gitlabReleases], &limited) || failed[gitlabProjects] != nil {
			t.Fatalf("failed = %v, want merge requests and what came after to fail on the rate limit", failed)
		}
		if strings.Contains(strings.Join(f.asked(), " "), "/projects/1/releases") {
			t.Error("asked GitLab again once the rate limit was spent")
		}
	})
}

func TestGitLabPullKeepsGoingPastAFailedResource(t *testing.T) {
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabYAML, f.url())
	f.deny("/deployments")
	pulled, failed := pullGitLab(t, config, gitlabDeployments, gitlabReleases, gitlabMembers)
	if err := failed[gitlabDeployments]; err == nil || !strings.Contains(err.Error(), "and 2 more failed") {
		t.Fatalf("failed = %v", failed)
	}
	if len(failed) != 1 || len(pulled[gitlabReleases]) != 6 || len(pulled[gitlabMembers]) != 3 {
		t.Fatalf("pulled = %v, failed = %v", pulled, failed)
	}

	f.deny("/jobs")
	pulled, failed = pullGitLab(t, config, gitlabPipelines, gitlabJobs)
	if failed[gitlabJobs] == nil || failed[gitlabPipelines] != nil || len(pulled[gitlabPipelines]) != 12 {
		t.Fatalf("pulled = %v, failed = %v, want only jobs failed", pulled, failed)
	}

	t.Setenv("GP_SECRET_GITLAB_TOKEN", "")
	_, failed = pullGitLab(t, config, gitlabProjects)
	if err := failed[gitlabProjects]; err == nil || !strings.Contains(err.Error(), "GP_SECRET_GITLAB_TOKEN isn't set") {
		t.Errorf("unset token: %v", err)
	}
}

func TestGitLabPullStaysWithinTheAPI(t *testing.T) {
	var leaked atomic.Value
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("PRIVATE-TOKEN"))
		_, _ = w.Write([]byte(`[]`))
	}))
	defer elsewhere.Close()

	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabYAML, f.url())
	f.answer("/projects/1/merge_requests", func(w http.ResponseWriter) {
		w.Header().Set("Location", elsewhere.URL+"/merge_requests")
		w.WriteHeader(http.StatusMovedPermanently)
	})
	f.answer("/projects/2/merge_requests", func(w http.ResponseWriter) {
		w.Header().Set("Link", `<`+elsewhere.URL+`/merge_requests?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`[]`))
	})
	_, failed := pullGitLab(t, config, gitlabMergeRequests)
	if err := failed[gitlabMergeRequests]; err == nil || !strings.Contains(err.Error(), "outside its API") || !strings.Contains(err.Error(), "and 1 more failed") {
		t.Fatalf("failed = %v", failed)
	}
	if leaked.Load() != nil {
		t.Fatalf("sent %v outside the API", leaked.Load())
	}
}

func TestGitLabReceive(t *testing.T) {
	f := newGitLabFixture(t)
	spec, config := gitlabSpec(t, gitlabYAML, f.url())

	// GitLab's own payloads, in TestGitLabReceivesRealPayloads, cover the
	// usual events; these are the cases they don't.
	project := `"project": {"id": 1, "path_with_namespace": "acme/api", "default_branch": "main"}`
	zeros := `"0000000000000000000000000000000000000000"`
	tests := []struct {
		event, body, want string
	}{
		{"Push Hook", `{"object_kind": "push", "ref": "refs/heads/dev", "after": "d3v", ` + project + `}`, ""},
		{"Push Hook", `{"object_kind": "push", "ref": "refs/heads/main", "after": "ma1n", ` + project + `}`, "project acme/api"},
		{"Push Hook", `{"object_kind": "push", "ref": "refs/heads/gone", "after": "abc", ` + project + `}`, ""},
		{"Tag Push Hook", `{"object_kind": "tag_push", "ref": "refs/tags/v9", "after": ` + zeros + `, ` + project + `}`, "tag acme/api@v9 deleted"},
		{"Tag Push Hook", `{"object_kind": "tag_push", "ref": "refs/tags/v1", "after": "abc", ` + project + `}`, "tag acme/api@v1"},
		{"Merge Request Hook", `{"object_kind": "merge_request", "object_attributes": {"iid": 2, "action": "merge"}, ` + project + `}`, "merge-request acme/api!2"},
		{"Merge Request Hook", `{"object_kind": "merge_request", "object_attributes": {"iid": 1, "action": "update"}, ` + project + `}`, "merge-request acme/api!1"},
		{"Issue Hook", `{"object_kind": "issue", "object_attributes": {"iid": 9, "action": "update"}, ` + project + `}`, "issue acme/api#9"},
		{"Issue Hook", `{"object_kind": "work_item", "object_attributes": {"iid": 6, "type": "Task"}, ` + project + `}`, "issue acme/api#6"},
		{"Issue Hook", `{"object_kind": "issue", "object_attributes": {"iid": 5, "action": "close"}, ` + project + `}`, ""},
		// A pipeline's event reads every attempt of its jobs again, oldest
		// first, each job's latest, and the runners they ran on.
		{
			"Pipeline Hook", `{"object_kind": "pipeline", "object_attributes": {"id": 13, "ref": "main", "tag": false}, ` + project + `}`,
			"pipeline 13, job-attempt 130, job-attempt 131, job-attempt 132, job 130, job 132, runner 1, project acme/api",
		},
		{"Pipeline Hook", `{"object_kind": "pipeline", "object_attributes": {"id": 12, "ref": "dev", "tag": false}, ` + project + `}`, "pipeline 12, job-attempt 120, job 120"},
		// However old the pipeline, as syncs read it.
		{"Pipeline Hook", `{"object_kind": "pipeline", "object_attributes": {"id": 11, "ref": "dev"}, ` + project + `}`, "pipeline 11, job-attempt 110, job 110"},
		{
			"Pipeline Hook", `{"object_kind": "pipeline", "object_attributes": {"id": 14, "ref": "main", "tag": false}, ` + project + `}`,
			"pipeline 14, job-attempt 140, job 140, project acme/api",
		},
		{"Pipeline Hook", `{"object_kind": "pipeline", "object_attributes": {"id": 15, "ref": "dev"}, ` + project + `}`, ""},
		// A job's own events are left to its pipeline's.
		{"Job Hook", `{"object_kind": "build", "build_id": 130, "pipeline_id": 13, "project_id": 1, ` + project + `}`, ""},
		// The project an event names must be the one its ID is.
		{"Push Hook", `{"object_kind": "push", "ref": "refs/heads/main", "after": "ma1n", "project": {"id": 3, "path_with_namespace": "acme/api", "default_branch": "main"}}`, ""},
		{"Deployment Hook", `{"object_kind": "deployment", "deployment_id": 22, "status": "success", ` + project + `}`, "deployment 22"},
		// A deployment that finished may be what's deployed to its environment.
		{"Deployment Hook", `{"object_kind": "deployment", "deployment_id": 21, "environment": "staging", ` + project + `}`, "deployment 21, environment acme/api/staging"},
		{"Release Hook", `{"object_kind": "release", "action": "delete", "tag": "v9", ` + project + `}`, "release acme/api@v9 deleted"},
		{"Release Hook", `{"object_kind": "release", "action": "delete", "tag": "v1", ` + project + `}`, "release acme/api@v1"},
		{"Release Hook", `{"object_kind": "release", "action": "update", "tag": "v2", ` + project + `}`, "release acme/api@v2"},
		{"Note Hook", `{"object_kind": "note", ` + project + `}`, ""},
		{"Push Hook", `{"object_kind": "push", "ref": "refs/heads/dev", "project": {"id": 9, "path_with_namespace": "other/api"}}`, ""},
	}
	for _, test := range tests {
		t.Run(test.event+" "+test.want, func(t *testing.T) {
			items, err := receiveGitLab(t, config, test.event, test.body)
			if err != nil {
				t.Fatal(err)
			}
			if _, got := gitlabMapped(t, spec, items); got != test.want {
				t.Fatalf("items = %s, want %s", got, test.want)
			}
		})
	}

	// What an event reads is shaped as a sync's list gives it.
	items, _ := receiveGitLab(t, config, "Merge Request Hook", `{"object_kind": "merge_request", "object_attributes": {"iid": 3}, `+project+`}`)
	if mr := items[0].Object.(map[string]any); mr["subscribed"] != nil || mr["allow_collaboration"] != true || mr["state"] != "opened" {
		t.Errorf("merge request = %v, want it as GitLab lists it", mr)
	}
	items, _ = receiveGitLab(t, config, "Deployment Hook", `{"object_kind": "deployment", "deployment_id": 22, `+project+`}`)
	if d := items[0].Object.(map[string]any); d["approvals"] != nil || d["pending_approval_count"] != nil {
		t.Errorf("deployment = %v, want it as GitLab lists it", d)
	}
	items, _ = receiveGitLab(t, config, "Pipeline Hook", `{"object_kind": "pipeline", "object_attributes": {"id": 13, "ref": "main"}, `+project+`}`)
	if p := items[len(items)-1].Object.(map[string]any); items[len(items)-1].Resource != gitlabProjects || p["permissions"] != nil || p["cicd_catalog_enabled"] != nil ||
		valueAt(p, parentKey, "latestPipeline", "status") != "success" {
		t.Errorf("project = %v, want it as a sync gives it, with its latest pipeline", p)
	}

	body := `{"object_kind": "pipeline", "object_attributes": {"id": 13, "ref": "main"}, ` + project + `}`
	only := Request{Resources: []string{gitlabPipelines}, Header: http.Header{"X-Gitlab-Event": {"Pipeline Hook"}, "X-Gitlab-Token": {"webhook-secret"}}, Body: []byte(body), WebhookSecret: "webhook-secret"}
	asked := len(f.asked())
	if items, err := config.Receive(t.Context(), only, false); err != nil || len(items) != 1 || items[0].Resource != gitlabPipelines || len(f.asked()) != asked+2 {
		t.Errorf("only pipelines: items = %+v, %v, asked %v", items, err, f.asked()[asked:])
	}
	header := http.Header{"X-Gitlab-Event": {"Pipeline Hook"}}
	if _, err := config.Receive(t.Context(), Request{Resources: config.Receives(), Header: header, Body: []byte(body), WebhookSecret: "webhook-secret"}, false); !errors.As(err, new(SignatureError)) {
		t.Errorf("without a token: err = %v", err)
	}
	header.Set("X-Gitlab-Token", "guess")
	if _, err := config.Receive(t.Context(), Request{Resources: config.Receives(), Header: header, Body: []byte(body), WebhookSecret: "webhook-secret"}, false); !errors.As(err, new(SignatureError)) {
		t.Errorf("another token: err = %v", err)
	}
	header.Set("X-Gitlab-Token", "webhook-secret")
	if _, err := config.Receive(t.Context(), Request{Resources: config.Receives(), Header: header, Body: []byte(body)}, false); !errors.Is(err, ErrUnverified) {
		t.Errorf("without the server's key: err = %v", err)
	}
	if items, err := config.Receive(t.Context(), Request{Resources: config.Receives(), Header: http.Header{"X-Gitlab-Event": {"Pipeline Hook"}}, Body: []byte(body)}, true); err != nil || len(items) != 8 {
		t.Errorf("with an ingestion token: items = %+v, err = %v", items, err)
	}
	if _, err := receiveGitLab(t, config, "", body); !errors.As(err, new(ReceiveError)) {
		t.Errorf("no event: err = %v", err)
	}
	if _, err := receiveGitLab(t, config, "Push Hook", "["); !errors.Is(err, ErrNotJSON) {
		t.Errorf("not JSON: err = %v", err)
	}
}

// TestGitLabReceiveFailsWhatItCantRead checks an event whose object can't
// be read writes nothing and fails, without waiting out a rate limit, and
// that the rate limit holds back the token's syncs too, while one hidden
// from the token gives nothing, lest GitLab turn the webhook off.
func TestGitLabReceiveFailsWhatItCantRead(t *testing.T) {
	shortenGitLabWaits(t)
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabYAML, f.url())
	project := `"project": {"id": 1, "path_with_namespace": "acme/api", "default_branch": "main"}`

	f.answer("/projects/1/issues", gitlabServerError, gitlabServerError, gitlabServerError, gitlabServerError)
	items, err := receiveGitLab(t, config, "Issue Hook", `{"object_kind": "issue", "object_attributes": {"iid": 9}, `+project+`}`)
	if !errors.As(err, new(FetchError)) || items != nil {
		t.Fatalf("items = %v, err = %v, want a fetch error", items, err)
	}

	f.answer("/projects/1/issues", func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message": "403 Forbidden"}`))
	})
	items, err = receiveGitLab(t, config, "Issue Hook", `{"object_kind": "issue", "object_attributes": {"iid": 9}, `+project+`}`)
	if !errors.As(err, new(FetchError)) || items != nil {
		t.Fatalf("hidden: items = %v, err = %v, want a fetch error, for the run to record", items, err)
	}

	f.answer("/projects/1/merge_requests", gitlabSpent(time.Now().Add(time.Hour)))
	started := time.Now()
	items, err = receiveGitLab(t, config, "Merge Request Hook", `{"object_kind": "merge_request", "object_attributes": {"iid": 3}, `+project+`}`)
	var limited rateLimitError
	if !errors.As(err, &limited) || items != nil || time.Since(started) > time.Second {
		t.Fatalf("items = %v, err = %v after %s, want the rate limit at once", items, err, time.Since(started))
	}
	asked := len(f.asked())
	if _, failed := pullGitLab(t, config, gitlabProjects); !errors.As(failed[gitlabProjects], &limited) || len(f.asked()) != asked {
		t.Errorf("failed = %v, want the sync held back by the event's rate limit", failed)
	}
}

// TestGitLabReceiveBurst checks a burst of events reads GitLab only a few
// requests at a time with one token.
func TestGitLabReceiveBurst(t *testing.T) {
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabYAML, f.url())
	slow := func(w http.ResponseWriter) {
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte(`[{"id": 120, "name": "build", "pipeline": {"id": 12, "created_at": "` + gitlabT2 + `"}}]`))
	}
	const events = 4 * apiConcurrency
	for range events {
		f.answer("/projects/1/pipelines/12/jobs", slow)
	}
	body := `{"object_kind": "pipeline", "object_attributes": {"id": 12, "ref": "dev"}, "project": {"id": 1, "path_with_namespace": "acme/api"}}`
	var wg sync.WaitGroup
	var received atomic.Int32
	for range events {
		wg.Go(func() {
			if items, err := receiveGitLab(t, config, "Pipeline Hook", body); err == nil && len(items) == 3 {
				received.Add(1)
			}
		})
	}
	wg.Wait()
	if received.Load() != events || f.mostInFlight > apiConcurrency {
		t.Errorf("received %d of %d, with up to %d requests at once, want at most %d", received.Load(), events, f.mostInFlight, apiConcurrency)
	}
}

func TestGitLabCheck(t *testing.T) {
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabYAML, f.url())
	ingest, secret := gitlabIngestURL, "webhook-secret"
	withHooks := func(status string) {
		for _, id := range []string{"1", "3"} {
			f.hooks["/projects/"+id+"/hooks"] = []map[string]any{{"id": float64(5), "alert_status": "executable", "url": gitlabIngestURL + "?fingerprint=x"}}
		}
		f.hooks["/projects/3/hooks"][0]["alert_status"] = status
	}
	withHooks("executable")
	// check gives the check's failure, then each warning after "warning: ".
	check := func(config *GitLabConfig) string {
		t.Helper()
		var out []string
		err := config.Check(t.Context(), nil, CheckRequest{
			Resources: gitlabResources, IngestURL: ingest, WebhookSecret: secret,
			Warn: func(warning string) { out = append(out, "warning: "+warning) },
		})
		if err != nil {
			out = append([]string{err.Error()}, out...)
		}
		return strings.Join(out, "\n")
	}

	if got := check(config); got != "" {
		t.Fatalf("check = %s", got)
	}
	f.deny("/pipelines")
	f.deny("/jobs")
	f.deny("/groups/10/members")
	want := "The token can't read members of the group acme; give its user at least the Reporter role there. " +
		"The token can't read pipelines, jobs, job_attempts and runners of octo/tools; give its user at least the Reporter role there, or turn those features on for it."
	if got := check(config); got != want {
		t.Errorf("check = %s\nwant    %s", got, want)
	}
	f.denied = nil

	f.scopes = []string{"read_api"}
	if got := check(config); got != "warning: The token has no api scope, so it can't manage webhooks, and syncs alone keep the catalog current; to have GitLab's webhooks, give it the api scope." {
		t.Errorf("check of a read_api token = %s", got)
	}
	f.scopes = []string{"read_user"}
	if got := check(config); !strings.Contains(got, "has the read_user scopes, which can't read GitLab's API; give it the read_api scope") {
		t.Errorf("check of a read_user token = %s", got)
	}
	f.scopes = nil
	f.deny("/projects/3/hooks")
	clear(f.hooks)
	if got := check(config); got != "warning: The token can't manage the webhooks of octo/tools, so webhooks are unavailable there, and syncs alone keep it current; to have them, give its user at least the Maintainer role there.\n"+
		"warning: There's no webhook on acme/api yet. The next sync makes one if the token has the api scope; with only read_api, webhooks are unavailable there, and syncs alone keep it current." {
		t.Errorf("check of a token whose scopes GitLab doesn't say = %s", got)
	}
	f.denied = nil
	f.scopes = []string{"api"}
	withHooks("disabled")
	if got := check(config); got != "warning: GitLab turned off the webhook on octo/tools after its deliveries kept failing, so its events don't reach the portal "+
		"until the next sync makes it anew; its recent events in GitLab say why it failed." {
		t.Errorf("check of a webhook turned off = %s", got)
	}
	withHooks("executable")
	ingest = ""
	if got := check(config); got != "warning: The portal doesn't know its public URL, as GP_BASE_URL gives it, so GitLab's webhooks are unavailable, and syncs alone keep the catalog current." {
		t.Errorf("check without the portal's URL = %s", got)
	}
	ingest, secret = gitlabIngestURL, ""
	if got := check(config); got != "warning: GP_WEBHOOK_KEY isn't set on the server, so GitLab's webhooks are unavailable, and syncs alone keep the catalog current." {
		t.Errorf("check without the server's key = %s", got)
	}
	secret = "webhook-secret"

	tests := []struct {
		name string
		edit func(t *testing.T, f *gitlabFixture)
		yaml string
		want string
	}{
		{"revoked token", func(t *testing.T, _ *gitlabFixture) { t.Setenv("GP_SECRET_GITLAB_TOKEN", "revoked") }, gitlabYAML, "GitLab doesn't accept the token in GP_SECRET_GITLAB_TOKEN"},
		{"missing project", nil, gitlabProjectYAML("octo/missing"), "GitLab has no project octo/missing that the token can see."},
		{"missing group", nil, strings.Replace(gitlabYAML, "groups: [acme]", "groups: [nobody]", 1), "GitLab has no group nobody that the token can see."},
		{"moved project", nil, gitlabProjectYAML("octo/old-tools"), "GitLab has moved the project octo/old-tools to octo/tools; list it under its new path."},
		{"not a member", func(_ *testing.T, f *gitlabFixture) { f.access = 0 }, gitlabYAML, "The token's user isn't a member of the project octo/tools"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newGitLabFixture(t)
			if test.edit != nil {
				test.edit(t, f)
			}
			_, config := gitlabSpec(t, test.yaml, f.url())
			if got := check(config); !strings.Contains(got, test.want) {
				t.Errorf("check = %s, want %q", got, test.want)
			}
		})
	}
}

// TestGitLabPullSetsUpTheWebhook checks a sync points each project's
// webhook at the connection, sending the events its sources read, and
// changes it only when something differs.
func TestGitLabPullSetsUpTheWebhook(t *testing.T) {
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabYAML, f.url())
	const api, tools = "/projects/1/hooks", "/projects/3/hooks"
	secret := "webhook-secret"
	var warnings []string
	sync := func(resources ...string) {
		t.Helper()
		warnings = nil
		request := PullRequest{
			Resources: resources, IngestURL: gitlabIngestURL, WebhookSecret: secret,
			Warn: func(warning string) { warnings = append(warnings, warning) },
		}
		if _, failed := pullGitLabAs(t, config, request); len(failed) != 0 {
			t.Fatal(failed)
		}
	}
	changes := func() int {
		return len(f.sent(http.MethodPost, api)) + len(f.sent(http.MethodPut, api+"/101"))
	}

	if _, failed := pullGitLab(t, config, gitlabMergeRequests); len(failed) != 0 || len(f.sent(http.MethodGet, api)) != 0 {
		t.Fatalf("a sync without the portal's URL looked at the webhooks: %v", f.asked())
	}
	f.hooks[api] = []map[string]any{{"id": float64(100), "alert_status": "executable", "push_events": true, "url": "https://elsewhere.example.com"}}
	sync(gitlabMergeRequests, gitlabProjects, gitlabMembers)
	if len(f.hooks[api]) != 2 || len(f.hooks[tools]) != 1 || len(f.hooks["/projects/2/hooks"]) != 1 || len(warnings) != 0 {
		t.Fatalf("hooks = %v, warnings = %v, want one made on each project, beside the other", f.hooks, warnings)
	}
	hook := f.hook(api, 1)
	address, _ := hook["url"].(string)
	if got := fmt.Sprintf("%v %v %v %v %v", ingestPart(address), hook["push_events"], hook["merge_requests_events"], hook["pipeline_events"], hook["enable_ssl_verification"]); got != gitlabIngestURL+" true true true true" ||
		hook["token"] != secret || !strings.Contains(address, "?fingerprint=") {
		t.Fatalf("hook = %v", hook)
	}

	sync(gitlabMergeRequests, gitlabProjects)
	if changes() != 1 {
		t.Errorf("an unchanged webhook was changed: %v", f.asked())
	}
	sync(gitlabMergeRequests, gitlabTags)
	if changes() != 2 || f.hook(api, 1)["push_events"] != false || f.hook(api, 1)["tag_push_events"] != true || f.hook(api, 1)["job_events"] != false {
		t.Errorf("hook = %v, want its events those of the resources mapped now", f.hook(api, 1))
	}
	// The server's key changed, and with it every connection's secret.
	secret = "rotated"
	sync(gitlabMergeRequests, gitlabTags)
	if changes() != 3 || f.hook(api, 1)["token"] != "rotated" || f.hook(api, 1)["url"] == address {
		t.Errorf("hook = %v, want the new secret and its fingerprint", f.hook(api, 1))
	}
	sync(gitlabMergeRequests, gitlabTags)
	if changes() != 3 {
		t.Errorf("a webhook with the new secret was changed again")
	}

	// GitLab turns off a webhook whose deliveries keep failing, as they do
	// once the secret changes and it has the old one, and only a test
	// delivery turns it back on, so a sync makes it anew with the new
	// secret, leaving its certificate checks as they were.
	f.mu.Lock()
	f.hooks[api][1]["alert_status"] = "disabled"
	f.hooks[api][1]["enable_ssl_verification"] = false
	f.mu.Unlock()
	secret = "rotated again"
	sync(gitlabMergeRequests, gitlabTags)
	if len(f.sent(http.MethodDelete, api+"/101")) != 1 || len(f.hooks[api]) != 2 || len(warnings) != 0 {
		t.Fatalf("hooks = %v, warnings = %v, want the one turned off made anew", f.hooks[api], warnings)
	}
	if hook := f.hook(api, 1); hook["id"] == float64(101) || hook["token"] != secret || hook["alert_status"] != "executable" ||
		hook["enable_ssl_verification"] != false || hook["tag_push_events"] != true {
		t.Errorf("hook = %v, want a new one, with the new secret, sending what it did, without certificate checks", hook)
	}
	// What lasts the check says, so syncs don't say it every time.
	f.deny(tools)
	sync(gitlabMergeRequests, gitlabTags)
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none of a webhook the token can't manage", warnings)
	}
	f.denied = nil
	if f.hook(api, 0)["url"] != "https://elsewhere.example.com" {
		t.Errorf("hooks = %v, want the other left alone", f.hooks[api])
	}

	// GitLab refuses webhooks to the local network unless allowed, which
	// only making one shows.
	clear(f.hooks)
	local := func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error": "Invalid url given", "message": "Invalid url given: Requests to the local network are not allowed"}`))
	}
	f.answer(api, func(w http.ResponseWriter) { _, _ = w.Write([]byte(`[]`)) }, local)
	f.answer(tools, func(w http.ResponseWriter) { _, _ = w.Write([]byte(`[]`)) }, local)
	sync(gitlabTags)
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "GitLab refused the webhook on acme/api and octo/tools: Invalid url given: Requests to the local network are not allowed. "+
		"An administrator can allow webhooks to the local network") {
		t.Errorf("warnings = %v, want GitLab's refusal, once", warnings)
	}
	shortenGitLabWaits(t)
	f.answer("/projects/2/hooks", gitlabServerError, gitlabServerError, gitlabServerError, gitlabServerError)
	sync(gitlabTags)
	if len(warnings) != 1 || warnings[0] != "The webhook on acme/platform/web couldn't be set up this time, so some events may not arrive until a later sync sets it up." {
		t.Errorf("warnings = %v, want one of what may pass", warnings)
	}

	// Without the server's key, or with nothing that has events, there are
	// none to set up.
	for _, resources := range [][]string{{gitlabTags}, {gitlabMembers, gitlabGroups}} {
		secret = ""
		if len(resources) > 1 {
			secret = "webhook-secret"
		}
		asked := len(f.asked())
		sync(resources...)
		if slices.ContainsFunc(f.asked()[asked:], func(r string) bool { return strings.Contains(r, "/hooks") }) {
			t.Errorf("a sync of %v with secret %q looked at the webhooks", resources, secret)
		}
	}
}

// TestGitLabPullMakesAWebhookOnce checks a webhook GitLab may have made
// before its answer was lost isn't asked for again.
func TestGitLabPullMakesAWebhookOnce(t *testing.T) {
	shortenGitLabWaits(t)
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabProjectYAML("acme/api"), f.url())
	f.answer("/projects/1/hooks", func(w http.ResponseWriter) { _, _ = w.Write([]byte(`[]`)) }, gitlabServerError)
	var warnings []string
	request := PullRequest{
		Resources: []string{gitlabTags}, IngestURL: gitlabIngestURL, WebhookSecret: "s",
		Warn: func(warning string) { warnings = append(warnings, warning) },
	}
	if _, failed := pullGitLabAs(t, config, request); len(failed) != 0 {
		t.Fatal(failed)
	}
	if posts := f.sent(http.MethodPost, "/projects/1/hooks"); len(posts) != 1 || len(warnings) != 1 {
		t.Errorf("asked to make the webhook %d times, warnings = %v, want once, and said so", len(posts), warnings)
	}
}

// TestGitLabPullSetsUpWebhooksAfterRunningOutOfTime checks a sync whose
// reads used up its time still sets up the webhooks of what it read.
func TestGitLabPullSetsUpWebhooksAfterRunningOutOfTime(t *testing.T) {
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabProjectYAML("acme/api"), f.url())
	ctx, cancel := context.WithCancel(t.Context())
	var once sync.Once
	f.answer("/projects/1/merge_requests", func(w http.ResponseWriter) {
		once.Do(cancel)
		// A next page leaves the read unfinished even when this answer beats the cancel.
		w.Header().Set("Link", `<`+f.url()+`/api/v4/projects/1/merge_requests?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`[]`))
	})
	failed, err := config.Pull(ctx, nil, PullRequest{Resources: []string{gitlabMergeRequests}, IngestURL: gitlabIngestURL, WebhookSecret: "s"},
		func(string, []Item) error { return nil })
	if err != nil || failedByResource(failed, []string{gitlabMergeRequests})[gitlabMergeRequests] == nil || len(f.hooks["/projects/1/hooks"]) != 1 {
		t.Fatalf("failed = %v, err = %v, hooks = %v", failed, err, f.hooks)
	}
}

// gitlabPayloadAPI is a GitLab API that answers each path, without its
// query, with an object, such as one a webhook payload is about, to any
// token.
func gitlabPayloadAPI(t *testing.T, objects map[string]any) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		object, ok := objects[strings.TrimPrefix(r.URL.EscapedPath(), "/api/v4")]
		if !ok {
			gitlabNotFound(w)
			return
		}
		_ = json.NewEncoder(w).Encode(object)
	}))
	t.Cleanup(server.Close)

	return server.URL
}

// gitlabPayloadJobs are the attempts of the example pipeline's jobs, newest
// first, shaped as GitLab's jobs API gives them: its test job failed on
// one runner and passed when retried on another.
func gitlabPayloadJobs(pipeline map[string]any) []any {
	attempt := func(id float64, name, status string, runner float64) map[string]any {
		return map[string]any{
			"id": id, "name": name, "stage": name, "status": status, "ref": "master", "tag": false, "allow_failure": false,
			"created_at": "2016-08-12T15:23:28.000Z", "started_at": "2016-08-12T15:26:29.000Z", "finished_at": "2016-08-12T15:27:32.000Z",
			"duration": 63.2, "queued_duration": 0.9, "failure_reason": map[string]any{"failed": "script_failure"}[status],
			"tag_list": []any{"docker", "linux"}, "web_url": "http://192.168.64.1:3005/gitlab-org/gitlab-test/-/jobs/" + idOf(id),
			"commit":   map[string]any{"id": pipeline["sha"], "short_id": "bcbb5ec3", "title": "Test commit"},
			"pipeline": map[string]any{"id": pipeline["id"], "project_id": 1, "ref": "master", "sha": pipeline["sha"], "status": "success", "created_at": pipeline["created_at"]},
			"runner": map[string]any{
				"id": runner, "description": "docker-" + idOf(runner), "ip_address": nil, "active": true, "paused": false, "is_shared": true,
				"runner_type": "instance_type", "name": nil, "online": true, "status": "online",
			},
			"runner_manager": map[string]any{
				"id": runner * 10, "system_id": "s_89e5e9956577", "version": "16.11.1", "revision": "535ced5f", "platform": "linux",
				"architecture": "amd64", "created_at": "2016-08-12T15:20:00.000Z", "contacted_at": "2016-08-12T15:28:00.000Z", "status": "online",
			},
			"artifacts": []any{}, "archived": false,
		}
	}
	return []any{attempt(380, "test", "success", 7), attempt(377, "test", "failed", 6), attempt(376, "build", "success", 6)}
}

// TestGitLabReceivesRealPayloads runs GitLab's own example payloads
// through Receive and the seed's GitLab sources, with what they're about
// answered as objects shaped as GitLab's API gives them, and checks what
// they make against the seed's blueprints, so the connector and the dev
// mock can't share a wrong idea of what GitLab sends.
func TestGitLabReceivesRealPayloads(t *testing.T) {
	newGitLabFixture(t)
	seed, err := os.ReadFile("../../deps/seed.sql")
	if err != nil {
		t.Fatal(err)
	}
	blueprints := map[string]blueprint.Spec{}
	var seededBlueprints []string
	var seeded string
	for _, match := range regexp.MustCompile(`(?s)\$yaml\$(.*?)\$yaml\$`).FindAllStringSubmatch(string(seed), -1) {
		if spec, issues := blueprint.Parse(match[1]); len(issues) == 0 {
			blueprints[spec.Identifier] = spec
			seededBlueprints = append(seededBlueprints, match[1])
		}
		if strings.Contains(match[1], "type: gitlab") {
			seeded = match[1]
		}
	}
	// The payloads are about GitLab's example projects, years ago.
	lists := "      projects: [mike/diaspora, jsmith/example, gitlabhq/gitlab-test, flightjs/flight-management, gitlab-org/gitlab-test, " +
		"root/test-deployment-webhooks, gitlab-org/release-webhook-example]"
	seeded = regexp.MustCompile(`(?m)^      groups: \[acme\]$`).ReplaceAllString(seeded, "      groups: [acme]\n"+lists)
	seeded = regexp.MustCompile(`(?m)^      url: .*$`).ReplaceAllString(seeded, "      url: https://gitlab.example.com")
	all := strings.Replace(strings.Replace(gitlabYAML, "      groups: [acme]\n", "", 1), "      projects: [octo/tools]", lists, 1)

	pipeline := map[string]any{
		"id": 31, "iid": 3, "project_id": 1, "ref": "master", "sha": "bcbb5ec396a2c0f828686f14fac9b80b780504f2", "status": "success",
		"source": "merge_request_event", "created_at": "2016-08-12T15:23:28Z", "started_at": "2016-08-12T15:26:29Z", "duration": 63,
		"web_url": "http://192.168.64.1:3005/gitlab-org/gitlab-test/-/pipelines/31",
	}
	objects := map[string]any{
		"/projects/15/repository/branches/master": map[string]any{
			"name": "master", "protected": true, "commit": map[string]any{"id": "da1560886d4f094c3e6c9ef40349f7d38b5d27d7", "short_id": "da156088"},
		},
		"/projects/15":                       map[string]any{"id": 15, "path_with_namespace": "mike/diaspora", "default_branch": "master", "builds_access_level": "enabled", "repository_access_level": "enabled"},
		"/projects/15/ci/lint":               map[string]any{"valid": true, "errors": []any{}, "warnings": []any{}, "includes": []any{}, "jobs": []any{}},
		"/projects/1/repository/tags/v1.0.0": map[string]any{"name": "v1.0.0", "commit": map[string]any{"id": "82b3d5ae55f7080f1e6022629cdb57bfae7cccc7", "short_id": "82b3d5ae"}},
		"/projects/1/issues":                 []any{map[string]any{"id": 301, "iid": 23, "project_id": 1, "title": "New API: create/update/delete file", "state": "opened", "author": map[string]any{"username": "root"}}},
		"/projects/1/pipelines/31":           pipeline,
		"/projects/1/pipelines":              []any{pipeline},
		"/projects/1":                        map[string]any{"id": 1, "path": "gitlab-test", "name": "Gitlab Test", "path_with_namespace": "gitlab-org/gitlab-test", "default_branch": "master", "visibility": "private", "archived": false, "web_url": "http://192.168.64.1:3005/gitlab-org/gitlab-test", "last_activity_at": "2016-08-12T15:23:28Z", "permissions": map[string]any{}},
		"/projects/2/merge_requests":         []any{map[string]any{"id": 93, "iid": 16, "project_id": 2, "title": "Draft: Update README.md", "state": "opened", "draft": true, "author": map[string]any{"username": "root"}, "source_branch": "feature", "web_url": "http://example.com/flightjs/flight-management/-/merge_requests/16", "created_at": "2026-01-16T05:56:22Z", "updated_at": "2026-01-16T05:56:22Z"}},
		"/projects/1/pipelines/31/jobs":      gitlabPayloadJobs(pipeline),
		"/projects/30/deployments/15":        map[string]any{"id": 15, "iid": 2, "sha": "279484c09fbe69ededfced8c1bb6e6d24616b468", "ref": "master", "status": "success", "created_at": "2021-04-28T19:49:00Z", "finished_at": "2021-04-28T19:50:00Z", "environment": map[string]any{"id": 9, "name": "staging"}},
		"/projects/2/releases/v1.1":          map[string]any{"tag_name": "v1.1", "name": "v1.1", "released_at": "2020-11-02T12:55:12Z"},
	}

	tests := []struct{ file, event, seeded, all string }{
		{"push.json", "Push Hook", "repository mike/diaspora", "project mike/diaspora"},
		{"tag_push.json", "Tag Push Hook", "", "tag jsmith/example@v1.0.0"},
		{"issue.json", "Issue Hook", "", "issue gitlabhq/gitlab-test#23"},
		{"merge_request.json", "Merge Request Hook", "merge-request flightjs/flight-management!16", "merge-request flightjs/flight-management!16"},
		{
			"pipeline.json", "Pipeline Hook",
			"pipeline 31, job-attempt 376, job-attempt 377, job-attempt 380, job 31:build, job 31:test, runner 6, runner 7, repository gitlab-org/gitlab-test",
			"pipeline 31, job-attempt 376, job-attempt 377, job-attempt 380, job 376, job 380, runner 6, runner 7, project gitlab-org/gitlab-test",
		},
		{"build.json", "Job Hook", "", ""},
		{"deployment.json", "Deployment Hook", "deployment gitlab-15", "deployment 15"},
		{"release.json", "Release Hook", "", "release gitlab-org/release-webhook-example@v1.1"},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("testdata", "gitlab", test.file))
			if err != nil {
				t.Fatal(err)
			}
			// Each event's project is read again, as the payload names it,
			// unless it's one the objects already have.
			var payload struct{ Project map[string]any }
			_ = json.Unmarshal(body, &payload)
			answers := maps.Clone(objects)
			if key := "/projects/" + idOf(payload.Project["id"]); valueAt(objects, key, "path_with_namespace") != payload.Project["path_with_namespace"] {
				answers[key] = payload.Project
			}
			api := gitlabPayloadAPI(t, answers)
			for _, c := range []struct {
				source, want string
				blueprints   []string
				seed         bool
			}{{seeded, test.seeded, seededBlueprints, true}, {all, test.all, nil, false}} {
				spec, config := gitlabSpec(t, c.source, api, c.blueprints...)
				items, err := receiveGitLab(t, config, test.event, string(body))
				if err != nil {
					t.Fatal(err)
				}
				entries, got := gitlabMapped(t, spec, items)
				if got != c.want {
					t.Errorf("seed %v gives %s, want %s", c.seed, got, c.want)
				}
				for _, entry := range entries {
					if bp, ok := blueprints[entry.Blueprint]; ok && c.seed && !entry.Deleted {
						if _, _, problems := bp.ValidateEntity(entry.Entity); len(problems) > 0 {
							t.Errorf("%s %s: %v", entry.Blueprint, entry.Entity.Identifier, problems)
						}
					}
				}
			}
		})
	}
}

// valueAt reads a nested field of an object.
func valueAt(object map[string]any, keys ...string) any {
	var value any = object
	for _, key := range keys {
		o, _ := value.(map[string]any)
		value = o[key]
	}

	return value
}

// nonJSON names the first value in v of a type JSON doesn't decode to, or
// gives "".
func nonJSON(v any) string {
	switch v := v.(type) {
	case nil, bool, float64, string:
		return ""
	case map[string]any:
		for _, child := range v {
			if bad := nonJSON(child); bad != "" {
				return bad
			}
		}
		return ""
	case []any:
		for _, child := range v {
			if bad := nonJSON(child); bad != "" {
				return bad
			}
		}
		return ""
	}

	return fmt.Sprintf("a %T", v)
}

// TestGitLabWithApprovals checks what a deployment says of its approvals:
// when the last one it needed was given, unless it still waits for one or
// one was a rejection, and nothing where GitLab says nothing of them.
func TestGitLabWithApprovals(t *testing.T) {
	approval := func(username, status, at string) any {
		return map[string]any{"user": map[string]any{"id": float64(1), "username": username, "name": username, "state": "active"}, "status": status, "created_at": at, "comment": ""}
	}
	rules := map[string]any{"rules": []any{map[string]any{"required_approvals": float64(2)}}}
	deployment := func() map[string]any {
		return map[string]any{"id": float64(1), "created_at": "2026-11-04T13:00:00.000Z", "deployable": map[string]any{"started_at": "2026-11-04T13:20:00.500Z"}}
	}
	for _, test := range []struct {
		name string
		one  map[string]any
		want string
	}{
		{"approved", map[string]any{
			"approvals":        []any{approval("ada", "approved", "2026-11-04T13:05:00.000Z"), approval("grace", "approved", "2026-11-04T13:15:00.000Z")},
			"approval_summary": rules, "pending_approval_count": float64(0),
		}, `{"approvalRequired":true,"approvalSeconds":900,"approvedAt":"2026-11-04T13:15:00.000Z","approvedBy":["ada","grace"],"waitSeconds":1200.5}`},
		{"waiting", map[string]any{
			"approvals": []any{approval("ada", "approved", "2026-11-04T13:05:00.000Z")}, "approval_summary": rules, "pending_approval_count": float64(1),
		}, `{"approvalRequired":true,"approvalSeconds":null,"approvedAt":null,"approvedBy":["ada"],"waitSeconds":1200.5}`},
		{"rejected", map[string]any{
			"approvals":        []any{approval("ada", "approved", "2026-11-04T13:05:00.000Z"), approval("grace", "rejected", "2026-11-04T13:15:00.000Z")},
			"approval_summary": rules, "pending_approval_count": float64(0),
		}, `{"approvalRequired":true,"approvalSeconds":null,"approvedAt":null,"approvedBy":["ada"],"waitSeconds":1200.5}`},
		{
			"not needed",
			map[string]any{"approvals": []any{}, "approval_summary": map[string]any{"rules": []any{}}, "pending_approval_count": float64(0)},
			`{"approvalRequired":false,"approvalSeconds":null,"approvedAt":null,"approvedBy":[],"waitSeconds":1200.5}`,
		},
		{"not said", map[string]any{}, `{"approvalRequired":null,"approvalSeconds":null,"approvedAt":null,"approvedBy":null,"waitSeconds":1200.5}`},
	} {
		d := deployment()
		gitlabWithApprovals(d, test.one, "acme/api")
		got := map[string]any{}
		for _, key := range []string{"approvalRequired", "approvedAt", "approvalSeconds", "waitSeconds"} {
			got[key] = valueAt(d, parentKey, key)
		}
		got["approvedBy"] = nil
		if approvals, ok := valueAt(d, parentKey, "approvals").([]any); ok {
			by := []string{}
			for _, a := range approvals {
				if valueAt(a.(map[string]any), "status") == "approved" {
					by = append(by, valueAt(a.(map[string]any), "user", "username").(string))
				}
			}
			got["approvedBy"] = by
		}
		if j, _ := json.Marshal(got); string(j) != test.want {
			t.Errorf("%s: deployment carries %s, want %s", test.name, j, test.want)
		}
		if bad := nonJSON(d); bad != "" {
			t.Errorf("%s: deployment carries %s, which jq can't read", test.name, bad)
		}
	}
}

// TestGitLabPullApprovals checks a sync reads each deployment on its own
// for its approvals, as an event does, except where one of its
// environment's said it needed none, or GitLab said nothing of them.
func TestGitLabPullApprovals(t *testing.T) {
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabProjectYAML("acme/api"), f.url())
	pulled, failed := pullGitLab(t, config, gitlabDeployments)
	if len(failed) != 0 {
		t.Fatalf("failed = %v", failed)
	}
	byID := map[string]map[string]any{}
	for _, d := range pulled[gitlabDeployments] {
		byID[idOf(d["id"])] = d
	}
	production, staging := byID["22"], byID["21"]
	if valueAt(production, parentKey, "approvalRequired") != true || valueAt(production, parentKey, "approvalSeconds") != float64(300) ||
		valueAt(production, parentKey, "approvedAt") != gitlabLater(gitlabT3, 5*time.Minute) || valueAt(production, parentKey, "waitSeconds") != float64(360) ||
		valueAt(production, parentKey, "project") != "acme/api" || production["approvals"] != nil {
		t.Errorf("deployment 22 = %v, want it as GitLab lists it, approved after five minutes and started after six", production)
	}
	if valueAt(staging, parentKey, "approvalRequired") != false {
		t.Errorf("deployment 21 = %v, want it needing no approval", staging)
	}
	items, err := receiveGitLab(t, config, "Deployment Hook", `{"object_kind": "deployment", "deployment_id": 22, `+
		`"project": {"id": 1, "path_with_namespace": "acme/api", "default_branch": "main"}}`)
	if err != nil || len(items) != 1 || !reflect.DeepEqual(items[0].Object, production) {
		t.Errorf("an event gives deployment 22 as %+v, %v; want it as a sync does, %v", items, err, production)
	}

	client, err := newGitLabClient(config)
	if err != nil {
		t.Fatal(err)
	}
	approvals := &gitlabApprovals{client: client, base: "projects/1/", fullPath: "acme/api", none: map[string]bool{}}
	asked := len(f.sent(http.MethodGet, "/projects/1/deployments/21"))
	for _, id := range []float64{21, 21} {
		if err := approvals.add(t.Context(), map[string]any{"id": id, "environment": map[string]any{"name": "staging"}}); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(f.sent(http.MethodGet, "/projects/1/deployments/21")) - asked; n != 1 {
		t.Errorf("read %d staging deployments on their own, want only the first, which said none need approval", n)
	}

	f.answer("/projects/1/deployments/22", func(w http.ResponseWriter) {
		f.reply(w, http.StatusOK, map[string]any{"id": 22, "environment": map[string]any{"name": "production"}})
	})
	approvals = &gitlabApprovals{client: client, base: "projects/1/", fullPath: "acme/api", none: map[string]bool{}}
	asked = len(f.sent(http.MethodGet, "/projects/1/deployments/22"))
	for range 2 {
		d := map[string]any{"id": float64(22), "environment": map[string]any{"name": "production"}}
		if err := approvals.add(t.Context(), d); err != nil || valueAt(d, parentKey, "approvalRequired") != nil {
			t.Errorf("without approvals from GitLab, deployment 22 = %v, %v; want them unknown", d, err)
		}
	}
	if n := len(f.sent(http.MethodGet, "/projects/1/deployments/22")) - asked; n != 1 {
		t.Errorf("read %d deployments on their own, want only the first, which said nothing of approvals", n)
	}
}
