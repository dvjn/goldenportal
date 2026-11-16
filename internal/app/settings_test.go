package app

import (
	"slices"
	"strings"
	"testing"

	"gofr.dev/pkg/gofr/config"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/index"
	"github.com/dvjn/goldenportal/internal/scim"
	"github.com/dvjn/goldenportal/internal/scorecard"
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
	if got.OIDC.RedirectURL != "https://portal.example.com/api/web/auth/callback" {
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
	if got.Server.ScorecardSchedule != "0 * * * *" {
		t.Errorf("scorecard schedule = %q, want hourly by default", got.Server.ScorecardSchedule)
	}
}

func TestDescribeConfigScorecardSchedule(t *testing.T) {
	cfg := config.NewMockConfig(map[string]string{scorecard.EvaluationScheduleEnv: "30 2 * * *"})
	oidc := auth.NewOIDC(auth.OIDCConfig{}, logging.NewMockLogger(logging.ERROR))
	scimServer := scim.New("", "", logging.NewMockLogger(logging.ERROR))

	if got := describeConfig(cfg, oidc, scimServer).Server.ScorecardSchedule; got != "30 2 * * *" {
		t.Errorf("scorecard schedule = %q, want the configured one", got)
	}
}

func TestScheduleSetting(t *testing.T) {
	for _, env := range []string{scorecard.EvaluationScheduleEnv, entity.RetentionScheduleEnv, index.ScheduleEnv} {
		got, err := scheduleSetting(config.NewMockConfig(nil), env, "0 * * * *")
		if err != nil || got != "0 * * * *" {
			t.Errorf("%s unset = %q, %v; want the default", env, got, err)
		}
		got, err = scheduleSetting(config.NewMockConfig(map[string]string{env: "*/15 * * * *"}), env, "0 * * * *")
		if err != nil || got != "*/15 * * * *" {
			t.Errorf("%s set = %q, %v; want the setting", env, got, err)
		}
		_, err = scheduleSetting(config.NewMockConfig(map[string]string{env: "every hour"}), env, "0 * * * *")
		if err == nil || !strings.Contains(err.Error(), env) {
			t.Errorf("%s invalid: err = %v, want one naming the setting", env, err)
		}
	}
}
