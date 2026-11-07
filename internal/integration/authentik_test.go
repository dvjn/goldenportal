package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const authentikYAML = `kind: Integration
metadata: {identifier: sso, title: authentik}
spec:
  connector:
    type: authentik
    config:
      url: https://auth.example.com
      token: {env: GP_SECRET_AUTHENTIK}
  mappings:
    - resource: groups
      blueprint: team
      filter: .attributes.goldenportal.team == true
      entity: {identifier: .name}
    - resource: users
      blueprint: user
      entity: {identifier: .username}
`

// authentikFixture is an authentik instance with two users, ada and a
// service account, and four groups: engineering; payments, its child, which
// is marked a team; payments-lead, payments' child, which ada is in; and
// legacy, which names engineering as its one parent the way older authentik
// does. Each list comes a page at a time.
func authentikFixture(t *testing.T) *httptest.Server {
	t.Helper()
	pages := map[string][]any{
		"/api/v3/core/users/": {
			map[string]any{"pk": 1, "username": "ada", "name": "Ada", "type": "internal", "attributes": map[string]any{}, "groups": []any{"g3"}},
			map[string]any{"pk": 7, "username": "ak-outpost", "type": "internal_service_account", "groups": []any{}},
		},
		"/api/v3/core/groups/": {
			map[string]any{"pk": "g1", "name": "engineering", "parents": []any{}, "users": []any{}},
			map[string]any{
				"pk": "g2", "name": "payments", "parents": []any{"g1"}, "users": []any{},
				"attributes": map[string]any{"goldenportal": map[string]any{"team": true}},
			},
			map[string]any{"pk": "g3", "name": "payments-lead", "parents": []any{"g2"}, "users": []any{1}},
			map[string]any{"pk": "g4", "name": "legacy", "parent": "g1", "users": []any{}},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ak-token" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"detail": "Token invalid/expired"}`))
			return
		}
		if r.URL.Path == "/api/v3/core/users/me/" {
			_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"username": "goldenportal"}})
			return
		}
		all, ok := pages[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		q := r.URL.Query()
		if q.Get("page_size") == "1" {
			_ = json.NewEncoder(w).Encode(map[string]any{"pagination": map[string]any{"count": len(all)}, "results": all[:1]})
			return
		}
		// Two to a page, so a list takes more than one.
		page := map[string]int{"1": 0, "2": 2}[q.Get("page")]
		end, next := min(page+2, len(all)), 0
		if end < len(all) {
			next = 2
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"pagination": map[string]any{"next": next, "count": len(all)}, "results": all[page:end]})
	}))
	t.Cleanup(server.Close)
	t.Setenv("GP_SECRET_AUTHENTIK", "ak-token")

	return server
}

func authentikConfig(t *testing.T, url string) *AuthentikConfig {
	t.Helper()
	spec := mustParse(t, strings.Replace(authentikYAML, "https://auth.example.com", url, 1))
	config, ok := spec.config.(*AuthentikConfig)
	if !ok {
		t.Fatalf("config = %T, want the authentik connector's", spec.config)
	}

	return config
}

func TestAuthentikConfigProblems(t *testing.T) {
	for _, c := range []struct{ config, want string }{
		{"      token: {env: GP_SECRET_AUTHENTIK}\n", "add the instance's url"},
		{"      url: https://auth.example.com\n", "add a token"},
		{"      url: auth.example.com\n      token: {env: GP_SECRET_AUTHENTIK}\n", "url must be"},
	} {
		source := strings.Replace(authentikYAML, "      url: https://auth.example.com\n      token: {env: GP_SECRET_AUTHENTIK}\n", c.config, 1)
		_, issues := Parse(source)
		if len(issues) != 1 || !strings.Contains(issues[0].Message, c.want) {
			t.Errorf("%q: issues = %+v, want one that says %q", c.config, issues, c.want)
		}
	}
}

func TestAuthentikPull(t *testing.T) {
	config := authentikConfig(t, authentikFixture(t).URL)
	items := map[string][]Item{}
	failed, err := config.Pull(t.Context(), nil, PullRequest{Resources: []string{authentikUsers, authentikGroups}, Warn: func(string) {}},
		func(resource string, page []Item) error {
			items[resource] = append(items[resource], page...)
			return nil
		})
	if err != nil || len(failed) != 0 {
		t.Fatalf("failed = %v, err = %v", failed, err)
	}
	if len(items[authentikUsers]) != 2 || len(items[authentikGroups]) != 4 {
		t.Fatalf("read %d users and %d groups, want every page of each", len(items[authentikUsers]), len(items[authentikGroups]))
	}
	got, _ := json.Marshal(items)
	for _, want := range []string{
		// A user's groups by name: those it's in through a child group too,
		// and those it was added to.
		`"_gp":{"directGroups":["payments-lead"],"groups":["engineering","payments","payments-lead"]}`,
		// A group's members the same two ways, and its ancestors by name,
		// whether it names its parents or its one parent.
		`"_gp":{"ancestors":["engineering"],"directMembers":[],"members":["ada"]},"attributes":{"goldenportal":{"team":true}}`,
		`"_gp":{"ancestors":["engineering","payments"],"directMembers":["ada"],"members":["ada"]}`,
		`"_gp":{"ancestors":["engineering"],"directMembers":[],"members":[]},"name":"legacy"`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("items = %s\nwant %s", got, want)
		}
	}
	for _, item := range append(items[authentikUsers], items[authentikGroups]...) {
		if v := nonJSON(item.Object); v != "" {
			t.Errorf("%s holds %s, which jq can't read", item.Resource, v)
		}
	}
}

func TestAuthentikPullsOnlyWhatsMapped(t *testing.T) {
	config := authentikConfig(t, authentikFixture(t).URL)
	read := map[string]int{}
	if _, err := config.Pull(t.Context(), nil, PullRequest{Resources: []string{authentikGroups}, Warn: func(string) {}},
		func(resource string, page []Item) error { read[resource] += len(page); return nil }); err != nil {
		t.Fatal(err)
	}
	if read[authentikUsers] != 0 || read[authentikGroups] != 4 {
		t.Errorf("read = %v, want only the groups", read)
	}
}

func TestAuthentikCheck(t *testing.T) {
	config := authentikConfig(t, authentikFixture(t).URL)
	var warned []string
	if err := config.Check(t.Context(), nil, CheckRequest{Warn: func(w string) { warned = append(warned, w) }}); err != nil || len(warned) != 0 {
		t.Errorf("check = %v, warned %v, want it to pass", err, warned)
	}
	t.Setenv("GP_SECRET_AUTHENTIK", "wrong")
	if err := config.Check(t.Context(), nil, CheckRequest{Warn: func(string) {}}); err == nil || !strings.Contains(err.Error(), "Token invalid") {
		t.Errorf("check = %v, want authentik's refusal", err)
	}
}
