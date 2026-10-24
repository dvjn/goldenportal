package auth

import (
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"
)

type Handlers struct {
	secureCookie bool
}

func NewHandlers(secureCookie bool) Handlers {
	return Handlers{secureCookie: secureCookie}
}

func (h Handlers) Register(app *gofr.App) {
	app.POST("/api/v1/auth/logout", h.logout)
	app.GET("/api/v1/me", h.me)
	app.GET("/api/v1/me/tokens", h.listTokens)
	app.POST("/api/v1/me/tokens", h.createToken)
	app.DELETE("/api/v1/me/tokens/{id}", h.revokeToken)
}

func (h Handlers) logout(ctx *gofr.Context) (any, error) {
	if info, _ := authFromContext(ctx); info.sessionHash != nil {
		if err := NewStore(ctx.SQL).DeleteSession(ctx, info.sessionHash); err != nil {
			return nil, err
		}
	}

	return response.Response{
		Headers: map[string]string{"Set-Cookie": newCookie(sessionCookie, "", -1, h.secureCookie).String()},
	}, nil
}

func (h Handlers) me(ctx *gofr.Context) (any, error) {
	p, _ := FromContext(ctx)
	return p, nil
}
