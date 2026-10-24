package app

import (
	"net/http"
	"strings"

	"github.com/dvjn/goldenportal/internal/access"
	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/web"
	"github.com/dvjn/goldenportal/migrations"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
)

func New() *gofr.App {
	app := gofr.New()
	app.Migrate(migrations.All())

	secureCookies := app.Config.GetOrDefault("GP_COOKIE_SECURE", "true") != "false"
	oidc := auth.NewOIDC(auth.OIDCConfig{
		Issuer:              app.Config.Get("GP_OIDC_ISSUER"),
		ClientID:            app.Config.Get("GP_OIDC_CLIENT_ID"),
		ClientSecret:        app.Config.Get("GP_OIDC_CLIENT_SECRET"),
		Scopes:              strings.Fields(app.Config.GetOrDefault("GP_OIDC_SCOPES", "openid profile email")),
		BaseURL:             app.Config.Get("GP_BASE_URL"),
		GroupsClaim:         app.Config.GetOrDefault("GP_OIDC_GROUPS_CLAIM", "groups"),
		BootstrapAdminEmail: app.Config.Get("GP_BOOTSTRAP_ADMIN_EMAIL"),
		SecureCookies:       secureCookies,
	}, app.Logger())
	if !oidc.Configured() {
		app.Logger().Warn("Single sign-on is not set up, so nobody can sign in. Set GP_OIDC_ISSUER, GP_OIDC_CLIENT_ID and GP_OIDC_CLIENT_SECRET.")
	}

	app.UseMiddleware(web.SPA(web.Dist()))
	app.UseMiddlewareWithContainer(func(c *container.Container, next http.Handler) http.Handler {
		store := auth.NewStore(c.SQL)
		return auth.Middleware(store)(oidc.Routes(store)(next))
	})

	app.GET("/api/v1/config", configHandler(Config{
		Name:    app.Config.GetOrDefault("GP_NAME", "Golden Portal"),
		LogoURL: app.Config.Get("GP_LOGO_URL"),
	}))
	auth.NewHandlers(secureCookies).Register(app)
	access.Register(app)
	registerSettings(app, oidc)

	return app
}
