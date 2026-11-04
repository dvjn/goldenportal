package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type pushConfig struct {
	API          string       `yaml:"api"`
	Integrations []pushTarget `yaml:"integrations"`
}

// pushTarget is pushed on its own cadence.
type pushTarget struct {
	Name    string `yaml:"name"`
	Token   string `yaml:"token"`
	File    string `yaml:"file"`
	cadence `yaml:",inline"`
	body    []byte
}

var deployPlaceholder = regexp.MustCompile(`@DEPLOY:([a-z0-9-]+)@`)

// pusher sends each file to its push integration, as an external system
// would. "@DEPLOY:<service>@" in a file becomes when the deploy tracker last
// put that service in production, so the two agree.
type pusher struct {
	api        string
	lastDeploy func(service string) (string, bool)
}

func newPusher(cfg pushConfig, dir string, lastDeploy func(string) (string, bool)) (*pusher, error) {
	for i, target := range cfg.Integrations {
		if target.Token == "" {
			return nil, fmt.Errorf("push integration %s has no token", target.Name)
		}
		body, err := os.ReadFile(filepath.Join(dir, target.File))
		if err != nil {
			return nil, err
		}
		cfg.Integrations[i].body = body
	}
	return &pusher{api: cfg.API, lastDeploy: lastDeploy}, nil
}

func (p *pusher) run(target pushTarget) {
	for {
		p.push(target)
		time.Sleep(target.pick())
	}
}

func (p *pusher) push(target pushTarget) {
	body := deployPlaceholder.ReplaceAllFunc(target.body, func(match []byte) []byte {
		if at, ok := p.lastDeploy(string(deployPlaceholder.FindSubmatch(match)[1])); ok {
			return []byte(at)
		}
		return []byte(time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339))
	})
	req, err := http.NewRequest(http.MethodPost, p.api+"/api/ingest/"+target.Name, bytes.NewReader(body))
	if err != nil {
		log.Printf("push %s: %v", target.Name, err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+target.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("push %s: %v", target.Name, err)
		return
	}
	defer res.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(res.Body, 300))
	log.Printf("push %s: HTTP %d %s", target.Name, res.StatusCode, answer)
}
