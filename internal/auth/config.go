// Package auth signs people in with OIDC and keeps their sessions.
package auth

import (
	"fmt"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"gofr.dev/pkg/gofr/config"
)

type Config struct {
	PublicURL          string
	IssuerURL          string
	ClientID           string
	ClientSecret       string
	SessionMaxAge      time.Duration
	SessionIdleTimeout time.Duration
	Cookies            Cookies
	BootstrapAdmins    access.BootstrapAdmins
}

// LoadConfig reads the auth settings. A missing client secret makes the
// portal a public client, which PKCE protects.
func LoadConfig(source config.Config) (Config, error) {
	if err := requireSettings(source, "GOLDPORT_PUBLIC_URL", "GOLDPORT_OIDC_ISSUER_URL", "GOLDPORT_OIDC_CLIENT_ID"); err != nil {
		return Config{}, err
	}
	maxAge, err := duration(source, "GOLDPORT_SESSION_MAX_AGE", "12h")
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := duration(source, "GOLDPORT_SESSION_IDLE_TIMEOUT", "1h")
	if err != nil {
		return Config{}, err
	}

	return Config{
		PublicURL:          strings.TrimSuffix(source.Get("GOLDPORT_PUBLIC_URL"), "/"),
		IssuerURL:          source.Get("GOLDPORT_OIDC_ISSUER_URL"),
		ClientID:           source.Get("GOLDPORT_OIDC_CLIENT_ID"),
		ClientSecret:       source.Get("GOLDPORT_OIDC_CLIENT_SECRET"),
		SessionMaxAge:      maxAge,
		SessionIdleTimeout: idleTimeout,
		Cookies:            CookieNames(source.GetOrDefault("APP_NAME", "goldport")),
		BootstrapAdmins:    access.ParseBootstrapAdmins(source.Get("GOLDPORT_BOOTSTRAP_ADMINS")),
	}, nil
}

func requireSettings(source config.Config, keys ...string) error {
	var missing []string
	for _, key := range keys {
		if source.Get(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("set the required settings: %s", strings.Join(missing, ", "))
	}

	return nil
}

func duration(source config.Config, key, fallback string) (time.Duration, error) {
	value, err := time.ParseDuration(source.GetOrDefault(key, fallback))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration such as %s", key, fallback)
	}

	return value, nil
}
