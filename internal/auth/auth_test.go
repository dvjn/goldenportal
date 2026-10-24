package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/httpx"
)

func TestPrincipalRolesNest(t *testing.T) {
	builder := Principal{Role: RoleBuilder}

	if !builder.Can(RoleMember) || !builder.Can(RoleBuilder) {
		t.Fatal("a builder should be able to do what members and builders can")
	}
	if builder.Can(RoleAdmin) {
		t.Fatal("a builder should not be able to do what admins can")
	}
}

type fakeResolver struct {
	sessions map[string]Principal
	tokens   map[string]Principal
	readOnly map[string]bool
}

func (f fakeResolver) SessionPrincipal(_ context.Context, hash []byte) (Principal, error) {
	if p, ok := f.sessions[string(hash)]; ok {
		return p, nil
	}
	if string(hash) == string(hashSecret("broken")) {
		return Principal{}, errors.New("database is down")
	}

	return Principal{}, errNotFound
}

func (f fakeResolver) TokenPrincipal(_ context.Context, hash []byte) (Principal, bool, error) {
	if p, ok := f.tokens[string(hash)]; ok {
		return p, f.readOnly[string(hash)], nil
	}

	return Principal{}, false, errNotFound
}

func TestMiddleware(t *testing.T) {
	alice := Principal{ID: "1", Email: "alice", Role: RoleAdmin}
	resolver := fakeResolver{
		sessions: map[string]Principal{string(hashSecret("alice-session")): alice},
		tokens: map[string]Principal{
			string(hashSecret("gpp_rw")): alice,
			string(hashSecret("gpp_ro")): alice,
		},
		readOnly: map[string]bool{string(hashSecret("gpp_ro")): true},
	}

	tests := []struct {
		name       string
		method     string
		path       string
		cookie     string
		bearer     string
		csrf       bool
		wantStatus int
		wantUser   string
	}{
		{name: "non-API paths pass through", method: http.MethodGet, path: "/catalog", wantStatus: http.StatusOK},
		{name: "public API paths need no session", method: http.MethodGet, path: "/api/v1/config", wantStatus: http.StatusOK},
		{name: "private API paths need a session", method: http.MethodGet, path: "/api/v1/me", wantStatus: http.StatusUnauthorized},
		{name: "a valid session signs the request in", method: http.MethodGet, path: "/api/v1/me", cookie: "alice-session", wantStatus: http.StatusOK, wantUser: "alice"},
		{name: "an unknown session counts as signed out", method: http.MethodGet, path: "/api/v1/me", cookie: "stale", wantStatus: http.StatusUnauthorized},
		{name: "an unknown session still reaches public paths", method: http.MethodGet, path: "/api/v1/config", cookie: "stale", wantStatus: http.StatusOK},
		{name: "the sign-in endpoints are public", method: http.MethodGet, path: "/api/v1/auth/callback", wantStatus: http.StatusOK},
		{name: "a session lookup failure is a server error", method: http.MethodGet, path: "/api/v1/me", cookie: "broken", wantStatus: http.StatusInternalServerError},
		{name: "writes need the CSRF header", method: http.MethodPost, path: "/api/v1/auth/logout", cookie: "alice-session", wantStatus: http.StatusForbidden},
		{name: "writes with the CSRF header pass", method: http.MethodPost, path: "/api/v1/auth/logout", cookie: "alice-session", csrf: true, wantStatus: http.StatusOK, wantUser: "alice"},
		{name: "public writes need the CSRF header too", method: http.MethodPost, path: "/api/v1/config", wantStatus: http.StatusForbidden},
		{name: "an API token signs the request in", method: http.MethodGet, path: "/api/v1/me", bearer: "gpp_rw", wantStatus: http.StatusOK, wantUser: "alice"},
		{name: "token writes need no CSRF header", method: http.MethodPost, path: "/api/v1/things", bearer: "gpp_rw", wantStatus: http.StatusOK, wantUser: "alice"},
		{name: "read-only tokens can read", method: http.MethodGet, path: "/api/v1/me", bearer: "gpp_ro", wantStatus: http.StatusOK, wantUser: "alice"},
		{name: "read-only tokens can't write", method: http.MethodPost, path: "/api/v1/things", bearer: "gpp_ro", wantStatus: http.StatusForbidden},
		{name: "an unknown token is rejected even on public paths", method: http.MethodGet, path: "/api/v1/config", bearer: "gpp_nope", wantStatus: http.StatusUnauthorized},
		{name: "a token wins over a session cookie", method: http.MethodGet, path: "/api/v1/me", bearer: "gpp_nope", cookie: "alice-session", wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotUser string
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if p, ok := FromContext(r.Context()); ok {
					gotUser = p.Email
				}
			})

			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: sessionCookie, Value: tt.cookie})
			}
			if tt.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tt.bearer)
			}
			if tt.csrf {
				req.Header.Set(csrfHeader, "1")
			}
			rec := httptest.NewRecorder()

			Middleware(resolver)(next).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if gotUser != tt.wantUser {
				t.Fatalf("user = %q, want %q", gotUser, tt.wantUser)
			}
		})
	}
}

func TestSafeRedirectStaysOnSite(t *testing.T) {
	tests := map[string]string{
		"/catalog/service?q=1": "/catalog/service?q=1",
		"":                     "/",
		"https://evil.example": "/",
		"//evil.example":       "/",
		`/\evil.example`:       "/",
	}

	for target, want := range tests {
		if got := safeRedirect(target); got != want {
			t.Errorf("safeRedirect(%q) = %q, want %q", target, got, want)
		}
	}
}

func TestDisplayNameFallsBackThroughTheClaims(t *testing.T) {
	tests := []struct {
		claims idClaims
		want   string
	}{
		{idClaims{Name: "Ada Lovelace", PreferredUsername: "ada", Email: "ada@example.com"}, "Ada Lovelace"},
		{idClaims{PreferredUsername: "ada", Email: "ada@example.com"}, "ada"},
		{idClaims{Email: "ada@example.com"}, "ada@example.com"},
		{idClaims{}, "Unnamed user"},
	}

	for _, tt := range tests {
		if got := tt.claims.displayName(); got != tt.want {
			t.Errorf("displayName() = %q, want %q", got, tt.want)
		}
	}
}

func signInError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || location.Path != "/sign-in" {
		t.Fatalf("Location = %q, want the sign-in page", rec.Header().Get("Location"))
	}

	return location.Query().Get("error")
}

func TestSignInExplainsMissingConfiguration(t *testing.T) {
	o := NewOIDC(OIDCConfig{}, logging.NewMockLogger(logging.ERROR))
	rec := httptest.NewRecorder()

	o.Routes(Store{})(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, signInPath, nil))

	if got := signInError(t, rec); !strings.Contains(got, "GP_OIDC_ISSUER") {
		t.Fatalf("error = %q, want it to name the missing settings", got)
	}
}

func TestCallbackRejectsProblemsBeforeTouchingTheProvider(t *testing.T) {
	o := NewOIDC(OIDCConfig{Issuer: "https://idp.invalid", ClientID: "portal"}, logging.NewMockLogger(logging.ERROR))

	tests := []struct {
		name   string
		query  string
		cookie string
		want   string
	}{
		{name: "the provider reports an error", query: "error=access_denied&error_description=You+said+no", want: "You said no"},
		{name: "the state cookie is missing", query: "code=abc&state=s1", want: "Sign in again"},
		{name: "the state does not match the cookie", query: "code=abc&state=s1", cookie: "s2", want: "Sign in again"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, callbackPath+"?"+tt.query, nil)
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: stateCookie, Value: tt.cookie})
			}
			rec := httptest.NewRecorder()

			o.Routes(Store{})(http.NotFoundHandler()).ServeHTTP(rec, req)

			if got := signInError(t, rec); !strings.Contains(got, tt.want) {
				t.Fatalf("error = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

func TestRequireChecksTheRole(t *testing.T) {
	ctx := withAuth(context.Background(), authInfo{principal: Principal{ID: "1", Role: RoleBuilder}})

	if _, err := Require(ctx, RoleBuilder); err != nil {
		t.Fatalf("Require(builder) = %v, want no error", err)
	}

	_, err := Require(ctx, RoleAdmin)
	var httpErr httpx.Error
	if !errors.As(err, &httpErr) || httpErr.Code != http.StatusForbidden {
		t.Fatalf("Require(admin) = %v, want a 403", err)
	}

	_, err = Require(context.Background(), RoleMember)
	if !errors.As(err, &httpErr) || httpErr.Code != http.StatusUnauthorized {
		t.Fatalf("Require() without a user = %v, want a 401", err)
	}
}
