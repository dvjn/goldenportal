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

type entityStore interface {
	Entities(ctx context.Context, filter EntityFilter, page Page) ([]Entity, int, error)
	Entity(ctx context.Context, blueprint, identifier string) (Entity, error)
	Incoming(ctx context.Context, entity EntityName) ([]Link, error)
	LinksFrom(ctx context.Context, blueprint string, identifiers []string) ([]Link, error)
	Reach(ctx context.Context, blueprint string, identifiers []string, path definition.Path) (map[string][]Reached, error)
	Changes(ctx context.Context, limit int) ([]Entity, error)
	BlueprintCounts(ctx context.Context) (map[string]int, error)
	IntegrationCounts(ctx context.Context, blueprint string) ([]IntegrationCount, error)
	Missing(ctx context.Context, blueprint string, identifiers []string) ([]string, error)
	Reaches(ctx context.Context, blueprint, relation, start, goal string) (bool, error)
	CreateEntity(ctx context.Context, entity Entity) (Entity, error)
	UpdateEntity(ctx context.Context, entity Entity) (Entity, error)
	DeleteEntity(ctx context.Context, blueprint, identifier string) error
}

type auditLog interface {
	Record(ctx context.Context, entry audit.Entry) error
}

type Service struct {
	definitions definitionStore
	entities    entityStore
	plans       *plans
	audit       auditLog
}

func NewService(definitions definitionStore, entities entityStore, log auditLog, now func() time.Time) *Service {
	return &Service{definitions: definitions, entities: entities, plans: newPlans(now), audit: log}
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

	routes.GET("/api/v1/catalog/blueprints", httpx.Requires(access.CatalogRead), s.listBlueprints)
	routes.GET("/api/v1/catalog/changes", httpx.Requires(access.CatalogRead), s.listChanges)
	routes.GET("/api/v1/catalog/blueprints/{blueprint}", httpx.Requires(access.CatalogRead), s.getBlueprint)
	routes.GET("/api/v1/catalog/blueprints/{blueprint}/entities", httpx.Requires(access.CatalogRead), s.listEntities)
	routes.POST("/api/v1/catalog/blueprints/{blueprint}/entities", httpx.Requires(access.CatalogWrite), s.createEntity)
	routes.GET(entityRoute, httpx.Requires(access.CatalogRead), s.getEntity)
	routes.PUT(entityRoute, httpx.Requires(access.CatalogWrite), s.updateEntity)
	routes.DELETE(entityRoute, httpx.Requires(access.CatalogWrite), s.deleteEntity)
}

// entityRoute matches the identifier across slashes: the router matches the
// decoded path, so an identifier sent with %2F arrives as several segments.
const entityRoute = "/api/v1/catalog/blueprints/{blueprint}/entities/{identifier:.+}"
