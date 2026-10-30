package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/httpx"
)

type resolver struct{}

func (resolver) SessionPrincipal(context.Context, []byte) (auth.Principal, error) {
	return auth.Principal{}, errors.New("no session")
}

func (resolver) TokenPrincipal(_ context.Context, hash []byte) (auth.Principal, bool, error) {
	for _, token := range []string{"builder", "readonly", "member"} {
		sum := sha256.Sum256([]byte(token))
		if string(sum[:]) == string(hash) {
			role := auth.RoleBuilder
			if token == "member" {
				role = auth.RoleMember
			}
			return auth.Principal{ID: token, Email: token + "@example.com", Role: role}, token == "readonly", nil
		}
	}
	return auth.Principal{}, false, errors.New("unknown token")
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

func testServer(t *testing.T, api http.Handler) *httptest.Server {
	t.Helper()
	protected := httpx.WithQuery(auth.Middleware(resolver{})(api))
	server := httptest.NewServer(auth.Middleware(resolver{})(Middleware(protected, "")(api)))
	t.Cleanup(server.Close)
	return server
}

func connect(t *testing.T, server *httptest.Server, token string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             server.URL + auth.MCPPath,
		HTTPClient:           &http.Client{Transport: bearerTransport{token, server.Client().Transport}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func call(t *testing.T, session *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func output(t *testing.T, result *mcp.CallToolResult) Output {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out Output
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestHTTPProtocolAndPermissions(t *testing.T) {
	server := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"user": p.ID}})
	}))
	for _, token := range []string{"builder", "readonly", "member"} {
		t.Run(token, func(t *testing.T) {
			session := connect(t, server, token)
			listed, err := session.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			tools := map[string]*mcp.Tool{}
			for _, tool := range listed.Tools {
				tools[tool.Name] = tool
			}
			if tools["query_entities"] == nil {
				t.Fatal("read tool missing")
			}
			if (tools["save_draft"] != nil) != (token == "builder") {
				t.Fatalf("wrong authoring permissions: %v", tools)
			}
			if (tools["preview_impact"] != nil) != (token != "member") {
				t.Fatal("wrong builder read permissions")
			}
			if token == "builder" && !*tools["sync_integration"].Annotations.OpenWorldHint {
				t.Fatal("sync must declare external access")
			}
			out := output(t, call(t, session, "list_catalog", map[string]any{}))
			if out.Status != 200 || out.Data.(map[string]any)["user"] != token {
				t.Fatalf("caller leaked across requests: %+v", out)
			}
			if token != "builder" {
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "save_draft", Arguments: map[string]any{"kind": "blueprints", "name": "x", "source": "x"}})
				if err == nil && !result.IsError {
					t.Fatal("write tool callable")
				}
			}
			resources, err := session.ListResources(context.Background(), nil)
			if err != nil || len(resources.Resources) != 1 {
				t.Fatalf("resources: %v, %v", resources, err)
			}
			help, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "portal://authoring"})
			if err != nil || !strings.Contains(help.Contents[0].Text, "GP_SECRET_") {
				t.Fatalf("authoring help: %v, %v", help, err)
			}
		})
	}
}

func TestAuthoringBridge(t *testing.T) {
	var gotMethod, gotPath, gotQuery, gotBody string
	server := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, httpx.Query(r.Context()).Encode()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		data, _ := json.Marshal(body)
		gotBody = string(data)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/validate") {
			w.WriteHeader(422)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid document","issues":[{"path":"spec.properties","line":4,"severity":"error"}]}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/export") {
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte("kind: Blueprint\n"))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	session := connect(t, server, "builder")
	for _, tt := range []struct {
		name                      string
		args                      any
		method, path, query, body string
	}{
		{"save_draft", map[string]any{"kind": "blueprints", "name": "service", "source": "yaml"}, "PUT", "/api/v1/blueprints/service/draft", "", `{"source":"yaml"}`},
		{"publish_document", map[string]any{"kind": "blueprints", "name": "service", "acknowledgeBreaking": true}, "POST", "/api/v1/blueprints/service/draft/publish", "", `{"acknowledgeBreaking":true}`},
		{"apply_documents", map[string]any{"source": "yaml"}, "POST", "/api/v1/apply", "dryRun=true", `{"source":"yaml"}`},
		{"apply_documents", map[string]any{"source": "yaml", "dryRun": false}, "POST", "/api/v1/apply", "dryRun=false", `{"source":"yaml"}`},
		{"query_entities", map[string]any{"blueprint": "service", "search": "a & b", "sort": "title", "page": 2, "size": 10}, "GET", "/api/v1/entities", "blueprint=service&page=2&q=a+%26+b&size=10&sort=title", "null"},
		{"get_entity", map[string]any{"blueprint": "service", "identifier": "team/repo#1?"}, "GET", "/api/v1/entities/service/team/repo#1?", "", "null"},
	} {
		t.Run(tt.name+tt.query, func(t *testing.T) {
			result := call(t, session, tt.name, tt.args)
			if result.IsError || gotMethod != tt.method || gotPath != tt.path || gotQuery != tt.query || gotBody != tt.body {
				t.Fatalf("got %s %s?%s %s; result %+v", gotMethod, gotPath, gotQuery, gotBody, result)
			}
		})
	}
	result := call(t, session, "validate_document", map[string]any{"kind": "blueprints", "source": "bad"})
	out := output(t, result)
	if !result.IsError || out.Status != 422 || out.Error.(map[string]any)["issues"] == nil {
		t.Fatalf("validation lost issues: %+v", out)
	}
	out = output(t, call(t, session, "export_document", map[string]any{"kind": "blueprints", "name": "service"}))
	if out.Source != "kind: Blueprint\n" {
		t.Fatalf("export = %+v", out)
	}
	for _, tt := range []struct {
		name string
		args any
	}{
		{"delete_document", map[string]any{"kind": "blueprints", "name": "service", "confirm": "wrong"}},
		{"get_document", map[string]any{"kind": "../users", "name": "service"}},
		{"get_entity", map[string]any{"blueprint": "service", "identifier": "../../users"}},
		{"list_catalog", map[string]any{"unexpected": true}},
		{"get_document", map[string]any{"kind": "blueprints"}},
	} {
		gotPath = ""
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tt.name, Arguments: tt.args})
		if err == nil && !result.IsError {
			t.Errorf("accepted invalid args: %s %+v", tt.name, tt.args)
		}
		if gotPath != "" {
			t.Errorf("invalid args reached API %s", gotPath)
		}
	}
}

func TestTransportRejectsForeignOriginAndUnsignedRequests(t *testing.T) {
	server := testServer(t, http.NotFoundHandler())
	for _, tt := range []struct {
		token, origin string
		status        int
	}{
		{"builder", "https://evil.example", 403},
		{"", "", 403}, // Cookie-authenticated POST needs CSRF even before session lookup.
	} {
		req, _ := http.NewRequest("POST", server.URL+auth.MCPPath, strings.NewReader(`{}`))
		if tt.token != "" {
			req.Header.Set("Authorization", "Bearer "+tt.token)
		}
		req.Header.Set("Origin", tt.origin)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tt.status {
			t.Errorf("status = %d, want %d", resp.StatusCode, tt.status)
		}
	}
}

func TestBridgeRechecksReadOnlyPolicy(t *testing.T) {
	api := auth.Middleware(resolver{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	r := httptest.NewRequest("POST", auth.MCPPath, nil)
	r.Header.Set("Authorization", "Bearer readonly")
	b := bridge{api: api, request: r}
	result, out, err := b.call(context.Background(), apiCall{method: "PUT", path: "/api/v1/blueprints/service/draft"})
	if err != nil || !result.IsError || out.Status != 403 {
		t.Fatalf("read-only write bypass: %+v, %v", out, err)
	}
}

// TestOriginFollowsConfiguredBaseURL checks a proxy rewriting Host doesn't
// block the public origin, and that the rewritten Host isn't trusted instead.
func TestOriginFollowsConfiguredBaseURL(t *testing.T) {
	handler := auth.Middleware(resolver{})(Middleware(http.NotFoundHandler(), "https://portal.example.com")(http.NotFoundHandler()))
	for origin, rejected := range map[string]bool{
		"https://portal.example.com": false,
		"http://internal:8080":       true,
	} {
		req := httptest.NewRequest("POST", "http://internal:8080"+auth.MCPPath, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer builder")
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if got := strings.Contains(w.Body.String(), "MCP origin is not allowed"); got != rejected {
			t.Errorf("origin %s rejected = %v: %d %s", origin, got, w.Code, w.Body)
		}
	}
}
