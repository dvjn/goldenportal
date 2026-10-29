package app

import (
	"context"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/admin"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/catalog"
	"github.com/dvjn/goldport/internal/connector"
	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/httpx"
	"github.com/dvjn/goldport/internal/integration"
	"github.com/dvjn/goldport/internal/scim"
	"github.com/dvjn/goldport/internal/token"
	"github.com/dvjn/goldport/internal/web"
	"github.com/dvjn/goldport/migrations"
	"gofr.dev/pkg/gofr"
)

func New() *gofr.App {
	app := gofr.New()
	app.Migrate(migrations.All())

	if err := register(app); err != nil {
		app.Logger().Fatal(err.Error())
	}

	return app
}

func register(app *gofr.App) error {
	authConfig, err := auth.LoadConfig(app.Config)
	if err != nil {
		return err
	}
	scimToken, err := token.SCIMToken(app.Config)
	if err != nil {
		return err
	}
	provider, err := auth.NewProvider(context.Background(), authConfig)
	if err != nil {
		return err
	}
	directoryStore := directory.NewStore(app.GetSQL())
	sessions := auth.NewSessions(app.GetSQL(), authConfig)
	tokens := token.NewStore(app.GetSQL())
	grants := access.NewGrants(app.GetSQL())
	resolver := access.NewResolver(grants, authConfig.BootstrapAdmins)
	auditLog := audit.NewLog(app.GetSQL())
	portalName := app.Config.GetOrDefault("GOLDPORT_NAME", "GoldPort")

	app.OnStart(token.SeedSCIMToken(tokens, scimToken))

	routes := httpx.NewRoutes(app)
	app.UseMiddleware(
		web.SPA(web.Dist()),
		httpx.ExposeRaw,
		auth.CrossOriginProtection(),
		auth.Middleware(routes, auth.NewAuthenticator(sessions, tokens, resolver, authConfig.Cookies)),
		scim.NewServer(directoryStore, sessions, tokens, auditLog, app.Logger()).Middleware(),
	)

	routes.GET("/api/v1/config", httpx.Public(), configHandler(Config{Name: portalName}))
	auth.NewService(authConfig, provider, sessions, directoryStore, auditLog).Register(routes)
	admin.NewService(admin.Settings{
		Name:               portalName,
		PublicURL:          authConfig.PublicURL,
		IssuerURL:          authConfig.IssuerURL,
		ClientID:           authConfig.ClientID,
		ClientSecretSet:    authConfig.ClientSecret != "",
		SessionMaxAge:      authConfig.SessionMaxAge,
		SessionIdleTimeout: authConfig.SessionIdleTimeout,
		BootstrapAdmins:    authConfig.BootstrapAdmins,
	}, grants, directoryStore, tokens, sessions, auditLog).Register(routes)
	catalogStore := catalog.NewStore(app.GetSQL())
	catalog.NewService(catalogStore, catalogStore, auditLog, time.Now).Register(routes)
	runs := integration.NewRuns(app.GetSQL())
	syncer := integration.NewSyncer(integration.SyncerConfig{
		Entities:   catalogStore,
		Runs:       runs,
		Connectors: integration.Connectors(connector.DefaultHTTPClient()),
		Locker:     integration.NewPostgresLocker(app.GetSQL()),
		Getenv:     app.Config.Get,
		Now:        time.Now,
		Logger:     app.Logger(),
	})
	scheduler := integration.NewScheduler(catalogStore, runs, syncer, time.Now, app.Logger())
	app.OnStart(func(*gofr.Context) error {
		go scheduler.Run(context.Background())
		return nil
	})
	integration.NewService(integration.ServiceConfig{
		Definitions: catalogStore,
		Entities:    catalogStore,
		Counts:      catalogStore,
		Runs:        runs,
		Syncer:      syncer,
		Scheduler:   scheduler,
		Tokens:      tokens,
		Audit:       auditLog,
		PublicURL:   authConfig.PublicURL,
		Now:         time.Now,
	}).Register(routes)

	return nil
}
