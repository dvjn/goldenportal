package auth

import (
	"net/http"
	"strings"

	"github.com/dvjn/goldport/internal/httpx"
	gofrHTTP "gofr.dev/pkg/gofr/http"
)

// CrossOriginProtection refuses unsafe cross-site browser requests that rely
// on the session cookie. Requests with a bearer token are exempt: a browser
// sends an Authorization header cross-origin only after a CORS preflight,
// which the portal never allows, so only machines send them. SCIM is one.
func CrossOriginProtection() gofrHTTP.Middleware {
	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusForbidden, "cross-origin request refused")
	}))

	return func(next http.Handler) http.Handler {
		protected := protection.Handler(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := bearerToken(r); ok {
				next.ServeHTTP(w, r)
				return
			}
			protected.ServeHTTP(w, r)
		})
	}
}

func bearerToken(r *http.Request) (string, bool) {
	return strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
}
