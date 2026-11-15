// Package oauth makes the portal an OAuth 2.1 authorization server for its
// own MCP servers, as the MCP authorization spec asks: clients register
// themselves, people sign in through the portal's usual sign-in and consent
// in the browser, and each token works only at the MCP endpoint it was
// issued for.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/dvjn/goldenportal/internal/auth"
)

// Scopes split what a client may do: read the catalog and configuration,
// run actions as the user, or author configuration.
const (
	ScopeRead   = "portal:read"
	ScopeRun    = "portal:run"
	ScopeAuthor = "portal:author"
)

// Scopes are every scope the portal issues, least first.
var Scopes = []string{ScopeRead, ScopeRun, ScopeAuthor}

// Endpoint is an MCP server the portal issues tokens for: a resource of
// its own, with the scopes it takes and the role a user needs to connect.
// Anyone with the role can grant all of its scopes, so a user never hands
// a client more than they could do themselves.
type Endpoint struct {
	Path   string
	Title  string
	Scopes []string
	Role   auth.Role
}

// UserMCP serves members what the portal's pages show them, and runs
// actions as them; BuilderMCP serves builders changing configuration.
var (
	UserMCP    = Endpoint{Path: "/api/mcp", Title: "User MCP", Scopes: []string{ScopeRead, ScopeRun}, Role: auth.RoleMember}
	BuilderMCP = Endpoint{Path: "/api/mcp/builder", Title: "Builder MCP", Scopes: []string{ScopeRead, ScopeAuthor}, Role: auth.RoleBuilder}
	Endpoints  = []Endpoint{UserMCP, BuilderMCP}
)

func EndpointAt(path string) (Endpoint, bool) {
	for _, e := range Endpoints {
		if e.Path == path {
			return e, true
		}
	}
	return Endpoint{}, false
}

const (
	authorizePath = "/oauth/authorize"
	tokenPath     = "/oauth/token"
	registerPath  = "/oauth/register"
	revokePath    = "/oauth/revoke"

	serverMetadataPath   = "/.well-known/oauth-authorization-server"
	resourceMetadataPath = "/.well-known/oauth-protected-resource"

	codeLifetime    = 10 * time.Minute
	accessLifetime  = time.Hour
	refreshLifetime = 30 * 24 * time.Hour

	accessPrefix  = "gpa_"
	refreshPrefix = "gpr_"
	clientPrefix  = "gpc_"
	codePrefix    = "gpk_"

	maxBody         = 64 << 10
	maxRedirectURIs = 10
	maxClientName   = 100
)

// Server answers the OAuth endpoints for a portal at baseURL, its public
// URL, which is also the issuer.
type Server struct {
	baseURL  string
	name     string
	metadata *http.Client
}

// NewServer makes the server. A portal on this machine may read client
// metadata from it too, which is how one is tried out locally.
func NewServer(baseURL, name string) Server {
	baseURL = strings.TrimSuffix(baseURL, "/")
	u, _ := url.Parse(baseURL)
	local := u != nil && isLoopback(u.Hostname())
	return Server{baseURL: baseURL, name: name, metadata: newMetadataClient(local)}
}

// Issuer is the portal's public URL, as clients see it.
func (s Server) Issuer() string { return s.baseURL }

// Resource is an endpoint's canonical URL, which its tokens are bound to.
func (s Server) Resource(e Endpoint) string { return s.baseURL + e.Path }

// ResourceMetadataURL is where clients find how to get a token for e.
func (s Server) ResourceMetadataURL(e Endpoint) string {
	return s.baseURL + resourceMetadataPath + e.Path
}

// Routes serves the OAuth protocol endpoints and metadata, which answer in
// OAuth's own JSON rather than the API's envelope. Each is public and
// takes no cookies, so any origin may call them.
func (s Server) Routes(db auth.DB) func(http.Handler) http.Handler {
	store := NewStore(db)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var serve func(http.ResponseWriter, *http.Request, Store)
			switch path := r.URL.Path; path {
			case serverMetadataPath:
				serve = s.serverMetadata
			case resourceMetadataPath:
				// Clients that don't name the resource find the user's server.
				serve = s.resourceMetadata(UserMCP)
			case resourceMetadataPath + UserMCP.Path, resourceMetadataPath + BuilderMCP.Path:
				e, _ := EndpointAt(strings.TrimPrefix(path, resourceMetadataPath))
				serve = s.resourceMetadata(e)
			case registerPath:
				serve = s.register
			case tokenPath:
				serve = s.token
			case revokePath:
				serve = s.revoke
			default:
				next.ServeHTTP(w, r)
				return
			}
			allowAnyOrigin(w)
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
			serve(w, r, store)
		})
	}
}

func allowAnyOrigin(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Protocol-Version")
}

func (s Server) serverMetadata(w http.ResponseWriter, r *http.Request, _ Store) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                         s.baseURL,
		"authorization_endpoint":                         s.baseURL + authorizePath,
		"token_endpoint":                                 s.baseURL + tokenPath,
		"registration_endpoint":                          s.baseURL + registerPath,
		"revocation_endpoint":                            s.baseURL + revokePath,
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"revocation_endpoint_auth_methods_supported":     []string{"none"},
		"scopes_supported":                               Scopes,
		"authorization_response_iss_parameter_supported": true,
		"client_id_metadata_document_supported":          true,
	})
}

func (s Server) resourceMetadata(e Endpoint) func(http.ResponseWriter, *http.Request, Store) {
	return func(w http.ResponseWriter, r *http.Request, _ Store) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"resource":                 s.Resource(e),
			"resource_name":            s.name + " " + e.Title,
			"authorization_servers":    []string{s.baseURL},
			"scopes_supported":         e.Scopes,
			"bearer_methods_supported": []string{"header"},
		})
	}
}

// register signs up a client, as dynamic client registration (RFC 7591)
// does, for clients without a metadata document. Every client is public:
// it gets an identifier but no secret, and PKCE stands in for one.
func (s Server) register(w http.ResponseWriter, r *http.Request, store Store) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
		return
	}
	var req clientMetadata
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_client_metadata", "the body must be a JSON object")
		return
	}
	name, p := req.check("MCP client")
	if p != nil {
		writeError(w, http.StatusBadRequest, p.code, p.description)
		return
	}

	clientID := clientPrefix + newSecret()
	created, err := store.CreateClient(r.Context(), clientID, name, req.RedirectURIs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "the client couldn't be registered")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        created.Unix(),
		"client_name":                name,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// checkRedirectURI accepts where an authorization can be sent back: an
// https URL, http only to this machine, or an app's own scheme. Fragments
// are never allowed, as OAuth requires.
func checkRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() {
		return errors.New("redirect URIs must be absolute URLs")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return errors.New("redirect URIs can't have a fragment")
	}
	switch scheme := strings.ToLower(u.Scheme); scheme {
	case "https":
		if u.Host == "" {
			return errors.New("https redirect URIs need a host")
		}
	case "http":
		if !isLoopback(u.Hostname()) {
			return errors.New("http redirect URIs must point at localhost; use https elsewhere")
		}
	case "javascript", "data", "file", "vbscript", "about", "blob":
		return errors.New("that redirect URI scheme isn't allowed")
	}

	return nil
}

// redirectAllowed matches a redirect URI against those registered
// exactly, except that a loopback one may use any port, since a native app
// listens wherever it can (RFC 8252, section 7.3).
func redirectAllowed(registered []string, uri string) bool {
	if slices.Contains(registered, uri) {
		return true
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "http" || !isLoopback(u.Hostname()) {
		return false
	}
	for _, r := range registered {
		allowed, err := url.Parse(r)
		if err == nil && allowed.Scheme == "http" && allowed.Hostname() == u.Hostname() &&
			allowed.Path == u.Path && allowed.RawQuery == u.RawQuery {
			return true
		}
	}

	return false
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// token exchanges an authorization code or a refresh token for tokens.
// Refresh tokens rotate: each works once, as public clients need.
func (s Server) token(w http.ResponseWriter, r *http.Request, store Store) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "send the request form-encoded")
		return
	}
	form := r.PostForm
	clientID := form.Get("client_id")
	asked, ok := s.endpointFor(form.Get("resource"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_target", s.onlyFor())
		return
	}

	var (
		grant    Grant
		resource string
		err      error
	)
	switch form.Get("grant_type") {
	case "authorization_code":
		var code Code
		code, err = store.TakeCode(r.Context(), hashSecret(form.Get("code")), clientID, s.named(asked, form))
		switch {
		case errors.Is(err, errNotFound):
			writeError(w, http.StatusBadRequest, "invalid_grant", "the code is invalid, expired or already used")
			return
		case errors.Is(err, errOtherClient):
			writeError(w, http.StatusBadRequest, "invalid_grant", "the code was issued to another client")
			return
		case errors.Is(err, errOtherResource):
			writeError(w, http.StatusBadRequest, "invalid_target", "the code is for another resource")
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "server_error", "the code couldn't be checked")
			return
		case code.RedirectURI != form.Get("redirect_uri"):
			writeError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri doesn't match the authorization request")
			return
		case !verifyPKCE(form.Get("code_verifier"), code.Challenge):
			writeError(w, http.StatusBadRequest, "invalid_grant", "code_verifier doesn't match the code_challenge")
			return
		}
		grant, resource = code.Grant, code.Resource
		grant.Scopes = code.Scopes
	case "refresh_token":
		grant, resource, err = store.TakeRefresh(r.Context(), hashSecret(form.Get("refresh_token")), clientID, s.named(asked, form))
		switch {
		case errors.Is(err, errNotFound):
			writeError(w, http.StatusBadRequest, "invalid_grant", "the refresh token is invalid, expired or already used")
			return
		case errors.Is(err, errOtherClient):
			writeError(w, http.StatusBadRequest, "invalid_grant", "the refresh token was issued to another client")
			return
		case errors.Is(err, errOtherResource):
			writeError(w, http.StatusBadRequest, "invalid_target", "the refresh token is for another resource")
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "server_error", "the refresh token couldn't be checked")
			return
		}
		// A client may ask for fewer scopes than it was granted, never more.
		if requested := strings.Fields(form.Get("scope")); len(requested) > 0 {
			for _, scope := range requested {
				if !slices.Contains(grant.Scopes, scope) {
					writeError(w, http.StatusBadRequest, "invalid_scope", "the refresh token wasn't granted "+scope)
					return
				}
			}
			grant.Scopes = requested
		}
	default:
		writeError(w, http.StatusBadRequest, "unsupported_grant_type", "use authorization_code or refresh_token")
		return
	}
	access, refresh := accessPrefix+newSecret(), refreshPrefix+newSecret()
	err = store.IssueTokens(r.Context(), grant, resource, hashSecret(access), hashSecret(refresh))
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusBadRequest, "invalid_grant", "the user who granted access can't sign in anymore")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "tokens couldn't be issued")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(accessLifetime.Seconds()),
		"refresh_token": refresh,
		"scope":         strings.Join(grant.Scopes, " "),
	})
}

// revoke ends a token (RFC 7009). Revoking a refresh token ends the whole
// grant's tokens, so a client that disconnects leaves nothing usable. It
// answers OK whatever the token was, so it can't be used to probe them.
func (s Server) revoke(w http.ResponseWriter, r *http.Request, store Store) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "send the request form-encoded")
		return
	}
	if err := store.RevokeToken(r.Context(), hashSecret(r.PostForm.Get("token")), r.PostForm.Get("client_id")); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "the token couldn't be revoked")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// named is the resource a token request binds its code or refresh token
// to, or empty when it names none and takes the one they're for.
func (s Server) named(e Endpoint, form url.Values) string {
	if form.Get("resource") == "" {
		return ""
	}
	return s.Resource(e)
}

// endpointFor finds the endpoint a resource URI names, comparing as RFC
// 8707 asks: the scheme and host ignore case, and a trailing slash makes no
// difference. A request that names none is for the user's server.
func (s Server) endpointFor(resource string) (Endpoint, bool) {
	if resource == "" {
		return UserMCP, true
	}
	canonical := func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + strings.TrimSuffix(u.Path, "/")
	}
	for _, e := range Endpoints {
		if c := canonical(resource); c != "" && c == canonical(s.Resource(e)) {
			return e, true
		}
	}
	return Endpoint{}, false
}

// Refusal tells a user whose role can't connect to e where to connect instead.
func (s Server) Refusal(e Endpoint) string {
	return "the " + e.Title + " server is for " + string(e.Role) + "s; connect to " + s.Resource(UserMCP) + " instead"
}

func (s Server) onlyFor() string {
	return "tokens are only issued for " + s.Resource(UserMCP) + " and " + s.Resource(BuilderMCP)
}

func verifyPKCE(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

func newSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError answers in OAuth's error format.
func writeError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}
