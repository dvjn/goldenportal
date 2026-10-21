package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/dvjn/goldport/internal/httpx"
	gofrHTTP "gofr.dev/pkg/gofr/http"
)

type principalKey struct{}

func PrincipalFrom(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}

type sessionFinder interface {
	Lookup(ctx context.Context, token string) (Principal, error)
}

// Middleware refuses every /api/ request whose route has no declared access,
// and every request to an authenticated route without a live session.
func Middleware(routes *httpx.Routes, sessions sessionFinder, cookies Cookies) gofrHTTP.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			access, declared := routes.Lookup(r.Method, gofrHTTP.RouteTemplate(r))
			if !declared {
				httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if !access.RequiresSession() {
				next.ServeHTTP(w, r)
				return
			}

			principal, err := lookup(r, sessions, cookies.Session)
			switch {
			case errors.Is(err, ErrNoSession):
				httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
			case err != nil:
				httpx.WriteError(w, http.StatusInternalServerError, "could not check the session")
			default:
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
			}
		})
	}
}

func lookup(r *http.Request, sessions sessionFinder, cookieName string) (Principal, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return Principal{}, ErrNoSession
	}

	return sessions.Lookup(r.Context(), cookie.Value)
}
