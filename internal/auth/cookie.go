package auth

import (
	"cmp"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Cookies struct {
	Session string
	Login   string
}

var cookieNameSeparators = regexp.MustCompile(`[^a-z0-9]+`)

// CookieNames derives the cookie names from the app name, so two portals on
// one host do not share cookies. The __Host- prefix makes browsers require
// Secure, Path=/ and no Domain.
func CookieNames(appName string) Cookies {
	name := strings.Trim(cookieNameSeparators.ReplaceAllString(strings.ToLower(appName), "_"), "_")
	name = cmp.Or(name, "app")

	return Cookies{
		Session: "__Host-" + name + "_session",
		Login:   "__Host-" + name + "_login",
	}
}

func newCookie(name, value string, maxAge time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

func expiredCookie(name string) *http.Cookie {
	cookie := newCookie(name, "", 0)
	cookie.MaxAge = -1

	return cookie
}
