package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Provider is the OpenID Connect identity provider that people sign in with.
type Provider struct {
	oauth      oauth2.Config
	verifier   *oidc.IDTokenVerifier
	endSession *url.URL
	publicURL  string
}

type identity struct {
	Subject string
	Email   string
	Name    string
	IDToken string
}

// NewProvider runs OIDC discovery against the issuer.
func NewProvider(ctx context.Context, config Config) (*Provider, error) {
	discovered, err := oidc.NewProvider(ctx, config.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover the OIDC provider at %s: %w", config.IssuerURL, err)
	}

	var metadata struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	if err := discovered.Claims(&metadata); err != nil {
		return nil, fmt.Errorf("read the OIDC provider metadata: %w", err)
	}
	endSession, err := parseEndSession(metadata.EndSessionEndpoint)
	if err != nil {
		return nil, err
	}

	return &Provider{
		oauth: oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			Endpoint:     discovered.Endpoint(),
			RedirectURL:  config.PublicURL + "/api/v1/auth/callback",
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier:   discovered.Verifier(&oidc.Config{ClientID: config.ClientID}),
		endSession: endSession,
		publicURL:  config.PublicURL,
	}, nil
}

// parseEndSession returns nil when the provider has no RP-initiated logout
// endpoint.
func parseEndSession(endpoint string) (*url.URL, error) {
	if endpoint == "" {
		return nil, nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse the OIDC end session endpoint: %w", err)
	}

	return parsed, nil
}

// EndSessionURL is where signing out sends the browser: the provider's
// RP-initiated logout endpoint when it has one, otherwise the portal root.
// Only with the session's ID token as the hint may it ask the provider to
// return to the portal: OIDC RP-Initiated Logout 1.0 lets providers refuse
// post_logout_redirect_uri without id_token_hint, and Authentik does.
func (p *Provider) EndSessionURL(idTokenHint string) string {
	if p.endSession == nil {
		return "/"
	}
	logout := *p.endSession
	if idTokenHint != "" {
		query := logout.Query()
		query.Set("id_token_hint", idTokenHint)
		query.Set("post_logout_redirect_uri", p.publicURL)
		logout.RawQuery = query.Encode()
	}

	return logout.String()
}

func (p *Provider) AuthCodeURL(attempt loginAttempt) string {
	return p.oauth.AuthCodeURL(attempt.State, oidc.Nonce(attempt.Nonce), oauth2.S256ChallengeOption(attempt.Verifier))
}

// Exchange redeems the code and verifies the ID token against the login
// attempt. It keeps only the raw ID token, as the logout hint; the access and
// refresh tokens are discarded because the portal keeps its own session.
func (p *Provider) Exchange(ctx context.Context, attempt loginAttempt, code string) (identity, error) {
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(attempt.Verifier))
	if err != nil {
		return identity{}, fmt.Errorf("exchange the authorization code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return identity{}, errors.New("the token response has no ID token")
	}
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return identity{}, fmt.Errorf("verify the ID token: %w", err)
	}
	if idToken.Nonce != attempt.Nonce {
		return identity{}, errors.New("the ID token nonce does not match")
	}

	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return identity{}, fmt.Errorf("read the ID token claims: %w", err)
	}

	return identity{Subject: idToken.Subject, Email: claims.Email, Name: claims.Name, IDToken: rawIDToken}, nil
}
