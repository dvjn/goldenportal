package app

import (
	"context"

	"github.com/dvjn/goldport/internal/web"
	"gofr.dev/pkg/gofr"
)

func New(_ context.Context) (*Server, error) {
	app := gofr.New()
	server := &Server{App: app}

	app.UseMiddleware(web.SPA(web.Dist()))
	app.GET("/api/v1/config", configHandler(Config{
		Name: app.Config.GetOrDefault("GOLDPORT_NAME", "GoldPort"),
	}))

	return server, nil
}
