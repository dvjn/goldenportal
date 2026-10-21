package auth

import (
	"net/http"
	"testing"
	"time"
)

func TestCookieNamesNormalizeTheAppName(t *testing.T) {
	cases := map[string]string{
		"Acme Portal":  "acme_portal",
		"GoldPort":     "goldport",
		"goldport-dev": "goldport_dev",
		"":             "app",
		"--x--":        "x",
	}
	for appName, want := range cases {
		got := CookieNames(appName)
		if got.Session != "__Host-"+want+"_session" || got.Login != "__Host-"+want+"_login" {
			t.Errorf("CookieNames(%q) = %+v, want the %q prefix", appName, got, want)
		}
	}
}

func TestCookiesAreHostOnlySecureAndLax(t *testing.T) {
	cookie := newCookie("__Host-app_session", "token", 10*time.Hour)

	want := "__Host-app_session=token; Path=/; Max-Age=36000; HttpOnly; Secure; SameSite=Lax"
	if got := cookie.String(); got != want {
		t.Fatalf("cookie = %q, want %q", got, want)
	}
	if got := expiredCookie("__Host-app_session").String(); got != "__Host-app_session=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax" {
		t.Fatalf("expired cookie = %q", got)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want Lax", cookie.SameSite)
	}
}
