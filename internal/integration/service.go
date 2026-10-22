package integration

import (
	"context"
	"net/http"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/catalog"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/httpx"
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

type auditLog interface {
	Record(ctx context.Context, entry audit.Entry) error
}

type Service struct {
	definitions definitionReader
	entities    entityWriter
	runs        runStore
	syncer      *Syncer
	audit       auditLog
	now         func() time.Time
}

func NewService(definitions definitionReader, entities entityWriter, runs runStore, syncer *Syncer, log auditLog, now func() time.Time) *Service {
	return &Service{definitions: definitions, entities: entities, runs: runs, syncer: syncer, audit: log, now: now}
}

func (s *Service) Register(routes *httpx.Routes) {
	routes.POST("/api/v1/ingest/{integration}/{kind}", httpx.Ingest(), s.ingest)
	routes.DELETE("/api/v1/ingest/{integration}/{kind}/{identifier}", httpx.Ingest(), s.ingestDelete)

	routes.POST("/api/v1/integrations/{integration}/sync", httpx.Requires(access.IntegrationsSync), s.syncNow)
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
