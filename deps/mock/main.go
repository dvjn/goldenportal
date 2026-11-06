// Command mock serves everything the local portal talks to that isn't the
// portal: a sign-in provider, an operations API, a deploy tracker, a service
// registry that also sends signed events, a GitLab group that sends webhook
// events, and a pusher that feeds an integration over the ingest API.
// mock.yaml sets it up.
package main

import (
	"encoding/json"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go.yaml.in/yaml/v3"
)

type config struct {
	Listen   string         `yaml:"listen"`
	SSO      ssoConfig      `yaml:"sso"`
	Ops      opsConfig      `yaml:"ops"`
	Push     pushConfig     `yaml:"push"`
	Tracker  trackerConfig  `yaml:"tracker"`
	Registry registryConfig `yaml:"registry"`
	GitLab   gitlabConfig   `yaml:"gitlab"`
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: mock <config.yaml>")
	}
	path := os.Args[1]
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	var cfg config
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(raw))), &cfg); err != nil {
		log.Fatalf("read %s: %v", path, err)
	}

	mux := http.NewServeMux()
	sso, err := newSSO(cfg.SSO)
	if err != nil {
		log.Fatal(err)
	}
	sso.routes(mux)
	tracker := newTracker(cfg.Tracker)
	tracker.routes(mux)

	registry, err := newRegistry(cfg.Registry, filepath.Dir(path), tracker.lastDeploy)
	if err != nil {
		log.Fatal(err)
	}
	registry.routes(mux)
	newOps(cfg.Ops, registry.add).routes(mux)
	go registry.run()

	gitlab := newGitLab(cfg.GitLab)
	gitlab.routes(mux)
	go gitlab.run()

	pusher, err := newPusher(cfg.Push, filepath.Dir(path))
	if err != nil {
		log.Fatal(err)
	}
	for _, target := range cfg.Push.Integrations {
		go pusher.run(target)
	}

	server := &http.Server{Addr: cfg.Listen, Handler: logged(mux), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("listening on %s", cfg.Listen)
	log.Fatal(server.ListenAndServe())
}

func logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d", r.Method, r.URL.Path, rec.status)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// cadence is a random gap of Every, give or take Variance.
type cadence struct {
	Every    time.Duration `yaml:"every"`
	Variance time.Duration `yaml:"variance"`
}

func (c cadence) pick() time.Duration {
	gap := c.Every
	if c.Variance > 0 {
		gap += rand.N(2*c.Variance) - c.Variance
	}
	return max(gap, time.Second)
}

// between is a random duration from Min up to Max.
type between struct {
	Min time.Duration `yaml:"min"`
	Max time.Duration `yaml:"max"`
}

func (b between) pick() time.Duration {
	if b.Max <= b.Min {
		return b.Min
	}
	return b.Min + rand.N(b.Max-b.Min)
}
