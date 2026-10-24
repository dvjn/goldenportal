package app

import (
	"net/http"
	"strings"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/config"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/scim"
)

// The configuration page summarises how the portal is set up, from settings
// read from the environment at start-up. Nothing here can be changed from the
// portal, and secrets only report whether they are set.

type serverConfig struct {
	Portal   portalConfig   `json:"portal"`
	OIDC     oidcConfig     `json:"oidc"`
	SCIM     scimConfig     `json:"scim"`
	Sessions sessionsConfig `json:"sessions"`
	Tokens   tokensConfig   `json:"tokens"`
	Database databaseConfig `json:"database"`
	Server   serverInfo     `json:"server"`
}

type portalConfig struct {
	Name          string `json:"name"`
	LogoURL       string `json:"logoUrl"`
	FaviconURL    string `json:"faviconUrl"`
	FaviconIsLogo bool   `json:"faviconIsLogo"`
	BaseURL       string `json:"baseUrl"`
}

type oidcConfig struct {
	Configured          bool     `json:"configured"`
	Issuer              string   `json:"issuer"`
	ClientID            string   `json:"clientId"`
	ClientSecretSet     bool     `json:"clientSecretSet"`
	Scopes              []string `json:"scopes"`
	GroupsClaim         string   `json:"groupsClaim"`
	RedirectURL         string   `json:"redirectUrl"`
	BootstrapAdminEmail string   `json:"bootstrapAdminEmail"`
}

type scimConfig struct {
	Enabled bool `json:"enabled"`
	// URL is the SCIM base address to give the identity provider.
	URL string `json:"url"`
}

type sessionsConfig struct {
	SecureCookies    bool `json:"secureCookies"`
	IdleTimeoutHours int  `json:"idleTimeoutHours"`
	MaxAgeHours      int  `json:"maxAgeHours"`
}

type tokensConfig struct {
	Prefix  string `json:"prefix"`
	MaxDays int    `json:"maxDays"`
}

type databaseConfig struct {
	Dialect     string `json:"dialect"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	Name        string `json:"name"`
	User        string `json:"user"`
	PasswordSet bool   `json:"passwordSet"`
	SSLMode     string `json:"sslMode"`
}

type serverInfo struct {
	Environment string `json:"environment"`
	Version     string `json:"version"`
	HTTPPort    string `json:"httpPort"`
	MetricsPort string `json:"metricsPort"`
	LogLevel    string `json:"logLevel"`
}

func describeConfig(cfg config.Config, oidc *auth.OIDC, scimServer *scim.Server) serverConfig {
	return serverConfig{
		Portal: portalConfig{
			Name:          cfg.GetOrDefault("GP_NAME", "Golden Portal"),
			LogoURL:       cfg.Get("GP_LOGO_URL"),
			FaviconURL:    cfg.GetOrDefault("GP_FAVICON_URL", cfg.Get("GP_LOGO_URL")),
			FaviconIsLogo: cfg.Get("GP_FAVICON_URL") == "",
			BaseURL:       cfg.Get("GP_BASE_URL"),
		},
		OIDC: oidcConfig{
			Configured:          oidc.Configured(),
			Issuer:              cfg.Get("GP_OIDC_ISSUER"),
			ClientID:            cfg.Get("GP_OIDC_CLIENT_ID"),
			ClientSecretSet:     cfg.Get("GP_OIDC_CLIENT_SECRET") != "",
			Scopes:              strings.Fields(cfg.GetOrDefault("GP_OIDC_SCOPES", "openid profile email")),
			GroupsClaim:         cfg.GetOrDefault("GP_OIDC_GROUPS_CLAIM", "groups"),
			RedirectURL:         oidc.RedirectURL(),
			BootstrapAdminEmail: cfg.Get("GP_BOOTSTRAP_ADMIN_EMAIL"),
		},
		SCIM: scimConfig{Enabled: scimServer.Enabled(), URL: scimServer.URL()},
		Sessions: sessionsConfig{
			SecureCookies:    cfg.GetOrDefault("GP_COOKIE_SECURE", "true") != "false",
			IdleTimeoutHours: int(auth.SessionIdleTimeout.Hours()),
			MaxAgeHours:      int(auth.SessionMaxAge.Hours()),
		},
		Tokens: tokensConfig{Prefix: auth.TokenPrefix, MaxDays: auth.MaxTokenDays},
		Database: databaseConfig{
			Dialect:     cfg.Get("DB_DIALECT"),
			Host:        cfg.Get("DB_HOST"),
			Port:        cfg.Get("DB_PORT"),
			Name:        cfg.Get("DB_NAME"),
			User:        cfg.Get("DB_USER"),
			PasswordSet: cfg.Get("DB_PASSWORD") != "",
			SSLMode:     cfg.Get("DB_SSL_MODE"),
		},
		Server: serverInfo{
			Environment: cfg.Get("APP_ENV"),
			Version:     cfg.Get("APP_VERSION"),
			HTTPPort:    cfg.GetOrDefault("HTTP_PORT", "8000"),
			MetricsPort: cfg.GetOrDefault("METRICS_PORT", "2121"),
			LogLevel:    cfg.GetOrDefault("LOG_LEVEL", "INFO"),
		},
	}
}

type actionResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func registerSettings(app *gofr.App, oidc *auth.OIDC, scimServer *scim.Server) {
	summary := describeConfig(app.Config, oidc, scimServer)

	app.GET("/api/v1/settings/configuration", func(ctx *gofr.Context) (any, error) {
		if _, err := auth.Require(ctx, auth.RoleAdmin); err != nil {
			return nil, err
		}

		return summary, nil
	})

	app.POST("/api/v1/settings/configuration/actions/{id}", func(ctx *gofr.Context) (any, error) {
		if _, err := auth.Require(ctx, auth.RoleAdmin); err != nil {
			return nil, err
		}

		switch ctx.PathParam("id") {
		case "oidc.check":
			return checkOIDC(ctx, oidc), nil
		default:
			return nil, httpx.Error{Code: http.StatusNotFound, Message: "no such action"}
		}
	})
}

// checkOIDC reports a failed check as a result, not an error: the request worked.
func checkOIDC(ctx *gofr.Context, oidc *auth.OIDC) actionResult {
	if !oidc.Configured() {
		return actionResult{Message: "Set GP_OIDC_ISSUER and GP_OIDC_CLIENT_ID first."}
	}

	issuer, err := oidc.Check(ctx)
	if err != nil {
		return actionResult{Message: "Couldn't load the identity provider's settings: " + err.Error()}
	}

	return actionResult{OK: true, Message: "Reached the identity provider at " + issuer + "."}
}
