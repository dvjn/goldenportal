package app

import (
	"net/http"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/telemetry"
)

type Config struct {
	Name    string `json:"name"`
	LogoURL string `json:"logoUrl,omitempty"`
	// Tracing is set when the server takes the UI's spans, which the UI
	// otherwise neither records nor loads the code for.
	Tracing *TracingConfig `json:"tracing,omitempty"`
	// MCP is where agents connect, once the portal knows its public URL.
	MCP *MCPConfig `json:"mcp,omitempty"`
}

// MCPConfig names the MCP servers' URLs: the user's, which reads the
// portal's pages and runs actions, and the builder's, which changes
// configuration.
type MCPConfig struct {
	User    string `json:"user"`
	Builder string `json:"builder"`
}

type TracingConfig struct {
	SampleRatio float64 `json:"sampleRatio"`
}

func configHandler(config Config) gofr.Handler {
	return func(*gofr.Context) (any, error) {
		return config, nil
	}
}

// browserTracesPath is where the UI sends its spans.
const browserTracesPath = httpx.WebAPI + "traces"

// browserTraces serves the UI's spans to relay, behind sign-in, or nothing
// when relay is nil.
func browserTraces(relay *telemetry.Relay) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if relay == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == browserTracesPath && r.Method == http.MethodPost {
				// Spans come from browsers, so a token can't send them.
				p, ok := auth.FromContext(r.Context())
				if !ok || !auth.InSession(r.Context()) {
					httpx.WriteError(w, http.StatusUnauthorized, "sign in to send spans")
					return
				}
				relay.Serve(w, r, p.ID)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
