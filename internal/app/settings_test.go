package app

import (
	"slices"
	"testing"

	"gofr.dev/pkg/gofr/config"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/auth"
)

func TestDescribeConfig(t *testing.T) {
	cfg := config.NewMockConfig(map[string]string{
		"GP_NAME":               "Acme Portal",
		"GP_OIDC_ISSUER":        "https://idp.example.com",
		"GP_OIDC_CLIENT_ID":     "portal",
		"GP_OIDC_CLIENT_SECRET": "super-secret",
		"DB_PASSWORD":           "",
	})
	oidc := auth.NewOIDC(auth.OIDCConfig{
		Issuer: "https://idp.example.com", ClientID: "portal", BaseURL: "https://portal.example.com/",
	}, logging.NewMockLogger(logging.ERROR))

	got := describeConfig(cfg, oidc)

	if got.Portal.Name != "Acme Portal" {
		t.Errorf("portal name = %q, want the configured name", got.Portal.Name)
	}
	if !got.OIDC.Configured || !got.OIDC.ClientSecretSet {
		t.Errorf("oidc = %+v, want configured with a secret set", got.OIDC)
	}
	if !slices.Equal(got.OIDC.Scopes, []string{"openid", "profile", "email"}) {
		t.Errorf("scopes = %v, want the default scopes", got.OIDC.Scopes)
	}
	if got.OIDC.RedirectURL != "https://portal.example.com/api/v1/auth/callback" {
		t.Errorf("redirect URL = %q, want the callback under GP_BASE_URL", got.OIDC.RedirectURL)
	}
	if got.Database.PasswordSet {
		t.Error("database password reported as set")
	}
	if !got.Sessions.SecureCookies || got.Sessions.MaxAgeHours != 168 {
		t.Errorf("sessions = %+v, want secure cookies and a 7-day maximum", got.Sessions)
	}
}
