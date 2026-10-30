package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/integration/scmfixture"
)

func scmYAML(provider, config, resource, identifier string) string {
	return fmt.Sprintf(`kind: Integration
metadata: {name: source, title: Source}
spec:
  connector:
    type: %s
    config: %s
  mappings:
    - resource: %s
      blueprint: object
      entity: {identifier: '%s'}
`, provider, config, resource, identifier)
}

func TestSCMExamples(t *testing.T) {
	for _, provider := range []string{"github", "gitlab"} {
		source, err := os.ReadFile("../../examples/" + provider + ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		chunks := configdoc.SplitDocuments(string(source))
		if len(chunks) != 5 {
			t.Fatalf("%s example documents = %d", provider, len(chunks))
		}
		for i, chunk := range chunks {
			if i == len(chunks)-1 {
				mustParse(t, chunk.Source)
			} else if _, issues := blueprint.Parse(chunk.Source); blueprint.HasErrors(issues) {
				t.Fatalf("%s example blueprint %d: %+v", provider, i, issues)
			}
		}
	}
}

func TestSCMConfig(t *testing.T) {
	for _, provider := range []string{"github", "gitlab"} {
		selector, resource := "repositories", "repositories"
		if provider == "gitlab" {
			selector, resource = "projects", "projects"
		}
		base := selector + ": [team/repo]"
		spec := mustParse(t, scmYAML(provider, "{"+base+"}", resource, ".id"))
		if spec.scm.MaxRuns != 20 || !strings.HasPrefix(spec.scm.APIURL, "https://") {
			t.Fatalf("defaults = %+v", spec.scm)
		}
		for _, config := range []string{
			"{}", "{" + selector + ": [../repo]}", "{" + base + ", token: plaintext}",
			"{" + base + ", token: {env: DB_PASSWORD}}", "{" + base + ", maxRuns: 0}",
			"{" + base + ", maxRuns: 1001}", "{" + base + ", apiURL: 'https://user:password@example.com'}",
			"{" + base + ", typo: true}",
		} {
			_, issues := Parse(scmYAML(provider, config, resource, ".id"))
			if len(issues) == 0 {
				t.Errorf("%s accepted invalid config %s", provider, config)
			}
		}
	}
}

func TestGitHubResourcesAndMapping(t *testing.T) {
	t.Setenv("GP_SECRET_GITHUB_TEST", "private-value")
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer private-value" || r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
			t.Errorf("unexpected method/auth/version for %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		body := ""
		switch r.URL.Path {
		case "/orgs/team":
			body = `{"id":1,"login":"team"}`
		case "/orgs/team/repos":
			if r.URL.Query().Get("page") == "2" {
				body = `[{"id":2,"full_name":"team/repo"}]`
			} else {
				w.Header().Set("Link", `<?page=2>; rel="next"`)
				body = `[{"id":2,"full_name":"team/repo"}]`
			}
		case "/repos/team/repo":
			body = `{"id":2,"full_name":"team/repo"}`
		case "/repos/team/repo/branches":
			body = `[{"name":"feature/a","protected":true}]`
		case "/repos/team/repo/actions/workflows":
			body = `{"workflows":[{"id":3,"name":"CI"}]}`
		case "/repos/team/repo/actions/runs":
			body = `{"workflow_runs":[{"id":4},{"id":5}]}`
		case "/repos/team/repo/actions/runs/4/jobs":
			body = `{"jobs":[{"id":6,"name":"test"}]}`
		case "/repos/team/repo/pulls":
			if r.URL.Query().Get("state") != "all" {
				t.Error("missing closed pull requests")
			}
			body = `[{"id":7}]`
		default:
			t.Errorf("unexpected fetch %s", r.URL)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	spec := mustParse(t, scmYAML("github", fmt.Sprintf("{apiURL: %s, organizations: [team], repositories: [team/repo], maxRuns: 1, token: {env: GP_SECRET_GITHUB_TEST}}", server.URL), "branches", `._gp.repository + "/" + .name`))
	resources := []string{"organizations", "repositories", "branches", "workflows", "workflow_runs", "jobs", "pull_requests"}
	pulled, err := githubPull(context.Background(), nil, spec, resources)
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range resources {
		if len(pulled[resource].([]any)) != 1 {
			t.Errorf("%s = %v", resource, pulled[resource])
		}
	}
	if calls["/repos/team/repo/branches"] != 1 || calls["/orgs/team/repos"] != 2 {
		t.Fatalf("pagination/deduplication: %v", calls)
	}
	job := pulled["jobs"].([]any)[0].(map[string]any)
	if job["_gp"].(map[string]any)["run_id"] != float64(4) {
		t.Fatalf("job parent = %v", job)
	}
	items, err := spec.Map(pulled)
	if err != nil || len(items) != 1 || items[0].Entity.Identifier != "team/repo/feature/a" {
		t.Fatalf("JQ mapping = %v, %v", items, err)
	}
	clear(calls)
	if _, err := githubPull(context.Background(), nil, spec, []string{"organizations"}); err != nil || len(calls) != 1 {
		t.Fatalf("selected resources fetched %v: %v", calls, err)
	}
}

func TestGitLabResourcesAndMapping(t *testing.T) {
	t.Setenv("GP_SECRET_GITLAB_TEST", "private-value")
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.EscapedPath()
		calls[path]++
		if r.Method != "GET" || r.Header.Get("PRIVATE-TOKEN") != "private-value" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected method/auth for %s", path)
		}
		body := ""
		switch path {
		case "/api/v4/groups/team%2Fsub":
			body = `{"id":1}`
		case "/api/v4/groups/team%2Fsub/projects":
			if r.URL.Query().Get("include_subgroups") != "true" || r.URL.Query().Get("with_shared") != "false" {
				t.Error("group project scope is wrong")
			}
			if r.URL.Query().Get("page") != "2" {
				w.Header().Set("X-Next-Page", "2")
			}
			body = `[{"id":2,"path_with_namespace":"team/sub/repo"}]`
		case "/api/v4/projects/team%2Fsub%2Frepo":
			body = `{"id":2,"path_with_namespace":"team/sub/repo"}`
		case "/api/v4/projects/2/repository/branches":
			body = `[{"name":"main"}]`
		case "/api/v4/projects/2/pipelines":
			body = `[{"id":3},{"id":4}]`
		case "/api/v4/projects/2/pipelines/3":
			body = `{"id":3,"duration":42,"status":"success"}`
		case "/api/v4/projects/2/pipelines/3/jobs":
			body = `[{"id":5,"name":"test"}]`
		case "/api/v4/projects/2/merge_requests":
			if r.URL.Query().Get("scope") != "all" || r.URL.Query().Get("state") != "all" {
				t.Error("merge request scope is wrong")
			}
			body = `[{"id":6}]`
		default:
			t.Errorf("unexpected fetch %s", r.URL)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	spec := mustParse(t, scmYAML("gitlab", fmt.Sprintf("{apiURL: %s/api/v4, groups: [team/sub], projects: [team/sub/repo], maxRuns: 1, token: {env: GP_SECRET_GITLAB_TEST}}", server.URL), "branches", `._gp.project + "/" + .name`))
	resources := []string{"groups", "projects", "branches", "pipelines", "jobs", "merge_requests"}
	pulled, err := gitlabPull(context.Background(), nil, spec, resources)
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range resources {
		if len(pulled[resource].([]any)) != 1 {
			t.Errorf("%s = %v", resource, pulled[resource])
		}
	}
	if calls["/api/v4/projects/2/repository/branches"] != 1 || calls["/api/v4/groups/team%2Fsub/projects"] != 2 {
		t.Fatalf("pagination/deduplication: %v", calls)
	}
	pipeline := pulled["pipelines"].([]any)[0].(map[string]any)
	if pipeline["duration"] != float64(42) || pipeline["_gp"].(map[string]any)["project"] != "team/sub/repo" {
		t.Fatalf("pipeline details/parent = %v", pipeline)
	}
	items, err := spec.Map(pulled)
	if err != nil || len(items) != 1 || items[0].Entity.Identifier != "team/sub/repo/main" {
		t.Fatalf("JQ mapping = %v, %v", items, err)
	}
}

func TestSCMFailures(t *testing.T) {
	for _, tt := range []struct {
		name, body, link, next, want string
		status                       int
	}{
		{name: "invalid JSON", body: "<html>", want: "invalid JSON"},
		{name: "wrong shape", body: `{}`, want: "expected an array"},
		{name: "wrong item", body: `[1]`, want: "non-object"},
		{name: "foreign pagination", body: `[]`, link: `<https://evil.invalid/steal>; rel="next"`, want: "API root"},
		{name: "outside root", body: `[]`, link: `</outside>; rel="next"`, want: "API root"},
		{name: "loop", body: `[]`, link: `<?per_page=100>; rel="next"`, want: "loop"},
		{name: "bad page", body: `[]`, next: "bad", want: "invalid next page"},
		{name: "unauthorized", body: "private-value", status: 401, want: "401"},
		{name: "forbidden", status: 403, want: "403"},
		{name: "server error", status: 503, want: "retry the sync later"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Link", tt.link)
				w.Header().Set("X-Next-Page", tt.next)
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			c, err := newSCMClient(&SCMConfig{APIURL: server.URL + "/api/v4"}, "gitlab")
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.list(context.Background(), "items", "", 0)
			if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), "private-value") {
				t.Fatalf("error = %v, want %s without response secrets", err, tt.want)
			}
			wantCalls := 1
			if tt.status == 503 {
				wantCalls = 3
			}
			if calls != wantCalls {
				t.Fatalf("calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestSCMRedirectAndMissingSecret(t *testing.T) {
	t.Setenv("GP_SECRET_MISSING_TEST", "")
	if _, err := newSCMClient(&SCMConfig{APIURL: "https://example.com", TokenEnv: "GP_SECRET_MISSING_TEST"}, "github"); err == nil {
		t.Fatal("missing configured secret accepted")
	}
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	c, err := newSCMClient(&SCMConfig{APIURL: source.URL}, "github")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.get(context.Background(), source.URL+"/items"); err == nil || forwarded {
		t.Fatalf("redirect followed: %v, %v", forwarded, err)
	}

	t.Setenv("GP_SECRET_RENAMED_TEST", "private-value")
	renamed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Header.Get("Authorization") != "Bearer private-value":
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/api/repos/old/name":
			http.Redirect(w, r, "/api/repositories/7", http.StatusMovedPermanently)
		case r.URL.Path == "/elsewhere":
			forwarded = true
		case r.URL.Path == "/api/repositories/7":
			_, _ = w.Write([]byte(`{"id":7,"full_name":"new/name"}`))
		case r.URL.Path == "/api/repos/leaves/root":
			http.Redirect(w, r, "/elsewhere", http.StatusMovedPermanently)
		}
	}))
	defer renamed.Close()
	c, err = newSCMClient(&SCMConfig{APIURL: renamed.URL + "/api", TokenEnv: "GP_SECRET_RENAMED_TEST"}, "github")
	if err != nil {
		t.Fatal(err)
	}
	if obj, err := c.object(context.Background(), "repos/old/name"); err != nil || obj["full_name"] != "new/name" {
		t.Fatalf("renamed repository: %v, %v", obj, err)
	}
	if _, err := c.object(context.Background(), "repos/leaves/root"); err == nil || forwarded {
		t.Fatalf("redirect outside the API root followed: %v, %v", forwarded, err)
	}
}

func TestSCMRateLimitsAndCancellation(t *testing.T) {
	for _, retry := range []string{"0", "120"} {
		t.Run(retry, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls == 1 {
					w.Header().Set("Retry-After", retry)
					w.WriteHeader(429)
					return
				}
				_, _ = w.Write([]byte(`[]`))
			}))
			defer server.Close()
			c, _ := newSCMClient(&SCMConfig{APIURL: server.URL}, "github")
			_, err := c.list(context.Background(), "items", "", 0)
			if retry == "0" && (err != nil || calls != 2) {
				t.Fatalf("recovery: %d, %v", calls, err)
			}
			if retry == "120" && (err == nil || calls != 1) {
				t.Fatalf("long wait: %d, %v", calls, err)
			}
		})
	}
	for name, limit := range map[string]func(http.Header){
		"reset soon": func(h http.Header) {
			h.Set("X-RateLimit-Remaining", "0")
			h.Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Unix()))
		},
		"reset later": func(h http.Header) {
			h.Set("X-RateLimit-Remaining", "0")
			h.Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(time.Hour).Unix()))
		},
		"retry-after date":    func(h http.Header) { h.Set("Retry-After", time.Now().Add(-time.Second).UTC().Format(http.TimeFormat)) },
		"secondary, no hints": func(http.Header) {},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls == 1 {
					limit(w.Header())
					w.WriteHeader(http.StatusForbidden)
					return
				}
				_, _ = w.Write([]byte(`[]`))
			}))
			defer server.Close()
			c, _ := newSCMClient(&SCMConfig{APIURL: server.URL}, "github")
			_, err := c.list(context.Background(), "items", "", 0)
			// A 403 without rate-limit headers is a permission problem, not worth retrying.
			recovers := name == "reset soon" || name == "retry-after date"
			if recovers && (err != nil || calls != 2) {
				t.Fatalf("recovery: %d, %v", calls, err)
			}
			if !recovers && (err == nil || calls != 1) {
				t.Fatalf("gave up: %d, %v", calls, err)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Retry-After", "10"); w.WriteHeader(429) }))
	defer server.Close()
	c, _ := newSCMClient(&SCMConfig{APIURL: server.URL}, "gitlab")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.list(ctx, "items", "", 0); err == nil {
		t.Fatal("cancelled sync succeeded")
	}
}

func TestSCMPageBoundsAndIncompleteSnapshots(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pages":
			w.Header().Set("X-Next-Page", fmt.Sprint(time.Now().UnixNano()))
			_, _ = w.Write([]byte(`[]`))
		case "/large":
			_, _ = w.Write([]byte(strings.Repeat(" ", maxPageBytes+1)))
		case "/repos/team/repo":
			_, _ = w.Write([]byte(`{"id":1,"full_name":"team/repo"}`))
		default:
			w.WriteHeader(401)
		}
	}))
	defer server.Close()
	c, _ := newSCMClient(&SCMConfig{APIURL: server.URL}, "github")
	for path, want := range map[string]string{"pages": "exceeds 100 pages", "large": "exceeds 10 MB"} {
		if _, err := c.list(context.Background(), path, "", 0); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", path, err)
		}
	}
	spec := mustParse(t, scmYAML("github", fmt.Sprintf("{apiURL: %s, repositories: [team/repo]}", server.URL), "branches", ".name"))
	got, err := githubPull(context.Background(), nil, spec, []string{"repositories", "branches"})
	if err == nil || got != nil {
		t.Fatalf("incomplete snapshot returned data: %v, %v", got, err)
	}
}

// TestSCMFixtures runs the shipped example mappings over the fixtures the dev
// stack's mock serves, through a private token and paginated discovery.
func TestSCMFixtures(t *testing.T) {
	server := httptest.NewServer(scmfixture.Handler("github-fixture", "gitlab-fixture"))
	defer server.Close()
	t.Setenv("GP_SECRET_GITHUB_FIXTURE", "github-fixture")
	t.Setenv("GP_SECRET_GITLAB_FIXTURE", "gitlab-fixture")
	t.Setenv("GP_SECRET_WRONG_FIXTURE", "wrong")
	for _, tc := range []struct {
		provider, selectors, apiPath string
		pull                         puller
		want                         []string
	}{
		{
			"github", "organizations: [acme]\n      repositories: [acme/payments-api]", "/github", githubPull,
			[]string{"acme/payments-api", "acme/checkout-web", "acme/payments-api/main", "acme/payments-api/feature/refunds", "acme/checkout-web/main", "9003", "9002", "9001", "9101", "10031", "10021", "10022", "10011", "11011"},
		},
		{
			"gitlab", "groups: [acme]\n      projects: ['81']", "/gitlab/api/v4", gitlabPull,
			[]string{"81", "82", "81/main", "81/fix/rounding", "82/main", "5102", "5101", "5201", "6201", "6102", "6101", "6301"},
		},
	} {
		source, err := os.ReadFile("../../examples/" + tc.provider + ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		chunks := configdoc.SplitDocuments(string(source))
		integration := chunks[len(chunks)-1].Source
		selector := map[string]string{"github": "repositories: [example/repository]", "gitlab": "projects: [example/project]"}[tc.provider]
		configure := func(env string) Spec {
			config := fmt.Sprintf("apiURL: '%s%s'\n      %s\n      token: {env: %s}", server.URL, tc.apiPath, tc.selectors, env)
			return mustParse(t, strings.Replace(integration, selector, config, 1))
		}
		spec := configure("GP_SECRET_" + strings.ToUpper(tc.provider) + "_FIXTURE")
		// Every resource, not just the mapped ones, so the fixture's project with
		// merge requests turned off shows that disabled features are skipped.
		c, _ := connector(tc.provider)
		resources := c.Resources
		pulled, err := tc.pull(context.Background(), nil, spec, resources)
		if err != nil {
			t.Fatalf("%s pull: %v", tc.provider, err)
		}
		if tc.provider == "gitlab" && len(pulled["merge_requests"].([]any)) != 1 {
			t.Errorf("gitlab merge requests = %v", pulled["merge_requests"])
		}
		data, err := toJSONValue(pulled)
		if err != nil {
			t.Fatal(err)
		}
		items, err := spec.Map(data)
		if err != nil {
			t.Fatalf("%s map: %v", tc.provider, err)
		}
		got := []string{}
		for _, item := range items {
			if len(item.Problems) > 0 {
				t.Errorf("%s %s problems: %v", tc.provider, item.Entity.Identifier, item.Problems)
			}
			got = append(got, item.Entity.Identifier)
		}
		if strings.Join(got, " ") != strings.Join(tc.want, " ") {
			t.Errorf("%s entities = %v, want %v", tc.provider, got, tc.want)
		}
		wrong := configure("GP_SECRET_WRONG_FIXTURE")
		if _, err := tc.pull(context.Background(), nil, wrong, resources); err == nil || !strings.Contains(err.Error(), "401") {
			t.Errorf("%s accepted a wrong token: %v", tc.provider, err)
		}
	}
}
