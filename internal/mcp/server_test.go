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
	"member-runner":  {auth.Principal{ID: "member-runner", Role: auth.RoleMember}, []string{oauth.ScopeRead, oauth.ScopeRun}},
	"builder":        {auth.Principal{ID: "builder", Role: auth.RoleBuilder}, oauth.Scopes},
	"builder-reader": {auth.Principal{ID: "builder-reader", Role: auth.RoleBuilder}, []string{oauth.ScopeRead}},
	// demoted consented as a builder and has since become a member.
	"demoted": {auth.Principal{ID: "demoted", Role: auth.RoleMember}, oauth.Scopes},
}

// fakeTokens accept the grants' tokens at the endpoints' resources.
type fakeTokens struct{ resources []string }

func (f fakeTokens) AccessPrincipal(_ context.Context, token, resource string) (auth.Principal, []string, error) {
	c, ok := grants[token]
	if !ok || !slices.Contains(f.resources, resource) {
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

var (
	userPath    = oauth.UserMCP.Path
	builderPath = oauth.BuilderMCP.Path
)

func testServer(t *testing.T, services Services) *httptest.Server {
	t.Helper()
	return testServerWith(t, services, NewLimiter())
}

func testServerWith(t *testing.T, services Services, limits *Limiter) *httptest.Server {
	t.Helper()
	server := oauth.NewServer(baseURL, "Acme")
	tokens := fakeTokens{[]string{server.Resource(oauth.UserMCP), server.Resource(oauth.BuilderMCP)}}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	s := httptest.NewServer(Middleware(services, testContext, tokens, server, "test", limits)(next))
	t.Cleanup(s.Close)
	return s
}

func connect(t *testing.T, server *httptest.Server, path, token string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             server.URL + path,
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

// The tool lists are what agents see, so a change to either is deliberate.
var (
	sharedToolNames = []string{"search", "list_blueprints", "get_catalog", "get_entity", "get_scorecard", "get_dashboard"}
	userToolNames   = append(slices.Clone(sharedToolNames),
		"list_dashboards", "list_self_service", "run_action", "run_workflow", "get_run", "list_runs", "cancel_run", "retry_run", "list_approvals")
	builderToolNames = append(slices.Clone(sharedToolNames),
		"list_documents", "get_document", "get_document_schema", "list_versions", "get_version", "export_document", "export_documents",
		"list_connectors", "get_blueprint_usage", "preview_impact", "preview_dashboard", "list_sync_runs", "list_ingestion_tokens",
		"validate_document", "save_draft", "discard_draft", "publish_document", "delete_document", "apply_documents", "publish_documents",
		"sync_connection", "check_connection", "evaluate_scorecard", "create_ingestion_token", "revoke_ingestion_token")
)

func sameNames(got, want []string) bool {
	got, want = slices.Clone(got), slices.Clone(want)
	slices.Sort(got)
	slices.Sort(want)
	return slices.Equal(got, want)
}

func TestEachEndpointListsExactlyItsTools(t *testing.T) {
	server := testServer(t, newFake())
	if names := listed(t, connect(t, server, userPath, "builder")); !sameNames(names, userToolNames) {
		t.Errorf("user tools = %v", names)
	}
	if names := listed(t, connect(t, server, builderPath, "builder")); !sameNames(names, builderToolNames) {
		t.Errorf("builder tools = %v", names)
	}
}

// A config document is an identifier in every tool; only a token's label
// is a name.
func TestToolsNameDocumentsByIdentifier(t *testing.T) {
	server := testServer(t, newFake())
	for _, path := range []string{userPath, builderPath} {
		result, err := connect(t, server, path, "builder").ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range result.Tools {
			raw, _ := json.Marshal(tool.InputSchema)
			var schema struct {
				Properties map[string]json.RawMessage `json:"properties"`
			}
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatal(err)
			}
			for _, banned := range []string{"connection", "workflow"} {
				// list_runs filters by action and workflow, as GET runs does.
				if banned == "workflow" && tool.Name == "list_runs" {
					continue
				}
				if _, ok := schema.Properties[banned]; ok {
					t.Errorf("%s takes %q, want identifier", tool.Name, banned)
				}
			}
			if _, ok := schema.Properties["name"]; ok && tool.Name != "create_ingestion_token" {
				t.Errorf("%s takes name, want identifier", tool.Name)
			}
		}
	}
}

func TestToolsFollowScope(t *testing.T) {
	server := testServer(t, newFake())
	for _, test := range []struct {
		path, token string
		more        string
		offered     bool
	}{
		{path: userPath, token: "member", more: "run_action"},
		{path: userPath, token: "member-runner", more: "run_action", offered: true},
		// A member who once consented as a builder still runs on the user's server.
		{path: userPath, token: "demoted", more: "run_action", offered: true},
		{path: builderPath, token: "builder", more: "save_draft", offered: true},
		{path: builderPath, token: "builder-reader", more: "save_draft"},
	} {
		t.Run(test.path+" "+test.token, func(t *testing.T) {
			session := connect(t, server, test.path, test.token)
			names := listed(t, session)
			if !slices.Contains(names, "get_catalog") || !slices.Contains(names, "search") {
				t.Errorf("read tools missing: %v", names)
			}
			if slices.Contains(names, test.more) != test.offered {
				t.Errorf("%s offered = %v, want %v", test.more, !test.offered, test.offered)
			}
			// Calls run as the token's user, never another request's.
			out := output(t, call(t, session, "list_blueprints", map[string]any{}))
			if out.Status != http.StatusOK || out.Data.([]any)[0].(map[string]any)["identifier"] != test.token {
				t.Errorf("list_blueprints ran as %+v", out)
			}
		})
	}
}

func TestBuilderServerRefusesMembers(t *testing.T) {
	server := testServer(t, newFake())
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	// The role decides, whatever the token was granted.
	for _, token := range []string{"member", "demoted"} {
		refused := post(t, server, builderPath, token, initialize, nil)
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(refused.Body).Decode(&body)
		if refused.StatusCode != http.StatusForbidden || !strings.Contains(body.Error.Message, "for builders") ||
			!strings.Contains(body.Error.Message, baseURL+"/api/mcp ") {
			t.Errorf("%s on the builder server: %d %+v", token, refused.StatusCode, body)
		}
	}
	if builder := post(t, server, builderPath, "builder", initialize, nil); builder.StatusCode != http.StatusOK {
		t.Errorf("builder on the builder server: %d", builder.StatusCode)
	}
}

func TestEndpointsAnswerBeforeTheWebAPI(t *testing.T) {
	issuer := oauth.NewServer(baseURL, "Acme")
	tokens := fakeTokens{[]string{issuer.Resource(oauth.UserMCP), issuer.Resource(oauth.BuilderMCP)}}
	shell := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	server := httptest.NewServer(Middleware(newFake(), testContext, tokens, issuer, "test", NewLimiter())(auth.Middleware(nil, true)(shell)))
	t.Cleanup(server.Close)
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	// A bearer token and no CSRF header, which the web API would refuse.
	for _, path := range []string{userPath, builderPath} {
		if got := post(t, server, path, "builder", initialize, nil); got.StatusCode != http.StatusOK {
			t.Errorf("POST %s: %d", path, got.StatusCode)
		}
	}
	for _, path := range []string{"/mcp", "/mcp/builder"} {
		if got := post(t, server, path, "builder", initialize, nil); got.StatusCode != http.StatusTeapot {
			t.Errorf("POST %s was served: %d", path, got.StatusCode)
		}
	}
}

func TestAnnotationsFollowScope(t *testing.T) {
	server := testServer(t, newFake())
	for _, s := range servers {
		result, err := connect(t, server, s.endpoint.Path, "builder").ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, listed := range result.Tools {
			def := s.tools[slices.IndexFunc(s.tools, func(d tool) bool { return d.name == listed.Name })]
			if listed.Annotations.ReadOnlyHint != (def.scope == oauth.ScopeRead) {
				t.Errorf("%s: readOnlyHint = %v, scope %s", listed.Name, listed.Annotations.ReadOnlyHint, def.scope)
			}
			if listed.Name == "sync_connection" && !*listed.Annotations.OpenWorldHint {
				t.Error("sync must declare it reaches outside the portal")
			}
		}
	}
}

func post(t *testing.T, server *httptest.Server, path, token, body string, header http.Header) *http.Response {
	t.Helper()
	r, _ := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
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

	// Each endpoint points at its own metadata and asks for its own scopes.
	for path, scope := range map[string]string{userPath: "portal:read portal:run", builderPath: "portal:read portal:author"} {
		metadata := `resource_metadata="` + baseURL + `/.well-known/oauth-protected-resource` + path + `"`
		missing := post(t, server, path, "", initialize, nil)
		challenge := missing.Header.Get("WWW-Authenticate")
		if missing.StatusCode != http.StatusUnauthorized || !strings.Contains(challenge, metadata) ||
			!strings.Contains(challenge, `scope="`+scope+`"`) {
			t.Errorf("%s without a token: %d %q", path, missing.StatusCode, challenge)
		}
		invalid := post(t, server, path, "stolen", initialize, nil)
		if invalid.StatusCode != http.StatusUnauthorized || !strings.Contains(invalid.Header.Get("WWW-Authenticate"), `error="invalid_token"`) {
			t.Errorf("%s with an unknown token: %d %q", path, invalid.StatusCode, invalid.Header.Get("WWW-Authenticate"))
		}
	}

	// A token granted only reading is asked for more when a tool needs it,
	// keeping what it has.
	for _, test := range []struct{ path, token, tool, scope string }{
		{builderPath, "builder-reader", "save_draft", "portal:read portal:author"},
		{userPath, "member", "run_action", "portal:read portal:run"},
	} {
		save := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"` + test.tool + `","arguments":{}}}`
		stepUp := post(t, server, test.path, test.token, save, nil)
		challenge := stepUp.Header.Get("WWW-Authenticate")
		if stepUp.StatusCode != http.StatusForbidden || !strings.Contains(challenge, `error="insufficient_scope"`) ||
			!strings.Contains(challenge, `scope="`+test.scope+`"`) ||
			!strings.Contains(challenge, `/.well-known/oauth-protected-resource`+test.path+`"`) {
			t.Errorf("step-up for %s: %d %q", test.tool, stepUp.StatusCode, challenge)
		}
	}
	// A tool the server doesn't have is the server's to answer.
	save := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"save_draft","arguments":{}}}`
	if user := post(t, server, userPath, "member", save, nil); user.StatusCode == http.StatusForbidden {
		t.Errorf("asked to step up for a tool the server lacks: %q", user.Header.Get("WWW-Authenticate"))
	}

	foreign := post(t, server, userPath, "builder", initialize, http.Header{"Origin": {"https://evil.example"}})
	if foreign.StatusCode != http.StatusForbidden {
		t.Errorf("foreign origin: %d", foreign.StatusCode)
	}
	own := post(t, server, userPath, "builder", initialize, http.Header{"Origin": {baseURL}})
	if own.StatusCode != http.StatusOK {
		t.Errorf("own origin: %d", own.StatusCode)
	}
}

func TestToolsCallServicesAsTheCaller(t *testing.T) {
	fake := newFake()
	session := connect(t, testServer(t, fake), builderPath, "builder")

	call(t, session, "save_draft", map[string]any{"kind": "blueprints", "identifier": "service", "source": "kind: Blueprint"})
	if got := *fake.last; got.name != "SaveDraft" || got.user != "builder" || !slices.Equal(got.args, []any{"blueprints", "service", "kind: Blueprint"}) {
		t.Errorf("save_draft called %+v", got)
	}
	call(t, session, "apply_documents", map[string]any{"source": "x"})
	if got := *fake.last; got.name != "Apply" || got.args[1] != true {
		t.Errorf("apply_documents defaults to a dry run, called %+v", got)
	}
	call(t, session, "create_ingestion_token", map[string]any{"identifier": "push", "name": "CI"})
	if got := *fake.last; got.name != "CreateIngestionToken" || !slices.Equal(got.args, []any{"push", "CI"}) {
		t.Errorf("create_ingestion_token called %+v", got)
	}
	call(t, session, "get_dashboard", map[string]any{"identifier": "services", "inputs": map[string]any{"team": []string{"payments"}, "tier": []string{}}})
	if inputs, _ := fake.last.args[1].(map[string][]string); fake.last.args[0] != "services" || fake.last.args[2] != false ||
		!slices.Equal(inputs["team"], []string{"payments"}) || inputs["tier"] == nil || len(inputs["tier"]) != 0 {
		t.Errorf("get_dashboard called %+v", *fake.last)
	}
	call(t, session, "preview_dashboard", map[string]any{"identifier": "services"})
	if fake.last.args[2] != true {
		t.Errorf("preview_dashboard called %+v", *fake.last)
	}

	// A service's refusal is the tool's error, with its status; anything
	// else is the portal's problem, which the caller isn't told the details of.
	refused := call(t, session, "delete_document", map[string]any{"kind": "blueprints", "identifier": "service", "confirm": "nope"})
	if out := output(t, refused); !refused.IsError || out.Status != http.StatusBadRequest {
		t.Errorf("refused delete = %+v", out)
	}
	failed := call(t, session, "sync_connection", map[string]any{"identifier": "source"})
	if out := output(t, failed); !failed.IsError || out.Status != http.StatusInternalServerError || strings.Contains(fmt.Sprint(out.Error), "10.0.0.5") {
		t.Errorf("failed sync = %+v", out)
	}
	unknown := call(t, session, "list_documents", map[string]any{"kind": "widgets"})
	if out := output(t, unknown); !unknown.IsError || out.Status != http.StatusBadRequest {
		t.Errorf("unknown kind = %+v", out)
	}
}
