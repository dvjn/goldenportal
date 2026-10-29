// Package admin is the API behind the system settings: the portal's
// identity settings, who holds which role, service tokens, the SCIM token and the
// audit log.
package admin

import (
	"context"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/httpx"
	"github.com/dvjn/goldport/internal/token"
)

type grantStore interface {
	List(ctx context.Context) ([]access.Grant, error)
	ForUser(ctx context.Context, userID string) ([]access.Grant, error)
	AdminCarrying(ctx context.Context) ([]string, error)
	Set(ctx context.Context, subject access.Subject, role access.Role, grantedBy string) (access.Role, error)
}

type userDirectory interface {
	UserByID(ctx context.Context, id string) (directory.User, error)
	GroupByID(ctx context.Context, id string) (directory.Group, error)
	ListUsers(ctx context.Context, filter directory.UserFilter, page directory.Page) ([]directory.User, int, error)
	ListGroups(ctx context.Context, filter directory.GroupFilter, page directory.Page) ([]directory.Group, int, error)
	GroupsOfUser(ctx context.Context, userID string) ([]directory.Group, error)
	LastSignIn(ctx context.Context, userID string) (time.Time, error)
}

type tokenStore interface {
	Create(ctx context.Context, newToken token.NewToken) (token.Token, string, error)
	Rotate(ctx context.Context, newToken token.NewToken) (token.Token, string, error)
	List(ctx context.Context, kind string) ([]token.Token, error)
	Revoke(ctx context.Context, kind, id string) (token.Token, error)
	LastUsed(ctx context.Context, kind string) (time.Time, error)
}

type sessionRevoker interface {
	DeleteForUser(ctx context.Context, userID string) error
}

type auditLog interface {
	Record(ctx context.Context, entry audit.Entry) error
	List(ctx context.Context, query audit.Query) ([]audit.Entry, int, error)
	Facets(ctx context.Context) (audit.Facets, error)
}

// Settings are the read-only values the portal runs with.
type Settings struct {
	Name               string
	PublicURL          string
	IssuerURL          string
	ClientID           string
	ClientSecretSet    bool
	SessionMaxAge      time.Duration
	SessionIdleTimeout time.Duration
	BootstrapAdmins    access.BootstrapAdmins
}

type Service struct {
	settings  Settings
	grants    grantStore
	directory userDirectory
	tokens    tokenStore
	sessions  sessionRevoker
	audit     auditLog
}

func NewService(settings Settings, grants grantStore, users userDirectory, tokens tokenStore, sessions sessionRevoker, log auditLog) *Service {
	return &Service{settings: settings, grants: grants, directory: users, tokens: tokens, sessions: sessions, audit: log}
}

func (s *Service) Register(routes *httpx.Routes) {
	routes.GET("/api/v1/settings", httpx.Requires(access.SettingsRead), s.getSettings)
	routes.GET("/api/v1/scim", httpx.Requires(access.AccessRead), s.getSCIM)
	routes.POST("/api/v1/scim/token", httpx.Requires(access.AccessWrite), s.rotateSCIMToken)

	routes.GET("/api/v1/access/grants", httpx.Requires(access.AccessRead), s.listGrants)
	routes.PUT("/api/v1/access/grants/{type}/{id}", httpx.Requires(access.AccessWrite), s.setGrant)
	routes.DELETE("/api/v1/access/grants/{type}/{id}", httpx.Requires(access.AccessWrite), s.removeGrant)
	routes.GET("/api/v1/access/subjects", httpx.Requires(access.AccessRead), s.listSubjects)
	routes.GET("/api/v1/access/users/{id}", httpx.Requires(access.AccessRead), s.checkUser)
	routes.DELETE("/api/v1/access/users/{id}/sessions", httpx.Requires(access.AccessWrite), s.revokeSessions)

	routes.GET("/api/v1/access/service-tokens", httpx.Requires(access.AccessRead), s.listServiceTokens)
	routes.POST("/api/v1/access/service-tokens", httpx.Requires(access.AccessWrite), s.createServiceToken)
	routes.DELETE("/api/v1/access/service-tokens/{id}", httpx.Requires(access.AccessWrite), s.revokeServiceToken)

	routes.GET("/api/v1/audit", httpx.Requires(access.AuditRead), s.listAudit)
	routes.GET("/api/v1/audit/facets", httpx.Requires(access.AuditRead), s.auditFacets)
}
