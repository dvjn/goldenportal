package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func httpYAML(url string) string {
	return `
kind: Integration
metadata: {name: tracker, title: Tracker}
spec:
  connector:
    type: http
    config:
      resources:
        deployments:
          url: ` + url + `
          next: .next
          headers:
            Authorization: {env: GP_SECRET_TRACKER}
            Accept-Language: en
  sync: {interval: 5m}
  mappings:
    - resource: deployments
      blueprint: deployment
      items: .deployments[]
      entity:
        identifier: .id
`
}

func TestParseHTTPConfig(t *testing.T) {
	spec := mustParse(t, httpYAML("https://tracker.example.com/deployments"))
	if spec.http == nil || spec.http.Resources["deployments"].Headers["Authorization"].Env != "GP_SECRET_TRACKER" {
		t.Fatalf("http = %+v", spec.http)
	}

	head := "kind: Integration\nmetadata: {name: a, title: A}\nspec:\n  connector:\n    type: http\n"
	mapping := "  mappings:\n    - resource: x\n      blueprint: s\n      entity: {identifier: .id}\n"
	tests := []struct{ name, source, want string }{
		{"no config", head + mapping, "add the resources to fetch"},
		{"bad url", head + "    config:\n      resources:\n        x: {url: 'file:///etc/passwd'}\n" + mapping, "url must be an http or https URL"},
		{"other env", head + "    config:\n      resources:\n        x:\n          url: https://a.example\n          headers: {Authorization: {env: DB_PASSWORD}}\n" + mapping, "starting with GP_SECRET_"},
		{"unknown resource", head + "    config:\n      resources:\n        y: {url: https://a.example}\n" + mapping, "resource must be one of y"},
		{"bad next", head + "    config:\n      resources:\n        x: {url: https://a.example, next: '.a |'}\n" + mapping, "next is not valid jq"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := Parse(test.source)
			var messages []string
			for _, issue := range issues {
				messages = append(messages, issue.Message)
			}
			if !strings.Contains(strings.Join(messages, "\n"), test.want) {
				t.Fatalf("issues = %v, want %q", messages, test.want)
			}
		})
	}
}

func TestHTTPPull(t *testing.T) {
	t.Setenv("GP_SECRET_TRACKER", "Bearer s3cret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer s3cret" || r.Header.Get("Accept-Language") != "en" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/deployments":
			_, _ = w.Write([]byte(`{"deployments": [{"id": 1}, {"id": 2}], "next": "page/2"}`))
		case "/page/2":
			_, _ = w.Write([]byte(`{"deployments": [{"id": 3}], "next": null}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	spec := mustParse(t, httpYAML(server.URL+"/deployments"))
	pulled, err := httpPull(context.Background(), nil, spec, []string{"deployments"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := spec.Map(mustJSON(t, pulled))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, item := range items {
		ids = append(ids, item.Entity.Identifier)
	}
	if got := strings.Join(ids, ","); got != "1,2,3" {
		t.Fatalf("identifiers = %s, want 1,2,3 across both pages", got)
	}
}

func TestHTTPPullStopsOnLoopsAndErrors(t *testing.T) {
	shortenRetries(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loop":
			_, _ = w.Write([]byte(`{"next": "/loop"}`))
		case "/html":
			_, _ = w.Write([]byte(`<html>`))
		default:
			http.Error(w, "down", http.StatusBadGateway)
		}
	}))
	defer server.Close()

	for path, want := range map[string]string{"/loop": "loop back", "/html": "didn't answer with JSON", "/down": "502"} {
		resource := HTTPResource{URL: server.URL + path, Next: ".next"}
		if _, err := fetchPages(context.Background(), "x", resource); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", path, err, want)
		}
	}
}

func TestFetchRetries(t *testing.T) {
	shortenRetries(t)
	tests := []struct {
		name       string
		answers    []int
		retryAfter string
		calls      int32
		want       string
	}{
		{name: "recovers from 503s", answers: []int{503, 503, 200}, calls: 3},
		{name: "recovers from a 429", answers: []int{429, 200}, retryAfter: "0", calls: 2},
		{name: "gives up after three retries", answers: []int{502, 502, 502, 502, 200}, calls: 4, want: "502"},
		{name: "doesn't retry a 404", answers: []int{404, 200}, calls: 1, want: "404"},
		{name: "won't wait long", answers: []int{429, 200}, retryAfter: "120", calls: 1, want: "asked to wait 2m0s"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				status := test.answers[calls.Add(1)-1]
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()

			_, err := fetchJSON(context.Background(), server.URL, nil)
			if calls.Load() != test.calls {
				t.Errorf("calls = %d, want %d", calls.Load(), test.calls)
			}
			if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Errorf("err = %v, want %q", err, test.want)
			}
		})
	}
}

// shortenRetries makes retried fetches wait a millisecond instead of seconds.
func shortenRetries(t *testing.T) {
	t.Helper()
	backoff := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = backoff })
}

func mustJSON(t *testing.T, v any) any {
	t.Helper()
	out, err := toJSONValue(v)
	if err != nil {
		t.Fatal(err)
	}

	return out
}
