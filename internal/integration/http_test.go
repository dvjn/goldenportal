package integration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

func httpYAML(url string) string {
	return `
kind: Integration
metadata: {identifier: tracker, title: Tracker}
spec:
  connector:
    type: http
    config:
      push:
        signature:
          header: X-Signature
          algorithm: sha256
          prefix: sha256=
          secret: {env: GP_SECRET_TRACKER_SIGNING}
      resources:
        deployments:
          url: ` + url + `
          next: .next
          items: .deployments[]
          deleted: .status == "deleted"
          headers:
            Authorization: {env: GP_SECRET_TRACKER}
            Accept-Language: en
          push:
            items: .body.deployment
            deleted: .headers["x-event"] == "deleted"
  sync: {interval: 5m}
  mappings:
    - resource: deployments
      blueprint: deployment
      entity:
        identifier: .id
`
}

func httpConfig(t *testing.T, spec Spec) *HTTPConfig {
	t.Helper()
	config, ok := spec.config.(*HTTPConfig)
	if !ok {
		t.Fatalf("config = %T, want the http connector's", spec.config)
	}

	return config
}

func TestParseHTTPConfig(t *testing.T) {
	spec := mustParse(t, httpYAML("https://tracker.example.com/deployments?since={{ $since }}"))
	config := httpConfig(t, spec)
	if config.Resource["deployments"].Headers["Authorization"].Env != "GP_SECRET_TRACKER" || config.Push.Signature.Header != "X-Signature" ||
		!spec.Pulls || !spec.Receives || !config.UsesSince() || config.Resource["deployments"].MaxPages != DefaultMaxPages {
		t.Fatalf("spec = %+v, config = %+v", spec, config)
	}

	head := "kind: Integration\nmetadata: {identifier: a, title: A}\nspec:\n  connector:\n    type: http\n"
	mapping := "  mappings:\n    - resource: x\n      blueprint: s\n      entity: {identifier: .id}\n"
	tests := []struct{ name, source, want string }{
		{"no config", head + mapping, "add the http connector's config"},
		{"bad url", head + "    config:\n      resources:\n        x: {url: 'file:///etc/passwd'}\n" + mapping, "url must be an http or https URL"},
		{"other env", head + "    config:\n      resources:\n        x:\n          url: https://a.example\n          headers: {Authorization: {env: DB_PASSWORD}}\n" + mapping, "starting with GP_SECRET_"},
		{"unknown resource", head + "    config:\n      resources:\n        y: {url: https://a.example}\n" + mapping, "resource must be one of y"},
		{"bad next", head + "    config:\n      resources:\n        x: {url: https://a.example, next: '.a |'}\n" + mapping, "next is not valid jq"},
		{"neither side", head + "    config:\n      resources:\n        x: {}\n" + mapping, "add a url to fetch the resource from, a push side"},
		{"fetching without a url", head + "    config:\n      resources:\n        x: {next: .next, push: {items: .body}}\n" + mapping, "next is for fetching"},
		{"push without items", head + "    config:\n      resources:\n        x: {push: {deleted: 'true'}}\n" + mapping, "add jq over {body, headers, query}"},
		{"no pages", head + "    config:\n      resources:\n        x: {url: https://a.example, maxPages: 0}\n" + mapping, "maxPages must be a whole number"},
		{"unknown algorithm", head + "    config:\n      push: {signature: {header: X-Sig, algorithm: md5, secret: {env: GP_SECRET_X}}}\n      resources:\n        x: {push: {items: .body}}\n" + mapping, "algorithm must be one of sha1, sha256, sha512"},
		{"inline secret", head + "    config:\n      push: {signature: {header: X-Sig, algorithm: sha256, secret: {env: SIGNING}}}\n      resources:\n        x: {push: {items: .body}}\n" + mapping, "starting with GP_SECRET_"},
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

// pulled is what a Pull emitted, by resource.
func pull(t *testing.T, config *HTTPConfig, p PullRequest) (map[string][]Item, map[string]error) {
	t.Helper()
	pulled := map[string][]Item{}
	failed, err := config.Pull(context.Background(), nil, p, func(resource string, items []Item) error {
		pulled[resource] = append(pulled[resource], items...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return pulled, failed
}

func TestHTTPPull(t *testing.T) {
	t.Setenv("GP_SECRET_TRACKER", "Bearer s3cret")
	var since atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer s3cret" || r.Header.Get("Accept-Language") != "en" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/deployments":
			since.Store(r.URL.Query().Get("since"))
			_, _ = w.Write([]byte(`{"deployments": [{"id": 1}, {"id": 2, "status": "deleted"}], "next": "page/2"}`))
		case "/page/2":
			_, _ = w.Write([]byte(`{"deployments": [{"id": 3}], "next": null}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	spec := mustParse(t, httpYAML(server.URL+"/deployments?since={{ $since }}"))
	from := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	pulled, failed := pull(t, httpConfig(t, spec), PullRequest{Resources: []string{"deployments"}, Since: from})
	if len(failed) != 0 {
		t.Fatalf("failed = %v", failed)
	}
	if since.Load() != "2026-11-05T10:00:00Z" {
		t.Fatalf("since = %v, want the time asked for", since.Load())
	}
	var ids []string
	for _, entry := range mustMap(t, spec, pulled["deployments"]...) {
		id := entry.Entity.Identifier
		if entry.Deleted {
			id += " deleted"
		}
		ids = append(ids, id)
	}
	if got := strings.Join(ids, ","); got != "1,2 deleted,3" {
		t.Fatalf("entries = %s, want 1,2 deleted,3 across both pages", got)
	}
}

func TestHTTPPullFollowsLinkHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "2" {
			w.Header().Set("Link", `<?page=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"id": 1}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"id": 2}]`))
	}))
	defer server.Close()

	config := &HTTPConfig{Resource: map[string]HTTPResource{"r": {
		URL: server.URL, Items: ".[]", MaxPages: 5,
		Next: `$headers.link // "" | capture("<(?<url>[^>]+)>; rel=\"next\"").url`,
	}}, order: []string{"r"}}
	pulled, failed := pull(t, config, PullRequest{Resources: []string{"r"}})
	if len(failed) != 0 || len(pulled["r"]) != 2 {
		t.Fatalf("pulled = %+v, failed = %v", pulled, failed)
	}
}

func TestHTTPPullStopsOnLoopsAndErrors(t *testing.T) {
	shortenRetries(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loop":
			_, _ = w.Write([]byte(`{"next": "/loop"}`))
		case "/endless":
			_, _ = w.Write([]byte(`{"next": "/endless?` + r.URL.RawQuery + `x"}`))
		case "/html":
			_, _ = w.Write([]byte(`<html>`))
		default:
			http.Error(w, "down", http.StatusBadGateway)
		}
	}))
	defer server.Close()

	for path, want := range map[string]string{
		"/loop": "loop back", "/endless": "more than 3 pages", "/html": "didn't answer with JSON", "/down": "502",
	} {
		resource := HTTPResource{URL: server.URL + path, Next: ".next", MaxPages: 3}
		if err := fetchPages(context.Background(), "x", resource, resource.URL, func(any) error { return nil }); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", path, err, want)
		}
	}
}

func TestHTTPPullKeepsGoingPastAFailedResource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id": 1}`))
	}))
	defer server.Close()

	config := &HTTPConfig{Resource: map[string]HTTPResource{
		"gone": {URL: server.URL + "/gone", MaxPages: 1}, "here": {URL: server.URL + "/here", MaxPages: 1},
	}, order: []string{"gone", "here"}}
	pulled, failed := pull(t, config, PullRequest{Resources: []string{"gone", "here"}})
	if failed["gone"] == nil || failed["here"] != nil || len(pulled["here"]) != 1 {
		t.Fatalf("pulled = %+v, failed = %v", pulled, failed)
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

			_, _, err := fetchJSON(context.Background(), server.URL, nil)
			if calls.Load() != test.calls {
				t.Errorf("calls = %d, want %d", calls.Load(), test.calls)
			}
			if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Errorf("err = %v, want %q", err, test.want)
			}
		})
	}
}

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))

	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestHTTPReceive(t *testing.T) {
	t.Setenv("GP_SECRET_TRACKER_SIGNING", "signing-secret")
	config := httpConfig(t, mustParse(t, httpYAML("https://tracker.example.com/deployments")))
	body := `{"deployment": {"id": "d1"}}`
	request := func(header http.Header) Request {
		return Request{Resources: config.Receives(), Header: header, Query: url.Values{}, Body: []byte(body)}
	}

	tests := []struct {
		name          string
		header        http.Header
		authenticated bool
		deleted       bool
		want          error
	}{
		{name: "with a token", header: http.Header{}, authenticated: true},
		{name: "signed", header: http.Header{"X-Signature": {sign("signing-secret", body)}}},
		{name: "signed deletion", header: http.Header{"X-Signature": {sign("signing-secret", body)}, "X-Event": {"deleted"}}, deleted: true},
		{name: "neither", header: http.Header{}, want: SignatureError{`sign the request in the X-Signature header, or send an ingestion token as "Authorization: Bearer <token>"`}},
		{name: "signed with another secret", header: http.Header{"X-Signature": {sign("guess", body)}}, want: SignatureError{"the X-Signature signature doesn't match the body"}},
		{name: "without the prefix", header: http.Header{"X-Signature": {strings.TrimPrefix(sign("signing-secret", body), "sha256=")}}, want: SignatureError{"the X-Signature header isn't a signature this integration reads"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			items, err := config.Receive(t.Context(), request(test.header), test.authenticated)
			if !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
			if test.want != nil {
				return
			}
			if len(items) != 1 || items[0].Resource != "deployments" || items[0].Deleted != test.deleted {
				t.Fatalf("items = %+v", items)
			}
		})
	}

	if _, err := config.Receive(t.Context(), Request{Resources: config.Receives(), Body: []byte("{")}, true); !errors.Is(err, ErrNotJSON) {
		t.Fatalf("bad JSON: err = %v", err)
	}
}

func TestHTTPCheck(t *testing.T) {
	t.Setenv("GP_SECRET_TRACKER", "Bearer s3cret")
	t.Setenv("GP_SECRET_TRACKER_SIGNING", "signing-secret")
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer up.Close()

	if err := httpConfig(t, mustParse(t, httpYAML(up.URL))).Check(context.Background(), nil, CheckRequest{}); err != nil {
		t.Fatalf("reachable: %v", err)
	}
	if err := httpConfig(t, mustParse(t, httpYAML(up.URL+"/x"))).Check(context.Background(), nil, CheckRequest{}); err != nil {
		t.Fatalf("reachable on another path: %v", err)
	}
	t.Setenv("GP_SECRET_TRACKER", "")
	if err := httpConfig(t, mustParse(t, httpYAML(up.URL))).Check(context.Background(), nil, CheckRequest{}); err == nil || !strings.Contains(err.Error(), "GP_SECRET_TRACKER isn't set") {
		t.Fatalf("unset secret: %v", err)
	}
}

// shortenRetries makes retried fetches wait a millisecond instead of seconds.
func shortenRetries(t *testing.T) {
	t.Helper()
	backoff := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = backoff })
}

// TestDocumentsCantReferenceTheWebhookKey checks the server's webhook key,
// which every integration's webhook secret derives from, is outside what a
// document can send as a secret.
func TestDocumentsCantReferenceTheWebhookKey(t *testing.T) {
	if strings.HasPrefix(WebhookKeyEnv, SecretEnvPrefix) {
		t.Fatalf("%s starts with %s, which documents can reference", WebhookKeyEnv, SecretEnvPrefix)
	}
	source := strings.Replace(httpYAML("https://tracker.example.com"), "{env: GP_SECRET_TRACKER}", "{env: "+WebhookKeyEnv+"}", 1)
	_, issues := Parse(source)
	if !configdoc.HasErrors(issues) || !strings.Contains(fmt.Sprint(issues), "starting with GP_SECRET_") {
		t.Fatalf("issues = %+v, want the key refused", issues)
	}
}

// TestWebhookSecret checks each integration's webhook secret is its own,
// derived from the server's key, and that there's none without one.
func TestWebhookSecret(t *testing.T) {
	before := webhookKey
	t.Cleanup(func() { webhookKey = before })
	webhookKey = ""
	if got := WebhookSecret("source"); got != "" {
		t.Fatalf("without a key: %q", got)
	}
	webhookKey = "key"
	if a, b := WebhookSecret("source"), WebhookSecret("other"); a == "" || a == b || a != WebhookSecret("source") {
		t.Fatalf("secrets = %q, %q, want each integration's own, the same every time", a, b)
	}
}
