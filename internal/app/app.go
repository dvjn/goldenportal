package app

import (
	"github.com/dvjn/goldport/internal/httpx"
	"github.com/dvjn/goldport/internal/web"
	"github.com/dvjn/goldport/migrations"
	"gofr.dev/pkg/gofr"
)

func New() *gofr.App {
	app := gofr.New()
	app.Migrate(migrations.All())

	app.UseMiddleware(web.SPA(web.Dist()), httpx.ExposeRaw)

	routes := httpx.NewRoutes(app)
	routes.GET("/api/v1/config", httpx.Public(), configHandler(Config{
		Name: app.Config.GetOrDefault("GOLDPORT_NAME", "GoldPort"),
	}))

	return app
}
