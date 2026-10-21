package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldport/internal/directory"
)

type portal struct {
	server   *httptest.Server
	client   *http.Client
	provider *fakeProvider
	users    *memoryDirectory
	sessions *memorySessions
}

func newPortal(t *testing.T, identity fakeIdentity, users *memoryDirectory) *portal {
	t.Helper()
	provider := newFakeProvider(t, "portal", identity)
	server := httptest.NewUnstartedServer(nil)
	server.StartTLS()
	t.Cleanup(server.Close)

	config := Config{
		PublicURL:          server.URL,
		IssuerURL:          provider.URL(),
		ClientID:           "portal",
		SessionMaxAge:      time.Hour,
		SessionIdleTimeout: time.Hour,
		Cookies:            testCookies,
	}
	oidcProvider, err := NewProvider(context.Background(), config)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	sessions := newMemorySessions(users)
	server.Config.Handler = newTestRouter(sessions, NewService(config, oidcProvider, sessions, users).Register)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	client := server.Client()
	client.Jar = jar

	return &portal{server: server, client: client, provider: provider, users: users, sessions: sessions}
}

func (p *portal) get(t *testing.T, path string) *http.Response {
	t.Helper()
	response, err := p.client.Get(p.server.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })

	return response
}

func (p *portal) cookieNames() []string {
	base, _ := url.Parse(p.server.URL)
	var names []string
	for _, cookie := range p.client.Jar.Cookies(base) {
		names = append(names, cookie.Name)
	}

	return names
}

func TestLoginSignsAProvisionedUserInAndReturnsThem(t *testing.T) {
	users := aliceDirectory()
	portal := newPortal(t, fakeIdentity{Subject: "sub-alice", Email: "alice@example.test"}, users)

	landed := portal.get(t, "/api/v1/auth/login?return_to="+url.QueryEscape("/catalog?tab=owned"))

	if landed.Request.URL.Path != "/catalog" || landed.Request.URL.RawQuery != "tab=owned" {
		t.Fatalf("landed on %s, want /catalog?tab=owned", landed.Request.URL)
	}
	if names := portal.cookieNames(); len(names) != 1 || names[0] != testCookies.Session {
		t.Fatalf("cookies = %v, want only the session cookie", names)
	}
	if users.users[0].OIDCSubject != "sub-alice" {
		t.Fatalf("oidc subject = %q, want it bound to sub-alice", users.users[0].OIDCSubject)
	}

	me := portal.get(t, "/api/v1/auth/me")
	var body struct {
		Data meResponse `json:"data"`
	}
	if err := json.NewDecoder(me.Body).Decode(&body); err != nil || me.StatusCode != http.StatusOK {
		t.Fatalf("me = %d, %v", me.StatusCode, err)
	}
	if body.Data.User.Email != "alice@example.test" || len(body.Data.Groups) != 2 {
		t.Fatalf("me = %+v", body.Data)
	}
}

func TestLoginMatchesAUserWithoutAnExternalIDByEmail(t *testing.T) {
	users := &memoryDirectory{users: []directory.User{{ID: "u-9", UserName: "boot", Email: "Boot@Example.test", Active: true}}}
	portal := newPortal(t, fakeIdentity{Subject: "sub-boot", Email: "boot@example.test"}, users)

	landed := portal.get(t, "/api/v1/auth/login")

	if landed.Request.URL.Path != "/" {
		t.Fatalf("landed on %s, want /", landed.Request.URL)
	}
	if users.users[0].OIDCSubject != "sub-boot" {
		t.Fatalf("oidc subject = %q, want sub-boot", users.users[0].OIDCSubject)
	}
}

func TestLoginSendsUnprovisionedUsersToTheNotProvisionedPage(t *testing.T) {
	inactive := aliceDirectory()
	inactive.users[0].Active = false
	cases := map[string]struct {
		identity fakeIdentity
		users    *memoryDirectory
	}{
		"unknown subject and email": {fakeIdentity{Subject: "sub-mallory", Email: "mallory@example.test"}, aliceDirectory()},
		"deactivated user":          {fakeIdentity{Subject: "sub-alice", Email: "alice@example.test"}, inactive},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			portal := newPortal(t, tc.identity, tc.users)

			landed := portal.get(t, "/api/v1/auth/login?return_to=/catalog")

			if landed.Request.URL.Path != "/not-provisioned" || landed.Request.URL.RawQuery != "" {
				t.Fatalf("landed on %s, want /not-provisioned", landed.Request.URL)
			}
			if names := portal.cookieNames(); len(names) != 0 {
				t.Fatalf("cookies = %v, want none", names)
			}
			if len(portal.sessions.userByToken) != 0 {
				t.Fatalf("sessions = %v, want none", portal.sessions.userByToken)
			}
		})
	}
}

func TestCallbackRefusesAStateThatDoesNotMatchTheLoginCookie(t *testing.T) {
	portal := newPortal(t, fakeIdentity{Subject: "sub-alice"}, aliceDirectory())

	response := portal.get(t, "/api/v1/auth/callback?code=forged&state=forged")

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.StatusCode)
	}
}

func TestLogoutDeletesTheSessionAndPointsAtTheProviderLogout(t *testing.T) {
	portal := newPortal(t, fakeIdentity{Subject: "sub-alice"}, aliceDirectory())
	portal.get(t, "/api/v1/auth/login")
	var issuedIDToken string
	for _, idToken := range portal.sessions.idTokenByToken {
		issuedIDToken = idToken
	}

	response, err := portal.client.Post(portal.server.URL+"/api/v1/auth/logout", "application/json", nil)
	if err != nil {
		t.Fatalf("POST logout: %v", err)
	}
	defer response.Body.Close()

	var body struct {
		Data logoutResponse `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	redirect, err := url.Parse(body.Data.RedirectTo)
	if err != nil || redirect.Scheme+"://"+redirect.Host+redirect.Path != portal.provider.URL()+"/end-session" {
		t.Fatalf("redirect_to = %q, want the provider end session endpoint", body.Data.RedirectTo)
	}
	query := redirect.Query()
	if issuedIDToken == "" || query.Get("id_token_hint") != issuedIDToken {
		t.Fatalf("id_token_hint = %q, want the ID token issued at sign-in", query.Get("id_token_hint"))
	}
	if query.Get("post_logout_redirect_uri") != portal.server.URL {
		t.Fatalf("post_logout_redirect_uri = %q, want %q", query.Get("post_logout_redirect_uri"), portal.server.URL)
	}
	if len(portal.sessions.userByToken) != 0 || len(portal.cookieNames()) != 0 {
		t.Fatalf("sessions = %v, cookies = %v, want both empty", portal.sessions.userByToken, portal.cookieNames())
	}
}

func TestSignoutOnlyRedirects(t *testing.T) {
	portal := newPortal(t, fakeIdentity{Subject: "sub-alice"}, aliceDirectory())
	portal.get(t, "/api/v1/auth/login")

	landed := portal.get(t, "/api/v1/auth/signout")

	if landed.Request.URL.Host != strings.TrimPrefix(portal.provider.URL(), "http://") ||
		landed.Request.URL.Path != "/end-session" || landed.Request.URL.RawQuery != "" {
		t.Fatalf("landed on %s, want the bare provider end session endpoint", landed.Request.URL)
	}
	if len(portal.sessions.userByToken) != 1 {
		t.Fatalf("sessions = %v, want the session kept", portal.sessions.userByToken)
	}
}

func TestCrossSitePostsAreRefused(t *testing.T) {
	portal := newPortal(t, fakeIdentity{Subject: "sub-alice"}, aliceDirectory())
	portal.get(t, "/api/v1/auth/login")
	request, _ := http.NewRequest(http.MethodPost, portal.server.URL+"/api/v1/auth/logout", nil)
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Sec-Fetch-Site", "cross-site")

	response, err := portal.client.Do(request)
	if err != nil {
		t.Fatalf("POST logout: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.StatusCode)
	}
	if len(portal.sessions.userByToken) != 1 {
		t.Fatal("the refused request deleted the session")
	}
}

func TestSafeReturnToKeepsRedirectsOnThisOrigin(t *testing.T) {
	cases := map[string]string{
		"":                         "/",
		"/catalog?tab=1":           "/catalog?tab=1",
		"//attacker.example":       "/",
		`/\attacker.example`:       "/",
		"https://attacker.example": "/",
		"catalog":                  "/",
	}
	for input, want := range cases {
		if got := safeReturnTo(input); got != want {
			t.Errorf("safeReturnTo(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEndSessionURLFallsBackToTheRoot(t *testing.T) {
	endSession, err := parseEndSession("")
	provider := &Provider{endSession: endSession, publicURL: "https://portal.example.test"}

	if err != nil || provider.EndSessionURL("id-token") != "/" {
		t.Fatalf("EndSessionURL() = %q, %v, want / without the hint", provider.EndSessionURL("id-token"), err)
	}
}
