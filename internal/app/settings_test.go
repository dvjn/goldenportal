package app

import (
	"slices"
	"testing"

	"gofr.dev/pkg/gofr/config"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/scim"
)

func TestDescribeConfig(t *testing.T) {
	cfg := config.NewMockConfig(map[string]string{
		"GP_NAME":               "Acme Portal",
		"GP_LOGO_URL":           "https://cdn.example.com/logo.svg",
		"GP_OIDC_ISSUER":        "https://idp.example.com",
		"GP_OIDC_CLIENT_ID":     "portal",
		"GP_OIDC_CLIENT_SECRET": "super-secret",
		"DB_PASSWORD":           "",
	})
	oidc := auth.NewOIDC(auth.OIDCConfig{
		Issuer: "https://idp.example.com", ClientID: "portal", BaseURL: "https://portal.example.com/",
	}, logging.NewMockLogger(logging.ERROR))

	scimServer := scim.New("a-long-enough-scim-token-for-the-test", "https://portal.example.com", logging.NewMockLogger(logging.ERROR))

	got := describeConfig(cfg, oidc, scimServer)

	if got.Portal.Name != "Acme Portal" {
		t.Errorf("portal name = %q, want the configured name", got.Portal.Name)
	}
	if got.Portal.FaviconURL != "https://cdn.example.com/logo.svg" || !got.Portal.FaviconIsLogo {
		t.Errorf("favicon = %q (is logo %v), want the logo", got.Portal.FaviconURL, got.Portal.FaviconIsLogo)
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
	if !got.SCIM.Enabled || got.SCIM.URL != "https://portal.example.com/scim/v2" {
		t.Errorf("scim = %+v, want on at the portal's /scim/v2", got.SCIM)
	}
	if got.Database.PasswordSet {
		t.Error("database password reported as set")
	}
	if !got.Sessions.SecureCookies || got.Sessions.MaxAgeHours != 168 {
		t.Errorf("sessions = %+v, want secure cookies and a 7-day maximum", got.Sessions)
	}
}
