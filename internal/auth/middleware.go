package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/dvjn/goldenportal/internal/httpx"
)

const (
	// csrfHeader must be present on every state-changing web API request.
	// Browsers cannot send a custom header cross-site without a CORS
	// preflight, so requiring it stops cross-site requests from riding on the
	// session cookie.
	csrfHeader = "X-GP-CSRF"
)

// sessionCookie names the session cookie. With secure cookies it carries
// the __Host-Http- prefix (RFC 10017 §6.1.3.2), so browsers only take it
// from this host, over HTTPS, for the whole site, and never from script:
// a sibling subdomain or an injected script can't plant a session.
func sessionCookie(secure bool) string {
	if secure {
		return "__Host-Http-gp_session"
	}
	return "gp_session"
}

// IngestPrefix is where integrations push entities. Requests there sign in
// with an ingestion token, which the ingest handlers check themselves.
const IngestPrefix = "/api/ingest/"

// MaxIngestBytes caps a push body, so one request can't hold the server's
// memory.
const MaxIngestBytes = 32 << 20

// publicPaths answer without a signed-in user.
var publicPaths = map[string]bool{
	httpx.WebAPI + "config": true,
	signInPath:              true,
	callbackPath:            true,
}

type principalResolver interface {
	SessionPrincipal(ctx context.Context, sessionHash []byte) (Principal, error)
}

// Middleware signs in API requests, by the one credential each API takes:
// the web API a browser session, and ingest an ingestion token, which its
// handlers check. It rejects web API requests that need a user and don't
// have one, and API paths that serve nothing. Other requests pass through.
func Middleware(resolver principalResolver, secureCookies bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasPrefix(r.URL.Path, IngestPrefix):
				// Read the body here, so a signature can be checked against it as sent.
				body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxIngestBytes))
				var tooLarge *http.MaxBytesError
				switch {
				case errors.As(err, &tooLarge):
					httpx.WriteError(w, http.StatusRequestEntityTooLarge,
						fmt.Sprintf("the request body is over %d MB; split it across requests", MaxIngestBytes>>20))
					return
				case err != nil:
					httpx.WriteError(w, http.StatusBadRequest, "could not read the request body")
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				secret, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
				in := Ingest{Secret: secret, Header: r.Header.Clone(), Query: r.URL.Query(), Body: body}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ingestKey{}, in)))
				return
			case strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, httpx.WebAPI):
				httpx.WriteError(w, http.StatusNotFound, "there is no API here")
				return
			case !strings.HasPrefix(r.URL.Path, httpx.WebAPI):
				next.ServeHTTP(w, r)
				return
			case r.Header.Get("Authorization") != "":
				// Only the portal's own pages call the web API. Agents connect
				// over MCP, which has its own sign-in.
				httpx.WriteError(w, http.StatusUnauthorized, "the web API takes a browser session, not a token; connect agents over MCP at /mcp")
				return
			}

			info, status, msg := cookieAuth(r, resolver, sessionCookie(secureCookies))
			if status != 0 {
				httpx.WriteError(w, status, msg)
				return
			}

			if info.principal.ID != "" {
				r = r.WithContext(withAuth(r.Context(), info))
			} else if !publicPaths[r.URL.Path] {
				httpx.WriteError(w, http.StatusUnauthorized, "sign in to continue")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func cookieAuth(r *http.Request, resolver principalResolver, name string) (authInfo, int, string) {
	if !isSafeMethod(r.Method) && r.Header.Get(csrfHeader) == "" {
		return authInfo{}, http.StatusForbidden, "the " + csrfHeader + " header is required"
	}

	cookie, err := r.Cookie(name)
	if err != nil || cookie.Value == "" {
		return authInfo{}, 0, ""
	}

	hash := hashSecret(cookie.Value)
	p, err := resolver.SessionPrincipal(r.Context(), hash)
	switch {
	case errors.Is(err, errNotFound):
		return authInfo{}, 0, ""
	case err != nil:
		return authInfo{}, http.StatusInternalServerError, "could not check the session"
	}

	return authInfo{principal: p, sessionHash: hash}, 0, ""
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

type ingestKey struct{}

// Ingest is a request to an ingest path, as sent.
type Ingest struct {
	// Secret is the bearer token, if any.
	Secret string
	Header http.Header
	Query  url.Values
	Body   []byte
}

// IngestRequest returns the request sent to an ingest path.
func IngestRequest(ctx context.Context) Ingest {
	in, _ := ctx.Value(ingestKey{}).(Ingest)
	return in
}
