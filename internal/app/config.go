package app

import "gofr.dev/pkg/gofr"

type Config struct {
	Name string `json:"name"`
}

func configHandler(config Config) gofr.Handler {
	return func(*gofr.Context) (any, error) {
		return config, nil
	}
}
