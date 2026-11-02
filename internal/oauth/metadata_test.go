package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// metadataServer serves a client's metadata document over TLS, from this
// machine, so the server under test must trust it and allow loopback.
func metadataServer(t *testing.T, handler func(w http.ResponseWriter, clientID string)) (Server, string) {
	t.Helper()
	var clientID string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handler(w, clientID)
	}))
	t.Cleanup(srv.Close)
	clientID = srv.URL + "/oauth/client.json"
	s := NewServer(base, "Acme")
	s.metadata = srv.Client()
	s.metadata.CheckRedirect = newMetadataClient(true).CheckRedirect
	return s, clientID
}

func document(clientID, extra string) string {
	return `{"client_id":"` + clientID + `","client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"]` + extra + `}`
}

func TestClientIDURLs(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://claude.ai/oauth/mcp-client.json": true,
		"https://claude.ai:8443/client":           true,
		"https://claude.ai/":                      true,
		"https://claude.ai":                       false,
		"http://claude.ai/client":                 false,
		"https://user@claude.ai/client":           false,
		"https://claude.ai/client#x":              false,
		"https://claude.ai/a/../client":           false,
		"https://claude.ai/./client":              false,
	} {
		if _, err := checkClientIDURL(raw); (err == nil) != ok {
			t.Errorf("%s: %v", raw, err)
		}
	}
	if isMetadataClient(clientPrefix+"abc") || !isMetadataClient("https://claude.ai/client") {
		t.Error("issued client IDs and metadata URLs are told apart wrongly")
	}
}

func TestMetadataDocumentsAreChecked(t *testing.T) {
	for _, test := range []struct {
		name    string
		respond func(w http.ResponseWriter, clientID string)
		want    string
	}{
		{"accepted", func(w http.ResponseWriter, id string) { _, _ = w.Write([]byte(document(id, ""))) }, ""},
		{"another client", func(w http.ResponseWriter, _ string) {
			_, _ = w.Write([]byte(document("https://elsewhere.example/client", "")))
		}, "names another client_id"},
		{"secret", func(w http.ResponseWriter, id string) {
			_, _ = w.Write([]byte(document(id, `,"client_secret":"s"`)))
		}, "client secret"},
		{"keys", func(w http.ResponseWriter, id string) {
			_, _ = w.Write([]byte(document(id, `,"token_endpoint_auth_method":"private_key_jwt"`)))
		}, "must be none"},
		{"bad redirect", func(w http.ResponseWriter, id string) {
			_, _ = w.Write([]byte(`{"client_id":"` + id + `","redirect_uris":["http://evil.example/cb"]}`))
		}, "localhost"},
		{"too big", func(w http.ResponseWriter, id string) {
			_, _ = w.Write([]byte(document(id, `,"client_uri":"`+strings.Repeat("x", maxMetadataBytes)+`"`)))
		}, "over 5 KB"},
		{"not found", func(w http.ResponseWriter, _ string) { w.WriteHeader(http.StatusNotFound) }, "answered 404"},
		{"redirected", func(w http.ResponseWriter, _ string) {
			w.Header().Set("Location", "https://elsewhere.example/client")
			w.WriteHeader(http.StatusFound)
		}, "answered 302"},
		{"not JSON", func(w http.ResponseWriter, _ string) { _, _ = w.Write([]byte("<html>")) }, "JSON object"},
	} {
		s, clientID := metadataServer(t, test.respond)
		f, err := s.fetchMetadata(context.Background(), clientID)
		switch {
		case test.want == "" && (err != nil || f.name != "Claude" || len(f.redirectURIs) != 1):
			t.Errorf("%s: %+v %v", test.name, f, err)
		case test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)):
			t.Errorf("%s: want %q, got %v", test.name, test.want, err)
		}
	}
}

// Published documents, as the clients serve them, list grant types the
// portal doesn't offer and loopback redirects without a port.
func TestRealClientsMetadata(t *testing.T) {
	for _, raw := range []string{
		`{"client_id":"%s","client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"grant_types":["authorization_code","refresh_token","urn:ietf:params:oauth:grant-type:jwt-bearer"],"response_types":["code"],"token_endpoint_auth_method":"none"}`,
		`{"client_name":"Visual Studio Code","grant_types":["authorization_code","refresh_token","urn:ietf:params:oauth:grant-type:device_code"],"response_types":["code"],"token_endpoint_auth_method":"none","application_type":"native","client_id":"%s","redirect_uris":["http://127.0.0.1:33418/","https://vscode.dev/redirect"]}`,
		`{"client_id":"%s","client_name":"Claude Code","redirect_uris":["http://localhost/callback","http://127.0.0.1/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`,
	} {
		s, clientID := metadataServer(t, func(w http.ResponseWriter, id string) { _, _ = fmt.Fprintf(w, raw, id) })
		if _, err := s.fetchMetadata(context.Background(), clientID); err != nil {
			t.Errorf("%.40s: %v", raw, err)
		}
	}
	if _, p := (clientMetadata{RedirectURIs: []string{"https://a.example/cb"}, GrantTypes: []string{"client_credentials"}}).check("x"); p == nil {
		t.Error("a client that can't use authorization codes was accepted")
	}
}

func TestLoopbackRedirectsUseAnyPort(t *testing.T) {
	registered := []string{"http://localhost/callback", "http://127.0.0.1:33418/", "https://claude.ai/cb"}
	for uri, ok := range map[string]bool{
		"http://localhost:51234/callback": true,
		"http://localhost/callback":       true,
		"http://127.0.0.1:9/":             true,
		"https://claude.ai/cb":            true,
		"http://127.0.0.1:9/callback":     false,
		"http://localhost:1/other":        false,
		"http://localhost:1/callback?x=1": false,
		"https://claude.ai:8443/cb":       false,
		"http://evil.example/callback":    false,
	} {
		if redirectAllowed(registered, uri) != ok {
			t.Errorf("%s allowed = %v", uri, !ok)
		}
	}
}

func TestMetadataNamesDefaultToTheHost(t *testing.T) {
	s, clientID := metadataServer(t, func(w http.ResponseWriter, id string) {
		_, _ = w.Write([]byte(`{"client_id":"` + id + `","redirect_uris":["http://127.0.0.1:1/cb"]}`))
	})
	if f, err := s.fetchMetadata(context.Background(), clientID); err != nil || f.name != "127.0.0.1" {
		t.Errorf("name = %q, %v", f.name, err)
	}
}

func TestCacheTTL(t *testing.T) {
	for header, want := range map[string]time.Duration{
		"":                         metadataDefaultTTL,
		"public, max-age=3600":     time.Hour,
		"max-age=31536000":         metadataMaxTTL,
		"no-store":                 0,
		"max-age=600, no-cache":    0,
		"max-age=nonsense":         0,
		`max-age="60", must-reval`: time.Minute,
	} {
		if got := cacheTTL(header); got != want {
			t.Errorf("%q: %v, want %v", header, got, want)
		}
	}
}

func TestOnlyPublicAddressesAreFetched(t *testing.T) {
	for addr, public := range map[string]bool{
		"8.8.8.8":         true,
		"2606:4700::1111": true,
		"127.0.0.1":       false,
		"::1":             false,
		"10.0.0.1":        false,
		"172.16.0.1":      false,
		"192.168.1.1":     false,
		"169.254.169.254": false,
		"100.64.0.1":      false,
		"0.0.0.0":         false,
		"::ffff:10.0.0.1": false,
		"fd00::1":         false,
		"fe80::1":         false,
		"64:ff9b::a00:1":  false,
		"2002:a00:1::":    false,
		"224.0.0.1":       false,
	} {
		if got := fetchable(netip.MustParseAddr(addr), false); got != public {
			t.Errorf("%s fetchable = %v", addr, got)
		}
	}
	if !fetchable(netip.MustParseAddr("127.0.0.1"), true) || fetchable(netip.MustParseAddr("10.0.0.1"), true) {
		t.Error("a local portal may reach this machine, and only that")
	}

	// The real client refuses this machine as it dials, whatever the name.
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	s := NewServer(base, "Acme")
	_, err := s.fetchMetadata(context.Background(), srv.URL+"/client")
	if err == nil || !strings.Contains(err.Error(), "isn't a public address") {
		t.Errorf("loopback fetch: %v", err)
	}
}

func TestMetadataClientsAreKeptUntilTheyExpire(t *testing.T) {
	fetches := 0
	s, clientID := metadataServer(t, func(w http.ResponseWriter, id string) {
		fetches++
		w.Header().Set("Cache-Control", "max-age=60")
		_, _ = w.Write([]byte(document(id, "")))
	})
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	columns := []string{"id", "name", "redirect_uris", "logo", "metadata_expires_at"}
	ctx := context.Background()

	// Unknown, so it's fetched and kept.
	mock.ExpectQuery(`SELECT id, name, redirect_uris, logo, metadata_expires_at`).WillReturnRows(sqlmock.NewRows(columns))
	mock.ExpectQuery(`INSERT INTO oauth_clients`).
		WithArgs(clientID, "Claude", sqlmock.AnyArg(), "", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("c1"))
	c, err := s.client(ctx, NewStore(db), clientID)
	if err != nil || c.ID != "c1" || c.Name != "Claude" || fetches != 1 {
		t.Fatalf("first: %+v %v, %d fetches", c, err, fetches)
	}

	// Still fresh, so it isn't.
	mock.ExpectQuery(`SELECT id, name, redirect_uris, logo, metadata_expires_at`).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("c1", "Claude", "{https://claude.ai/cb}", "", time.Now().Add(time.Minute)))
	if _, err := s.client(ctx, NewStore(db), clientID); err != nil || fetches != 1 {
		t.Errorf("fresh: %v, %d fetches", err, fetches)
	}

	// Expired and now unusable: the request fails rather than use the old copy.
	broken, brokenID := metadataServer(t, func(w http.ResponseWriter, _ string) { w.WriteHeader(http.StatusInternalServerError) })
	mock.ExpectQuery(`SELECT id, name, redirect_uris, logo, metadata_expires_at`).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("c2", "Old", "{https://claude.ai/cb}", "", time.Now().Add(-time.Minute)))
	_, err = broken.client(ctx, NewStore(db), brokenID)
	var unusable metadataError
	if !errors.As(err, &unusable) {
		t.Errorf("expired and broken: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestServerAdvertisesMetadataDocuments(t *testing.T) {
	w, _ := serve(t, http.MethodGet, "/.well-known/oauth-authorization-server", "", "")
	if decode(t, w)["client_id_metadata_document_supported"] != true {
		t.Error("support for client ID metadata documents isn't advertised")
	}
}
