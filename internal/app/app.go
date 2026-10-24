package app

import (
	"github.com/dvjn/goldenportal/internal/web"
	"github.com/dvjn/goldenportal/migrations"
	"gofr.dev/pkg/gofr"
)

func New() *gofr.App {
	app := gofr.New()
	app.Migrate(migrations.All())

	app.UseMiddleware(web.SPA(web.Dist()))
	app.GET("/api/v1/config", configHandler(Config{
		Name: app.Config.GetOrDefault("GP_NAME", "Golden Portal"),
	}))

	return app
}
