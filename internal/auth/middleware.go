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
	// csrfHeader must be present on every state-changing API request signed in
	// by cookie. Browsers cannot send a custom header cross-site without a CORS
	// preflight, so requiring it stops cross-site requests from riding on the
	// session cookie. Token requests don't need it: browsers never attach tokens.
	csrfHeader = "X-GP-CSRF"
)

// publicPaths answer without a signed-in user.
var publicPaths = map[string]bool{
	"/api/v1/config": true,
	signInPath:       true,
	callbackPath:     true,
}

type principalResolver interface {
	SessionPrincipal(ctx context.Context, sessionHash []byte) (Principal, error)
	TokenPrincipal(ctx context.Context, secretHash []byte) (Principal, bool, error)
}

// Middleware attaches the signed-in user to API requests and rejects API
// requests that need one and don't have it. A request signs in with either an
// API token in the Authorization header or a session cookie. Non-API requests
// pass through.
func Middleware(resolver principalResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			var (
				info   authInfo
				status int
				msg    string
			)
			if header := r.Header.Get("Authorization"); header != "" {
				info, status, msg = tokenAuth(r, resolver, header)
			} else {
				info, status, msg = cookieAuth(r, resolver)
			}
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

func tokenAuth(r *http.Request, resolver principalResolver, header string) (authInfo, int, string) {
	secret, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || secret == "" {
		return authInfo{}, http.StatusUnauthorized, "send the API token as \"Authorization: Bearer <token>\""
	}

	p, readOnly, err := resolver.TokenPrincipal(r.Context(), hashSecret(secret))
	switch {
	case errors.Is(err, errNotFound):
		return authInfo{}, http.StatusUnauthorized, "the API token is invalid, expired or revoked"
	case err != nil:
		return authInfo{}, http.StatusInternalServerError, "could not check the API token"
	case readOnly && !isSafeMethod(r.Method):
		return authInfo{}, http.StatusForbidden, "this API token is read-only"
	}

	return authInfo{principal: p, viaToken: true}, 0, ""
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
