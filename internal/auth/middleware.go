package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/directory"
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

type accessResolver interface {
	Resolve(ctx context.Context, user directory.User) (access.Resolution, error)
}

// Authenticator finds the principal behind a request and resolves its role.
type Authenticator struct {
	sessions sessionFinder
	resolver accessResolver
	cookie   string
}

func NewAuthenticator(sessions sessionFinder, resolver accessResolver, cookies Cookies) *Authenticator {
	return &Authenticator{sessions: sessions, resolver: resolver, cookie: cookies.Session}
}

func (a *Authenticator) Authenticate(r *http.Request) (Principal, error) {
	cookie, err := r.Cookie(a.cookie)
	if err != nil {
		return Principal{}, ErrNoSession
	}
	principal, err := a.sessions.Lookup(r.Context(), cookie.Value)
	if err != nil {
		return Principal{}, err
	}
	principal.Access, err = a.resolver.Resolve(r.Context(), principal.User)

	return principal, err
}

// Middleware refuses every /api/ request whose route has no declared access,
// every request to an authenticated route without a live session, and every
// request without the permission its route requires.
func Middleware(routes *httpx.Routes, authenticator *Authenticator) gofrHTTP.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			declared, ok := routes.Lookup(r.Method, gofrHTTP.RouteTemplate(r))
			if !ok {
				httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if !declared.RequiresSession() {
				next.ServeHTTP(w, r)
				return
			}

			principal, err := authenticator.Authenticate(r)
			switch {
			case errors.Is(err, ErrNoSession):
				httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
				return
			case err != nil:
				httpx.WriteError(w, http.StatusInternalServerError, "could not check the session")
				return
			}
			if permission, required := declared.Permission(); required && !principal.Can(permission) {
				httpx.WriteError(w, http.StatusForbidden,
					fmt.Sprintf("this needs the %s permission, ask an admin if you need access", permission))
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
		})
	}
}
