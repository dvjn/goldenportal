package mcp

import (
	"context"
	"errors"
	"strings"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/dashboard"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/integration"
	"github.com/dvjn/goldenportal/internal/scorecard"
)

// fakeServices is a portal with a small catalog: a service and a team
// blueprint, and a ready scorecard. It records the last call it was made,
// as who, so tests can see what a tool asked for. Calls it doesn't fake
// panic.
type fakeServices struct {
	Services
	last *recorded
}

type recorded struct {
	name, user string
	args       []any
}

func newFake() fakeServices { return fakeServices{last: &recorded{}} }

func (f fakeServices) record(ctx *gofr.Context, name string, args ...any) {
	p, _ := auth.FromContext(ctx)
	*f.last = recorded{name: name, user: p.ID, args: args}
}

func notFound() error { return httpx.NotFound("not found") }

func (f fakeServices) Documents(kind string) (configdoc.Documents, bool) {
	if kind != "blueprints" && kind != "scorecards" {
		return nil, kind == "integrations" || kind == "dashboards"
	}
	return fakeDocuments{f: f, kind: kind}, true
}

func (f fakeServices) Catalog(ctx *gofr.Context) ([]entity.CatalogEntry, error) {
	p, _ := auth.FromContext(ctx)
	return []entity.CatalogEntry{{Identifier: p.ID}}, nil
}

func (f fakeServices) Entities(ctx *gofr.Context, blueprint string, req entity.ListRequest) (entity.Page, error) {
	f.record(ctx, "Entities", blueprint, req)
	return entity.Page{Items: []*entity.Entity{{Identifier: "payments/" + req.Search}, {Identifier: "billing"}}}, nil
}

func (f fakeServices) Entity(ctx *gofr.Context, blueprint, identifier string) (entity.Detail, error) {
	if blueprint != "service" {
		return entity.Detail{}, notFound()
	}
	return entity.Detail{Entity: &entity.Entity{Blueprint: blueprint, Identifier: identifier}}, nil
}

func (f fakeServices) EntityScorecards(*gofr.Context, string, string) ([]scorecard.EntityScorecard, error) {
	return []scorecard.EntityScorecard{{Scorecard: "ready", EntityResult: scorecard.EntityResult{Result: scorecard.Result{Tier: "gold"}}}}, nil
}

func (f fakeServices) ScorecardSummary(_ *gofr.Context, name string) (scorecard.SummaryView, error) {
	if name != "ready" {
		return scorecard.SummaryView{}, notFound()
	}
	return scorecard.SummaryView{}, nil
}

func (f fakeServices) Apply(ctx *gofr.Context, source string, dryRun bool) (configdoc.Applied, error) {
	f.record(ctx, "Apply", source, dryRun)
	return configdoc.Applied{DryRun: dryRun}, nil
}

func (f fakeServices) CreateIngestionToken(ctx *gofr.Context, name, tokenName string) (integration.CreatedToken, error) {
	f.record(ctx, "CreateIngestionToken", name, tokenName)
	return integration.CreatedToken{Secret: "gpi_secret"}, nil
}

func (f fakeServices) DashboardData(ctx *gofr.Context, name string, inputs map[string][]string) (dashboard.PageData, error) {
	f.record(ctx, "DashboardData", name, inputs)
	return dashboard.PageData{}, nil
}

func (f fakeServices) CheckIntegration(*gofr.Context, string) (integration.CheckResult, error) {
	return integration.CheckResult{Version: "published", Passed: true}, nil
}

func (f fakeServices) Sync(*gofr.Context, string, string) (*integration.Run, error) {
	return nil, errors.New("connection refused to the database at 10.0.0.5")
}

// fakeDocuments are the blueprints and scorecards: service and ready.
type fakeDocuments struct {
	configdoc.Documents
	f    fakeServices
	kind string
}

func (d fakeDocuments) known(name string) bool {
	return (d.kind == "blueprints" && name == "service") || (d.kind == "scorecards" && name == "ready")
}

func (d fakeDocuments) List(*gofr.Context) ([]configdoc.Summary, error) {
	if d.kind == "scorecards" {
		return []configdoc.Summary{{Identifier: "ready"}}, nil
	}
	return []configdoc.Summary{{Identifier: "service"}, {Identifier: "team"}}, nil
}

func (d fakeDocuments) Get(_ *gofr.Context, name string) (any, error) {
	if !d.known(name) {
		return nil, notFound()
	}
	return map[string]any{"identifier": name}, nil
}

func (d fakeDocuments) Export(_ *gofr.Context, name string, _ bool) (string, error) {
	if !d.known(name) {
		return "", notFound()
	}
	return "kind: " + strings.TrimSuffix(strings.ToUpper(d.kind[:1])+d.kind[1:], "s") + "\n", nil
}

func (d fakeDocuments) SaveDraft(ctx *gofr.Context, name, source string) (configdoc.Saved, error) {
	d.f.record(ctx, "SaveDraft", d.kind, name, source)
	return configdoc.Saved{Created: true}, nil
}

func (d fakeDocuments) Delete(ctx *gofr.Context, name, confirm string) error {
	d.f.record(ctx, "Delete", d.kind, name, confirm)
	if confirm != name {
		return httpx.BadRequest("confirm the deletion")
	}
	return nil
}

func testContext(ctx context.Context) *gofr.Context {
	return &gofr.Context{Context: ctx, ContextLogger: logging.ContextLoggerFor(ctx, logging.NewLogger(logging.FATAL))}
}
