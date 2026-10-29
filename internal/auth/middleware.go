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
	"github.com/dvjn/goldport/internal/token"
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

type tokenFinder interface {
	Find(ctx context.Context, kind, secret string) (token.Token, error)
}

// Authenticator finds the principal behind a request and resolves its role:
// an ingest token on an ingest route, a service token when the request has a
// bearer token, otherwise the user of the session cookie.
type Authenticator struct {
	sessions sessionFinder
	tokens   tokenFinder
	resolver accessResolver
	cookie   string
}

func NewAuthenticator(sessions sessionFinder, tokens tokenFinder, resolver accessResolver, cookies Cookies) *Authenticator {
	return &Authenticator{sessions: sessions, tokens: tokens, resolver: resolver, cookie: cookies.Session}
}

func (a *Authenticator) Authenticate(r *http.Request, declared httpx.Access) (Principal, error) {
	secret, bearer := bearerToken(r)
	switch {
	case declared.Ingest() && bearer:
		return a.token(r.Context(), token.KindIngest, secret)
	case declared.Ingest():
		return Principal{}, ErrNoSession
	case bearer:
		return a.token(r.Context(), token.KindService, secret)
	}

	return a.session(r)
}

func (a *Authenticator) token(ctx context.Context, kind, secret string) (Principal, error) {
	found, err := a.tokens.Find(ctx, kind, secret)
	if errors.Is(err, token.ErrNotFound) {
		return Principal{}, ErrNoSession
	}

	return Principal{User: directory.User{Name: found.Name}, Token: found, Access: access.Resolution{Role: found.Role}}, err
}

func (a *Authenticator) session(r *http.Request) (Principal, error) {
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
// every request to an authenticated route without a live session, every
// request without the permission its route requires, and every ingest
// request for another integration than its token's.
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

			principal, err := authenticator.Authenticate(r, declared)
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
			if scope := principal.Token.Integration; declared.Ingest() && scope != gofrHTTP.NewRequest(r).PathParam(httpx.IntegrationParam) {
				httpx.WriteError(w, http.StatusForbidden, fmt.Sprintf("this token ingests into the %s integration only", scope))
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
		})
	}
}
