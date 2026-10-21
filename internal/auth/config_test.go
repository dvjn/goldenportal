package auth

import (
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

func TestLoadConfigAppliesDefaults(t *testing.T) {
	config, err := LoadConfig(mapConfig{})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if config.SessionMaxAge != 12*time.Hour || config.SessionIdleTimeout != time.Hour {
		t.Fatalf("durations = %v, %v, want 12h, 1h", config.SessionMaxAge, config.SessionIdleTimeout)
	}
	if config.Cookies.Session != "__Host-goldport_session" {
		t.Fatalf("session cookie = %q", config.Cookies.Session)
	}
}

func TestLoadConfigReadsOverrides(t *testing.T) {
	config, err := LoadConfig(mapConfig{
		"GOLDPORT_SESSION_MAX_AGE":      "10h",
		"GOLDPORT_SESSION_IDLE_TIMEOUT": "45m",
		"APP_NAME":                      "goldport-dev",
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if config.SessionMaxAge != 10*time.Hour || config.SessionIdleTimeout != 45*time.Minute {
		t.Fatalf("durations = %v, %v, want 10h, 45m", config.SessionMaxAge, config.SessionIdleTimeout)
	}
	if config.Cookies.Login != "__Host-goldport_dev_login" {
		t.Fatalf("login cookie = %q", config.Cookies.Login)
	}
}

func TestLoadConfigRejectsBadDurations(t *testing.T) {
	for _, value := range []string{"soon", "0s", "-1h"} {
		if _, err := LoadConfig(mapConfig{"GOLDPORT_SESSION_IDLE_TIMEOUT": value}); err == nil {
			t.Errorf("LoadConfig(idle %q) error = nil, want an error", value)
		}
	}
}
