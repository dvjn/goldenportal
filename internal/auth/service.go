package auth

import (
	"context"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/httpx"
)

type userDirectory interface {
	UserByOIDCSubject(ctx context.Context, subject string) (directory.User, error)
	UserByExternalID(ctx context.Context, externalID string) (directory.User, error)
	ActiveUserByEmail(ctx context.Context, email string) (directory.User, error)
	CreateUnprovisionedUser(ctx context.Context, email, name string) (directory.User, error)
	RecordSignIn(ctx context.Context, userID, subject string) error
	GroupsOfUser(ctx context.Context, userID string) ([]directory.Group, error)
}

type sessionStore interface {
	Create(ctx context.Context, userID, idToken string) (string, error)
	Delete(ctx context.Context, id string) (string, error)
}

type auditLog interface {
	Record(ctx context.Context, entry audit.Entry) error
}

type Service struct {
	config    Config
	provider  *Provider
	sessions  sessionStore
	directory userDirectory
	audit     auditLog
}

func NewService(config Config, provider *Provider, sessions sessionStore, directory userDirectory, log auditLog) *Service {
	return &Service{config: config, provider: provider, sessions: sessions, directory: directory, audit: log}
}

func (s *Service) Register(routes *httpx.Routes) {
	routes.GET("/api/v1/auth/login", httpx.Public(), s.login)
	routes.GET("/api/v1/auth/callback", httpx.Public(), s.callback)
	routes.GET("/api/v1/auth/signout", httpx.Public(), s.signout)
	routes.POST("/api/v1/auth/logout", httpx.Authenticated(), s.logout)
	routes.GET("/api/v1/auth/me", httpx.Authenticated(), s.me)
}
