package integration

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/httpx"
	"github.com/dvjn/goldport/internal/token"
	"gofr.dev/pkg/gofr"
)

const (
	maxTokenNameLength = 100
	maxTokenDays       = 365
)

var (
	errTokenName     = httpx.Error{Code: http.StatusBadRequest, Message: "give the token a name of at most 100 characters"}
	errTokenExpiry   = httpx.Error{Code: http.StatusBadRequest, Message: "a token must expire within 1 to 365 days"}
	errTokenNotFound = httpx.Error{
		Code:    http.StatusNotFound,
		Message: "the integration has no such token, or it is already revoked",
	}
)

type tokenView struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	LastUsedAt time.Time `json:"last_used_at,omitzero"`
}

func newTokenView(ingest token.Token) tokenView {
	return tokenView{
		ID:         ingest.ID,
		Name:       ingest.Name,
		CreatedBy:  ingest.CreatedByEmail,
		CreatedAt:  ingest.CreatedAt,
		ExpiresAt:  ingest.ExpiresAt,
		LastUsedAt: ingest.LastUsedAt,
	}
}

// pushIntegration returns the push integration the path names; only a push
// integration takes ingest tokens.
func (s *Service) pushIntegration(ctx *gofr.Context) (*definition.Integration, error) {
	_, integration, err := s.integration(ctx)
	if err != nil {
		return nil, err
	}
	if integration.Connector != definition.ConnectorPush {
		return nil, httpx.Error{
			Code:    http.StatusConflict,
			Message: fmt.Sprintf("the %s integration reads its source itself and takes no ingest tokens", integration.Title),
		}
	}

	return integration, nil
}

type tokensResponse struct {
	Tokens []tokenView `json:"tokens"`
}

func (s *Service) listTokens(ctx *gofr.Context) (any, error) {
	integration, err := s.pushIntegration(ctx)
	if err != nil {
		return nil, err
	}
	tokens, err := s.tokens.ListIngest(ctx, integration.Identifier)
	if err != nil {
		return nil, err
	}
	response := tokensResponse{Tokens: make([]tokenView, 0, len(tokens))}
	for _, token := range tokens {
		response.Tokens = append(response.Tokens, newTokenView(token))
	}

	return response, nil
}

type createTokenRequest struct {
	Name          string `json:"name"`
	ExpiresInDays int    `json:"expires_in_days"`
}

// createdToken carries the secret, which is never readable again, and the
// address to push to.
type createdToken struct {
	Token     tokenView `json:"token"`
	Secret    string    `json:"secret"`
	IngestURL string    `json:"ingest_url"`
}

func (s *Service) createToken(ctx *gofr.Context) (any, error) {
	integration, err := s.pushIntegration(ctx)
	if err != nil {
		return nil, err
	}
	var request createTokenRequest
	if err := ctx.Bind(&request); err != nil {
		return nil, errInvalidBody
	}
	name := strings.TrimSpace(request.Name)
	switch {
	case name == "" || len(name) > maxTokenNameLength:
		return nil, errTokenName
	case request.ExpiresInDays < 1 || request.ExpiresInDays > maxTokenDays:
		return nil, errTokenExpiry
	}
	principal, _ := auth.PrincipalFrom(ctx)
	created, secret, err := s.tokens.Create(ctx, token.NewToken{
		Kind:        token.KindIngest,
		Name:        name,
		Integration: integration.Identifier,
		CreatedBy:   principal.User.ID,
		ExpiresAt:   s.now().AddDate(0, 0, request.ExpiresInDays),
	})
	if err != nil {
		return nil, err
	}
	if err := s.audit.Record(ctx, audit.Entry{
		Actor:    principal.Actor(),
		Action:   audit.IngestTokenCreated,
		Resource: ingestTokenResource(integration, created),
		Details:  map[string]any{"expires_at": created.ExpiresAt},
	}); err != nil {
		return nil, err
	}

	return createdToken{Token: newTokenView(created), Secret: secret, IngestURL: s.ingestURL(integration)}, nil
}

func (s *Service) revokeToken(ctx *gofr.Context) (any, error) {
	integration, err := s.pushIntegration(ctx)
	if err != nil {
		return nil, err
	}
	revoked, err := s.tokens.RevokeIngest(ctx, integration.Identifier, ctx.PathParam("id"))
	if errors.Is(err, token.ErrNotFound) {
		return nil, errTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	principal, _ := auth.PrincipalFrom(ctx)

	return nil, s.audit.Record(ctx, audit.Entry{
		Actor:    principal.Actor(),
		Action:   audit.IngestTokenRevoked,
		Resource: ingestTokenResource(integration, revoked),
	})
}

func ingestTokenResource(integration *definition.Integration, ingest token.Token) audit.Resource {
	return audit.Resource{
		Kind: audit.ResourceIngestToken, ID: ingest.ID, Name: ingest.Name,
		Parent: audit.Parent{Kind: audit.ResourceIntegration, ID: integration.Identifier, Name: integration.Title},
	}
}
