package auth

import (
	"maps"
	"strings"
	"testing"
	"time"
)

type mapConfig map[string]string

func (m mapConfig) Get(key string) string { return m[key] }

func (m mapConfig) GetOrDefault(key, fallback string) string {
	if value, ok := m[key]; ok {
		return value
	}

	return fallback
}

func requiredSettings() mapConfig {
	return mapConfig{
		"GOLDPORT_PUBLIC_URL":      "https://portal.example.test/",
		"GOLDPORT_OIDC_ISSUER_URL": "https://login.example.test/",
		"GOLDPORT_OIDC_CLIENT_ID":  "portal",
	}
}

func TestLoadConfigAppliesDefaults(t *testing.T) {
	config, err := LoadConfig(requiredSettings())
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if config.PublicURL != "https://portal.example.test" || config.ClientSecret != "" {
		t.Fatalf("public URL = %q, client secret = %q", config.PublicURL, config.ClientSecret)
	}
	if config.SessionMaxAge != 12*time.Hour || config.SessionIdleTimeout != time.Hour {
		t.Fatalf("durations = %v, %v, want 12h, 1h", config.SessionMaxAge, config.SessionIdleTimeout)
	}
	if config.Cookies.Session != "__Host-goldport_session" {
		t.Fatalf("session cookie = %q", config.Cookies.Session)
	}
	if len(config.BootstrapAdmins) != 0 {
		t.Fatalf("bootstrap admins = %v, want none", config.BootstrapAdmins)
	}
}

func TestLoadConfigReadsOverrides(t *testing.T) {
	source := requiredSettings()
	maps.Copy(source, mapConfig{
		"GOLDPORT_OIDC_CLIENT_SECRET":   "secret",
		"GOLDPORT_SESSION_MAX_AGE":      "10h",
		"GOLDPORT_SESSION_IDLE_TIMEOUT": "45m",
		"APP_NAME":                      "goldport-dev",
		"GOLDPORT_BOOTSTRAP_ADMINS":     "Alice@example.test",
	})

	config, err := LoadConfig(source)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if config.ClientSecret != "secret" {
		t.Fatalf("client secret = %q", config.ClientSecret)
	}
	if config.SessionMaxAge != 10*time.Hour || config.SessionIdleTimeout != 45*time.Minute {
		t.Fatalf("durations = %v, %v, want 10h, 45m", config.SessionMaxAge, config.SessionIdleTimeout)
	}
	if config.Cookies.Login != "__Host-goldport_dev_login" {
		t.Fatalf("login cookie = %q", config.Cookies.Login)
	}
	if !config.BootstrapAdmins.Contains("alice@example.test") {
		t.Fatalf("bootstrap admins = %v, want alice", config.BootstrapAdmins)
	}
}

func TestLoadConfigNamesEveryMissingSetting(t *testing.T) {
	_, err := LoadConfig(mapConfig{"GOLDPORT_OIDC_CLIENT_ID": "portal"})

	want := "GOLDPORT_PUBLIC_URL, GOLDPORT_OIDC_ISSUER_URL"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("LoadConfig() error = %v, want it to name %s", err, want)
	}
}

func TestLoadConfigRejectsBadDurations(t *testing.T) {
	for _, value := range []string{"soon", "0s", "-1h"} {
		source := requiredSettings()
		source["GOLDPORT_SESSION_IDLE_TIMEOUT"] = value
		if _, err := LoadConfig(source); err == nil {
			t.Errorf("LoadConfig(idle %q) error = nil, want an error", value)
		}
	}
}
