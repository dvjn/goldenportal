package auth

import (
	"net/http"

	"github.com/dvjn/goldport/internal/httpx"
	gofrHTTP "gofr.dev/pkg/gofr/http"
)

// CrossOriginProtection refuses unsafe cross-site browser requests. SCIM is
// exempt: Authentik calls it server to server with a bearer token.
func CrossOriginProtection() gofrHTTP.Middleware {
	protection := http.NewCrossOriginProtection()
	protection.AddInsecureBypassPattern("/scim/")
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusForbidden, "cross-origin request refused")
	}))

	return protection.Handler
}
