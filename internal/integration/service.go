package integration

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/catalog"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/httpx"
	"github.com/dvjn/goldport/internal/token"
)

type definitionReader interface {
	Definitions(ctx context.Context) (definition.Set, int, error)
}

type entityWriter interface {
	WriteOwned(ctx context.Context, write catalog.OwnedWrite) (catalog.WriteResult, error)
	DeleteOwned(ctx context.Context, integration, blueprint, identifier string) (bool, error)
}

type runStore interface {
	Start(ctx context.Context, run Run) (Run, error)
	Finish(ctx context.Context, run Run) error
	Record(ctx context.Context, run Run) (Run, error)
	List(ctx context.Context, integration string, limit int) ([]Run, error)
	Latest(ctx context.Context) (map[string]Run, error)
	LastPull(ctx context.Context, integration string) (time.Time, error)
}

type entityCounter interface {
	OwnedCounts(ctx context.Context) ([]catalog.OwnedCount, error)
}

type tokenStore interface {
	Create(ctx context.Context, token token.NewToken) (token.Token, string, error)
	ListIngest(ctx context.Context, integration string) ([]token.Token, error)
	RevokeIngest(ctx context.Context, integration, id string) (token.Token, error)
}

type auditLog interface {
	Record(ctx context.Context, entry audit.Entry) error
}

// ServiceConfig is what the integrations API needs. PublicURL is where
// pushes reach the portal.
type ServiceConfig struct {
	Definitions definitionReader
	Entities    entityWriter
	Counts      entityCounter
	Runs        runStore
	Syncer      *Syncer
	Scheduler   *Scheduler
	Tokens      tokenStore
	Audit       auditLog
	PublicURL   string
	Now         func() time.Time
}

type Service struct {
	definitions definitionReader
	entities    entityWriter
	counts      entityCounter
	runs        runStore
	syncer      *Syncer
	scheduler   *Scheduler
	tokens      tokenStore
	audit       auditLog
	publicURL   string
	now         func() time.Time
}

func NewService(config ServiceConfig) *Service {
	return &Service{
		definitions: config.Definitions,
		entities:    config.Entities,
		counts:      config.Counts,
		runs:        config.Runs,
		syncer:      config.Syncer,
		scheduler:   config.Scheduler,
		tokens:      config.Tokens,
		audit:       config.Audit,
		publicURL:   strings.TrimRight(config.PublicURL, "/"),
		now:         config.Now,
	}
}

func (s *Service) Register(routes *httpx.Routes) {
	routes.POST("/api/v1/ingest/{integration}/{kind}", httpx.Ingest(), s.ingest)
	routes.DELETE("/api/v1/ingest/{integration}/{kind}/{identifier}", httpx.Ingest(), s.ingestDelete)

	routes.GET("/api/v1/integrations", httpx.Requires(access.CatalogRead), s.listIntegrations)
	routes.GET("/api/v1/integrations/{integration}", httpx.Requires(access.CatalogRead), s.getIntegration)
	routes.GET("/api/v1/integrations/{integration}/runs", httpx.Requires(access.CatalogRead), s.listRuns)
	routes.POST("/api/v1/integrations/{integration}/sync", httpx.Requires(access.IntegrationsSync), s.syncNow)
	routes.GET("/api/v1/integrations/{integration}/tokens", httpx.Requires(access.IntegrationsTokens), s.listTokens)
	routes.POST("/api/v1/integrations/{integration}/tokens", httpx.Requires(access.IntegrationsTokens), s.createToken)
	routes.DELETE("/api/v1/integrations/{integration}/tokens/{id}", httpx.Requires(access.IntegrationsTokens), s.revokeToken)
}

// detailedError is an API error whose body carries more than its message.
type detailedError struct {
	status  int
	message string
	details map[string]any
}

func (e detailedError) Error() string { return e.message }

func (e detailedError) StatusCode() int { return e.status }

func (e detailedError) Response() map[string]any { return e.details }

const unexpectedFailure = "Something went wrong. Try again."

// failureText is what a run records about the error that ended it: the
// message of an error the caller can act on, a generic one otherwise.
func failureText(err error) string {
	if err == nil {
		return ""
	}
	if expected, ok := err.(interface{ StatusCode() int }); ok && expected.StatusCode() < http.StatusInternalServerError {
		return err.Error()
	}
	if failure, ok := err.(syncError); ok {
		return failure.Error()
	}

	return unexpectedFailure
}

// syncError is a sync failure whose message is meant for the people who
// read the run, such as an unreachable source.
type syncError struct {
	message string
}

func (e syncError) Error() string { return e.message }
