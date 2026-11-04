package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

type registryConfig struct {
	API         string  `yaml:"api"`
	Integration string  `yaml:"integration"`
	Secret      string  `yaml:"secret"`
	File        string  `yaml:"file"`
	Events      cadence `yaml:"events"`
}

// registry is a service registry for the service-registry integration to
// pull, at /registry/services. It also signs and pushes an event when a
// service changes, as a webhook would: a service deploys, or one that isn't
// critical leaves the registry and later comes back.
type registry struct {
	cfg        registryConfig
	lastDeploy func(service string) (string, bool)
	mu         sync.Mutex
	services   []map[string]any
	// gone are services that left, which may come back.
	gone []map[string]any
}

func newRegistry(cfg registryConfig, dir string, lastDeploy func(string) (string, bool)) (*registry, error) {
	if cfg.Secret == "" {
		return nil, fmt.Errorf("the registry has no signing secret")
	}
	raw, err := os.ReadFile(filepath.Join(dir, cfg.File))
	if err != nil {
		return nil, err
	}
	var file struct {
		Services []map[string]any `json:"services"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("read %s: %w", cfg.File, err)
	}

	return &registry{cfg: cfg, lastDeploy: lastDeploy, services: file.Services}, nil
}

func (g *registry) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /registry/services", g.list)
}

func (g *registry) list(w http.ResponseWriter, _ *http.Request) {
	g.mu.Lock()
	services := make([]map[string]any, len(g.services))
	for i, s := range g.services {
		services[i] = g.current(s)
	}
	g.mu.Unlock()
	reply(w, http.StatusOK, map[string]any{"services": services})
}

// current is a service with when the deploy tracker last put it in
// production, so the two agree.
func (g *registry) current(s map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range s {
		out[k] = v
	}
	name, _ := s["name"].(string)
	if at, ok := g.lastDeploy(name); ok {
		out["lastDeploy"] = at
	} else {
		out["lastDeploy"] = time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	}

	return out
}

// add registers a service, as the operations API does for a new one. It sends
// no event, so the portal only sees it when it next pulls.
func (g *registry) add(service map[string]any) (map[string]any, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	named := func(s map[string]any) bool { return s["name"] == service["name"] }
	if slices.ContainsFunc(g.services, named) {
		return nil, false
	}
	g.gone = slices.DeleteFunc(g.gone, named)
	g.services = append(g.services, service)

	return g.current(service), true
}

func (g *registry) run() {
	for {
		time.Sleep(g.cfg.Events.pick())
		g.mu.Lock()
		action, service := g.change()
		g.mu.Unlock()
		g.send(action, service)
	}
}

// change picks what happens next: mostly deploys, sometimes a service
// leaving or coming back.
func (g *registry) change() (string, map[string]any) {
	roll := rand.Float64()
	switch {
	case roll < 0.15 && len(g.gone) > 0:
		i := rand.N(len(g.gone))
		service := g.gone[i]
		g.gone = slices.Delete(g.gone, i, i+1)
		g.services = append(g.services, service)
		return "created", g.current(service)
	case roll < 0.3:
		var leaving []int
		for i, s := range g.services {
			if s["tier"] != "critical" {
				leaving = append(leaving, i)
			}
		}
		if len(leaving) > 0 {
			i := leaving[rand.N(len(leaving))]
			service := g.services[i]
			g.services = slices.Delete(g.services, i, i+1)
			g.gone = append(g.gone, service)
			return "deleted", map[string]any{"name": service["name"]}
		}
	}

	return "updated", g.current(g.services[rand.N(len(g.services))])
}

// send pushes an event signed with the secret, without an ingestion token.
func (g *registry) send(action string, service map[string]any) {
	body, _ := json.Marshal(map[string]any{"action": action, "service": service})
	mac := hmac.New(sha256.New, []byte(g.cfg.Secret))
	mac.Write(body)
	req, err := http.NewRequest(http.MethodPost, g.cfg.API+"/api/ingest/"+g.cfg.Integration, bytes.NewReader(body))
	if err != nil {
		log.Printf("registry event: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Registry-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("registry event %s %v: %v", action, service["name"], err)
		return
	}
	defer res.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(res.Body, 300))
	log.Printf("registry event %s %v: HTTP %d %s", action, service["name"], res.StatusCode, answer)
}
