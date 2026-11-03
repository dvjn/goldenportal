package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/oauth"
)

// grants are the tokens the tests sign in with: who they sign in, at their
// role now, and the scopes the user granted.
var grants = map[string]caller{
	"member":         {auth.Principal{ID: "member", Role: auth.RoleMember}, []string{oauth.ScopeRead}},
	"builder":        {auth.Principal{ID: "builder", Role: auth.RoleBuilder}, oauth.Scopes},
	"builder-reader": {auth.Principal{ID: "builder-reader", Role: auth.RoleBuilder}, []string{oauth.ScopeRead}},
	// demoted consented as a builder and has since become a member.
	"demoted": {auth.Principal{ID: "demoted", Role: auth.RoleMember}, oauth.Scopes},
}

type fakeTokens struct{ resource string }

func (f fakeTokens) AccessPrincipal(_ context.Context, token, resource string) (auth.Principal, []string, error) {
	c, ok := grants[token]
	if !ok || resource != f.resource {
		return auth.Principal{}, nil, oauth.ErrInvalidToken
	}
	return c.principal, c.scopes, nil
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

const baseURL = "https://portal.example.com"

func testServer(t *testing.T, services Services) *httptest.Server {
	t.Helper()
	return testServerWith(t, services, NewLimiter())
}

func testServerWith(t *testing.T, services Services, limits *Limiter) *httptest.Server {
	t.Helper()
	server := oauth.NewServer(baseURL, "Acme")
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	s := httptest.NewServer(Middleware(services, testContext, fakeTokens{server.Resource()}, server, "test", limits)(next))
	t.Cleanup(s.Close)
	return s
}

func connect(t *testing.T, server *httptest.Server, token string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             server.URL + oauth.MCPPath,
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

func listed(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestToolsFollowRoleAndScope(t *testing.T) {
	server := testServer(t, newFake())
	for _, test := range []struct {
		token                       string
		builderReads, authoringOpen bool
	}{
		{token: "member"},
		{token: "builder", builderReads: true, authoringOpen: true},
		{token: "builder-reader", builderReads: true},
		// The role now decides, whatever the token was granted.
		{token: "demoted"},
	} {
		t.Run(test.token, func(t *testing.T) {
			session := connect(t, server, test.token)
			names := listed(t, session)
			if !slices.Contains(names, "query_entities") || !slices.Contains(names, "search") {
				t.Errorf("read tools missing: %v", names)
			}
			if slices.Contains(names, "preview_impact") != test.builderReads {
				t.Errorf("builder reads offered = %v, want %v", !test.builderReads, test.builderReads)
			}
			if slices.Contains(names, "save_draft") != test.authoringOpen {
				t.Errorf("authoring offered = %v, want %v", !test.authoringOpen, test.authoringOpen)
			}
			// Calls run as the token's user, never another request's.
			out := output(t, call(t, session, "list_catalog", map[string]any{}))
			if out.Status != http.StatusOK || out.Data.([]any)[0].(map[string]any)["identifier"] != test.token {
				t.Errorf("list_catalog ran as %+v", out)
			}
			help, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: guideURI})
			if err != nil || !strings.Contains(help.Contents[0].Text, "GP_SECRET_") {
				t.Errorf("authoring help: %v, %v", help, err)
			}
		})
	}
}

func TestAuthoringAnnotations(t *testing.T) {
	session := connect(t, testServer(t, newFake()), "builder")
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, listed := range result.Tools {
		def := tools[slices.IndexFunc(tools, func(d tool) bool { return d.name == listed.Name })]
		if listed.Annotations.ReadOnlyHint != (def.scope == oauth.ScopeRead) {
			t.Errorf("%s: readOnlyHint = %v, scope %s", listed.Name, listed.Annotations.ReadOnlyHint, def.scope)
		}
		if listed.Name == "sync_integration" && !*listed.Annotations.OpenWorldHint {
			t.Error("sync must declare it reaches outside the portal")
		}
	}
}

func post(t *testing.T, server *httptest.Server, token string, body string, header http.Header) *http.Response {
	t.Helper()
	r, _ := http.NewRequest(http.MethodPost, server.URL+oauth.MCPPath, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for name, values := range header {
		r.Header[name] = values
	}
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func TestChallengesSayWhereToSignIn(t *testing.T) {
	server := testServer(t, newFake())
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	metadata := `resource_metadata="` + baseURL + `/.well-known/oauth-protected-resource/mcp"`

	missing := post(t, server, "", initialize, nil)
	challenge := missing.Header.Get("WWW-Authenticate")
	if missing.StatusCode != http.StatusUnauthorized || !strings.Contains(challenge, metadata) ||
		!strings.Contains(challenge, `scope="portal:read portal:run portal:author"`) {
		t.Errorf("no token: %d %q", missing.StatusCode, challenge)
	}
	invalid := post(t, server, "stolen", initialize, nil)
	if invalid.StatusCode != http.StatusUnauthorized || !strings.Contains(invalid.Header.Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Errorf("unknown token: %d %q", invalid.StatusCode, invalid.Header.Get("WWW-Authenticate"))
	}

	// A builder who granted only reading is asked for authoring when they
	// use it, keeping what they have.
	save := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"save_draft","arguments":{}}}`
	stepUp := post(t, server, "builder-reader", save, nil)
	challenge = stepUp.Header.Get("WWW-Authenticate")
	if stepUp.StatusCode != http.StatusForbidden || !strings.Contains(challenge, `error="insufficient_scope"`) ||
		!strings.Contains(challenge, `scope="portal:read portal:author"`) || !strings.Contains(challenge, metadata) {
		t.Errorf("step-up: %d %q", stepUp.StatusCode, challenge)
	}
	// A member couldn't grant it, so there's nothing to ask for: the tool
	// just isn't offered.
	if member := post(t, server, "member", save, nil); member.StatusCode == http.StatusForbidden {
		t.Errorf("member was asked to step up: %q", member.Header.Get("WWW-Authenticate"))
	}

	foreign := post(t, server, "builder", initialize, http.Header{"Origin": {"https://evil.example"}})
	if foreign.StatusCode != http.StatusForbidden {
		t.Errorf("foreign origin: %d", foreign.StatusCode)
	}
	own := post(t, server, "builder", initialize, http.Header{"Origin": {baseURL}})
	if own.StatusCode != http.StatusOK {
		t.Errorf("own origin: %d", own.StatusCode)
	}
}

func TestToolsCallServicesAsTheCaller(t *testing.T) {
	fake := newFake()
	session := connect(t, testServer(t, fake), "builder")

	call(t, session, "save_draft", map[string]any{"kind": "blueprints", "name": "service", "source": "kind: Blueprint"})
	if got := *fake.last; got.name != "SaveDraft" || got.user != "builder" || !slices.Equal(got.args, []any{"blueprints", "service", "kind: Blueprint"}) {
		t.Errorf("save_draft called %+v", got)
	}
	call(t, session, "apply_documents", map[string]any{"source": "x"})
	if got := *fake.last; got.name != "Apply" || got.args[1] != true {
		t.Errorf("apply_documents defaults to a dry run, called %+v", got)
	}
	call(t, session, "create_ingestion_token", map[string]any{"integration": "push", "name": "CI"})
	if got := *fake.last; got.name != "CreateIngestionToken" || !slices.Equal(got.args, []any{"push", "CI"}) {
		t.Errorf("create_ingestion_token called %+v", got)
	}
	call(t, session, "get_dashboard_data", map[string]any{"name": "services", "inputs": map[string]any{"team": []string{"payments"}, "tier": []string{}}})
	if inputs, _ := fake.last.args[1].(map[string][]string); fake.last.args[0] != "services" ||
		!slices.Equal(inputs["team"], []string{"payments"}) || inputs["tier"] == nil || len(inputs["tier"]) != 0 {
		t.Errorf("get_dashboard_data called %+v", *fake.last)
	}

	// A service's refusal is the tool's error, with its status; anything
	// else is the portal's problem, which the caller isn't told the details of.
	refused := call(t, session, "delete_document", map[string]any{"kind": "blueprints", "name": "service", "confirm": "nope"})
	if out := output(t, refused); !refused.IsError || out.Status != http.StatusBadRequest {
		t.Errorf("refused delete = %+v", out)
	}
	failed := call(t, session, "sync_integration", map[string]any{"name": "source"})
	if out := output(t, failed); !failed.IsError || out.Status != http.StatusInternalServerError || strings.Contains(fmt.Sprint(out.Error), "10.0.0.5") {
		t.Errorf("failed sync = %+v", out)
	}
	unknown := call(t, session, "list_documents", map[string]any{"kind": "widgets"})
	if out := output(t, unknown); !unknown.IsError || out.Status != http.StatusBadRequest {
		t.Errorf("unknown kind = %+v", out)
	}
}
