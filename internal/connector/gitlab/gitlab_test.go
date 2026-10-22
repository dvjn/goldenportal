package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dvjn/goldport/internal/connector"
)

const token = "glpat-test-token"

// fakeGitLab serves the acme group with one subgroup on each of two pages,
// three projects on two pages, and a pipeline for the first project.
func fakeGitLab(t *testing.T) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	page := func(w http.ResponseWriter, r *http.Request, pages ...string) {
		number := r.URL.Query().Get("page")
		index := map[string]int{"": 0, "1": 0, "2": 1}[number]
		if index+1 < len(pages) {
			next := *r.URL
			query := next.Query()
			query.Set("page", fmt.Sprint(index+2))
			next.RawQuery = query.Encode()
			w.Header().Set("Link", fmt.Sprintf(`<%s%s>; rel="next", <%s%s>; rel="first"`, server.URL, next.RequestURI(), server.URL, r.URL.Path))
		}
		_, _ = w.Write([]byte(pages[index]))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/groups/acme", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id": 1, "path": "acme", "full_path": "acme", "name": "Acme"}`))
	})
	mux.HandleFunc("GET /api/v4/groups/acme/descendant_groups", func(w http.ResponseWriter, r *http.Request) {
		page(w, r, `[{"id": 2, "path": "payments", "full_path": "acme/payments"}]`, `[{"id": 3, "path": "platform", "full_path": "acme/platform"}]`)
	})
	mux.HandleFunc("GET /api/v4/groups/acme/projects", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("include_subgroups") != "true" {
			http.Error(w, "want include_subgroups", http.StatusBadRequest)
			return
		}
		page(w, r,
			`[{"id": 11, "name": "Ledger", "path_with_namespace": "acme/payments/ledger", "default_branch": "main", "web_url": "https://gitlab.example.test/acme/payments/ledger"},
			  {"id": 12, "name": "Search", "path_with_namespace": "acme/platform/search", "default_branch": "trunk"}]`,
			`[{"id": 13, "name": "Empty", "path_with_namespace": "acme/empty", "default_branch": null}]`,
		)
	})
	mux.HandleFunc("GET /api/v4/projects/11/pipelines", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != "main" || r.URL.Query().Get("per_page") != "1" {
			http.Error(w, "want the latest pipeline of main", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`[{"id": 501, "status": "success", "ref": "main"}]`))
	})
	mux.HandleFunc("GET /api/v4/projects/12/pipelines", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Private-Token") != token {
			http.Error(w, `{"message": "401 Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	return server
}

func request(server *httptest.Server, kinds ...string) connector.Request {
	return connector.Request{
		Config:  json.RawMessage(fmt.Sprintf(`{"url": %q, "group": "acme"}`, server.URL+"/")),
		Secrets: map[string]string{"token": token},
		Kinds:   kinds,
	}
}

func identifiers(records []any, field string) string {
	var values []string
	for _, record := range records {
		values = append(values, fmt.Sprint(record.(map[string]any)[field]))
	}

	return strings.Join(values, ",")
}

func TestFetchReadsEveryPageOfGroupsAndProjects(t *testing.T) {
	server := fakeGitLab(t)

	records, err := New(server.Client()).Fetch(context.Background(), request(server, KindGroup, KindProject))
	if err != nil {
		t.Fatal(err)
	}

	if got := identifiers(records[KindGroup], "full_path"); got != "acme,acme/payments,acme/platform" {
		t.Fatalf("groups = %s, want acme and both subgroups", got)
	}
	if got := identifiers(records[KindProject], "path_with_namespace"); got != "acme/payments/ledger,acme/platform/search,acme/empty" {
		t.Fatalf("projects = %s, want all three", got)
	}
	if _, read := records[KindPipeline]; read {
		t.Fatal("read pipelines, want only the kinds asked for")
	}
}

func TestFetchReadsTheLatestPipelineOfEachDefaultBranch(t *testing.T) {
	server := fakeGitLab(t)

	records, err := New(server.Client()).Fetch(context.Background(), request(server, KindPipeline))
	if err != nil {
		t.Fatal(err)
	}

	pipelines := records[KindPipeline]
	if len(pipelines) != 1 {
		t.Fatalf("pipelines = %v, want the one of ledger", pipelines)
	}
	pipeline := pipelines[0].(map[string]any)
	project := pipeline["project"].(map[string]any)
	if pipeline["status"] != "success" || project["path_with_namespace"] != "acme/payments/ledger" {
		t.Fatalf("pipeline = %v, want ledger's successful pipeline", pipeline)
	}
	if _, read := records[KindProject]; read {
		t.Fatal("returned projects, want only pipelines")
	}
}

func TestFetchExplainsWhatWentWrong(t *testing.T) {
	server := fakeGitLab(t)
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://elsewhere.example.test/api/v4/groups?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(outside.Close)

	cases := []struct {
		name    string
		request connector.Request
		message string
	}{
		{
			name:    "rejected token",
			request: connector.Request{Config: request(server).Config, Secrets: map[string]string{"token": "wrong"}, Kinds: []string{KindGroup}},
			message: "GitLab answered 401 Unauthorized for /api/v4/groups/acme",
		},
		{
			name:    "unreachable instance",
			request: connector.Request{Config: request(closed).Config, Secrets: map[string]string{"token": token}, Kinds: []string{KindGroup}},
			message: "could not reach GitLab at " + closed.URL,
		},
		{
			name:    "next page outside the instance",
			request: connector.Request{Config: request(outside).Config, Secrets: map[string]string{"token": token}, Kinds: []string{KindProject}},
			message: "GitLab gave a next page outside " + outside.URL + "/api/v4",
		},
		{
			name:    "no token",
			request: connector.Request{Config: request(server).Config, Kinds: []string{KindGroup}},
			message: "give the integration a token secret under secrets",
		},
		{
			name:    "no group",
			request: connector.Request{Config: json.RawMessage(`{"url": "https://gitlab.example.test"}`), Secrets: map[string]string{"token": token}},
			message: "give the config a group",
		},
		{
			name:    "unknown config key",
			request: connector.Request{Config: json.RawMessage(`{"url": "https://gitlab.example.test", "group": "acme", "groups": []}`)},
			message: `the config is not valid: json: unknown field "groups"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(server.Client()).Fetch(context.Background(), tc.request)

			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("Fetch() error = %v, want %q", err, tc.message)
			}
		})
	}
}

func TestNextLinkReadsTheNextRelation(t *testing.T) {
	header := http.Header{"Link": {`<https://gitlab.example.test/api/v4/groups?page=1>; rel="first", <https://gitlab.example.test/api/v4/groups?page=3>; rel="next"`}}

	if got := nextLink(header); got != "https://gitlab.example.test/api/v4/groups?page=3" {
		t.Fatalf("nextLink() = %q", got)
	}
	if got := nextLink(http.Header{}); got != "" {
		t.Fatalf("nextLink() = %q, want none", got)
	}
}
