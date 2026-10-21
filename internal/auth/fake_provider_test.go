package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

const fakeProviderKeyID = "test-key"

type fakeIdentity struct {
	Subject string
	Email   string
	Name    string
}

type pendingCode struct {
	nonce     string
	challenge string
}

// fakeProvider is an in-process OpenID Connect provider: discovery, an
// authorize endpoint that signs the configured identity in at once, the
// token endpoint with PKCE, the key set and an end session endpoint.
type fakeProvider struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	clientID string
	identity fakeIdentity

	mutex sync.Mutex
	codes map[string]pendingCode
}

func newFakeProvider(t *testing.T, clientID string, identity fakeIdentity) *fakeProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate the signing key: %v", err)
	}
	provider := &fakeProvider{key: key, clientID: clientID, identity: identity, codes: map[string]pendingCode{}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", provider.discovery)
	mux.HandleFunc("GET /authorize", provider.authorize)
	mux.HandleFunc("POST /token", provider.token)
	mux.HandleFunc("GET /jwks", provider.jwks)
	mux.HandleFunc("GET /end-session", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	provider.server = httptest.NewServer(mux)
	t.Cleanup(provider.server.Close)

	return provider
}

func (p *fakeProvider) URL() string { return p.server.URL }

func (p *fakeProvider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                p.server.URL,
		"authorization_endpoint":                p.server.URL + "/authorize",
		"token_endpoint":                        p.server.URL + "/token",
		"jwks_uri":                              p.server.URL + "/jwks",
		"end_session_endpoint":                  p.server.URL + "/end-session",
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (p *fakeProvider) authorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("code_challenge_method") != "S256" || query.Get("client_id") != p.clientID {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	code := rand.Text()
	p.mutex.Lock()
	p.codes[code] = pendingCode{nonce: query.Get("nonce"), challenge: query.Get("code_challenge")}
	p.mutex.Unlock()

	callback, _ := url.Parse(query.Get("redirect_uri"))
	callback.RawQuery = url.Values{"code": {code}, "state": {query.Get("state")}}.Encode()
	http.Redirect(w, r, callback.String(), http.StatusFound)
}

func (p *fakeProvider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.mutex.Lock()
	pending, ok := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code"))
	p.mutex.Unlock()

	verifierHash := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(verifierHash[:]) != pending.challenge {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	}

	writeJSON(w, map[string]any{
		"access_token": rand.Text(),
		"token_type":   "Bearer",
		"expires_in":   300,
		"id_token":     p.signIDToken(pending.nonce),
	})
}

func (p *fakeProvider) signIDToken(nonce string) string {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": fakeProviderKeyID, "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"iss":   p.server.URL,
		"aud":   p.clientID,
		"sub":   p.identity.Subject,
		"email": p.identity.Email,
		"name":  p.identity.Name,
		"nonce": nonce,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	signature, _ := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, digest[:])

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func (p *fakeProvider) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"keys": []map[string]string{{
		"kty": "RSA",
		"kid": fakeProviderKeyID,
		"use": "sig",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(p.key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(p.key.E)).Bytes()),
	}}})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
