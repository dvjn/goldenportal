package app

import (
	"context"

	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/httpx"
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
	provider, err := auth.NewProvider(context.Background(), authConfig)
	if err != nil {
		return err
	}
	directoryStore := directory.NewStore(app.GetSQL())
	sessions := auth.NewSessions(app.GetSQL(), authConfig)

	routes := httpx.NewRoutes(app)
	app.UseMiddleware(
		web.SPA(web.Dist()),
		httpx.ExposeRaw,
		auth.CrossOriginProtection(),
		auth.Middleware(routes, sessions, authConfig.Cookies),
	)

	routes.GET("/api/v1/config", httpx.Public(), configHandler(Config{
		Name: app.Config.GetOrDefault("GOLDPORT_NAME", "GoldPort"),
	}))
	auth.NewService(authConfig, provider, sessions, directoryStore).Register(routes)

	return nil
}
