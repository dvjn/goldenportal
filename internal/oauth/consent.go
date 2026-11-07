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
	scopes      []string
	// withheld are scopes the client asked for that the user's role can't
	// grant.
	withheld []string
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
	case req.Resource != "" && !s.sameResource(req.Resource):
		c.problem = &problem{"invalid_target", "tokens are only issued for " + s.Resource()}
	}
	if c.problem != nil {
		return c, nil
	}

	// Without a scope, the client asks for all it can have, which the
	// user's role then narrows.
	requested := strings.Fields(req.Scope)
	if len(requested) == 0 {
		requested = Scopes
	}
	allowed := RoleScopes(p.Role)
	for _, scope := range requested {
		switch {
		case !slices.Contains(Scopes, scope):
			c.problem = &problem{"invalid_scope", "unknown scope " + scope}
			return c, nil
		case slices.Contains(allowed, scope):
			c.scopes = append(c.scopes, scope)
		default:
			c.withheld = append(c.withheld, scope)
		}
	}
	if len(c.scopes) == 0 {
		c.problem = &problem{"invalid_scope", "the user's role can't grant any of the scopes asked for"}
	}

	return c, nil
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
	RedirectURI  string   `json:"redirectUri"`
	RedirectHost string   `json:"redirectHost"`
	Local        bool     `json:"local"`
	Scopes       []string `json:"scopes"`
	Withheld     []string `json:"withheld,omitempty"`
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
		Client: c.client.Name, Logo: c.client.Logo, RedirectURI: c.redirectURI, RedirectHost: u.Host, Local: isLoopback(u.Hostname()),
		Scopes: c.scopes, Withheld: c.withheld,
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

	// The only resource there is, so the code is bound to its canonical URL
	// however the client spelled it.
	code := codePrefix + newSecret()
	grantID, err := NewStore(ctx.SQL).Authorize(ctx, p.ID, c.client, Code{
		RedirectURI: c.redirectURI, Challenge: body.CodeChallenge, Resource: s.Resource(), Scopes: c.scopes,
	}, hashSecret(code))
	if err != nil {
		return nil, err
	}
	auth.Record(ctx, p, audit.AppAuthorize, audit.Target{Type: "app", ID: grantID, Label: c.client.Name},
		map[string]any{"scopes": c.scopes, "redirectUri": c.redirectURI})

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
