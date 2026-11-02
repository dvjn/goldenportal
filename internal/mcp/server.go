// Package mcp serves the portal's MCP server at /mcp. Clients sign in with
// the portal's own OAuth (see package oauth), and each tool runs an API
// call as the signed-in user, so the API's own permission checks apply.
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

To answer a question, find what it's about with search, then read it: get_entity for one entity with its relations, query_entities to filter, sort and page a blueprint's entities, get_entity_scorecards for how one entity fares, get_scorecard_summary and list_scorecard_results for a scorecard across entities, and get_dashboard_data for a dashboard's numbers. Use get_home for the dashboards the user sees first. Answer from the portal's data, name the entities you mean by title and identifier, and say when the catalog doesn't hold something rather than guessing.`
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
// portal issued for /mcp: anything else is told where to get one. api is
// the API, which tool calls run against as the token's user; version is the
// release clients see.
func Middleware(api http.Handler, tokens Tokens, server oauth.Server, version string, limits *Limiter) func(http.Handler) http.Handler {
	transport := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		c, _ := callerFrom(r.Context())
		return newServer(bridge{api: api, caller: c}, c, version)
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
	// bridge runs a tool's API call in-process, as the caller.
	bridge struct {
		api    http.Handler
		caller caller
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

// call runs c against the API signed in as the caller. Their token is
// never passed on: the API sees the user, as the MCP spec asks of servers
// calling further services.
func (b bridge) call(ctx context.Context, t tool, c apiCall) (*mcp.CallToolResult, Output, error) {
	// A tool that only reads can only make reads, whatever its arguments.
	if t.readOnly && c.method != http.MethodGet {
		return nil, Output{}, fmt.Errorf("%s only reads", t.name)
	}
	out, err := b.fetch(ctx, c)
	if err != nil {
		return nil, Output{}, err
	}
	if out.Status >= 400 {
		raw, _ := json.Marshal(out)
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}, StructuredContent: out}, out, nil
	}
	return nil, out, nil
}

// fetch runs c against the API as the caller.
func (b bridge) fetch(ctx context.Context, c apiCall) (Output, error) {
	var body bytes.Buffer
	if c.body != nil {
		if err := json.NewEncoder(&body).Encode(c.body); err != nil {
			return Output{}, err
		}
	}
	u := &url.URL{Path: c.path, RawQuery: c.query.Encode()}
	r, err := http.NewRequestWithContext(auth.WithPrincipal(ctx, b.caller.principal), c.method, u.String(), &body)
	if err != nil {
		return Output{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	w := &capture{header: http.Header{}}
	b.api.ServeHTTP(w, r)
	if w.status == 0 {
		w.status = http.StatusOK
	}
	out := Output{Status: w.status}
	switch {
	case w.tooLarge:
		out.Status = http.StatusRequestEntityTooLarge
		out.Error = map[string]any{"message": "response exceeds 10 MB; use a smaller page or a single-document export"}
	case strings.Contains(w.header.Get("Content-Type"), "yaml"):
		out.Source = w.body.String()
	case w.body.Len() > 0:
		var envelope map[string]any
		if err := json.Unmarshal(w.body.Bytes(), &envelope); err != nil {
			out.Status = http.StatusBadGateway
			out.Error = map[string]any{"message": "API returned an unexpected response"}
		} else {
			out.Data = envelope["data"]
			out.Error = envelope["error"]
		}
	}
	if out.Status >= 400 && out.Error == nil {
		out.Error = map[string]any{"message": http.StatusText(out.Status)}
	}
	return out, nil
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
		c.status = http.StatusOK
	}
	if c.tooLarge || c.body.Len()+len(p) > maxResponseBytes {
		c.tooLarge = true
		return len(p), nil
	}
	return c.body.Write(p)
}

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
