package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

type ssoConfig struct {
	Issuer  string      `yaml:"issuer"`
	Clients []ssoClient `yaml:"clients"`
	Users   []ssoUser   `yaml:"users"`
}

type ssoClient struct {
	ID       string `yaml:"id"`
	Secret   string `yaml:"secret"`
	Redirect string `yaml:"redirect"`
	redirect *regexp.Regexp
}

type ssoUser struct {
	Email string `yaml:"email"`
	Name  string `yaml:"name"`
}

// grant is an authorization code waiting to be exchanged.
type grant struct {
	client, redirect, challenge, nonce string
	user                               ssoUser
	expires                            time.Time
}

// sso is an OpenID Connect provider that signs in whoever is picked from a
// list, with the authorization code flow and PKCE.
type sso struct {
	cfg     ssoConfig
	clients map[string]ssoClient
	key     *rsa.PrivateKey
	keyID   string

	mu     sync.Mutex
	grants map[string]grant
}

func newSSO(cfg ssoConfig) (*sso, error) {
	if cfg.Issuer == "" {
		return nil, errors.New("sso.issuer is empty")
	}
	clients := map[string]ssoClient{}
	for _, c := range cfg.Clients {
		if c.ID == "" || c.Secret == "" {
			return nil, errors.New("every sso client needs an id and a secret")
		}
		pattern, err := regexp.Compile(c.Redirect)
		if err != nil {
			return nil, fmt.Errorf("sso client %s: %w", c.ID, err)
		}
		c.redirect = pattern
		clients[c.ID] = c
	}
	// A new key each start; the portal fetches the keys again when it sees an unknown key ID.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &sso{cfg: cfg, clients: clients, key: key, keyID: randomString(), grants: map[string]grant{}}, nil
}

func (s *sso) routes(mux *http.ServeMux) {
	issuer, _ := url.Parse(s.cfg.Issuer)
	base := strings.TrimSuffix(issuer.Path, "/")
	mux.HandleFunc("GET "+base+"/.well-known/openid-configuration", s.discovery)
	mux.HandleFunc("GET "+base+"/authorize", s.authorize)
	mux.HandleFunc("POST "+base+"/token", s.token)
	mux.HandleFunc("GET "+base+"/jwks", s.jwks)
}

func (s *sso) discovery(w http.ResponseWriter, _ *http.Request) {
	base := strings.TrimSuffix(s.cfg.Issuer, "/")
	reply(w, http.StatusOK, map[string]any{
		"issuer":                                s.cfg.Issuer,
		"authorization_endpoint":                base + "/authorize",
		"token_endpoint":                        base + "/token",
		"jwks_uri":                              base + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
		"scopes_supported":                      []string{"openid", "email", "profile"},
	})
}

var chooser = template.Must(template.New("").Parse(`<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in</title>
<style>
  :root { color-scheme: light dark; font-family: system-ui, sans-serif; }
  body { display: grid; place-items: center; min-height: 100vh; margin: 0; }
  main { width: min(22rem, 100% - 2rem); }
  h1 { font-size: 1.1rem; font-weight: 600; }
  a { display: block; padding: .75rem 1rem; margin: .5rem 0; border: 1px solid #8884; border-radius: .75rem; color: inherit; text-decoration: none; }
  a:hover { background: #8881; }
  small { display: block; opacity: .65; }
</style>
<main>
  <h1>Sign in as</h1>
  {{range .}}<a href="{{.Href}}">{{.Name}}<small>{{.Email}}</small></a>{{end}}
</main>
</html>`))

func (s *sso) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	client, ok := s.clients[q.Get("client_id")]
	redirect := q.Get("redirect_uri")
	if !ok || !client.redirect.MatchString(redirect) {
		http.Error(w, "unknown client_id or redirect_uri", http.StatusBadRequest)
		return
	}
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "send response_type=code with an S256 code_challenge", http.StatusBadRequest)
		return
	}

	email := q.Get("login_as")
	if email == "" {
		type choice struct{ Name, Email, Href string }
		choices := make([]choice, 0, len(s.cfg.Users))
		for _, u := range s.cfg.Users {
			pick := r.URL.Query()
			pick.Set("login_as", u.Email)
			choices = append(choices, choice{u.Name, u.Email, r.URL.Path + "?" + pick.Encode()})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = chooser.Execute(w, choices)
		return
	}
	user, ok := s.user(email)
	if !ok {
		http.Error(w, "no user "+email, http.StatusBadRequest)
		return
	}

	code := randomString()
	s.mu.Lock()
	s.grants[code] = grant{
		client: client.ID, redirect: redirect, challenge: q.Get("code_challenge"), nonce: q.Get("nonce"),
		user: user, expires: time.Now().Add(time.Minute),
	}
	s.mu.Unlock()

	back, _ := url.Parse(redirect)
	params := back.Query()
	params.Set("code", code)
	params.Set("state", q.Get("state"))
	back.RawQuery = params.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (s *sso) user(email string) (ssoUser, bool) {
	for _, u := range s.cfg.Users {
		if strings.EqualFold(u.Email, email) {
			return u, true
		}
	}
	return ssoUser{}, false
}

func (s *sso) token(w http.ResponseWriter, r *http.Request) {
	fail := func(code, description string) {
		reply(w, http.StatusBadRequest, map[string]string{"error": code, "error_description": description})
	}
	if err := r.ParseForm(); err != nil {
		fail("invalid_request", err.Error())
		return
	}
	id, secret, ok := r.BasicAuth()
	if ok {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	client, known := s.clients[id]
	if !known || subtle.ConstantTimeCompare([]byte(client.Secret), []byte(secret)) != 1 {
		fail("invalid_client", "unknown client or wrong secret")
		return
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		fail("unsupported_grant_type", "only authorization_code")
		return
	}

	s.mu.Lock()
	g, found := s.grants[r.PostForm.Get("code")]
	delete(s.grants, r.PostForm.Get("code"))
	s.mu.Unlock()
	switch {
	case !found || time.Now().After(g.expires):
		fail("invalid_grant", "the code is unknown, used or expired")
		return
	case g.client != client.ID || g.redirect != r.PostForm.Get("redirect_uri"):
		fail("invalid_grant", "the code was issued to another client or redirect_uri")
		return
	case pkceChallenge(r.PostForm.Get("code_verifier")) != g.challenge:
		fail("invalid_grant", "code_verifier doesn't match the code_challenge")
		return
	}

	issued := time.Now()
	idToken, err := s.sign(map[string]any{
		"iss": s.cfg.Issuer, "sub": g.user.Email, "aud": client.ID,
		"iat": issued.Unix(), "exp": issued.Add(time.Hour).Unix(), "nonce": g.nonce,
		"email": g.user.Email, "email_verified": true, "name": g.user.Name,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reply(w, http.StatusOK, map[string]any{
		"access_token": randomString(), "token_type": "Bearer", "expires_in": 3600, "id_token": idToken,
	})
}

func (s *sso) sign(claims map[string]any) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": s.keyID})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := b64(header) + "." + b64(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + b64(signature), nil
}

func (s *sso) jwks(w http.ResponseWriter, _ *http.Request) {
	public := s.key.PublicKey
	reply(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": s.keyID,
		"n": b64(public.N.Bytes()), "e": b64(big.NewInt(int64(public.E)).Bytes()),
	}}})
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return b64(sum[:])
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func randomString() string { return rand.Text() }
