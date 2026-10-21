package catalog

import (
	"context"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/httpx"
)

type definitionStore interface {
	Definitions(ctx context.Context) (definition.Set, int, error)
	Impact(ctx context.Context, effects definition.Effects) (Impact, error)
	Apply(ctx context.Context, plan Plan, author audit.Actor, message string) (Revision, error)
	Revisions(ctx context.Context, page Page) ([]Revision, int, error)
	Revision(ctx context.Context, number int) (Revision, error)
}

type auditLog interface {
	Record(ctx context.Context, entry audit.Entry) error
}

type Service struct {
	definitions definitionStore
	plans       *plans
	audit       auditLog
}

func NewService(definitions definitionStore, log auditLog, now func() time.Time) *Service {
	return &Service{definitions: definitions, plans: newPlans(now), audit: log}
}

func (s *Service) Register(routes *httpx.Routes) {
	routes.GET("/api/v1/definitions", httpx.Requires(access.CatalogRead), s.listDefinitions)
	routes.GET("/api/v1/definitions/schema", httpx.Requires(access.CatalogRead), s.getSchema)
	routes.GET("/api/v1/definitions/export", httpx.Requires(access.CatalogRead), s.exportDefinitions)
	routes.GET("/api/v1/definitions/diff", httpx.Requires(access.CatalogRead), s.diffRevisions)
	routes.GET("/api/v1/definitions/revisions", httpx.Requires(access.CatalogRead), s.listRevisions)
	routes.GET("/api/v1/definitions/revisions/{number}", httpx.Requires(access.CatalogRead), s.getRevision)
	routes.POST("/api/v1/definitions/revisions/{number}/rollback", httpx.Requires(access.CatalogModel), s.planRollback)
	routes.POST("/api/v1/definitions/plan", httpx.Requires(access.CatalogModel), s.plan)
	routes.POST("/api/v1/definitions/apply", httpx.Requires(access.CatalogModel), s.apply)
}
