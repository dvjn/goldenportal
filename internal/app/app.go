package app

import (
	"net/http"
	"strings"

	"github.com/dvjn/goldenportal/internal/access"
	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/dashboard"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/integration"
	"github.com/dvjn/goldenportal/internal/scim"
	"github.com/dvjn/goldenportal/internal/scorecard"
	"github.com/dvjn/goldenportal/internal/web"
	"github.com/dvjn/goldenportal/migrations"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
)

func New() *gofr.App {
	app := gofr.New()
	app.Migrate(migrations.All())

	secureCookies := app.Config.GetOrDefault("GP_COOKIE_SECURE", "true") != "false"
	scimServer := scim.New(app.Config.Get("GP_SCIM_TOKEN"), app.Config.Get("GP_BASE_URL"), app.Logger())
	oidc := auth.NewOIDC(auth.OIDCConfig{
		Issuer:              app.Config.Get("GP_OIDC_ISSUER"),
		ClientID:            app.Config.Get("GP_OIDC_CLIENT_ID"),
		ClientSecret:        app.Config.Get("GP_OIDC_CLIENT_SECRET"),
		Scopes:              strings.Fields(app.Config.GetOrDefault("GP_OIDC_SCOPES", "openid profile email")),
		BaseURL:             app.Config.Get("GP_BASE_URL"),
		GroupsClaim:         app.Config.GetOrDefault("GP_OIDC_GROUPS_CLAIM", "groups"),
		SCIMManagesGroups:   scimServer.Enabled(),
		BootstrapAdminEmail: app.Config.Get("GP_BOOTSTRAP_ADMIN_EMAIL"),
		SecureCookies:       secureCookies,
	}, app.Logger())
	if !oidc.Configured() {
		app.Logger().Warn("Single sign-on is not set up, so nobody can sign in. Set GP_OIDC_ISSUER, GP_OIDC_CLIENT_ID and GP_OIDC_CLIENT_SECRET.")
	}

	// The favicon falls back to the logo.
	favicon, err := web.Favicon(app.Config.GetOrDefault("GP_FAVICON_URL", app.Config.Get("GP_LOGO_URL")))
	if err != nil {
		app.Logger().Errorf("The favicon can't be served: %v", err)
		favicon, _ = web.Favicon("")
	}
	app.UseMiddleware(favicon, web.SPA(web.Dist()))
	app.UseMiddlewareWithContainer(func(c *container.Container, next http.Handler) http.Handler {
		store := auth.NewStore(c.SQL)
		return scimServer.Routes(c.SQL)(auth.Middleware(store)(oidc.Routes(store)(next)))
	})

	app.GET("/api/v1/config", configHandler(Config{
		Name:    app.Config.GetOrDefault("GP_NAME", "Golden Portal"),
		LogoURL: app.Config.Get("GP_LOGO_URL"),
	}))
	auth.NewHandlers(secureCookies).Register(app)
	access.Register(app)
	blueprint.Register(app, dashboard.CheckPages)
	entity.Register(app)
	integration.Register(app)
	scorecard.Register(app)
	dashboard.Register(app)
	configdoc.RegisterBulk(app)
	registerSettings(app, oidc, scimServer)

	return app
}
