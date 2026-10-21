package app

import (
	"context"

	"gofr.dev/pkg/gofr"
)

func New(_ context.Context) (*Server, error) {
	app := gofr.New()
	server := &Server{App: app}

	return server, nil
}
