package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/dvjn/goldenportal/internal/httpx"
)

const (
	sessionCookie = "gp_session"
	// csrfHeader must be present on every state-changing web API request.
	// Browsers cannot send a custom header cross-site without a CORS
	// preflight, so requiring it stops cross-site requests from riding on the
	// session cookie.
	csrfHeader = "X-GP-CSRF"
)

// IngestPrefix is where integrations push entities. Requests there sign in
// with an ingestion token, which the ingest handlers check themselves.
const IngestPrefix = "/api/ingest/"

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
func Middleware(resolver principalResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasPrefix(r.URL.Path, IngestPrefix):
				secret, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ingestKey{}, secret)))
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

			info, status, msg := cookieAuth(r, resolver)
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

func cookieAuth(r *http.Request, resolver principalResolver) (authInfo, int, string) {
	if !isSafeMethod(r.Method) && r.Header.Get(csrfHeader) == "" {
		return authInfo{}, http.StatusForbidden, "the " + csrfHeader + " header is required"
	}

	cookie, err := r.Cookie(sessionCookie)
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

// IngestionSecret returns the bearer token sent to an ingest path, if any.
func IngestionSecret(ctx context.Context) string {
	secret, _ := ctx.Value(ingestKey{}).(string)
	return secret
}
