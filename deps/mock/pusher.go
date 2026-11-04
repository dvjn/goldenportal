package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type pushConfig struct {
	API          string       `yaml:"api"`
	Integrations []pushTarget `yaml:"integrations"`
}

// pushTarget is pushed the whole file as a complete push every so often, and
// between those, one of the file's items at a time as an incremental push.
type pushTarget struct {
	Name     string  `yaml:"name"`
	Token    string  `yaml:"token"`
	File     string  `yaml:"file"`
	Complete cadence `yaml:"complete"`
	Changes  cadence `yaml:"changes"`
	body     []byte
	// key and items are the file's one list, which changes pick from.
	key   string
	items []any
}

// pusher sends each file to its integration, as an external system would.
type pusher struct{ api string }

func newPusher(cfg pushConfig, dir string) (*pusher, error) {
	for i, target := range cfg.Integrations {
		if target.Token == "" {
			return nil, fmt.Errorf("pushing to %s needs a token", target.Name)
		}
		body, err := os.ReadFile(filepath.Join(dir, target.File))
		if err != nil {
			return nil, err
		}
		var lists map[string][]any
		if err := json.Unmarshal(body, &lists); err != nil || len(lists) != 1 {
			return nil, fmt.Errorf("%s must hold one list, such as {\"teams\": [...]}", target.File)
		}
		for key, items := range lists {
			cfg.Integrations[i].key, cfg.Integrations[i].items = key, items
		}
		cfg.Integrations[i].body = body
	}
	return &pusher{api: cfg.API}, nil
}

func (p *pusher) run(target pushTarget) {
	go func() {
		for {
			time.Sleep(target.Changes.pick())
			one, _ := json.Marshal(map[string]any{target.key: []any{target.items[rand.N(len(target.items))]}})
			p.push(target, "incremental", one)
		}
	}()
	for {
		p.push(target, "complete", target.body)
		time.Sleep(target.Complete.pick())
	}
}

func (p *pusher) push(target pushTarget, mode string, body []byte) {
	req, err := http.NewRequest(http.MethodPost, p.api+"/api/ingest/"+target.Name+"?mode="+mode, bytes.NewReader(body))
	if err != nil {
		log.Printf("push %s: %v", target.Name, err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+target.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("push %s %s: %v", mode, target.Name, err)
		return
	}
	defer res.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(res.Body, 300))
	log.Printf("push %s %s: HTTP %d %s", mode, target.Name, res.StatusCode, answer)
}
