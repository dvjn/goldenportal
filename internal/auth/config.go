// Package auth signs people in with OIDC and keeps their sessions.
package auth

import (
	"fmt"
	"time"

	"gofr.dev/pkg/gofr/config"
)

type Config struct {
	SessionMaxAge      time.Duration
	SessionIdleTimeout time.Duration
	Cookies            Cookies
}

func LoadConfig(source config.Config) (Config, error) {
	maxAge, err := duration(source, "GOLDPORT_SESSION_MAX_AGE", "12h")
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := duration(source, "GOLDPORT_SESSION_IDLE_TIMEOUT", "1h")
	if err != nil {
		return Config{}, err
	}

	return Config{
		SessionMaxAge:      maxAge,
		SessionIdleTimeout: idleTimeout,
		Cookies:            CookieNames(source.GetOrDefault("APP_NAME", "goldport")),
	}, nil
}

func duration(source config.Config, key, fallback string) (time.Duration, error) {
	value, err := time.ParseDuration(source.GetOrDefault(key, fallback))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration such as %s", key, fallback)
	}

	return value, nil
}
