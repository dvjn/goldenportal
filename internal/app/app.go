package app

import (
	"github.com/dvjn/goldport/internal/web"
	"github.com/dvjn/goldport/migrations"
	"gofr.dev/pkg/gofr"
)

func New() *gofr.App {
	app := gofr.New()
	app.Migrate(migrations.All())

	app.UseMiddleware(web.SPA(web.Dist()))
	app.GET("/api/v1/config", configHandler(Config{
		Name: app.Config.GetOrDefault("GOLDPORT_NAME", "GoldPort"),
	}))

	return app
}
