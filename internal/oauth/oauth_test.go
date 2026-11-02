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

	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource"} {
		w, _ = serve(t, http.MethodGet, path, "", "")
		resource := decode(t, w)
		if resource["resource"] != base+"/mcp" || resource["authorization_servers"].([]any)[0] != base {
			t.Errorf("%s = %v", path, resource)
		}
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
		{"grant_type=authorization_code&code=x&resource=https://elsewhere.example/mcp", "invalid_target"},
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
	for resource, same := range map[string]bool{
		base + "/mcp":                         true,
		base + "/mcp/":                        true,
		"HTTPS://Portal.Example.com/mcp":      true,
		base:                                  false,
		"https://portal.example.com.evil/mcp": false,
	} {
		if s.sameResource(resource) != same {
			t.Errorf("sameResource(%q) = %v", resource, !same)
		}
	}
}

func TestRolesCapScopes(t *testing.T) {
	if got := RoleScopes(auth.RoleMember); !slices.Equal(got, []string{ScopeRead}) {
		t.Errorf("member can grant %v", got)
	}
	if got := RoleScopes(auth.RoleAdmin); !slices.Equal(got, Scopes) {
		t.Errorf("admin can grant %v", got)
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
