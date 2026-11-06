package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	mrand "math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type gitlabConfig struct {
	// URL is where the mock serves GitLab, with its API under /api/v4.
	URL string `yaml:"url"`
	// Token is the access token the API takes, of an owner of the group.
	Token string `yaml:"token"`
	Group string `yaml:"group"`
	// Subgroups are the group's, each a team, whose members make its
	// changes, with its projects, each named for a service.
	Subgroups map[string]gitlabSubgroupConfig `yaml:"subgroups"`
	// RateLimit is how many requests the token makes a minute.
	RateLimit int     `yaml:"rateLimit"`
	Events    cadence `yaml:"events"`
	// Pipelines is how long a pipeline, or a deployment, takes.
	Pipelines between `yaml:"pipelines"`
}

type gitlabSubgroupConfig struct {
	Members  []string `yaml:"members"`
	Projects []string `yaml:"projects"`
}

// gitlab is a GitLab group the gitlab integration reads, with a subgroup
// for each team. Things keep happening in its projects: pushes, merge
// requests, pipelines and deployments, each sent to the project's
// webhooks, which the integration sets up through the API.
type gitlab struct {
	cfg      gitlabConfig
	mu       sync.Mutex
	nextID   int
	groups   []map[string]any
	projects []*gitlabProject
	// used counts the token's requests in the current minute.
	used    int
	resetAt time.Time
	// hooks are each project's webhooks. The mock holds no state, so they're
	// gone when it restarts, until the integration's next sync.
	hooks map[int][]*gitlabHook
}

type gitlabHook struct {
	obj   map[string]any
	token string
}

type gitlabProject struct {
	obj         map[string]any
	team        []string
	nextIID     int
	branches    []map[string]any
	mergeReqs   []map[string]any
	pipelines   []map[string]any
	deployments []map[string]any
}

// gitlabHookFlags are the webhook setting each event needs.
var gitlabHookFlags = map[string]string{
	"Push Hook": "push_events", "Merge Request Hook": "merge_requests_events", "Pipeline Hook": "pipeline_events",
	"Deployment Hook": "deployment_events",
}

func newGitLab(cfg gitlabConfig) *gitlab {
	g := &gitlab{cfg: cfg, nextID: 100, hooks: map[int][]*gitlabHook{}, resetAt: time.Now().Add(time.Minute)}
	top := g.group(cfg.Group, nil)
	for _, name := range slices.Sorted(maps.Keys(cfg.Subgroups)) {
		group := g.group(name, top)
		for _, project := range cfg.Subgroups[name].Projects {
			g.projects = append(g.projects, g.seedProject(project, group, cfg.Subgroups[name].Members))
		}
	}

	return g
}

func (g *gitlab) id() int {
	g.nextID++
	return g.nextID
}

func gitlabAt(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func gitlabSHA() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (g *gitlab) web(path string) string { return g.cfg.URL + "/" + strings.TrimPrefix(path, "/") }

// The objects below are shaped as GitLab's REST API gives them, with the
// fields mappings are likely to read.

func (g *gitlab) user(username string) map[string]any {
	return map[string]any{
		"id": len(username) * 7, "username": username, "name": strings.ToUpper(username[:1]) + username[1:], "state": "active",
		"web_url": g.web(username),
	}
}

func (g *gitlab) group(path string, parent map[string]any) map[string]any {
	fullPath, parentID := path, any(nil)
	if parent != nil {
		fullPath, parentID = parent["full_path"].(string)+"/"+path, parent["id"]
	}
	group := map[string]any{
		"id": g.id(), "name": strings.ToUpper(path[:1]) + path[1:], "path": path, "full_path": fullPath, "full_name": fullPath,
		"description": "The " + path + " team's projects.", "visibility": "private", "web_url": g.web("groups/" + fullPath),
		"parent_id": parentID, "created_at": "2023-01-10T09:00:00.000Z",
	}
	g.groups = append(g.groups, group)
	return group
}

func (g *gitlab) seedProject(name string, group map[string]any, team []string) *gitlabProject {
	fullPath := group["full_path"].(string) + "/" + name
	created := time.Now().Add(-time.Duration(200+mrand.N(400)) * 24 * time.Hour)
	p := &gitlabProject{nextIID: 1, team: team}
	p.obj = map[string]any{
		"id": g.id(), "name": name, "path": name, "path_with_namespace": fullPath, "description": "The " + name + " service.",
		"default_branch": "main", "visibility": "private", "archived": false, "empty_repo": false, "web_url": g.web(fullPath),
		"topics": []string{group["path"].(string)}, "star_count": mrand.N(30), "created_at": gitlabAt(created),
		"last_activity_at": gitlabAt(created), "marked_for_deletion_on": nil,
		"namespace":               map[string]any{"id": group["id"], "path": group["path"], "kind": "group", "full_path": group["full_path"]},
		"repository_access_level": "enabled", "merge_requests_access_level": "enabled", "builds_access_level": "enabled",
		"environments_access_level": "enabled",
	}
	p.branches = []map[string]any{g.branch(p, "main", true, created)}

	// A fortnight of history, oldest first.
	for i, ago := 0, 14*24*time.Hour; ago > time.Hour; i, ago = i+1, ago-time.Duration(3+mrand.N(9))*time.Hour {
		when := time.Now().Add(-ago)
		if i%2 == 0 {
			g.openMR(p, when)
		} else if mr := p.openMR(); mr != nil {
			g.closeMR(p, mr, when, mrand.Float64() < 0.8)
		}
		pipeline := g.startPipeline(p, when)
		g.finishPipeline(pipeline, when.Add(g.cfg.Pipelines.pick()))
		if i%3 == 0 {
			d := g.deploy(p, []string{"staging", "production"}[i%2], pipeline, when)
			g.finishDeploy(d, []string{"success", "success", "failed"}[mrand.N(3)], when.Add(2*time.Minute))
		}
	}

	return p
}

func (p *gitlabProject) full() string { return p.obj["path_with_namespace"].(string) }

func (p *gitlabProject) id() int { return p.obj["id"].(int) }

func (p *gitlabProject) someone() string { return p.team[mrand.N(len(p.team))] }

// branch is a branch as GitLab gives one, whose settings a scorecard may
// check: the default one is protected, the rest open to developers.
func (g *gitlab) branch(p *gitlabProject, name string, isDefault bool, when time.Time) map[string]any {
	return map[string]any{
		"name": name, "default": isDefault, "protected": isDefault, "merged": false, "developers_can_push": !isDefault,
		"developers_can_merge": !isDefault, "can_push": true, "web_url": g.web(p.full() + "/-/tree/" + name), "commit": g.commit(p, when),
	}
}

func (g *gitlab) commit(p *gitlabProject, when time.Time) map[string]any {
	sha := gitlabSHA()
	titles := []string{"Fix the retry loop", "Add tracing spans", "Bump dependencies", "Tidy the handlers", "Cache hot keys"}
	title := titles[mrand.N(len(titles))]
	return map[string]any{
		"id": sha, "short_id": sha[:8], "title": title, "created_at": gitlabAt(when), "author_name": p.someone(),
		"web_url": g.web(p.full() + "/-/commit/" + sha),
	}
}

// push moves a branch, creating it if it's new, and returns its old head.
func (g *gitlab) push(p *gitlabProject, branch string, when time.Time) (map[string]any, string) {
	p.obj["last_activity_at"] = gitlabAt(when)
	for _, b := range p.branches {
		if b["name"] == branch {
			before := b["commit"].(map[string]any)["id"].(string)
			b["commit"] = g.commit(p, when)
			return b, before
		}
	}
	b := g.branch(p, branch, false, when)
	p.branches = append(p.branches, b)
	return b, strings.Repeat("0", 40)
}

func (g *gitlab) openMR(p *gitlabProject, when time.Time) map[string]any {
	topics := []string{"retry failed payments", "faster index warmup", "drop legacy endpoint", "add tracing spans", "cache hot keys"}
	title := topics[mrand.N(len(topics))]
	iid := p.nextIID
	p.nextIID++
	branch := fmt.Sprintf("%s-%d", strings.ReplaceAll(title, " ", "-"), iid)
	g.push(p, branch, when)
	draft := mrand.Float64() < 0.2
	mr := map[string]any{
		"id": g.id(), "iid": iid, "project_id": p.id(), "title": title, "state": "opened", "draft": draft,
		"created_at": gitlabAt(when), "updated_at": gitlabAt(when), "merged_at": nil, "closed_at": nil,
		"target_branch": "main", "source_branch": branch, "author": g.user(p.someone()),
		"web_url": g.web(fmt.Sprintf("%s/-/merge_requests/%d", p.full(), iid)),
	}
	p.mergeReqs = append(p.mergeReqs, mr)
	return mr
}

func (p *gitlabProject) openMR() map[string]any {
	for _, mr := range p.mergeReqs {
		if mr["state"] == "opened" {
			return mr
		}
	}
	return nil
}

// closeMR merges or closes a merge request, and deletes its branch.
func (g *gitlab) closeMR(p *gitlabProject, mr map[string]any, when time.Time, merged bool) string {
	mr["updated_at"] = gitlabAt(when)
	if merged {
		mr["state"], mr["merged_at"] = "merged", gitlabAt(when)
		g.push(p, "main", when)
	} else {
		mr["state"], mr["closed_at"] = "closed", gitlabAt(when)
	}
	branch := mr["source_branch"].(string)
	p.branches = slices.DeleteFunc(p.branches, func(b map[string]any) bool { return b["name"] == branch })
	return branch
}

func (g *gitlab) startPipeline(p *gitlabProject, when time.Time) map[string]any {
	id := g.id()
	pipeline := map[string]any{
		"id": id, "iid": len(p.pipelines) + 1, "project_id": p.id(), "sha": p.branches[0]["commit"].(map[string]any)["id"], "ref": "main",
		"tag": false, "status": "running", "source": []string{"push", "push", "schedule", "web"}[mrand.N(4)],
		"created_at": gitlabAt(when), "updated_at": gitlabAt(when), "started_at": gitlabAt(when), "finished_at": nil, "duration": nil,
		"user": g.user(p.someone()), "web_url": g.web(fmt.Sprintf("%s/-/pipelines/%d", p.full(), id)),
	}
	p.pipelines = append(p.pipelines, pipeline)
	return pipeline
}

func (g *gitlab) finishPipeline(pipeline map[string]any, when time.Time) {
	status := "success"
	if mrand.Float64() < 0.2 {
		status = "failed"
	}
	started, _ := time.Parse(time.RFC3339, pipeline["started_at"].(string))
	pipeline["status"], pipeline["finished_at"], pipeline["updated_at"] = status, gitlabAt(when), gitlabAt(when)
	pipeline["duration"] = int(when.Sub(started).Seconds())
}

// gitlabListed is a pipeline as GitLab lists it, without what only one on
// its own says.
func gitlabListed(pipeline map[string]any) map[string]any {
	out := maps.Clone(pipeline)
	for _, key := range []string{"started_at", "finished_at", "duration", "user", "tag"} {
		delete(out, key)
	}
	return out
}

func (g *gitlab) deploy(p *gitlabProject, env string, pipeline map[string]any, when time.Time) map[string]any {
	d := map[string]any{
		"id": g.id(), "iid": len(p.deployments) + 1, "ref": "main", "sha": pipeline["sha"], "status": "running",
		"created_at": gitlabAt(when), "updated_at": gitlabAt(when), "finished_at": nil, "user": pipeline["user"],
		"environment": map[string]any{"name": env, "slug": env, "tier": env, "external_url": "https://" + env + ".example.com/" + p.obj["name"].(string)},
	}
	p.deployments = append(p.deployments, d)
	return d
}

func (g *gitlab) finishDeploy(d map[string]any, status string, when time.Time) {
	d["status"], d["updated_at"], d["finished_at"] = status, gitlabAt(when), gitlabAt(when)
}

// run makes something happen every so often, and sends its events.
func (g *gitlab) run() {
	for {
		time.Sleep(g.cfg.Events.pick())
		g.mu.Lock()
		p, events := g.happen()
		g.mu.Unlock()
		for _, e := range events {
			g.send(p, e.name, e.payload)
		}
	}
}

type gitlabEvent struct {
	name    string
	payload map[string]any
}

// hookProject is a project as webhook payloads show it.
func (g *gitlab) hookProject(p *gitlabProject) map[string]any {
	return map[string]any{
		"id": p.id(), "name": p.obj["name"], "web_url": p.obj["web_url"], "path_with_namespace": p.full(), "default_branch": p.obj["default_branch"],
	}
}

// happen changes something in a random project and returns the events it
// sends. Pipelines and deployments finish a little later.
func (g *gitlab) happen() (*gitlabProject, []gitlabEvent) {
	p := g.projects[mrand.N(len(g.projects))]
	now := time.Now()
	sender := g.user(p.someone())
	pushed := func(branch, before, after string) gitlabEvent {
		return gitlabEvent{"Push Hook", map[string]any{
			"object_kind": "push", "event_name": "push", "before": before, "after": after, "ref": "refs/heads/" + branch,
			"user_username": sender["username"], "project_id": p.id(), "project": g.hookProject(p), "commits": []any{},
		}}
	}
	mrEvent := func(mr map[string]any, action string) gitlabEvent {
		attributes := maps.Clone(mr)
		attributes["action"] = action
		return gitlabEvent{"Merge Request Hook", map[string]any{
			"object_kind": "merge_request", "user": sender, "project": g.hookProject(p), "object_attributes": attributes,
		}}
	}

	switch roll := mrand.Float64(); {
	case roll < 0.35:
		b, before := g.push(p, "main", now)
		pipeline := g.startPipeline(p, now)
		g.later(p, pipeline)
		return p, []gitlabEvent{pushed("main", before, b["commit"].(map[string]any)["id"].(string)), g.pipelineEvent(p, pipeline)}
	case roll < 0.6:
		mr := g.openMR(p, now)
		head := p.branches[len(p.branches)-1]["commit"].(map[string]any)["id"].(string)
		return p, []gitlabEvent{pushed(mr["source_branch"].(string), strings.Repeat("0", 40), head), mrEvent(mr, "open")}
	case roll < 0.85:
		mr := p.openMR()
		if mr == nil {
			return p, nil
		}
		merged := mrand.Float64() < 0.8
		head := p.branches[0]["commit"].(map[string]any)["id"].(string)
		branch := g.closeMR(p, mr, now, merged)
		action := map[bool]string{true: "merge", false: "close"}[merged]
		return p, []gitlabEvent{mrEvent(mr, action), pushed(branch, head, strings.Repeat("0", 40))}
	default:
		d := g.deploy(p, []string{"staging", "production"}[mrand.N(2)], p.pipelines[len(p.pipelines)-1], now)
		g.laterDeploy(p, d)
		return p, []gitlabEvent{g.deploymentEvent(p, d)}
	}
}

func (g *gitlab) pipelineEvent(p *gitlabProject, pipeline map[string]any) gitlabEvent {
	return gitlabEvent{"Pipeline Hook", map[string]any{
		"object_kind": "pipeline", "object_attributes": maps.Clone(pipeline), "user": pipeline["user"], "project": g.hookProject(p),
	}}
}

func (g *gitlab) deploymentEvent(p *gitlabProject, d map[string]any) gitlabEvent {
	return gitlabEvent{"Deployment Hook", map[string]any{
		"object_kind": "deployment", "status": d["status"], "deployment_id": d["id"],
		"environment": d["environment"].(map[string]any)["name"], "project": g.hookProject(p), "short_sha": d["sha"].(string)[:8],
	}}
}

// later finishes a pipeline once it's taken a while, and sends its event.
func (g *gitlab) later(p *gitlabProject, pipeline map[string]any) {
	time.AfterFunc(g.cfg.Pipelines.pick(), func() {
		g.mu.Lock()
		g.finishPipeline(pipeline, time.Now())
		event := g.pipelineEvent(p, pipeline)
		g.mu.Unlock()
		g.send(p, event.name, event.payload)
	})
}

func (g *gitlab) laterDeploy(p *gitlabProject, d map[string]any) {
	time.AfterFunc(g.cfg.Pipelines.pick(), func() {
		g.mu.Lock()
		g.finishDeploy(d, []string{"success", "success", "success", "failed"}[mrand.N(4)], time.Now())
		event := g.deploymentEvent(p, d)
		g.mu.Unlock()
		g.send(p, event.name, event.payload)
	})
}

// send delivers an event to each of the project's webhooks that takes it,
// as GitLab does, with the webhook's secret token in a header.
func (g *gitlab) send(p *gitlabProject, event string, payload map[string]any) {
	g.mu.Lock()
	body, err := json.Marshal(payload)
	var hooks []gitlabHook
	for _, h := range g.hooks[p.id()] {
		if h.obj[gitlabHookFlags[event]] == true {
			hooks = append(hooks, gitlabHook{obj: maps.Clone(h.obj), token: h.token})
		}
	}
	g.mu.Unlock()
	if err != nil {
		log.Printf("gitlab %s: %v", event, err)
		return
	}
	if len(hooks) == 0 {
		log.Printf("gitlab %s on %s: no webhook takes it yet; the gitlab integration's next sync sets one up", event, p.full())
	}
	for _, h := range hooks {
		target, _ := h.obj["url"].(string)
		req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			log.Printf("gitlab %s: %v", event, err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "GitLab/mock")
		req.Header.Set("X-Gitlab-Event", event)
		req.Header.Set("X-Gitlab-Token", h.token)
		res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			log.Printf("gitlab %s to webhook %v: %v", event, h.obj["id"], err)
			continue
		}
		answer, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		res.Body.Close()
		log.Printf("gitlab %s to webhook %v: HTTP %d %s", event, h.obj["id"], res.StatusCode, answer)
	}
}

const gitlabPrefix = "/gitlab/api/v4"

func (g *gitlab) routes(mux *http.ServeMux) {
	handlers := map[string]http.HandlerFunc{
		"GET /user":                                         g.currentUser,
		"GET /personal_access_tokens/self":                  g.tokenSelf,
		"GET /groups/{group}":                               g.groupObject,
		"GET /groups/{group}/descendant_groups":             g.descendants,
		"GET /groups/{group}/projects":                      g.groupProjects,
		"GET /groups/{group}/members/all/{user}":            g.owner,
		"GET /projects/{project}":                           g.project(func(w http.ResponseWriter, _ *http.Request, p *gitlabProject) { reply(w, http.StatusOK, p.obj) }),
		"GET /projects/{project}/members/all/{user}":        g.owner,
		"GET /projects/{project}/repository/branches/{ref}": g.project(g.branchObject),
		"GET /projects/{project}/merge_requests":            g.project(g.mergeRequests),
		"GET /projects/{project}/pipelines":                 g.project(g.pipelineList),
		"GET /projects/{project}/pipelines/{id}":            g.project(g.pipeline),
		"GET /projects/{project}/deployments":               g.project(g.deploymentList),
		"GET /projects/{project}/deployments/{id}":          g.project(g.deployment),
		"GET /projects/{project}/ci/lint":                   g.project(g.lint),
		"GET /projects/{project}/repository/tree":           g.project(g.tree),
		"GET /projects/{project}/hooks":                     g.list(g.hookObjects),
		"POST /projects/{project}/hooks":                    g.project(g.createHook),
		"PUT /projects/{project}/hooks/{id}":                g.project(g.updateHook),
	}
	for pattern, h := range handlers {
		method, path, _ := strings.Cut(pattern, " ")
		mux.HandleFunc(method+" "+gitlabPrefix+path, g.api(h))
	}
}

// api checks the token and counts the request against its rate limit,
// sending the headers GitLab does.
func (g *gitlab) api(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if g.cfg.Token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("PRIVATE-TOKEN")), []byte(g.cfg.Token)) != 1 {
			reply(w, http.StatusUnauthorized, map[string]any{"message": "401 Unauthorized"})
			return
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if time.Now().After(g.resetAt) {
			g.used, g.resetAt = 0, time.Now().Add(time.Minute)
		}
		g.used++
		h := w.Header()
		h.Set("RateLimit-Name", "throttle_authenticated_api")
		h.Set("RateLimit-Limit", strconv.Itoa(g.cfg.RateLimit))
		h.Set("RateLimit-Remaining", strconv.Itoa(max(g.cfg.RateLimit-g.used, 0)))
		h.Set("RateLimit-Reset", strconv.FormatInt(g.resetAt.Unix(), 10))
		if g.used > g.cfg.RateLimit {
			h.Set("Retry-After", strconv.Itoa(max(int(time.Until(g.resetAt).Seconds()), 1)))
			h.Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("Retry later\n"))
			return
		}
		next(w, r)
	}
}

func gitlabNotFound(w http.ResponseWriter, what string) {
	reply(w, http.StatusNotFound, map[string]any{"message": "404 " + what + " Not Found"})
}

// page answers a page of a list, per_page at a time, linking the next page
// as GitLab does.
func (g *gitlab) page(w http.ResponseWriter, r *http.Request, objects []map[string]any) {
	q := r.URL.Query()
	size, err := strconv.Atoi(q.Get("per_page"))
	if err != nil || size < 1 {
		size = 20
	}
	size = min(size, 100)
	page, err := strconv.Atoi(q.Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	from := min((page-1)*size, len(objects))
	to := min(from+size, len(objects))
	if to < len(objects) {
		q.Set("page", strconv.Itoa(page+1))
		w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?%s>; rel="next"`, r.Host, r.URL.EscapedPath(), q.Encode()))
	}
	reply(w, http.StatusOK, append([]map[string]any{}, objects[from:to]...))
}

// findGroup is a group by its ID or full path, which GitLab's paths ignore
// the case of.
func (g *gitlab) findGroup(key string) map[string]any {
	for _, group := range g.groups {
		if strconv.Itoa(group["id"].(int)) == key || strings.EqualFold(group["full_path"].(string), key) {
			return group
		}
	}
	return nil
}

func (g *gitlab) currentUser(w http.ResponseWriter, _ *http.Request) {
	u := g.user("portal-bot")
	u["is_admin"] = false
	reply(w, http.StatusOK, u)
}

func (g *gitlab) tokenSelf(w http.ResponseWriter, _ *http.Request) {
	reply(w, http.StatusOK, map[string]any{"id": 1, "name": "golden-portal-dev", "active": true, "revoked": false, "scopes": []string{"api"}})
}

// owner is the token's user's membership, which owns the top group.
func (g *gitlab) owner(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("user") != strconv.Itoa(g.user("portal-bot")["id"].(int)) {
		gitlabNotFound(w, "Member")
		return
	}
	m := g.user("portal-bot")
	m["access_level"] = 50
	reply(w, http.StatusOK, m)
}

// groupObject is a group as GitLab gives one on its own, which says more
// than a list of them, such as the token its runners register with.
func (g *gitlab) groupObject(w http.ResponseWriter, r *http.Request) {
	group := g.findGroup(r.PathValue("group"))
	if group == nil {
		gitlabNotFound(w, "Group")
		return
	}
	out := maps.Clone(group)
	out["runners_token"], out["shared_with_groups"] = "GR1348941"+gitlabSHA()[:20], []any{}
	reply(w, http.StatusOK, out)
}

func (g *gitlab) descendants(w http.ResponseWriter, r *http.Request) {
	group := g.findGroup(r.PathValue("group"))
	if group == nil {
		gitlabNotFound(w, "Group")
		return
	}
	var out []map[string]any
	for _, sub := range g.groups {
		if strings.HasPrefix(sub["full_path"].(string), group["full_path"].(string)+"/") {
			out = append(out, sub)
		}
	}
	g.page(w, r, out)
}

func (g *gitlab) groupProjects(w http.ResponseWriter, r *http.Request) {
	group := g.findGroup(r.PathValue("group"))
	if group == nil {
		gitlabNotFound(w, "Group")
		return
	}
	var out []map[string]any
	for _, p := range g.projects {
		namespace := p.obj["namespace"].(map[string]any)["full_path"].(string)
		if namespace == group["full_path"] || r.URL.Query().Get("include_subgroups") == "true" && strings.HasPrefix(namespace, group["full_path"].(string)+"/") {
			out = append(out, p.obj)
		}
	}
	g.page(w, r, out)
}

// project answers about one project, by its ID or full path.
func (g *gitlab) project(answer func(http.ResponseWriter, *http.Request, *gitlabProject)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, p := range g.projects {
			if key := r.PathValue("project"); strconv.Itoa(p.id()) == key || strings.EqualFold(p.full(), key) {
				answer(w, r, p)
				return
			}
		}
		gitlabNotFound(w, "Project")
	}
}

func (g *gitlab) list(objects func(*gitlabProject) []map[string]any) http.HandlerFunc {
	return g.project(func(w http.ResponseWriter, r *http.Request, p *gitlabProject) { g.page(w, r, objects(p)) })
}

func (g *gitlab) branchObject(w http.ResponseWriter, r *http.Request, p *gitlabProject) {
	for _, b := range p.branches {
		if b["name"] == r.PathValue("ref") {
			reply(w, http.StatusOK, b)
			return
		}
	}
	gitlabNotFound(w, "Branch")
}

// mergeRequests answers merge requests as GitLab filters them: by state,
// updated_after, iids and order.
func (g *gitlab) mergeRequests(w http.ResponseWriter, r *http.Request, p *gitlabProject) {
	q := r.URL.Query()
	after, _ := time.Parse(time.RFC3339, q.Get("updated_after"))
	out := slices.DeleteFunc(newestFirst(p.mergeReqs), func(mr map[string]any) bool {
		updated, _ := time.Parse(time.RFC3339, mr["updated_at"].(string))
		state := q.Get("state")
		return state != "" && state != "all" && mr["state"] != state || updated.Before(after) ||
			q.Has("iids[]") && !slices.Contains(q["iids[]"], fmt.Sprint(mr["iid"]))
	})
	if q.Get("order_by") == "updated_at" {
		slices.SortStableFunc(out, func(a, b map[string]any) int {
			return strings.Compare(b["updated_at"].(string), a["updated_at"].(string))
		})
	}
	g.page(w, r, out)
}

// pipelineList lists pipelines as GitLab does, which has no child pipelines
// here, asked for alone.
func (g *gitlab) pipelineList(w http.ResponseWriter, r *http.Request, p *gitlabProject) {
	if r.URL.Query().Get("source") == "parent_pipeline" {
		g.page(w, r, nil)
		return
	}
	ref := r.URL.Query().Get("ref")
	g.page(w, r, mapped(slices.DeleteFunc(newestFirst(p.pipelines), func(pl map[string]any) bool { return ref != "" && pl["ref"] != ref }), gitlabListed))
}

func (g *gitlab) pipeline(w http.ResponseWriter, r *http.Request, p *gitlabProject) {
	g.one(w, r, p.pipelines, "Pipeline")
}

func (g *gitlab) deployment(w http.ResponseWriter, r *http.Request, p *gitlabProject) {
	g.one(w, r, p.deployments, "Deployment")
}

// one answers the object of objects whose ID the path has.
func (g *gitlab) one(w http.ResponseWriter, r *http.Request, objects []map[string]any, what string) {
	for _, o := range objects {
		if fmt.Sprint(o["id"]) == r.PathValue("id") {
			reply(w, http.StatusOK, o)
			return
		}
	}
	gitlabNotFound(w, what)
}

func (g *gitlab) deploymentList(w http.ResponseWriter, r *http.Request, p *gitlabProject) {
	q := r.URL.Query()
	out := slices.DeleteFunc(newestFirst(p.deployments), func(d map[string]any) bool {
		return q.Has("environment") && d["environment"].(map[string]any)["name"] != q.Get("environment") ||
			q.Has("status") && d["status"] != q.Get("status")
	})
	g.page(w, r, out)
}

func (g *gitlab) lint(w http.ResponseWriter, _ *http.Request, _ *gitlabProject) {
	job := func(name, stage string, script ...string) map[string]any {
		return map[string]any{
			"name": name, "stage": stage, "before_script": []string{"make deps"}, "script": script, "after_script": []string{},
			"tag_list": []string{"docker"}, "only": nil, "except": nil, "environment": nil, "when": "on_success", "allow_failure": false, "needs": nil,
		}
	}
	reply(w, http.StatusOK, map[string]any{
		"valid": true, "errors": []string{}, "warnings": []string{}, "merged_yaml": "stages: [build, test, deploy]\n",
		"includes": []any{map[string]any{"type": "template", "location": "Security/SAST.gitlab-ci.yml"}},
		"jobs":     []any{job("build", "build", "make build"), job("test", "test", "make test"), job("deploy", "deploy", "make deploy")},
	})
}

// tree answers the files a mapping looks for: a README and CODEOWNERS
// everywhere, and renovate.json in .gitlab.
func (g *gitlab) tree(w http.ResponseWriter, r *http.Request, _ *gitlabProject) {
	if r.URL.Query().Get("path") == ".gitlab" {
		reply(w, http.StatusOK, []map[string]any{{"path": ".gitlab/renovate.json", "type": "blob"}})
		return
	}
	reply(w, http.StatusOK, []map[string]any{
		{"path": ".gitlab", "type": "tree"}, {"path": ".gitlab-ci.yml", "type": "blob"}, {"path": "CODEOWNERS", "type": "blob"}, {"path": "README.md", "type": "blob"},
	})
}

func newestFirst(objects []map[string]any) []map[string]any {
	out := slices.Clone(objects)
	slices.Reverse(out)
	return out
}

func mapped(objects []map[string]any, fn func(map[string]any) map[string]any) []map[string]any {
	out := make([]map[string]any, len(objects))
	for i, o := range objects {
		out[i] = fn(o)
	}
	return out
}

// hookObjects are the project's webhooks as GitLab shows them, without
// their tokens.
func (g *gitlab) hookObjects(p *gitlabProject) []map[string]any {
	var out []map[string]any
	for _, h := range g.hooks[p.id()] {
		out = append(out, maps.Clone(h.obj))
	}
	return out
}

// applyHook sets what a request to make or change a webhook sends.
func applyHook(h *gitlabHook, body map[string]any) {
	for key, value := range body {
		switch {
		case key == "token":
			h.token, _ = value.(string)
		case key == "url" || key == "enable_ssl_verification" || strings.HasSuffix(key, "_events"):
			h.obj[key] = value
		}
	}
	h.obj["token_present"] = h.token != ""
}

func (g *gitlab) createHook(w http.ResponseWriter, r *http.Request, p *gitlabProject) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["url"] == nil {
		reply(w, http.StatusBadRequest, map[string]any{"error": "url is missing"})
		return
	}
	h := &gitlabHook{obj: map[string]any{
		"id": g.id(), "project_id": p.id(), "push_events": true, "enable_ssl_verification": true, "alert_status": "executable",
		"created_at": gitlabAt(time.Now()),
	}}
	applyHook(h, body)
	g.hooks[p.id()] = append(g.hooks[p.id()], h)
	reply(w, http.StatusCreated, h.obj)
}

func (g *gitlab) updateHook(w http.ResponseWriter, r *http.Request, p *gitlabProject) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["url"] == nil {
		reply(w, http.StatusBadRequest, map[string]any{"error": "url is missing"})
		return
	}
	for _, h := range g.hooks[p.id()] {
		if fmt.Sprint(h.obj["id"]) == r.PathValue("id") {
			applyHook(h, body)
			reply(w, http.StatusOK, h.obj)
			return
		}
	}
	gitlabNotFound(w, "Hook")
}
