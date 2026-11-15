// Package mcp serves the portal's MCP servers: the user's at /api/mcp, which
// reads the portal's pages and runs actions, and the builder's at
// /api/mcp/builder, which changes configuration. Clients sign in with the
// portal's own OAuth (see package oauth), and each tool calls the portal's
// services as the signed-in user, as the web API does, so their permission
// checks apply.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/oauth"
)

const (
	maxResponseBytes = 10 << 20
	maxRequestBytes  = 2 << 20
)

// server is one of the portal's MCP servers: the endpoint it answers at,
// what it tells agents it's for, and what it offers them.
type server struct {
	endpoint    oauth.Endpoint
	name, title string
	// documents is whether it serves configuration documents, which its
	// completions then suggest.
	documents    bool
	instructions string
	tools        []tool
	prompts      []prompt
	resources    func(*mcp.Server, bridge)
}

var servers = []server{userServer, builderServer}

// Tokens finds who an access token signs in, with the scopes it carries.
type Tokens interface {
	AccessPrincipal(ctx context.Context, token, resource string) (auth.Principal, []string, error)
}

// Middleware serves the MCP endpoints. A request needs an access token the
// portal issued for the endpoint: anything else is told where to get one.
// Tools call services as the token's user, in contexts newContext makes;
// version is the release clients see.
func Middleware(services Services, newContext func(context.Context) *gofr.Context, tokens Tokens, issuer oauth.Server, version string, limits *Limiter) func(http.Handler) http.Handler {
	handlers := map[string]http.Handler{}
	for _, s := range servers {
		handlers[s.endpoint.Path] = s.handler(services, newContext, tokens, issuer, version, limits)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if h, ok := handlers[r.URL.Path]; ok {
				h.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s server) handler(services Services, newContext func(context.Context) *gofr.Context, tokens Tokens, issuer oauth.Server, version string, limits *Limiter) http.Handler {
	transport := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		c, _ := callerFrom(r.Context())
		return s.newServer(bridge{services: services, newContext: newContext, caller: c}, c, version)
	}, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: maxRequestBytes,
		// The SDK's DNS-rebinding guard rejects a public Host arriving on
		// loopback, which is every request through a reverse proxy on the same
		// host. Rebinding gains nothing here: every call needs a token, and the
		// Origin check below covers browsers.
		DisableLocalhostProtection: true,
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedOrigin(r, issuer.Issuer()) {
			httpx.WriteError(w, http.StatusForbidden, "MCP origin is not allowed")
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			s.challenge(w, issuer, http.StatusUnauthorized, "", "sign in to continue")
			return
		}
		p, scopes, err := tokens.AccessPrincipal(r.Context(), token, issuer.Resource(s.endpoint))
		if errors.Is(err, oauth.ErrInvalidToken) {
			s.challenge(w, issuer, http.StatusUnauthorized, "invalid_token", err.Error())
			return
		}
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "could not check the access token")
			return
		}
		// The role is the user's now, so one who has lost it since consenting
		// is refused here too.
		if !p.Can(s.endpoint.Role) {
			httpx.WriteError(w, http.StatusForbidden, issuer.Refusal(s.endpoint))
			return
		}
		if ok, wait := limits.allow(p.ID); !ok {
			tooManyRequests(w, wait)
			return
		}
		c := caller{principal: p, scopes: scopes}
		if need := s.missingScope(c, r); need != "" {
			// The token wasn't granted what the tool needs, so the client can
			// ask the user for more, as step-up authorization in the MCP spec
			// describes.
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer error="insufficient_scope", scope=%q, resource_metadata=%q, error_description=%q`,
				strings.Join(append(slices.Clone(scopes), need), " "), issuer.ResourceMetadataURL(s.endpoint),
				"this tool needs "+need))
			httpx.WriteError(w, http.StatusForbidden, "this tool needs the "+need+" scope")
			return
		}
		transport.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, c)))
	})
}

// newServer offers the caller what their token lets them use, as of this
// request.
func (s server) newServer(b bridge, c caller, version string) *mcp.Server {
	m := mcp.NewServer(&mcp.Implementation{Name: s.name, Title: s.title, Version: version}, &mcp.ServerOptions{
		Instructions: s.instructions, CompletionHandler: complete(b, c, s.documents),
	})
	if c.granted(oauth.ScopeRead) {
		s.resources(m, b)
	}
	for _, p := range s.prompts {
		if c.granted(p.scope) {
			m.AddPrompt(p.prompt, p.handler(b))
		}
	}
	for _, t := range s.tools {
		if c.granted(t.scope) {
			t.register(m, b, t)
		}
	}

	return m
}

// challenge answers a request without a usable token with where to get
// one, and the scopes the endpoint offers.
func (s server) challenge(w http.ResponseWriter, issuer oauth.Server, status int, code, message string) {
	params := []string{
		fmt.Sprintf("resource_metadata=%q", issuer.ResourceMetadataURL(s.endpoint)),
		fmt.Sprintf("scope=%q", strings.Join(s.endpoint.Scopes, " ")),
	}
	if code != "" {
		params = append(params, fmt.Sprintf("error=%q", code), fmt.Sprintf("error_description=%q", message))
	}
	w.Header().Set("WWW-Authenticate", "Bearer "+strings.Join(params, ", "))
	httpx.WriteError(w, status, message)
}

// allowedOrigin lets browsers call only from the portal itself. Clients
// outside a browser send no Origin.
func allowedOrigin(r *http.Request, issuer string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	// The configured base URL is the public origin; Host may have been
	// rewritten by a proxy, so it's only the fallback.
	if configured, err := url.Parse(issuer); err == nil && configured.Host != "" {
		return u.Scheme == configured.Scheme && strings.EqualFold(u.Host, configured.Host)
	}
	return (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host)
}

// caller is who a request signs in as and what their token was granted.
type caller struct {
	principal auth.Principal
	scopes    []string
}

type callerKey struct{}

func callerFrom(ctx context.Context) (caller, bool) {
	c, ok := ctx.Value(callerKey{}).(caller)
	return c, ok
}

func (c caller) granted(scope string) bool { return slices.Contains(c.scopes, scope) }

// missingScope is the scope a tools/call request needs that the token
// lacks. Anything else, a tool the server doesn't have included, goes to
// the server, which answers it.
func (s server) missingScope(c caller, r *http.Request) string {
	if r.Method != http.MethodPost || r.Body == nil {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var message struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &message) != nil || message.Method != "tools/call" {
		return ""
	}
	for _, t := range s.tools {
		if t.name == message.Params.Name && !c.granted(t.scope) {
			return t.scope
		}
	}
	return ""
}

type (
	// bridge calls services as the caller.
	bridge struct {
		services   Services
		newContext func(context.Context) *gofr.Context
		caller     caller
	}
	Output struct {
		Status int    `json:"status"`
		Data   any    `json:"data,omitempty"`
		Error  any    `json:"error,omitempty"`
		Source string `json:"source,omitempty"`
	}
	// yaml is a document's source, which a tool returns as written.
	yaml string
)

// context signs ctx in as the caller. Their token is never passed on: the
// services see the user, as the MCP spec asks of servers calling further
// services.
func (b bridge) context(ctx context.Context) *gofr.Context {
	return b.newContext(auth.WithPrincipal(ctx, b.caller.principal))
}

// call runs do as the caller, and makes what it returns a tool result: a
// failure the caller can act on is an error result, not a protocol error.
func (b bridge) call(ctx context.Context, do func(*gofr.Context) (any, error)) (*mcp.CallToolResult, Output, error) {
	out := b.run(ctx, do)
	if out.Status >= 400 {
		raw, _ := json.Marshal(out)
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}, StructuredContent: out}, out, nil
	}
	return nil, out, nil
}

// run runs do as the caller and says how it went, as the web API would.
func (b bridge) run(ctx context.Context, do func(*gofr.Context) (any, error)) Output {
	gctx := b.context(ctx)
	v, err := do(gctx)
	if err != nil {
		status, message := failure(gctx, err)
		return Output{Status: status, Error: map[string]any{"message": message}}
	}
	out := Output{Status: http.StatusOK}
	if source, ok := v.(yaml); ok {
		out.Source = string(source)
	} else {
		out.Data = v
	}
	if raw, err := json.Marshal(out); err != nil || len(raw) > maxResponseBytes {
		return Output{Status: http.StatusRequestEntityTooLarge, Error: map[string]any{"message": "response exceeds 10 MB; use a smaller page or a single-document export"}}
	}
	return out
}

// failure is how a call failed, as far as the caller should know: a
// refusal they can act on, or else that the portal failed, which is logged.
func failure(ctx *gofr.Context, err error) (int, string) {
	var status interface{ StatusCode() int }
	if errors.As(err, &status) {
		return status.StatusCode(), err.Error()
	}
	ctx.Errorf("MCP call failed: %v", err)
	return http.StatusInternalServerError, "something went wrong in the portal"
}

// documents is the service for a kind of document, by its plural name.
func (b bridge) documents(kind string) (configdoc.Documents, error) {
	if d, ok := b.services.Documents(kind); ok {
		return d, nil
	}
	return nil, httpx.BadRequest("kind must be blueprints, connections, scorecards, dashboards, actions or workflows")
}
