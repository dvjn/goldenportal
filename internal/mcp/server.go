package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
)

const maxResponseBytes = 10 << 20

const instructions = `Golden Portal is a configurable catalog. Read portal://authoring before creating configuration. Use list_connectors to discover resource types. Configure blueprints before integrations, scorecards and dashboards. Validate changes, save drafts and preview impact before publishing. Bulk apply saves drafts; publish_documents publishes them in dependency order. Never invent built-in blueprints. All tools use the caller's existing API permissions; member and read-only tokens cannot author configuration.`

func Middleware(api http.Handler, baseURL string) func(http.Handler) http.Handler {
	transport := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		p, _ := auth.FromContext(r.Context())
		return newServer(bridge{api: api, request: r}, p, p.Can(auth.RoleBuilder) && !auth.ReadOnly(r.Context()))
	}, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 2 << 20,
		// The SDK's DNS-rebinding guard rejects a public Host arriving on
		// loopback, which is every request through a reverse proxy on the same
		// host. Rebinding gains nothing here: every call needs the caller's
		// credentials, and the Origin check below covers browsers.
		DisableLocalhostProtection: true,
	})
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != auth.MCPPath {
				next.ServeHTTP(w, r)
				return
			}
			if _, ok := auth.FromContext(r.Context()); !ok {
				httpx.WriteError(w, 401, "sign in to continue")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				configured, _ := url.Parse(baseURL)
				allowed := err == nil && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Scheme == "http" || u.Scheme == "https") && (u.Path == "" || u.Path == "/")
				// The configured base URL is the public origin; Host may have been
				// rewritten by a proxy, so it's only the fallback.
				if configured != nil && configured.Host != "" {
					allowed = allowed && u.Scheme == configured.Scheme && strings.EqualFold(u.Host, configured.Host)
				} else {
					allowed = allowed && strings.EqualFold(u.Host, r.Host)
				}
				if !allowed {
					httpx.WriteError(w, 403, "MCP origin is not allowed")
					return
				}
			}
			transport.ServeHTTP(w, r)
		})
	}
}

type (
	bridge struct {
		api     http.Handler
		request *http.Request
	}
	apiCall struct {
		method, path string
		query        url.Values
		body         any
	}
	Output struct {
		Status int    `json:"status"`
		Data   any    `json:"data,omitempty"`
		Error  any    `json:"error,omitempty"`
		Source string `json:"source,omitempty"`
	}
)

func (b bridge) call(ctx context.Context, c apiCall) (*mcp.CallToolResult, Output, error) {
	var body bytes.Buffer
	if c.body != nil {
		if err := json.NewEncoder(&body).Encode(c.body); err != nil {
			return nil, Output{}, err
		}
	}
	u := &url.URL{Scheme: "http", Host: b.request.Host, Path: c.path, RawQuery: c.query.Encode()}
	r, err := http.NewRequestWithContext(ctx, c.method, u.String(), &body)
	if err != nil {
		return nil, Output{}, err
	}
	for _, name := range []string{"Authorization", "Cookie", "X-GP-CSRF"} {
		if v := b.request.Header.Get(name); v != "" {
			r.Header.Set(name, v)
		}
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	w := &capture{header: http.Header{}}
	b.api.ServeHTTP(w, r)
	if w.status == 0 {
		w.status = 200
	}
	out := Output{Status: w.status}
	if w.tooLarge {
		out.Status = 413
		out.Error = map[string]any{"message": "response exceeds 10 MB; use a smaller page or a single-document export"}
	} else if strings.Contains(w.header.Get("Content-Type"), "yaml") {
		out.Source = w.body.String()
	} else if w.body.Len() > 0 {
		var envelope map[string]any
		if err := json.Unmarshal(w.body.Bytes(), &envelope); err != nil {
			out.Status = 502
			out.Error = map[string]any{"message": "API returned an unexpected response"}
		} else {
			out.Data = envelope["data"]
			out.Error = envelope["error"]
		}
	}
	if out.Status >= 400 {
		if out.Error == nil {
			out.Error = map[string]any{"message": http.StatusText(out.Status)}
		}
		raw, _ := json.Marshal(out)
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}, StructuredContent: out}, out, nil
	}
	return nil, out, nil
}

type capture struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	tooLarge bool
}

func (c *capture) Header() http.Header { return c.header }
func (c *capture) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *capture) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = 200
	}
	if c.body.Len()+len(p) > maxResponseBytes {
		c.tooLarge = true
		return len(p), nil
	}
	if c.tooLarge {
		return len(p), nil
	}
	return c.body.Write(p)
}

func addTool[Args any](s *mcp.Server, b bridge, name, description string, readOnly, destructive bool, build func(Args) (apiCall, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: &destructive, OpenWorldHint: boolPointer(name == "sync_integration")}}, func(ctx context.Context, _ *mcp.CallToolRequest, a Args) (*mcp.CallToolResult, Output, error) {
		call, err := build(a)
		if err != nil {
			return nil, Output{}, err
		}
		return b.call(ctx, call)
	})
}

func boolPointer(v bool) *bool { return &v }
func documentPath(kind, name string) (string, error) {
	switch kind {
	case "blueprints", "integrations", "scorecards", "dashboards":
	default:
		return "", fmt.Errorf("kind must be blueprints, integrations, scorecards or dashboards")
	}
	if name == "" {
		return "/api/v1/" + kind, nil
	}
	if !configdoc.ValidIdentifier(name) {
		return "", fmt.Errorf("name must be a document identifier")
	}
	return "/api/v1/" + kind + "/" + name, nil
}
