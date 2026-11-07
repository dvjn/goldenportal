// Package mcp serves the portal's MCP server at /mcp. Clients sign in with
// the portal's own OAuth (see package oauth), and each tool calls the
// portal's services as the signed-in user, as the web API does, so their
// permission checks apply.
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

// Instructions tell the agent how to use the portal. Everyone reads the
// catalog; authoring is added only for callers who may author.
const (
	readingInstructions = `Golden Portal is the organization's software catalog. Blueprints define kinds of things, such as services, teams or repositories; entities are the things themselves, kept up to date by integrations. Scorecards grade entities against standards in tiers, and dashboards summarize them.

To answer a question, find what it's about with search, then read it: get_entity for one entity with its relations, query_entities to filter, sort and page a blueprint's entities, get_entity_scorecards for how one entity fares, get_scorecard_summary and list_scorecard_results for a scorecard across entities, and get_dashboard_data for a dashboard's numbers. Use get_home for the dashboards the user sees first. Answer from the portal's data, name the entities you mean by title and identifier, and say when the catalog doesn't hold something rather than guessing.

Actions do things to one entity, such as restarting a service. list_entity_actions shows what the user can run on an entity and the inputs each takes. Run one with run_action only when the user asks, then follow it with get_run until it finishes and report what it gave. list_self_service shows the Self-Service catalog, whose workflows run_workflow starts the same way. A failed run can be retried with retry_run when the user asks. list_approvals shows runs waiting on the user's approval; tell them, since only they can approve, in the portal.`
	authoringInstructions = `

You may also change configuration. Read portal://guide/authoring and the kind's schema before writing documents, and list_connectors for what integrations can read. Configure blueprints before the integrations, scorecards and dashboards that use them, and never invent blueprints that don't exist. Validate, save drafts and preview impact, then publish only when the user agrees. Bulk apply saves drafts; publish_documents publishes them in dependency order.`
)

// instructions are what the caller's role and token let them do.
func instructions(c caller) string {
	for _, t := range tools {
		if t.scope == oauth.ScopeAuthor && c.allows(t) {
			return readingInstructions + authoringInstructions
		}
	}
	return readingInstructions
}

// Tokens finds who an access token signs in, with the scopes it carries.
type Tokens interface {
	AccessPrincipal(ctx context.Context, token, resource string) (auth.Principal, []string, error)
}

// Middleware serves the MCP endpoint. A request needs an access token the
// portal issued for /mcp: anything else is told where to get one. Tools call
// services as the token's user, in contexts newContext makes; version is the
// release clients see.
func Middleware(services Services, newContext func(context.Context) *gofr.Context, tokens Tokens, server oauth.Server, version string, limits *Limiter) func(http.Handler) http.Handler {
	transport := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		c, _ := callerFrom(r.Context())
		return newServer(bridge{services: services, newContext: newContext, caller: c}, c, version)
	}, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: maxRequestBytes,
		// The SDK's DNS-rebinding guard rejects a public Host arriving on
		// loopback, which is every request through a reverse proxy on the same
		// host. Rebinding gains nothing here: every call needs a token, and the
		// Origin check below covers browsers.
		DisableLocalhostProtection: true,
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != oauth.MCPPath {
				next.ServeHTTP(w, r)
				return
			}
			if !allowedOrigin(r, server.Issuer()) {
				httpx.WriteError(w, http.StatusForbidden, "MCP origin is not allowed")
				return
			}
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || token == "" {
				challenge(w, server, http.StatusUnauthorized, "", "sign in to continue")
				return
			}
			p, scopes, err := tokens.AccessPrincipal(r.Context(), token, server.Resource())
			if errors.Is(err, oauth.ErrInvalidToken) {
				challenge(w, server, http.StatusUnauthorized, "invalid_token", err.Error())
				return
			}
			if err != nil {
				httpx.WriteError(w, http.StatusInternalServerError, "could not check the access token")
				return
			}
			if ok, wait := limits.allow(p.ID); !ok {
				tooManyRequests(w, wait)
				return
			}
			c := caller{principal: p, scopes: scopes}
			if need := c.missingScope(r); need != "" {
				// The role allows the tool but the token wasn't granted it, so
				// the client can ask the user for more, as step-up authorization
				// in the MCP spec describes.
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(
					`Bearer error="insufficient_scope", scope=%q, resource_metadata=%q, error_description=%q`,
					strings.Join(append(slices.Clone(scopes), need), " "), server.ResourceMetadataURL(),
					"this tool needs "+need))
				httpx.WriteError(w, http.StatusForbidden, "this tool needs the "+need+" scope")
				return
			}
			transport.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, c)))
		})
	}
}

// challenge answers a request without a usable token with where to get
// one, and the scopes the server offers.
func challenge(w http.ResponseWriter, server oauth.Server, status int, code, message string) {
	params := []string{
		fmt.Sprintf("resource_metadata=%q", server.ResourceMetadataURL()),
		fmt.Sprintf("scope=%q", strings.Join(oauth.Scopes, " ")),
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

// allows reports whether the caller may use t: their role now includes
// the tool's, and their token was granted its scope.
func (c caller) allows(t tool) bool {
	return c.principal.Can(t.role) && slices.Contains(c.scopes, t.scope)
}

// missingScope is the scope a tools/call request needs that the token
// lacks, when the caller's role would allow the tool. Anything else, a
// tool their role doesn't allow included, goes to the server, which
// doesn't offer it.
func (c caller) missingScope(r *http.Request) string {
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
	for _, t := range tools {
		if t.name == message.Params.Name && c.principal.Can(t.role) && !slices.Contains(c.scopes, t.scope) {
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
		var status interface{ StatusCode() int }
		if errors.As(err, &status) {
			return Output{Status: status.StatusCode(), Error: map[string]any{"message": err.Error()}}
		}
		gctx.Errorf("MCP call failed: %v", err)
		return Output{Status: http.StatusInternalServerError, Error: map[string]any{"message": "something went wrong in the portal"}}
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

// documents is the service for a kind of document, by its plural name.
func (b bridge) documents(kind string) (configdoc.Documents, error) {
	if d, ok := b.services.Documents(kind); ok {
		return d, nil
	}
	return nil, httpx.BadRequest("kind must be blueprints, integrations, scorecards, dashboards, actions or workflows")
}
