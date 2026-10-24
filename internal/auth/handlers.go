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
}

func (h Handlers) logout(ctx *gofr.Context) (any, error) {
	if hash := sessionFromContext(ctx); hash != nil {
		if err := NewStore(ctx.SQL).DeleteSession(ctx, hash); err != nil {
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
