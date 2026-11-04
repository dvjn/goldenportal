package main

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"
)

type trackerConfig struct {
	Token    string           `yaml:"token"`
	Takes    between          `yaml:"takes"`
	Services []trackerService `yaml:"services"`
}

// trackerService deploys on its own cadence, and Fails is the share of its
// deployments that fail.
type trackerService struct {
	Name    string  `yaml:"name"`
	Fails   float64 `yaml:"fails"`
	cadence `yaml:",inline"`
}

type deployment struct {
	ID          string  `json:"id"`
	Service     string  `json:"service"`
	Environment string  `json:"environment"`
	Version     string  `json:"version"`
	Status      string  `json:"status"`
	StartedAt   string  `json:"startedAt"`
	FinishedAt  *string `json:"finishedAt"`
	started     time.Time
	// changed is when it started or finished, whichever was last.
	changed time.Time
}

// tombstone is a deployment that dropped out of the tracker, which it
// reports to syncs that ask for changes since before then.
type tombstone struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

const (
	trackerKept    = 20
	trackerPerPage = 10
	// tombstonesKept is how long the tracker remembers what dropped out.
	tombstonesKept = 24 * time.Hour
)

// tracker is a deploy tracker for the http integration to pull. It serves the
// latest deployments newest first, ten a page. Only the latest 20 are kept,
// so older ones drop out of syncs.
//
// /deployments?since=<time> answers with what started or finished since
// then, and the deployments that dropped out since, marked deleted, paging
// with Link headers. /deployments/1.json links to 2.json in its body.
//
// Each service deploys on its own cadence, and each deployment runs for its
// own time, so some overlap. A new version goes to staging first; once it
// succeeds, the service's next deployment promotes it to production.
type tracker struct {
	cfg      trackerConfig
	mu       sync.Mutex
	all      []deployment
	releases map[string]int
	// staged is each service's succeeded staging version, waiting for production.
	staged map[string]string
}

func newTracker(cfg trackerConfig) *tracker {
	t := &tracker{cfg: cfg, releases: map[string]int{}, staged: map[string]string{}}
	// Start with a day of finished history, then number it in order.
	for _, s := range cfg.Services {
		for at := time.Now().Add(-24 * time.Hour).Add(s.pick()); ; at = at.Add(s.pick()) {
			end := at.Add(cfg.Takes.pick())
			if end.After(time.Now()) {
				break
			}
			t.finish(t.start(s.Name, at), end, s.Fails)
		}
	}
	slices.SortFunc(t.all, func(a, b deployment) int { return a.started.Compare(b.started) })
	for i := range t.all {
		t.all[i].ID = fmt.Sprintf("deploy-%d", i+1)
	}
	for _, s := range cfg.Services {
		go t.run(s)
	}
	return t
}

func (t *tracker) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /deploy-tracker/deployments", t.changes)
	mux.HandleFunc("GET /deploy-tracker/deployments/{page}", t.page)
}

func (t *tracker) run(s trackerService) {
	for {
		time.Sleep(s.pick())
		t.mu.Lock()
		i := t.start(s.Name, time.Now())
		t.mu.Unlock()
		time.AfterFunc(t.cfg.Takes.pick(), func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.finish(i, time.Now(), s.Fails)
		})
	}
}

// start adds a running deployment and returns its index. Each service counts
// its own releases, so versions read like semver.
func (t *tracker) start(service string, at time.Time) int {
	d := deployment{
		ID: fmt.Sprintf("deploy-%d", len(t.all)+1), Service: service, Status: "running",
		StartedAt: at.UTC().Format(time.RFC3339), started: at, changed: at,
	}
	if version, ok := t.staged[service]; ok {
		d.Version, d.Environment = version, "production"
		delete(t.staged, service)
	} else {
		t.releases[service] = max(t.releases[service], 20) + 1
		release := t.releases[service]
		d.Version, d.Environment = fmt.Sprintf("1.%d.%d", release/10, release%10), "staging"
	}
	t.all = append(t.all, d)
	return len(t.all) - 1
}

func (t *tracker) finish(i int, at time.Time, fails float64) {
	d := &t.all[i]
	d.Status = "succeeded"
	if rand.Float64() < fails {
		d.Status = "failed"
	}
	finished := at.UTC().Format(time.RFC3339)
	d.FinishedAt = &finished
	d.changed = at
	if d.Status == "succeeded" && d.Environment == "staging" {
		t.staged[d.Service] = d.Version
	}
}

// lastDeploy is when a service last reached production, as the pusher's
// service registry reports it.
func (t *tracker) lastDeploy(service string) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, d := range slices.Backward(t.all) {
		if d.Service == service && d.Environment == "production" && d.Status == "succeeded" {
			return *d.FinishedAt, true
		}
	}
	return "", false
}

func (t *tracker) page(w http.ResponseWriter, r *http.Request) {
	if !authorized(r, t.cfg.Token) {
		reply(w, http.StatusUnauthorized, map[string]string{"error": "missing or wrong token"})
		return
	}
	var n int
	if _, err := fmt.Sscanf(r.PathValue("page"), "%d.json", &n); err != nil || n < 1 {
		reply(w, http.StatusNotFound, map[string]string{"error": "no page " + r.PathValue("page")})
		return
	}
	t.mu.Lock()
	latest := slices.Clone(t.all)
	t.mu.Unlock()
	slices.SortFunc(latest, func(a, b deployment) int { return cmp.Compare(b.started.UnixNano(), a.started.UnixNano()) })
	latest = latest[:min(trackerKept, len(latest))]

	from := (n - 1) * trackerPerPage
	if from >= len(latest) {
		reply(w, http.StatusNotFound, map[string]string{"error": "no page " + r.PathValue("page")})
		return
	}
	to := min(from+trackerPerPage, len(latest))
	var next *string
	if to < len(latest) {
		link := fmt.Sprintf("%d.json", n+1)
		next = &link
	}
	reply(w, http.StatusOK, map[string]any{"deployments": latest[from:to], "next": next})
}

// changes serves what changed since ?since=, or everything without it.
func (t *tracker) changes(w http.ResponseWriter, r *http.Request) {
	if !authorized(r, t.cfg.Token) {
		reply(w, http.StatusUnauthorized, map[string]string{"error": "missing or wrong token"})
		return
	}
	var since time.Time
	if s := r.URL.Query().Get("since"); s != "" {
		var err error
		if since, err = time.Parse(time.RFC3339, s); err != nil {
			reply(w, http.StatusBadRequest, map[string]string{"error": "since must be an RFC 3339 time"})
			return
		}
	}
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		var err error
		if page, err = strconv.Atoi(p); err != nil || page < 1 {
			reply(w, http.StatusBadRequest, map[string]string{"error": "page must be a whole number from 1"})
			return
		}
	}

	t.mu.Lock()
	all := slices.Clone(t.all)
	t.mu.Unlock()
	slices.SortFunc(all, func(a, b deployment) int { return cmp.Compare(b.started.UnixNano(), a.started.UnixNano()) })
	remembers := since.After(time.Now().Add(-tombstonesKept))
	var changed []any
	for i, d := range all {
		if i < trackerKept {
			if !d.changed.Before(since) {
				changed = append(changed, d)
			}
			continue
		}
		// A deployment dropped out when the one trackerKept newer started.
		// Asked for changes since before it remembers, the tracker has no
		// tombstones to give, as a sync then gets everything anyway.
		dropped := all[i-trackerKept].started
		if remembers && !dropped.Before(since) {
			changed = append(changed, tombstone{ID: d.ID, Deleted: true})
		}
	}

	from := (page - 1) * trackerPerPage
	to := min(from+trackerPerPage, len(changed))
	if from > len(changed) {
		from = len(changed)
	}
	if to < len(changed) {
		next := url.Values{"page": {strconv.Itoa(page + 1)}}
		if !since.IsZero() {
			next.Set("since", since.Format(time.RFC3339))
		}
		w.Header().Set("Link", fmt.Sprintf(`<?%s>; rel="next"`, next.Encode()))
	}
	reply(w, http.StatusOK, map[string]any{"deployments": append([]any{}, changed[from:to]...)})
}
