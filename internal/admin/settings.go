package admin

import (
	"time"

	"github.com/dvjn/goldport/internal/apitoken"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/directory"
	"gofr.dev/pkg/gofr"
)

type settingsResponse struct {
	Name                      string            `json:"name"`
	PublicURL                 string            `json:"public_url"`
	OIDCIssuerURL             string            `json:"oidc_issuer_url"`
	OIDCClientID              string            `json:"oidc_client_id"`
	OIDCClientSecretSet       bool              `json:"oidc_client_secret_set"`
	SessionMaxAgeSeconds      int               `json:"session_max_age_seconds"`
	SessionIdleTimeoutSeconds int               `json:"session_idle_timeout_seconds"`
	Bootstrap                 bootstrapResponse `json:"bootstrap"`
}

// bootstrapResponse tells whether access relies on GOLDPORT_BOOTSTRAP_ADMINS:
// AdminGranted is false while no grant gives Admin to an active provisioned
// user.
type bootstrapResponse struct {
	Admins       []string `json:"admins"`
	AdminGranted bool     `json:"admin_granted"`
}

func (s *Service) getSettings(ctx *gofr.Context) (any, error) {
	carrying, err := s.grants.AdminCarrying(ctx)
	if err != nil {
		return nil, err
	}

	return settingsResponse{
		Name:                      s.settings.Name,
		PublicURL:                 s.settings.PublicURL,
		OIDCIssuerURL:             s.settings.IssuerURL,
		OIDCClientID:              s.settings.ClientID,
		OIDCClientSecretSet:       s.settings.ClientSecretSet,
		SessionMaxAgeSeconds:      int(s.settings.SessionMaxAge.Seconds()),
		SessionIdleTimeoutSeconds: int(s.settings.SessionIdleTimeout.Seconds()),
		Bootstrap: bootstrapResponse{
			Admins:       append([]string{}, s.settings.BootstrapAdmins...),
			AdminGranted: len(carrying) > 0,
		},
	}, nil
}

type scimResponse struct {
	Endpoint   string    `json:"endpoint"`
	LastPushAt time.Time `json:"last_push_at,omitzero"`
	Users      int       `json:"users"`
	Groups     int       `json:"groups"`
}

func (s *Service) getSCIM(ctx *gofr.Context) (any, error) {
	lastPush, err := s.tokens.LastUsed(ctx, apitoken.KindSCIM)
	if err != nil {
		return nil, err
	}
	countOnly := directory.Page{Limit: 0}
	_, users, err := s.directory.ListUsers(ctx, directory.UserFilter{}, countOnly)
	if err != nil {
		return nil, err
	}
	_, groups, err := s.directory.ListGroups(ctx, directory.GroupFilter{}, countOnly)
	if err != nil {
		return nil, err
	}

	return scimResponse{
		Endpoint:   s.settings.PublicURL + "/scim/v2",
		LastPushAt: lastPush,
		Users:      users,
		Groups:     groups,
	}, nil
}

type secretResponse[T any] struct {
	Token  T      `json:"token"`
	Secret string `json:"secret"`
}

type scimTokenResponse struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// rotateSCIMToken creates a SCIM token and revokes the others at once, so
// the identity provider fails until it has the new token.
func (s *Service) rotateSCIMToken(ctx *gofr.Context) (any, error) {
	principal, _ := auth.PrincipalFrom(ctx)
	token, secret, err := s.tokens.Rotate(ctx, apitoken.NewToken{
		Kind:      apitoken.KindSCIM,
		Name:      "Rotated by " + principal.Actor().Label,
		CreatedBy: principal.User.ID,
	})
	if err != nil {
		return nil, err
	}
	if err := s.audit.Record(ctx, audit.Entry{
		Actor:  principal.Actor(),
		Action: audit.SCIMTokenRotated,
		Target: audit.Target{Kind: "scim_token", ID: token.ID, Name: "SCIM token"},
	}); err != nil {
		return nil, err
	}

	return secretResponse[scimTokenResponse]{Token: scimTokenResponse{ID: token.ID, CreatedAt: token.CreatedAt}, Secret: secret}, nil
}
