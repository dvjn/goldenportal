package admin

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/apitoken"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

const (
	maxTokenNameLength = 100
	maxTokenDays       = 365
)

var (
	errTokenName = httpx.Error{
		Code:    http.StatusBadRequest,
		Message: "give the token a name of at most 100 characters",
	}
	errTokenRole = httpx.Error{
		Code:    http.StatusBadRequest,
		Message: "a token's role must be member or maintainer, no token can be an admin",
	}
	errTokenExpiry = httpx.Error{
		Code:    http.StatusBadRequest,
		Message: "a token must expire within 1 to 365 days",
	}
	errTokenNotFound = httpx.Error{Code: http.StatusNotFound, Message: "the token does not exist or is already revoked"}
	errInvalidBody   = httpx.Error{Code: http.StatusBadRequest, Message: "the request body is not valid JSON"}
)

const auditTargetToken = "api_token"

type tokenResponse struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Role       access.Role `json:"role"`
	CreatedBy  string      `json:"created_by"`
	CreatedAt  time.Time   `json:"created_at"`
	ExpiresAt  time.Time   `json:"expires_at"`
	LastUsedAt time.Time   `json:"last_used_at,omitzero"`
}

func newTokenResponse(token apitoken.Token) tokenResponse {
	return tokenResponse{
		ID:         token.ID,
		Name:       token.Name,
		Role:       token.Role,
		CreatedBy:  token.CreatedByEmail,
		CreatedAt:  token.CreatedAt,
		ExpiresAt:  token.ExpiresAt,
		LastUsedAt: token.LastUsedAt,
	}
}

type tokensResponse struct {
	Tokens []tokenResponse `json:"tokens"`
}

func (s *Service) listTokens(ctx *gofr.Context) (any, error) {
	tokens, err := s.tokens.List(ctx, apitoken.KindAPI)
	if err != nil {
		return nil, err
	}
	response := tokensResponse{Tokens: make([]tokenResponse, 0, len(tokens))}
	for _, token := range tokens {
		response.Tokens = append(response.Tokens, newTokenResponse(token))
	}

	return response, nil
}

type createTokenRequest struct {
	Name          string      `json:"name"`
	Role          access.Role `json:"role"`
	ExpiresInDays int         `json:"expires_in_days"`
}

func (r createTokenRequest) validate() error {
	switch {
	case strings.TrimSpace(r.Name) == "" || len(r.Name) > maxTokenNameLength:
		return errTokenName
	case !slices.Contains(access.TokenRoles, r.Role):
		return errTokenRole
	case r.ExpiresInDays < 1 || r.ExpiresInDays > maxTokenDays:
		return errTokenExpiry
	}

	return nil
}

func (s *Service) createToken(ctx *gofr.Context) (any, error) {
	var request createTokenRequest
	if err := ctx.Bind(&request); err != nil {
		return nil, errInvalidBody
	}
	if err := request.validate(); err != nil {
		return nil, err
	}
	principal, _ := auth.PrincipalFrom(ctx)
	token, secret, err := s.tokens.Create(ctx, apitoken.NewToken{
		Kind:      apitoken.KindAPI,
		Name:      strings.TrimSpace(request.Name),
		Role:      request.Role,
		CreatedBy: principal.User.ID,
		ExpiresAt: time.Now().AddDate(0, 0, request.ExpiresInDays),
	})
	if err != nil {
		return nil, err
	}
	if err := s.audit.Record(ctx, audit.Entry{
		Actor:   principal.Actor(),
		Action:  audit.APITokenCreated,
		Target:  audit.Target{Kind: auditTargetToken, ID: token.ID, Name: token.Name},
		Details: map[string]any{"role": token.Role, "expires_at": token.ExpiresAt},
	}); err != nil {
		return nil, err
	}

	return secretResponse[tokenResponse]{Token: newTokenResponse(token), Secret: secret}, nil
}

func (s *Service) revokeToken(ctx *gofr.Context) (any, error) {
	token, err := s.tokens.Revoke(ctx, apitoken.KindAPI, ctx.PathParam("id"))
	if errors.Is(err, apitoken.ErrNotFound) {
		return nil, errTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	principal, _ := auth.PrincipalFrom(ctx)
	if err := s.audit.Record(ctx, audit.Entry{
		Actor:  principal.Actor(),
		Action: audit.APITokenRevoked,
		Target: audit.Target{Kind: auditTargetToken, ID: token.ID, Name: token.Name},
	}); err != nil {
		return nil, err
	}

	return nil, nil
}
