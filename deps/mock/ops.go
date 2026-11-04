package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"regexp"
	"sync"
	"time"
)

type opsConfig struct {
	Token     string                 `yaml:"token"`
	Endpoints map[string]opsEndpoint `yaml:"endpoints"`
	// Jobs is how long a restart job runs before it's done.
	Jobs between `yaml:"jobs"`
}

// opsEndpoint is how slow an endpoint is and the share of calls it answers
// 503, so retries and failed runs show.
type opsEndpoint struct {
	Latency between `yaml:"latency"`
	Fails   float64 `yaml:"fails"`
}

type job struct {
	ID          string `json:"id"`
	Service     string `json:"service"`
	Environment any    `json:"environment"`
	Status      string `json:"status"`
	At          string `json:"at"`
	done        time.Time
}

var (
	repoName   = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	incidentID = regexp.MustCompile(`^inc-\d+$`)
	// epoch keeps IDs short; they count minutes from it at start, so they
	// keep going up across restarts and never reuse one the catalog holds.
	epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

// ops answers the example actions as an operations API would.
type ops struct {
	cfg       opsConfig
	mu        sync.Mutex
	next      int
	jobs      map[string]*job
	incidents map[string]string
	repos     map[string]bool
	// register adds a service to the registry, false when it's there already.
	register func(service map[string]any) (map[string]any, bool)
}

func newOps(cfg opsConfig, register func(map[string]any) (map[string]any, bool)) *ops {
	return &ops{
		cfg: cfg, next: int(time.Since(epoch).Minutes()),
		jobs: map[string]*job{}, incidents: map[string]string{}, repos: map[string]bool{}, register: register,
	}
}

func (o *ops) routes(mux *http.ServeMux) {
	o.handle(mux, "POST /ops/services/{service}/restart", "restart", o.restart)
	o.handle(mux, "POST /ops/services/{service}/scale", "scale", o.scale)
	o.handle(mux, "POST /ops/repos", "repos", o.createRepo)
	o.handle(mux, "POST /ops/services", "services", o.registerService)
	o.handle(mux, "POST /ops/incidents", "incidents", o.openIncident)
	o.handle(mux, "POST /ops/incidents/{id}/resolve", "incidents", o.resolveIncident)
	o.handle(mux, "GET /ops/jobs/{id}", "jobs", o.job)
}

// handle checks the token, then waits and sometimes fails as the endpoint is
// set up to.
func (o *ops) handle(mux *http.ServeMux, pattern, endpoint string, next http.HandlerFunc) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, o.cfg.Token) {
			reply(w, http.StatusUnauthorized, map[string]string{"error": "missing or wrong token"})
			return
		}
		e := o.cfg.Endpoints[endpoint]
		time.Sleep(e.Latency.pick())
		if rand.Float64() < e.Fails {
			w.Header().Set("Retry-After", "2")
			reply(w, http.StatusServiceUnavailable, map[string]string{"error": "busy, try again"})
			return
		}
		next(w, r)
	})
}

func authorized(r *http.Request, token string) bool {
	return token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(token)) == 1
}

func (o *ops) id(prefix string) string {
	o.next++
	return fmt.Sprintf("%s-%d", prefix, o.next)
}

func (o *ops) restart(w http.ResponseWriter, r *http.Request) {
	fields := body(r)
	o.mu.Lock()
	j := &job{
		ID: o.id("job"), Service: r.PathValue("service"), Environment: fields["environment"],
		Status: "running", At: now(), done: time.Now().Add(o.cfg.Jobs.pick()),
	}
	o.jobs[j.ID] = j
	o.mu.Unlock()
	reply(w, http.StatusAccepted, j)
}

func (o *ops) job(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	j, ok := o.jobs[r.PathValue("id")]
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{"error": "no job " + r.PathValue("id")})
		return
	}
	if j.Status == "running" && time.Now().After(j.done) {
		j.Status = "succeeded"
	}
	reply(w, http.StatusOK, j)
}

func (o *ops) scale(w http.ResponseWriter, r *http.Request) {
	replicas, ok := body(r)["replicas"].(float64)
	if !ok || replicas != math.Trunc(replicas) || replicas < 0 || replicas > 50 {
		reply(w, http.StatusUnprocessableEntity, map[string]string{"error": "replicas must be a whole number from 0 to 50"})
		return
	}
	reply(w, http.StatusOK, map[string]any{"service": r.PathValue("service"), "replicas": int(replicas), "at": now()})
}

func (o *ops) createRepo(w http.ResponseWriter, r *http.Request) {
	name, _ := body(r)["name"].(string)
	if !repoName.MatchString(name) {
		reply(w, http.StatusUnprocessableEntity, map[string]string{"error": "name must be lowercase letters, digits and dashes, starting with a letter"})
		return
	}
	o.mu.Lock()
	exists := o.repos[name]
	o.repos[name] = true
	o.mu.Unlock()
	if exists {
		reply(w, http.StatusConflict, map[string]string{"error": "a repository named " + name + " already exists"})
		return
	}
	reply(w, http.StatusCreated, map[string]any{"name": name, "url": "https://git.example.com/" + name, "at": now()})
}

func (o *ops) registerService(w http.ResponseWriter, r *http.Request) {
	fields := body(r)
	name, _ := fields["name"].(string)
	if !repoName.MatchString(name) {
		reply(w, http.StatusUnprocessableEntity, map[string]string{"error": "name must be lowercase letters, digits and dashes, starting with a letter"})
		return
	}
	service := map[string]any{
		"name": name, "displayName": fields["displayName"], "language": fields["language"], "tier": fields["tier"],
		"repo": fields["repo"], "owner": fields["owner"], "tags": []any{}, "dependencies": []any{}, "lifecycle": "production",
	}
	registered, ok := o.register(service)
	if !ok {
		reply(w, http.StatusConflict, map[string]string{"error": "a service named " + name + " is already registered"})
		return
	}
	reply(w, http.StatusCreated, registered)
}

func (o *ops) openIncident(w http.ResponseWriter, _ *http.Request) {
	o.mu.Lock()
	id := o.id("inc")
	o.incidents[id] = "open"
	o.mu.Unlock()
	reply(w, http.StatusCreated, map[string]any{"id": id, "channel": "https://slack.example.com/archives/" + id, "at": now()})
}

// resolveIncident also resolves incidents opened before the mock last
// started, which it no longer remembers.
func (o *ops) resolveIncident(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !incidentID.MatchString(id) {
		reply(w, http.StatusNotFound, map[string]string{"error": "no incident " + id})
		return
	}
	o.mu.Lock()
	status := o.incidents[id]
	o.incidents[id] = "resolved"
	o.mu.Unlock()
	if status == "resolved" {
		reply(w, http.StatusConflict, map[string]string{"error": id + " is already resolved"})
		return
	}
	reply(w, http.StatusOK, map[string]any{"id": id, "status": "resolved", "at": now()})
}

// body reads the request's JSON object; fields it can't read are left out.
func body(r *http.Request) map[string]any {
	fields := map[string]any{}
	_ = json.NewDecoder(r.Body).Decode(&fields)
	return fields
}
