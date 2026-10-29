package admin

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/httpx"
	"github.com/dvjn/goldport/internal/token"
	"gofr.dev/pkg/gofr"
)

const (
	maxServiceTokenNameLength = 100
	maxServiceTokenDays       = 365
)

var (
	errServiceTokenName = httpx.Error{
		Code:    http.StatusBadRequest,
		Message: "give the token a name of at most 100 characters",
	}
	errServiceTokenRole = httpx.Error{
		Code:    http.StatusBadRequest,
		Message: "a token's role must be member or maintainer, no token can be an admin",
	}
	errServiceTokenExpiry = httpx.Error{
		Code:    http.StatusBadRequest,
		Message: "a token must expire within 1 to 365 days",
	}
	errServiceTokenNotFound = httpx.Error{Code: http.StatusNotFound, Message: "the token does not exist or is already revoked"}
	errInvalidBody          = httpx.Error{Code: http.StatusBadRequest, Message: "the request body is not valid JSON"}
)

type serviceTokenResponse struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Role       access.Role `json:"role"`
	CreatedBy  string      `json:"created_by"`
	CreatedAt  time.Time   `json:"created_at"`
	ExpiresAt  time.Time   `json:"expires_at"`
	LastUsedAt time.Time   `json:"last_used_at,omitzero"`
}

func newServiceTokenResponse(serviceToken token.Token) serviceTokenResponse {
	return serviceTokenResponse{
		ID:         serviceToken.ID,
		Name:       serviceToken.Name,
		Role:       serviceToken.Role,
		CreatedBy:  serviceToken.CreatedByEmail,
		CreatedAt:  serviceToken.CreatedAt,
		ExpiresAt:  serviceToken.ExpiresAt,
		LastUsedAt: serviceToken.LastUsedAt,
	}
}

type serviceTokensResponse struct {
	Tokens []serviceTokenResponse `json:"tokens"`
}

func (s *Service) listServiceTokens(ctx *gofr.Context) (any, error) {
	serviceTokens, err := s.tokens.List(ctx, token.KindService)
	if err != nil {
		return nil, err
	}
	response := serviceTokensResponse{Tokens: make([]serviceTokenResponse, 0, len(serviceTokens))}
	for _, serviceToken := range serviceTokens {
		response.Tokens = append(response.Tokens, newServiceTokenResponse(serviceToken))
	}

	return response, nil
}

type createServiceTokenRequest struct {
	Name          string      `json:"name"`
	Role          access.Role `json:"role"`
	ExpiresInDays int         `json:"expires_in_days"`
}

func (r createServiceTokenRequest) validate() error {
	switch {
	case strings.TrimSpace(r.Name) == "" || len(r.Name) > maxServiceTokenNameLength:
		return errServiceTokenName
	case !slices.Contains(access.ServiceTokenRoles, r.Role):
		return errServiceTokenRole
	case r.ExpiresInDays < 1 || r.ExpiresInDays > maxServiceTokenDays:
		return errServiceTokenExpiry
	}

	return nil
}

func (s *Service) createServiceToken(ctx *gofr.Context) (any, error) {
	var request createServiceTokenRequest
	if err := ctx.Bind(&request); err != nil {
		return nil, errInvalidBody
	}
	if err := request.validate(); err != nil {
		return nil, err
	}
	principal, _ := auth.PrincipalFrom(ctx)
	created, secret, err := s.tokens.Create(ctx, token.NewToken{
		Kind:      token.KindService,
		Name:      strings.TrimSpace(request.Name),
		Role:      request.Role,
		CreatedBy: principal.User.ID,
		ExpiresAt: time.Now().AddDate(0, 0, request.ExpiresInDays),
	})
	if err != nil {
		return nil, err
	}
	if err := s.audit.Record(ctx, audit.Entry{
		Actor:    principal.Actor(),
		Action:   audit.ServiceTokenCreated,
		Resource: audit.Resource{Kind: audit.ResourceServiceToken, ID: created.ID, Name: created.Name},
		Details:  map[string]any{"role": created.Role, "expires_at": created.ExpiresAt},
	}); err != nil {
		return nil, err
	}

	return secretResponse[serviceTokenResponse]{Token: newServiceTokenResponse(created), Secret: secret}, nil
}

func (s *Service) revokeServiceToken(ctx *gofr.Context) (any, error) {
	revoked, err := s.tokens.Revoke(ctx, token.KindService, ctx.PathParam("id"))
	if errors.Is(err, token.ErrNotFound) {
		return nil, errServiceTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	principal, _ := auth.PrincipalFrom(ctx)
	if err := s.audit.Record(ctx, audit.Entry{
		Actor:    principal.Actor(),
		Action:   audit.ServiceTokenRevoked,
		Resource: audit.Resource{Kind: audit.ResourceServiceToken, ID: revoked.ID, Name: revoked.Name},
		Details:  map[string]any{"role": revoked.Role},
	}); err != nil {
		return nil, err
	}

	return nil, nil
}
