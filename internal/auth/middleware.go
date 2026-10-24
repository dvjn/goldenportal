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
	// csrfHeader must be present on every state-changing API request. Browsers
	// cannot send a custom header cross-site without a CORS preflight, so
	// requiring it stops cross-site requests from riding on the session cookie.
	csrfHeader = "X-GP-CSRF"
)

// publicPaths answer without a signed-in user.
var publicPaths = map[string]bool{
	"/api/v1/config": true,
	signInPath:       true,
	callbackPath:     true,
}

type sessionResolver interface {
	SessionPrincipal(ctx context.Context, sessionHash []byte) (Principal, error)
}

// Middleware attaches the signed-in user to API requests and rejects API
// requests that need one and don't have it. Non-API requests pass through.
func Middleware(sessions sessionResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			if !isSafeMethod(r.Method) && r.Header.Get(csrfHeader) == "" {
				httpx.WriteError(w, http.StatusForbidden, "the "+csrfHeader+" header is required")
				return
			}

			if cookie, err := r.Cookie(sessionCookie); err == nil && cookie.Value != "" {
				hash := hashSecret(cookie.Value)
				p, err := sessions.SessionPrincipal(r.Context(), hash)
				switch {
				case err == nil:
					r = r.WithContext(withPrincipal(r.Context(), p, hash))
				case !errors.Is(err, errNotFound):
					httpx.WriteError(w, http.StatusInternalServerError, "could not check the session")
					return
				}
			}

			if _, ok := FromContext(r.Context()); !ok && !publicPaths[r.URL.Path] {
				httpx.WriteError(w, http.StatusUnauthorized, "sign in to continue")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
