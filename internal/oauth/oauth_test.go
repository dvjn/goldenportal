package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/dvjn/goldenportal/internal/auth"
)

const base = "https://portal.example.com"

func serve(t *testing.T, method, path, contentType, body string) (*httptest.ResponseRecorder, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	NewServer(base+"/", "Acme").Routes(db)(http.NotFoundHandler()).ServeHTTP(w, r)
	return w, mock
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body, err)
	}
	return out
}

func TestMetadataPointsAtThePortal(t *testing.T) {
	w, _ := serve(t, http.MethodGet, "/.well-known/oauth-authorization-server", "", "")
	server := decode(t, w)
	if server["issuer"] != base || server["token_endpoint"] != base+"/oauth/token" ||
		server["authorization_endpoint"] != base+"/oauth/authorize" {
		t.Errorf("server metadata = %v", server)
	}
	if methods, _ := server["code_challenge_methods_supported"].([]any); len(methods) != 1 || methods[0] != "S256" {
		t.Errorf("PKCE must be advertised as S256 only: %v", methods)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("metadata must be readable from any origin")
	}

	// Each endpoint is a resource of its own, with the scopes it takes.
	for path, want := range map[string]struct {
		resource string
		scopes   []any
	}{
		"/.well-known/oauth-protected-resource/api/mcp":         {base + "/api/mcp", []any{ScopeRead, ScopeRun}},
		"/.well-known/oauth-protected-resource":                 {base + "/api/mcp", []any{ScopeRead, ScopeRun}},
		"/.well-known/oauth-protected-resource/api/mcp/builder": {base + "/api/mcp/builder", []any{ScopeRead, ScopeAuthor}},
	} {
		w, _ = serve(t, http.MethodGet, path, "", "")
		resource := decode(t, w)
		if resource["resource"] != want.resource || resource["authorization_servers"].([]any)[0] != base ||
			!slices.Equal(resource["scopes_supported"].([]any), want.scopes) {
			t.Errorf("%s = %v", path, resource)
		}
	}
	if scopes, _ := server["scopes_supported"].([]any); len(scopes) != len(Scopes) {
		t.Errorf("the server issues %v", scopes)
	}
}

func TestRegisterChecksRedirectURIs(t *testing.T) {
	for _, test := range []struct {
		uris []string
		ok   bool
	}{
		{[]string{"https://claude.ai/api/mcp/auth_callback"}, true},
		{[]string{"http://localhost:33418/callback"}, true},
		{[]string{"http://127.0.0.1:9000/cb"}, true},
		{[]string{"cursor://anysphere.cursor-retrieval/oauth/callback"}, true},
		{[]string{"http://evil.example/callback"}, false},
		{[]string{"https://app.example/cb#frag"}, false},
		{[]string{"javascript:alert(1)"}, false},
		{[]string{"/relative"}, false},
		{nil, false},
	} {
		body, _ := json.Marshal(map[string]any{"client_name": "Claude", "redirect_uris": test.uris})
		w, _ := serve(t, http.MethodPost, "/oauth/register", "application/json", string(body))
		// An accepted client gets as far as being stored, which the mock refuses.
		if test.ok {
			if w.Code != http.StatusInternalServerError {
				t.Errorf("%v: %d %s", test.uris, w.Code, w.Body)
			}
		} else if w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_redirect_uri" {
			t.Errorf("%v: %d %s", test.uris, w.Code, w.Body)
		}
	}

	w, _ := serve(t, http.MethodPost, "/oauth/register", "application/json",
		`{"redirect_uris":["http://localhost:1/cb"],"token_endpoint_auth_method":"none"}`)
	if w.Code == http.StatusBadRequest {
		t.Errorf("a public client was refused: %s", w.Body)
	}
	w, _ = serve(t, http.MethodPost, "/oauth/register", "application/json",
		`{"redirect_uris":["http://localhost:1/cb"],"token_endpoint_auth_method":"client_secret_basic"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("a confidential client was accepted: %d", w.Code)
	}
}

func TestRegisterIssuesAPublicClient(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`INSERT INTO oauth_clients`).
		WithArgs(sqlmock.AnyArg(), "Claude", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(mustTime()))
	r := httptest.NewRequest(http.MethodPost, "/oauth/register",
		strings.NewReader(`{"client_name":"Claude","redirect_uris":["http://localhost:1/cb"]}`))
	w := httptest.NewRecorder()
	NewServer(base, "Acme").Routes(db)(http.NotFoundHandler()).ServeHTTP(w, r)
	out := decode(t, w)
	if w.Code != http.StatusCreated || !strings.HasPrefix(out["client_id"].(string), clientPrefix) ||
		out["token_endpoint_auth_method"] != "none" || out["client_secret"] != nil {
		t.Errorf("register = %d %v", w.Code, out)
	}
}

func TestTokenRefusesBadRequests(t *testing.T) {
	for _, test := range []struct {
		form, code string
	}{
		{"grant_type=password&username=a", "unsupported_grant_type"},
		{"grant_type=authorization_code&code=x&resource=https://elsewhere.example/api/mcp", "invalid_target"},
		{"grant_type=authorization_code&code=x&resource=" + base + "/api/mcp/other", "invalid_target"},
	} {
		w, _ := serve(t, http.MethodPost, "/oauth/token", "application/x-www-form-urlencoded", test.form)
		if out := decode(t, w); w.Code != http.StatusBadRequest || out["error"] != test.code {
			t.Errorf("%s: %d %v", test.form, w.Code, out)
		}
	}

	// An unknown code, or one already used, is no grant.
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`DELETE FROM oauth_codes`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT cl.client_id, t.resource FROM oauth_codes`).WillReturnRows(sqlmock.NewRows([]string{"client_id", "resource"}))
	r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("grant_type=authorization_code&code=gpk_used&client_id=c"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	NewServer(base, "Acme").Routes(db)(http.NotFoundHandler()).ServeHTTP(w, r)
	if out := decode(t, w); w.Code != http.StatusBadRequest || out["error"] != "invalid_grant" {
		t.Errorf("used code: %d %v", w.Code, out)
	}
}

func TestPKCEAndResources(t *testing.T) {
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if !verifyPKCE(verifier, challenge) || verifyPKCE(verifier+"x", challenge) || verifyPKCE("short", challenge) {
		t.Error("PKCE S256 check is wrong")
	}

	s := NewServer(base, "Acme")
	for resource, want := range map[string]string{
		base + "/api/mcp":                    UserMCP.Path,
		base + "/api/mcp/":                   UserMCP.Path,
		"HTTPS://Portal.Example.com/api/mcp": UserMCP.Path,
		"":                                   UserMCP.Path,
		base + "/api/mcp/builder":            BuilderMCP.Path,
		base + "/MCP/builder":                "",
		base:                                 "",
		"https://portal.example.com.evil/api/mcp": "",
	} {
		e, ok := s.endpointFor(resource)
		if ok != (want != "") || e.Path != want {
			t.Errorf("endpointFor(%q) = %q, %v", resource, e.Path, ok)
		}
	}
}

func TestAnswerKeepsTheClientsQuery(t *testing.T) {
	got, _ := url.Parse(NewServer(base, "Acme").answer("https://app.example/cb?app=1", "s1", map[string]string{"code": "c"}))
	q := got.Query()
	if q.Get("app") != "1" || q.Get("code") != "c" || q.Get("state") != "s1" || q.Get("iss") != base {
		t.Errorf("answer = %s", got)
	}
}

func mustTime() time.Time { return time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC) }

func TestConsentFollowsTheEndpoint(t *testing.T) {
	s := NewServer(base, "Acme")
	member := auth.Principal{ID: "m", Role: auth.RoleMember}
	builder := auth.Principal{ID: "b", Role: auth.RoleBuilder}
	for _, test := range []struct {
		name            string
		who             auth.Principal
		resource, scope string
		endpoint        string
		scopes          []string
		problem         string
	}{
		{name: "user server by default", who: member, endpoint: "/api/mcp", scopes: []string{ScopeRead, ScopeRun}},
		{name: "builder server", who: builder, resource: base + "/api/mcp/builder", endpoint: "/api/mcp/builder", scopes: []string{ScopeRead, ScopeAuthor}},
		{name: "member refused the builder server", who: member, resource: base + "/api/mcp/builder", problem: "access_denied"},
		{
			name: "scopes of the other server left out", who: builder, resource: base + "/api/mcp",
			scope: "portal:read portal:run portal:author", endpoint: "/api/mcp", scopes: []string{ScopeRead, ScopeRun},
		},
		{name: "nothing grantable here", who: builder, resource: base + "/api/mcp", scope: ScopeAuthor, problem: "invalid_scope"},
		{name: "authoring without naming the builder server", who: builder, scope: "portal:read portal:author", problem: "invalid_target"},
		{name: "unknown resource", who: builder, resource: base + "/api/mcp/admin", problem: "invalid_target"},
		{name: "unknown scope", who: builder, scope: "portal:everything", problem: "invalid_scope"},
	} {
		t.Run(test.name, func(t *testing.T) {
			e, scopes, p := s.grantable(test.who, test.resource, test.scope)
			switch {
			case test.problem != "":
				if p == nil || p.code != test.problem {
					t.Errorf("problem = %+v, want %s", p, test.problem)
				}
			case p != nil || e.Path != test.endpoint || !slices.Equal(scopes, test.scopes):
				t.Errorf("grantable = %s %v %+v", e.Path, scopes, p)
			}
		})
	}
}

// A refresh token or code asked for by another client, or for another
// resource, isn't taken, so the right request can still use it.
func TestTokensAreTakenOnlyAsIssued(t *testing.T) {
	for _, test := range []struct {
		form, table, client, code string
	}{
		{"grant_type=refresh_token&refresh_token=gpr_x&client_id=c&resource=" + url.QueryEscape(base+"/api/mcp/builder"), "oauth_tokens", "c", "invalid_target"},
		{"grant_type=refresh_token&refresh_token=gpr_x&client_id=other", "oauth_tokens", "c", "invalid_grant"},
		{"grant_type=authorization_code&code=gpk_x&client_id=c&resource=" + url.QueryEscape(base+"/api/mcp/builder"), "oauth_codes", "c", "invalid_target"},
	} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectQuery(`DELETE FROM ` + test.table).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectQuery(`SELECT cl.client_id, t.resource FROM ` + test.table).
			WillReturnRows(sqlmock.NewRows([]string{"client_id", "resource"}).AddRow(test.client, base+"/api/mcp"))
		r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(test.form))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		NewServer(base, "Acme").Routes(db)(http.NotFoundHandler()).ServeHTTP(w, r)
		if out := decode(t, w); w.Code != http.StatusBadRequest || out["error"] != test.code {
			t.Errorf("%s: %d %v", test.form, w.Code, out)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("%s: %v", test.form, err)
		}
		_ = db.Close()
	}
}
