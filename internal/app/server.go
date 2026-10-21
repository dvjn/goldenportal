package app

import (
	"context"
	"errors"

	"gofr.dev/pkg/gofr"
)

type Server struct {
	*gofr.App
	startupErr error
}

func (s *Server) OnStart(hook func(*gofr.Context) error) {
	s.App.OnStart(func(ctx *gofr.Context) error {
		err := hook(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			s.startupErr = err
		}
		return err
	})
}

func (s *Server) Run() error {
	s.App.Run()
	return s.startupErr
}
