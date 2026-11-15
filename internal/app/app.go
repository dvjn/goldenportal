package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/dvjn/goldenportal/internal/access"
	"github.com/dvjn/goldenportal/internal/action"
	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/connection"
	"github.com/dvjn/goldenportal/internal/dashboard"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
	portalmcp "github.com/dvjn/goldenportal/internal/mcp"
	"github.com/dvjn/goldenportal/internal/oauth"
	"github.com/dvjn/goldenportal/internal/scim"
	"github.com/dvjn/goldenportal/internal/scorecard"
	"github.com/dvjn/goldenportal/internal/search"
	"github.com/dvjn/goldenportal/internal/telemetry"
	"github.com/dvjn/goldenportal/internal/web"
	"github.com/dvjn/goldenportal/migrations"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/config"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/logging"
)

func New() *gofr.App {
	telemetry.InstallSQL()
	app := gofr.New()
	telemetry.SetLogger(app.Logger())
	api := webRoutes{app}
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

	// The portal is its own OAuth server, for MCP clients, at its public URL.
	name := app.Config.GetOrDefault("GP_NAME", "Golden Portal")
	oauthServer := oauth.NewServer(app.Config.Get("GP_BASE_URL"), name)
	if app.Config.Get("GP_BASE_URL") == "" {
		app.Logger().Warn("MCP clients can't sign in until GP_BASE_URL is set to the portal's public URL.")
	}

	// The favicon falls back to the logo.
	favicon, err := web.Favicon(app.Config.GetOrDefault("GP_FAVICON_URL", app.Config.Get("GP_LOGO_URL")))
	if err != nil {
		app.Logger().Errorf("The favicon can't be served: %v", err)
		favicon, _ = web.Favicon("")
	}
	app.UseMiddleware(favicon, web.SPA(web.Dist()), web.BuildHeader(web.Build(web.Dist())), httpx.WithQuery)
	mcpLimits := portalmcp.NewLimiter()
	relay := telemetry.NewRelay(app.Config)
	searcher := search.New(blueprint.Search, entity.Search, dashboard.Search, scorecard.Search, connection.Search, action.Search)
	app.UseMiddlewareWithContainer(func(c *container.Container, next http.Handler) http.Handler {
		store := auth.NewStore(c.SQL)
		backend := auth.Middleware(store, secureCookies)(oidc.Routes(store)(browserTraces(relay)(next)))
		// MCP tools call the services as the token's user, who the MCP
		// endpoint already signed in.
		newContext := func(ctx context.Context) *gofr.Context {
			return &gofr.Context{Context: ctx, Container: c, ContextLogger: logging.ContextLoggerFor(ctx, c.Logger)}
		}
		// MCP answers its paths under /api before the web API's sign-in, which refuses tokens.
		mcp := portalmcp.Middleware(portalmcp.NewServices(searcher), newContext, oauth.NewStore(c.SQL), oauthServer, app.Config.Get("APP_VERSION"), mcpLimits)(backend)
		return scimServer.Routes(c.SQL)(oauthServer.Routes(c.SQL)(mcp))
	})

	config := Config{Name: name, LogoURL: app.Config.Get("GP_LOGO_URL")}
	if app.Config.Get("GP_BASE_URL") != "" {
		config.MCP = &MCPConfig{User: oauthServer.Resource(oauth.UserMCP), Builder: oauthServer.Resource(oauth.BuilderMCP)}
	}
	if relay != nil {
		config.Tracing = &TracingConfig{SampleRatio: relay.SampleRatio}
	}
	api.GET("config", configHandler(config))
	auth.NewHandlers(secureCookies).Register(api)
	oauthServer.Register(api)
	access.Register(api)
	connection.Register(api, app.Config.Get("GP_BASE_URL"), app.Config.Get(connection.WebhookKeyEnv))
	blueprint.Register(api, []blueprint.Checker{dashboard.CheckPages, connection.CheckSources, scorecard.CheckGraders}, scorecard.EvaluateScoring)
	entity.Register(api, mustSchedule(app, entity.RetentionScheduleEnv, entity.DefaultRetentionSchedule))
	connection.RegisterIngest(app)
	scorecard.Register(api, mustSchedule(app, scorecard.EvaluationScheduleEnv, scorecard.DefaultEvaluationSchedule))
	dashboard.Register(api)
	action.Register(api, app.Config.Get("GP_BASE_URL"))
	search.Register(api, searcher)
	configdoc.RegisterBulk(api)
	configdoc.RegisterSchemas(api)
	registerSettings(api, describeConfig(app.Config, oidc, scimServer), oidc, scimServer)

	return app
}

// mustSchedule reads a schedule setting and stops the server if gofr can't
// run it, as gofr would only log that and drop the job.
func mustSchedule(app *gofr.App, env, fallback string) string {
	schedule, err := scheduleSetting(app.Config, env, fallback)
	if err != nil {
		app.Logger().Fatal(err)
	}

	return schedule
}

// scheduleSetting reads a cron expression setting, parsed the way gofr's
// scheduler parses it.
func scheduleSetting(cfg config.Config, env, fallback string) (string, error) {
	schedule := cfg.GetOrDefault(env, fallback)
	if err := new(gofr.Crontab).AddJob(schedule, env, nil); err != nil {
		return "", fmt.Errorf("%s=%q isn't a schedule: %w", env, schedule, err)
	}

	return schedule, nil
}
