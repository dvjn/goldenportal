package integration

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// gitlabFixture is a GitLab API with a group, its subgroup and a project
// elsewhere. It takes one access token, pages every list two at a time
// with Link headers, filters as GitLab does by state, updated_after and
// the like, answers about single objects, and keeps each project's
// webhooks.
type gitlabFixture struct {
	t      *testing.T
	server *httptest.Server

	// admin makes the token's user an administrator, and access is its
	// role in acme, or 0 for none.
	admin  bool
	access int
	// adminMode is whether Admin Mode is on, when an administrator's token
	// sees everything only with its scope.
	adminMode bool
	// archived is the ID of a project that's archived, if any, and noCI one
	// without a CI configuration file.
	archived, noCI string

	// scopes are the token's, or nil when the instance doesn't say.
	scopes []string

	mu       sync.Mutex
	requests []string
	// answers are canned answers to give a path, in turn, before its own.
	answers map[string][]func(http.ResponseWriter)
	// denied are path suffixes refused for want of access.
	denied []string
	// hooks are the webhooks, by the path that lists them, and tokens
	// their secret tokens, which GitLab never shows, by URL.
	hooks  map[string][]map[string]any
	tokens map[string]string
	// deletedHooks counts those deleted, so a new one's ID is new.
	deletedHooks int
	// inFlight and mostInFlight count the requests being answered.
	inFlight, mostInFlight int
}

const gitlabFixtureToken = "glpat-fixture"

// The times the fixture's objects were made or updated, oldest first. Only
// gitlabT0 is older than a sync reads by default.
var (
	gitlabT0 = gitlabDaysAgo(60)
	gitlabT2 = gitlabDaysAgo(3)
	gitlabT3 = gitlabDaysAgo(1)
)

func gitlabDaysAgo(days int) string {
	return time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339)
}

func newGitLabFixture(t *testing.T) *gitlabFixture {
	t.Helper()
	f := &gitlabFixture{
		t: t, access: 40, scopes: []string{"api"},
		answers: map[string][]func(http.ResponseWriter){}, hooks: map[string][]map[string]any{}, tokens: map[string]string{},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	t.Setenv("GP_SECRET_GITLAB_TOKEN", gitlabFixtureToken)

	return f
}

// url is the fixture's instance, whose API is under /api/v4.
func (f *gitlabFixture) url() string { return f.server.URL }

// answer has path, as escaped, answered by each of answers in turn, before
// its own answer.
func (f *gitlabFixture) answer(path string, answers ...func(http.ResponseWriter)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers[path] = append(f.answers[path], answers...)
}

func (f *gitlabFixture) deny(suffix string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.denied = append(f.denied, suffix)
}

func (f *gitlabFixture) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

// sent is each query of a request with method to path, in order.
func (f *gitlabFixture) sent(method, path string) []string {
	var out []string
	for _, r := range f.asked() {
		if p, q, _ := strings.Cut(r, "?"); p == method+" "+path {
			out = append(out, q)
		}
	}
	return out
}

func gitlabTooMany(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "0")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte("Retry later"))
}

// gitlabSpent is the token's limit spent, which GitLab's HTTP layer names.
func gitlabSpent(reset time.Time) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("RateLimit-Name", "throttle_authenticated_api")
		w.Header().Set("RateLimit-Remaining", "0")
		w.Header().Set("RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(reset).Seconds())))
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("Retry later"))
	}
}

func gitlabNotFound(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"message": "404 Not found"}`))
}

func (f *gitlabFixture) reply(w http.ResponseWriter, status int, body any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (f *gitlabFixture) serve(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.EscapedPath(), "/api/v4")
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+path+"?"+r.URL.RawQuery)
	var canned func(http.ResponseWriter)
	if queue := f.answers[path]; len(queue) > 0 {
		canned, f.answers[path] = queue[0], queue[1:]
	}
	denied := slices.ContainsFunc(f.denied, func(suffix string) bool { return strings.HasSuffix(path, suffix) })
	f.inFlight++
	f.mostInFlight = max(f.mostInFlight, f.inFlight)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "application/json")
	if canned != nil {
		canned(w)
		return
	}
	if r.Header.Get("PRIVATE-TOKEN") != gitlabFixtureToken {
		f.reply(w, http.StatusUnauthorized, map[string]any{"message": "401 Unauthorized"})
		return
	}
	if denied {
		f.reply(w, http.StatusForbidden, map[string]any{"message": "403 Forbidden"})
		return
	}
	// GitLab's paths ignore case.
	f.serveData(w, r, strings.ToLower(path))
}

// page answers one page of objects, two at a time, linking the next page.
func (f *gitlabFixture) page(w http.ResponseWriter, r *http.Request, objects []map[string]any) {
	size := 2
	if n, err := strconv.Atoi(r.URL.Query().Get("per_page")); err == nil && n < size {
		size = n
	}
	page := 1
	if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil {
		page = n
	}
	from := min((page-1)*size, len(objects))
	to := min(from+size, len(objects))
	if to < len(objects) {
		next := *r.URL
		q := next.Query()
		q.Set("page", strconv.Itoa(page+1))
		next.RawQuery = q.Encode()
		w.Header().Set("Link", fmt.Sprintf(`<%s%s>; rel="next", <%s>; rel="first"`, f.server.URL, next.RequestURI(), f.server.URL))
	}
	f.reply(w, http.StatusOK, objects[from:to])
}

// acme/platform makes merge requests wait for their pipelines, for every
// project beneath it.
func gitlabFixtureGroup(id float64, fullPath string) map[string]any {
	name := fullPath[strings.LastIndex(fullPath, "/")+1:]
	return map[string]any{
		"id": id, "name": strings.ToUpper(name[:1]) + name[1:], "path": name, "full_path": fullPath, "web_url": "https://gitlab.example.com/groups/" + fullPath,
		"only_allow_merge_if_pipeline_succeeds": fullPath == "acme/platform",
	}
}

// gitlabFixtureProjects are the fixture's projects. acme/platform/web has
// issues turned off.
func gitlabFixtureProject(id string) map[string]any {
	paths := map[string]string{"1": "acme/api", "2": "acme/platform/web", "3": "octo/tools"}
	fullPath, ok := paths[id]
	if !ok {
		return nil
	}
	n, _ := strconv.Atoi(id)
	p := map[string]any{
		"id": float64(n), "path_with_namespace": fullPath, "name": fullPath[strings.LastIndex(fullPath, "/")+1:], "default_branch": "main",
		"archived": false, "empty_repo": false, "last_activity_at": gitlabT3, "issues_access_level": "enabled", "builds_access_level": "enabled",
		"repository_access_level": "enabled", "merge_requests_access_level": "enabled", "environments_access_level": "enabled",
		"namespace": map[string]any{"full_path": fullPath[:strings.LastIndex(fullPath, "/")]}, "only_allow_merge_if_all_discussions_are_resolved": id == "3",
	}
	if id == "2" {
		p["issues_access_level"] = "disabled"
	}
	return p
}

func (f *gitlabFixture) serveData(w http.ResponseWriter, r *http.Request, path string) {
	groups := map[string]map[string]any{"acme": gitlabFixtureGroup(10, "acme"), "acme%2fplatform": gitlabFixtureGroup(11, "acme/platform")}
	groups["10"], groups["11"] = groups["acme"], groups["acme%2fplatform"]
	// acme was called old-acme, which GitLab follows.
	groups["old-acme"] = groups["acme"]
	members := map[string][]map[string]any{
		"10": {{"id": float64(1), "username": "ada", "access_level": float64(50)}, {"id": float64(2), "username": "grace", "access_level": float64(30)}},
		"11": {{"id": float64(3), "username": "linus", "access_level": float64(30)}},
	}
	projects := map[string]string{"acme%2fapi": "1", "acme%2fplatform%2fweb": "2", "octo%2ftools": "3", "octo%2fold-tools": "3", "1": "1", "2": "2", "3": "3"}

	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	switch {
	case path == "/api/graphql":
		f.vulnerabilities(w, r)
	case path == "/user":
		user := map[string]any{"id": float64(7), "username": "portal-bot"}
		if f.admin {
			user["is_admin"] = true
		}
		f.reply(w, http.StatusOK, user)
	case path == "/application/settings":
		if !f.admin || f.adminMode && !slices.Contains(f.scopes, "admin_mode") {
			f.reply(w, http.StatusForbidden, map[string]any{"message": "403 Forbidden"})
			return
		}
		f.reply(w, http.StatusOK, map[string]any{"admin_mode": f.adminMode})
	case path == "/personal_access_tokens/self":
		if f.scopes == nil {
			f.reply(w, http.StatusNotFound, map[string]any{"error": "404 Not Found"})
			return
		}
		f.reply(w, http.StatusOK, map[string]any{"id": 1, "scopes": f.scopes, "active": true})
	case parts[0] == "groups" && len(parts) == 2 && groups[parts[1]] != nil:
		group := maps.Clone(groups[parts[1]])
		group["runners_token"], group["shared_with_groups"] = "glrt-secret", []any{}
		f.reply(w, http.StatusOK, group)
	case parts[0] == "groups" && len(parts) == 3 && parts[2] == "descendant_groups" && parts[1] == "10":
		f.page(w, r, []map[string]any{groups["11"]})
	case parts[0] == "groups" && len(parts) == 3 && parts[2] == "members" && members[parts[1]] != nil:
		f.page(w, r, members[parts[1]])
	case (parts[0] == "groups" || parts[0] == "projects") && len(parts) == 5 && parts[2] == "members" && parts[3] == "all":
		if f.access == 0 || parts[4] != "7" {
			gitlabNotFound(w)
			return
		}
		f.reply(w, http.StatusOK, map[string]any{"id": float64(7), "username": "portal-bot", "access_level": f.access})
	case parts[0] == "groups" && len(parts) == 3 && parts[2] == "projects" && (parts[1] == "10" || parts[1] == "11"):
		if r.URL.Query().Get("include_subgroups") != "true" && parts[1] == "10" {
			f.page(w, r, []map[string]any{f.project("1")})
			return
		}
		ids := []string{"1", "2"}
		if parts[1] == "11" {
			ids = ids[1:]
		}
		var out []map[string]any
		for _, id := range ids {
			if p := f.project(id); r.URL.Query().Get("archived") != "false" || p["archived"] != true {
				out = append(out, p)
			}
		}
		f.page(w, r, out)
	case parts[0] == "projects" && len(parts) >= 2 && projects[parts[1]] != "":
		id := projects[parts[1]]
		if len(parts) == 2 {
			p := f.project(id)
			p["cicd_catalog_enabled"], p["import_error"] = false, nil
			f.reply(w, http.StatusOK, p)
			return
		}
		f.serveProject(w, r, id, strings.Join(parts[2:], "/"))
	default:
		gitlabNotFound(w)
	}
}

func (f *gitlabFixture) serveProject(w http.ResponseWriter, r *http.Request, id, rest string) {
	q := r.URL.Query()
	object := func(n float64, at string, more ...any) map[string]any {
		o := map[string]any{"id": n, "iid": n, "updated_at": at, "created_at": at, "state": "opened"}
		for i := 0; i+1 < len(more); i += 2 {
			o[more[i].(string)] = more[i+1]
		}
		return o
	}
	if rest == "hooks" || strings.HasPrefix(rest, "hooks/") {
		f.serveHooks(w, r, "/projects/"+id+"/hooks", strings.TrimPrefix(strings.TrimPrefix(rest, "hooks"), "/"))
		return
	}
	branches := []map[string]any{{"name": "main", "protected": true, "commit": map[string]any{"id": "ma1n"}}, {"name": "dev", "commit": map[string]any{"id": "d3v"}}, {"name": "feature"}}
	if id != "1" {
		branches = branches[:1]
	}
	// Each list is in the order GitLab gives it, newest first.
	mergeRequests := []map[string]any{
		object(4, gitlabT0), object(3, gitlabT3, "allow_collaboration", true), object(2, gitlabT2, "state", "merged", "merged_at", gitlabT2), object(1, gitlabT0, "state", "closed", "closed_at", gitlabT0),
	}
	issues := []map[string]any{
		object(9, gitlabT3), object(8, gitlabT2, "state", "closed", "closed_at", gitlabT2), object(7, gitlabT0, "state", "closed", "closed_at", gitlabT0), object(6, gitlabT0),
	}
	// 14 is a child pipeline, which GitLab lists only when asked for them.
	pipelines := []map[string]any{
		object(14, gitlabT3, "ref", "main", "source", "parent_pipeline"), object(13, gitlabT3, "ref", "main", "status", "success"),
		object(12, gitlabT2, "ref", "dev"), object(11, gitlabT0, "ref", "main"),
	}
	deployments := []map[string]any{
		object(22, gitlabT3, "environment", map[string]any{"name": "production"}, "status", "success"),
		object(21, gitlabT0, "environment", map[string]any{"name": "staging"}, "status", "success"),
	}
	lists := map[string][]map[string]any{
		"repository/branches": branches,
		"repository/tags":     {{"name": "v3"}, {"name": "v2"}, {"name": "v1"}},
		"releases":            {{"tag_name": "v2", "released_at": gitlabT2}, {"tag_name": "v1", "released_at": gitlabT0}},
		"pipelines": slices.DeleteFunc(slices.Clone(pipelines), func(p map[string]any) bool {
			return (p["source"] == "parent_pipeline") != (q.Get("source") == "parent_pipeline") || q.Has("ref") && p["ref"] != q.Get("ref")
		}),
		"environments": {{"id": float64(1), "name": "production"}, {"id": float64(2), "name": "staging"}},
	}
	switch {
	case rest == "issues" && id == "2":
		f.reply(w, http.StatusForbidden, map[string]any{"message": "403 Forbidden"})
	case rest == "merge_requests" || rest == "issues":
		all := mergeRequests
		if rest == "issues" {
			all = issues
		}
		f.page(w, r, gitlabStateFiltered(all, q))
	case rest == "environments" && q.Get("name") != "":
		f.page(w, r, slices.DeleteFunc(slices.Clone(lists[rest]), func(e map[string]any) bool { return e["name"] != q.Get("name") }))
	case rest == "deployments":
		if env := q.Get("environment"); env != "" {
			f.page(w, r, slices.DeleteFunc(slices.Clone(deployments), func(d map[string]any) bool { return valueAt(d, "environment", "name") != env }))
			return
		}
		f.page(w, r, deployments)
	// acme/api's scanners found four dependencies, one under either of two
	// licenses, one with no license known and one GitLab flags as malware,
	// and an image's OS package.
	case rest == "dependencies" && id == "1":
		f.page(w, r, []map[string]any{
			{"name": "a", "licenses": []any{map[string]any{"spdx_identifier": "MIT"}}},
			{"name": "b", "licenses": []any{map[string]any{"spdx_identifier": "GPL-3.0-only"}}, "malware": true},
			{"name": "c", "licenses": []any{}},
			{"name": "d", "licenses": []any{map[string]any{"spdx_identifier": "GPL-2.0-only"}, map[string]any{"spdx_identifier": "EPL-2.0"}}},
			{"name": "busybox", "package_manager": "apk", "licenses": []any{map[string]any{"spdx_identifier": "GPL-2.0-only"}}},
		})
	case rest == "repository/tree" && q.Get("path") == ".gitlab":
		f.page(w, r, []map[string]any{{"path": ".gitlab/CODEOWNERS", "type": "blob"}})
	case rest == "repository/tree":
		f.page(w, r, []map[string]any{{"path": ".gitlab", "type": "tree"}, {"path": "README.md", "type": "blob"}, {"path": "src", "type": "tree"}})
	case lists[rest] != nil:
		f.page(w, r, lists[rest])
	case strings.HasPrefix(rest, "pipelines/") && strings.HasSuffix(rest, "/jobs"):
		n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rest, "pipelines/"), "/jobs"))
		f.page(w, r, []map[string]any{f.job(n*10, pipelines)})
	case strings.HasPrefix(rest, "jobs/"):
		n, _ := strconv.Atoi(strings.TrimPrefix(rest, "jobs/"))
		if job := f.job(n, pipelines); job != nil {
			f.reply(w, http.StatusOK, job)
			return
		}
		gitlabNotFound(w)
	case strings.HasPrefix(rest, "pipelines/"):
		for _, p := range pipelines {
			if idOf(p["id"]) == strings.TrimPrefix(rest, "pipelines/") {
				p = maps.Clone(p)
				p["duration"], p["started_at"] = float64(60), p["created_at"]
				f.reply(w, http.StatusOK, p)
				return
			}
		}
		gitlabNotFound(w)
	case rest == "ci/lint" && id == f.noCI:
		f.reply(w, http.StatusOK, map[string]any{"valid": false, "errors": []any{"Please provide content of .gitlab-ci.yml"}, "warnings": []any{}, "includes": nil, "jobs": []any{}})
	case rest == "ci/lint":
		f.reply(w, http.StatusOK, map[string]any{
			"valid": true, "errors": []any{}, "warnings": []any{}, "includes": []any{}, "merged_yaml": "build: ...",
			"jobs": []any{map[string]any{"name": "build", "stage": "build", "script": []any{"make"}, "before_script": []any{"setup"}}},
		})
	case rest == "jobs":
		f.page(w, r, []map[string]any{f.job(130, pipelines)})
	case strings.HasPrefix(rest, "repository/branches/"):
		name, _ := url.PathUnescape(strings.TrimPrefix(rest, "repository/branches/"))
		f.single(w, branches, "name", name)
	case strings.HasPrefix(rest, "repository/tags/"):
		name, _ := url.PathUnescape(strings.TrimPrefix(rest, "repository/tags/"))
		f.single(w, lists["repository/tags"], "name", name)
	case strings.HasPrefix(rest, "releases/"):
		name, _ := url.PathUnescape(strings.TrimPrefix(rest, "releases/"))
		f.single(w, lists["releases"], "tag_name", name)
	case strings.HasPrefix(rest, "merge_requests/"):
		mr := f.find(mergeRequests, "iid", strings.TrimPrefix(rest, "merge_requests/"))
		if mr != nil {
			mr["subscribed"], mr["diff_refs"] = true, map[string]any{}
			delete(mr, "allow_collaboration")
		}
		f.replyFound(w, mr)
	case strings.HasPrefix(rest, "issues/"):
		f.replyFound(w, f.find(issues, "iid", strings.TrimPrefix(rest, "issues/")))
	case strings.HasPrefix(rest, "deployments/"):
		d := f.find(deployments, "id", strings.TrimPrefix(rest, "deployments/"))
		if d != nil {
			d["approvals"], d["pending_approval_count"] = []any{}, 0
		}
		f.replyFound(w, d)
	default:
		gitlabNotFound(w)
	}
}

// job is the job of a pipeline by ID, ten times the pipeline's, with the
// pipeline as GitLab shows it on a job.
func (f *gitlabFixture) job(id int, pipelines []map[string]any) map[string]any {
	for _, p := range pipelines {
		if idOf(p["id"]) == strconv.Itoa(id/10) {
			return map[string]any{
				"id": float64(id), "name": "build", "status": "success", "created_at": p["created_at"],
				"pipeline": map[string]any{"id": p["id"], "created_at": p["created_at"], "ref": p["ref"]},
			}
		}
	}
	return nil
}

func (f *gitlabFixture) find(objects []map[string]any, key, value string) map[string]any {
	for _, o := range objects {
		if v, ok := o[key].(string); ok && v == value || idOf(o[key]) == value {
			return maps.Clone(o)
		}
	}
	return nil
}

func (f *gitlabFixture) single(w http.ResponseWriter, objects []map[string]any, key, value string) {
	f.replyFound(w, f.find(objects, key, value))
}

func (f *gitlabFixture) replyFound(w http.ResponseWriter, object map[string]any) {
	if object == nil {
		gitlabNotFound(w)
		return
	}
	f.reply(w, http.StatusOK, object)
}

// gitlabStateFiltered filters merge requests or issues by ?state= and
// ?updated_after=, and sorts them by update, newest first, if asked to.
func gitlabStateFiltered(objects []map[string]any, q url.Values) []map[string]any {
	state := q.Get("state")
	out := slices.DeleteFunc(slices.Clone(objects), func(o map[string]any) bool {
		return state != "" && state != "all" && o["state"] != state ||
			q.Has("iids[]") && !slices.Contains(q["iids[]"], idOf(o["iid"])) ||
			q.Get("updated_after") != "" && o["updated_at"].(string) < q.Get("updated_after")
	})
	if q.Get("order_by") == "updated_at" {
		slices.SortStableFunc(out, func(a, b map[string]any) int {
			return strings.Compare(b["updated_at"].(string), a["updated_at"].(string))
		})
	}
	return out
}

// serveHooks keeps the webhooks listed at list, as GitLab's API does,
// never showing their tokens.
func (f *gitlabFixture) serveHooks(w http.ResponseWriter, r *http.Request, list, id string) {
	var body map[string]any
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.reply(w, http.StatusBadRequest, map[string]any{"error": "body is invalid"})
			return
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	shown := func(hook map[string]any) map[string]any {
		out := maps.Clone(hook)
		delete(out, "token")
		out["token_present"] = hook["token"] != nil
		return out
	}
	switch {
	case r.Method == http.MethodGet && id == "":
		hooks := make([]map[string]any, len(f.hooks[list]))
		for i, hook := range f.hooks[list] {
			hooks[i] = shown(hook)
		}
		f.page(w, r, hooks)
	case r.Method == http.MethodPost && id == "":
		hook := map[string]any{"id": float64(100 + len(f.hooks[list]) + f.deletedHooks), "alert_status": "executable", "push_events": true}
		maps.Copy(hook, body)
		f.hooks[list] = append(f.hooks[list], hook)
		f.reply(w, http.StatusCreated, shown(hook))
	case r.Method == http.MethodGet:
		for _, hook := range f.hooks[list] {
			if idOf(hook["id"]) == id {
				f.reply(w, http.StatusOK, shown(hook))
				return
			}
		}
		gitlabNotFound(w)
	case r.Method == http.MethodDelete:
		f.hooks[list] = slices.DeleteFunc(f.hooks[list], func(hook map[string]any) bool { return idOf(hook["id"]) == id })
		f.deletedHooks++
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		for _, hook := range f.hooks[list] {
			if idOf(hook["id"]) == id {
				maps.Copy(hook, body)
				f.reply(w, http.StatusOK, shown(hook))
				return
			}
		}
		gitlabNotFound(w)
	default:
		gitlabNotFound(w)
	}
}

// hook is the webhook listed at list, at i, with its token.
func (f *gitlabFixture) hook(list string, i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hooks[list][i]
}

func gitlabServerError(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) }

// project is one of the fixture's projects, archived if it's the one the
// fixture says.
func (f *gitlabFixture) project(id string) map[string]any {
	p := gitlabFixtureProject(id)
	p["permissions"] = map[string]any{"project_access": map[string]any{"access_level": 40}}
	if f.archived == id {
		p["archived"] = true
	}
	return p
}

// vulnerabilities answers the vulnerability query: acme/api has some, and
// the token can't see octo/tools's, which GitLab gives as null.
func (f *gitlabFixture) vulnerabilities(w http.ResponseWriter, r *http.Request) {
	var query struct {
		Variables struct {
			Path string `json:"path"`
		} `json:"variables"`
	}
	_ = json.NewDecoder(r.Body).Decode(&query)
	none := map[string]any{"critical": 0, "high": 0, "medium": 0, "low": 0}
	project := map[string]any{
		"vulnerabilitySeveritiesCount": none, "sast": none, "secretDetection": none, "dependencyScanning": none,
		"containerScanning": none, "dast": none,
		"oldestCritical": map[string]any{"nodes": []any{}},
		"oldestHigh":     map[string]any{"nodes": []any{}},
	}
	switch query.Variables.Path {
	case "acme/api":
		project["vulnerabilitySeveritiesCount"] = map[string]any{"critical": 2, "high": 1, "medium": 5, "low": 0}
		project["sast"] = map[string]any{"critical": 2, "high": 1, "medium": 5, "low": 0}
		project["oldestCritical"] = map[string]any{"nodes": []any{map[string]any{"detectedAt": gitlabT0}}}
	case "octo/tools":
		project["vulnerabilitySeveritiesCount"] = nil
	}
	f.reply(w, http.StatusOK, map[string]any{"data": map[string]any{"project": project}})
}
