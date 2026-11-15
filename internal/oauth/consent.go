package oauth

import (
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/audit"
	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// Register serves the consent the browser's authorize page asks for, and
// the apps a user has authorized, which they can revoke. These are API
// calls the signed-in user makes in person, with their session.
func (s Server) Register(app httpx.Routes) {
	app.GET("oauth/authorize", s.describe)
	app.POST("oauth/authorize", s.decide)
	app.GET("account/apps", listGrants)
	app.DELETE("account/apps/{id}", revokeGrant)
}

// authorizeRequest is an authorization request as the client sent it to
// the authorize page.
type authorizeRequest struct {
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	ResponseType        string `json:"response_type"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	Scope               string `json:"scope"`
	Resource            string `json:"resource"`
	State               string `json:"state"`
}

// checked is an authorization request found sound: who asks, where the
// answer goes and what can be granted.
type checked struct {
	client      Client
	redirectURI string
	endpoint    Endpoint
	scopes      []string
	// problem, when set, is why the request can't be granted, sent back to
	// the client at redirectURI, since that's known to be the client's.
	problem *problem
}

type problem struct{ code, description string }

// check validates a request. An unknown client or redirect URI is an
// error shown to the user, never followed: the answer could go anywhere.
func (s Server) check(ctx *gofr.Context, p auth.Principal, req authorizeRequest) (checked, error) {
	client, err := s.client(ctx, NewStore(ctx.SQL), req.ClientID)
	var unusable metadataError
	switch {
	case errors.Is(err, errNotFound):
		return checked{}, httpx.BadRequest("the app asking for access isn't registered")
	case errors.As(err, &unusable):
		return checked{}, httpx.BadRequest(unusable.Error())
	case err != nil:
		return checked{}, err
	}
	c := checked{client: client, redirectURI: req.RedirectURI}
	if c.redirectURI == "" && len(client.RedirectURIs) == 1 {
		c.redirectURI = client.RedirectURIs[0]
	}
	if !redirectAllowed(client.RedirectURIs, c.redirectURI) {
		return checked{}, httpx.BadRequest("the app's redirect URI doesn't match any it registered")
	}

	switch {
	case req.ResponseType != "code":
		c.problem = &problem{"unsupported_response_type", "only response_type=code is supported"}
	case req.CodeChallenge == "" || req.CodeChallengeMethod != "S256":
		c.problem = &problem{"invalid_request", "PKCE with code_challenge_method=S256 is required"}
	default:
		c.endpoint, c.scopes, c.problem = s.grantable(p, req.Resource, req.Scope)
	}

	return c, nil
}

// grantable is the endpoint a request names and the scopes it asks for
// there, or why the user can't grant them.
func (s Server) grantable(p auth.Principal, resource, scope string) (Endpoint, []string, *problem) {
	e, known := s.endpointFor(resource)
	requested := strings.Fields(scope)
	switch {
	case !known:
		return e, nil, &problem{"invalid_target", s.onlyFor()}
	case resource == "" && slices.Contains(requested, ScopeAuthor):
		// Issuing for the user's server would quietly drop what was asked for.
		return e, nil, &problem{"invalid_target", ScopeAuthor + " is offered at " + s.Resource(BuilderMCP) + "; ask for that resource"}
	case !p.Can(e.Role):
		return e, nil, &problem{"access_denied", s.Refusal(e)}
	}

	// Without a scope, the client asks for all the endpoint offers. Scopes
	// another endpoint takes are left out, since a client may ask for every
	// scope the portal issues.
	if len(requested) == 0 {
		requested = e.Scopes
	}
	var scopes []string
	for _, scope := range requested {
		switch {
		case !slices.Contains(Scopes, scope):
			return e, nil, &problem{"invalid_scope", "unknown scope " + scope}
		case slices.Contains(e.Scopes, scope):
			scopes = append(scopes, scope)
		}
	}
	if len(scopes) == 0 {
		return e, nil, &problem{"invalid_scope", "none of the scopes asked for are offered at " + s.Resource(e)}
	}

	return e, scopes, nil
}

// consentView is what the authorize page shows: the app, where it sends
// the answer, and what it would be allowed. Problem, when set, is why it
// can't be, with Redirect to tell the app.
type consentView struct {
	Client string `json:"client"`
	// ClientHost is the domain that published the app's metadata, when it
	// did; an app that registered itself has only the name it gave.
	ClientHost string `json:"clientHost,omitempty"`
	// Logo is the app's logo, from its metadata, as a PNG data URL.
	Logo         string   `json:"logo,omitempty"`
	Server       string   `json:"server"`
	RedirectURI  string   `json:"redirectUri"`
	RedirectHost string   `json:"redirectHost"`
	Local        bool     `json:"local"`
	Scopes       []string `json:"scopes"`
	Problem      string   `json:"problem,omitempty"`
	Redirect     string   `json:"redirect,omitempty"`
}

func (s Server) describe(ctx *gofr.Context) (any, error) {
	p, err := s.consenter(ctx)
	if err != nil {
		return nil, err
	}
	q := httpx.Query(ctx)
	req := authorizeRequest{
		ClientID: q.Get("client_id"), RedirectURI: q.Get("redirect_uri"), ResponseType: q.Get("response_type"),
		CodeChallenge: q.Get("code_challenge"), CodeChallengeMethod: q.Get("code_challenge_method"),
		Scope: q.Get("scope"), Resource: q.Get("resource"), State: q.Get("state"),
	}
	c, err := s.check(ctx, p, req)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(c.redirectURI)
	view := consentView{
		Client: c.client.Name, Logo: c.client.Logo, Server: c.endpoint.Title,
		RedirectURI: c.redirectURI, RedirectHost: u.Host, Local: isLoopback(u.Hostname()),
		Scopes: c.scopes,
	}
	if isMetadataClient(c.client.ClientID) {
		clientURL, _ := url.Parse(c.client.ClientID)
		view.ClientHost = clientURL.Host
	}
	if view.RedirectHost == "" {
		// An app's own scheme has no host; its scheme is what names it.
		view.RedirectHost = u.Scheme + ":"
	}
	if c.problem != nil {
		view.Problem = c.problem.description
		view.Redirect = s.answer(c.redirectURI, req.State, map[string]string{
			"error": c.problem.code, "error_description": c.problem.description,
		})
	}

	return view, nil
}

// decide answers the request: with a code when the user approves, and
// access_denied when they don't. Either way the browser goes back to the
// app with the answer.
func (s Server) decide(ctx *gofr.Context) (any, error) {
	p, err := s.consenter(ctx)
	if err != nil {
		return nil, err
	}
	var body struct {
		authorizeRequest
		Approve bool `json:"approve"`
	}
	if err := ctx.Bind(&body); err != nil {
		return nil, httpx.BadRequest("the body must be a JSON object")
	}
	c, err := s.check(ctx, p, body.authorizeRequest)
	if err != nil {
		return nil, err
	}
	switch {
	case c.problem != nil:
		return map[string]string{"redirect": s.answer(c.redirectURI, body.State, map[string]string{
			"error": c.problem.code, "error_description": c.problem.description,
		})}, nil
	case !body.Approve:
		return map[string]string{"redirect": s.answer(c.redirectURI, body.State, map[string]string{
			"error": "access_denied", "error_description": "the user declined",
		})}, nil
	}

	// The code is bound to the endpoint's canonical URL however the client
	// spelled it.
	code := codePrefix + newSecret()
	grantID, err := NewStore(ctx.SQL).Authorize(ctx, p.ID, c.client, c.endpoint, Code{
		RedirectURI: c.redirectURI, Challenge: body.CodeChallenge, Resource: s.Resource(c.endpoint), Scopes: c.scopes,
	}, hashSecret(code))
	if err != nil {
		return nil, err
	}
	auth.Record(ctx, p, audit.AppAuthorize, audit.Target{Type: "app", ID: grantID, Label: c.client.Name},
		map[string]any{"scopes": c.scopes, "redirectUri": c.redirectURI, "endpoint": c.endpoint.Path})

	return map[string]string{"redirect": s.answer(c.redirectURI, body.State, map[string]string{"code": code})}, nil
}

// consenter is the signed-in user, who must be here in person, with a
// browser session: a token can't consent on its owner's behalf.
func (s Server) consenter(ctx *gofr.Context) (auth.Principal, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return auth.Principal{}, err
	}
	if !auth.InSession(ctx) {
		return auth.Principal{}, httpx.Forbidden("authorize apps from the portal, signed in")
	}

	return p, nil
}

// answer builds the redirect back to the client, keeping its own query
// and naming the issuer, as RFC 9207 asks, so it knows who answered.
func (s Server) answer(redirectURI, state string, params map[string]string) string {
	u, _ := url.Parse(redirectURI)
	q := u.Query()
	for key, value := range params {
		q.Set(key, value)
	}
	if state != "" {
		q.Set("state", state)
	}
	q.Set("iss", s.baseURL)
	u.RawQuery = q.Encode()

	return u.String()
}

func listGrants(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return nil, err
	}

	return NewStore(ctx.SQL).Grants(ctx, p.ID)
}

func revokeGrant(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return nil, err
	}
	id := ctx.PathParam("id")
	if _, err := uuid.Parse(id); err != nil {
		return nil, httpx.NotFound("no such app")
	}
	name, err := NewStore(ctx.SQL).RevokeGrant(ctx, p.ID, id)
	if errors.Is(err, errNotFound) {
		return nil, httpx.NotFound("no such app")
	}
	if err != nil {
		return nil, err
	}
	auth.Record(ctx, p, audit.AppRevoke, audit.Target{Type: "app", ID: id, Label: name}, nil)

	return nil, nil
}
