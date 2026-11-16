package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/action"
	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/connection"
	"github.com/dvjn/goldenportal/internal/dashboard"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/scorecard"
	"github.com/dvjn/goldenportal/internal/widget"
)

// fakeServices is a portal with a small catalog: a service and a team
// blueprint, a ready scorecard and a services dashboard. It records the
// last call it was made, as who, so tests can see what a tool asked for.
// Calls it doesn't fake panic.
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
		return nil, kind == "connections" || kind == "dashboards"
	}
	return fakeDocuments{f: f, kind: kind}, true
}

func (f fakeServices) Catalog(ctx *gofr.Context) ([]entity.CatalogEntry, error) {
	p, _ := auth.FromContext(ctx)
	return []entity.CatalogEntry{{Identifier: p.ID}}, nil
}

func (f fakeServices) Entities(ctx *gofr.Context, blueprint string, req entity.ListRequest) (entity.Page, error) {
	f.record(ctx, "Entities", blueprint, req)
	page := entity.Page{Items: []*entity.Entity{{Identifier: "payments/" + req.Search}, {Identifier: "billing"}}}
	if req.Cursor != "" {
		page.Previous = "before-" + req.Cursor
	}
	return page, nil
}

func (f fakeServices) Entity(ctx *gofr.Context, blueprint, identifier string) (entity.Detail, error) {
	if blueprint != "service" {
		return entity.Detail{}, notFound()
	}
	return entity.Detail{Entity: &entity.Entity{Blueprint: blueprint, Identifier: identifier, Title: "Payments"}, HiddenTabs: []string{"oncall"}}, nil
}

func (f fakeServices) EntityScorecards(*gofr.Context, string, string) ([]scorecard.EntityScorecard, error) {
	return []scorecard.EntityScorecard{{Scorecard: "ready", EntityResult: scorecard.EntityResult{Result: scorecard.Result{Tier: "gold"}}}}, nil
}

func (f fakeServices) ScorecardSummary(_ *gofr.Context, name string, _ scorecard.SummaryRequest) (scorecard.SummaryView, error) {
	if name != "ready" {
		return scorecard.SummaryView{}, notFound()
	}
	return scorecard.SummaryView{}, nil
}

func (f fakeServices) Apply(ctx *gofr.Context, source string, dryRun bool) (configdoc.Applied, error) {
	f.record(ctx, "Apply", source, dryRun)
	return configdoc.Applied{DryRun: dryRun}, nil
}

func (f fakeServices) CreateIngestionToken(ctx *gofr.Context, name, tokenName string) (connection.CreatedToken, error) {
	f.record(ctx, "CreateIngestionToken", name, tokenName)
	return connection.CreatedToken{Secret: "gpi_secret"}, nil
}

// stat is a widget as a page lays it out, which tools leave out.
var stat = widget.Widget{
	ID: "count", Type: "stat", Title: "Services", Width: 4, Icon: "server", Color: "blue",
	Value: &widget.Value{Query: widget.Query{Blueprint: "service"}},
}

// offer lists actions, of which the caller can run only restart, and
// workflows, of which they can run only register.
var offer = widget.Widget{ID: "run", Type: widget.TypeActions, Actions: []string{"deploy", "restart"}, Workflows: []string{"retire", "register"}}

// pageData is what a page's service loads for its widgets, which is only
// built from JSON outside its package.
func pageData(raw string) dashboard.PageData {
	var data dashboard.PageData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		panic(err)
	}
	return data
}

func (f fakeServices) Home(*gofr.Context) ([]dashboard.HomeDashboard, error) {
	return []dashboard.HomeDashboard{
		{Spec: dashboard.Spec{Identifier: "teams", Title: "Teams"}},
		{Spec: dashboard.Spec{Identifier: "services", Title: "Services", Icon: "server"}, Preferred: true},
	}, nil
}

func (f fakeServices) ViewDashboard(_ *gofr.Context, name string, draft bool) (dashboard.HomeDashboard, error) {
	if name != "services" {
		return dashboard.HomeDashboard{}, notFound()
	}
	title := "Services"
	if draft {
		title = "Services draft"
	}
	return dashboard.HomeDashboard{
		Spec: dashboard.Spec{
			Identifier: name, Title: title, Icon: "server", Order: 2,
			Inputs:  []dashboard.Input{{ID: "team", Title: "Team", Type: "relation", Relation: "team"}, {ID: "tier", Title: "Tier", Type: "enum", Values: []string{"gold"}}},
			Widgets: []widget.Widget{stat},
		},
		Defaults: map[string][]string{"team": {"payments"}},
	}, nil
}

func (f fakeServices) DashboardData(ctx *gofr.Context, name string, inputs map[string][]string, draft bool) (dashboard.PageData, error) {
	f.record(ctx, "DashboardData", name, inputs, draft)
	return pageData(`{"widgets":{"count":{"data":{"value":3}}},"inputs":{"team":[{"identifier":"payments","title":"Payments"}]}}`), nil
}

func (f fakeServices) Blueprint(_ *gofr.Context, identifier string) (blueprint.Spec, error) {
	if identifier != "service" {
		return blueprint.Spec{}, notFound()
	}
	return blueprint.Spec{
		Identifier: "service", Title: "Service", Icon: "server",
		Properties: []blueprint.Property{{ID: "tier", Title: "Tier", Type: "string"}, {ID: "spec", Title: "Spec", Type: "object"}},
		Catalog:    &blueprint.Page{Tabs: []widget.Tab{{ID: "health", Title: "Health", Widgets: []widget.Widget{stat, offer}}}},
		Entity: &blueprint.Page{Tabs: []widget.Tab{
			{ID: "deploys", Title: "Deploys", Widgets: []widget.Widget{stat}},
			{ID: "overview", Title: "Overview", Widgets: []widget.Widget{stat, offer}},
			{ID: "oncall", Title: "On call", Widgets: []widget.Widget{stat}},
		}},
	}, nil
}

func (f fakeServices) GradingScorecards(*gofr.Context, string) ([]scorecard.Spec, error) {
	return []scorecard.Spec{{Identifier: "ready", Title: "Ready", Blueprint: "service"}}, nil
}

func (f fakeServices) PageTab(ctx *gofr.Context, req dashboard.PageRequest) (dashboard.PageData, error) {
	f.record(ctx, "PageTab", req)
	return pageData(`{"widgets":{"count":{"data":{"value":7},"error":""}}}`), nil
}

// EntityTab fails on deploys, as a tab whose query the portal can't run.
func (f fakeServices) EntityTab(ctx *gofr.Context, spec blueprint.Spec, current *entity.Entity, tab string) (dashboard.PageData, error) {
	f.record(ctx, "EntityTab", spec.Identifier, current.Identifier, tab)
	if tab == "deploys" {
		return dashboard.PageData{}, errors.New("connection refused to the database at 10.0.0.5")
	}
	return pageData(`{"widgets":{"count":{"data":{"value":5}}}}`), nil
}

func (f fakeServices) EntityActions(*gofr.Context, string, string) ([]action.Available, error) {
	return []action.Available{{Identifier: "restart", Title: "Restart"}}, nil
}

func (f fakeServices) SelfServiceCatalog(*gofr.Context) (action.Catalog, error) {
	return action.Catalog{Groups: []action.CatalogGroup{{Title: "Ops", Items: []action.Offer{
		{Kind: action.Name, Identifier: "restart", Title: "Restart"},
		{Kind: action.WorkflowName, Identifier: "register", Title: "Register a service"},
	}}}}, nil
}

func (f fakeServices) RowActions(ctx *gofr.Context, blueprint string, identifiers []string) (action.RowActions, error) {
	f.record(ctx, "RowActions", blueprint, identifiers)
	return action.RowActions{
		Actions:  []action.Available{{Identifier: "restart", Title: "Restart"}},
		Entities: map[string][]string{"billing": {"restart"}},
	}, nil
}

func (f fakeServices) Runs(ctx *gofr.Context, req action.RunsRequest) (action.RunPage, error) {
	f.record(ctx, "Runs", req)
	return action.RunPage{Items: []action.Run{{ID: "r1", Title: "Restart"}}, Total: 1}, nil
}

func (f fakeServices) ScorecardResults(ctx *gofr.Context, name string, req scorecard.ResultsRequest) (scorecard.ResultPage, error) {
	f.record(ctx, "ScorecardResults", name, req)
	total := 1
	return scorecard.ResultPage{Items: []scorecard.EntityResult{{Identifier: "payments", Title: "Payments"}}, Total: &total}, nil
}

func (f fakeServices) CheckConnection(*gofr.Context, string) (connection.CheckResult, error) {
	return connection.CheckResult{Version: "published", Passed: true}, nil
}

func (f fakeServices) Sync(*gofr.Context, string, string) (*connection.Run, error) {
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
