package mcp

import (
	"encoding/json"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/action"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/dashboard"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/integration"
	"github.com/dvjn/goldenportal/internal/scorecard"
	"github.com/dvjn/goldenportal/internal/search"
)

// Services are what tools, resources and prompts call: the services the
// web API calls too, which check the caller's role themselves.
type Services interface {
	Search(ctx *gofr.Context, text string) ([]search.Result, error)

	Documents(kind string) (configdoc.Documents, bool)
	Apply(ctx *gofr.Context, source string, dryRun bool) (configdoc.Applied, error)
	PublishMany(ctx *gofr.Context, documents []configdoc.DocumentRef, acknowledgeBreaking bool) ([]configdoc.Published, error)
	ExportAll(ctx *gofr.Context) (string, error)
	Schema(ctx *gofr.Context, kind string) (json.RawMessage, error)

	Catalog(ctx *gofr.Context) ([]entity.CatalogEntry, error)
	Entities(ctx *gofr.Context, blueprint string, req entity.ListRequest) (entity.Page, error)
	Entity(ctx *gofr.Context, blueprint, identifier string) (entity.Detail, error)
	BlueprintUsage(ctx *gofr.Context, blueprint string) (integration.Usage, error)

	Home(ctx *gofr.Context) ([]dashboard.HomeDashboard, error)
	ViewDashboard(ctx *gofr.Context, name string, draft bool) (dashboard.HomeDashboard, error)
	DashboardData(ctx *gofr.Context, name string, inputs map[string][]string) (dashboard.PageData, error)

	EntityScorecards(ctx *gofr.Context, blueprint, identifier string) ([]scorecard.EntityScorecard, error)
	ScorecardSummary(ctx *gofr.Context, name string) (scorecard.SummaryView, error)
	ScorecardResults(ctx *gofr.Context, name string, req scorecard.ResultsRequest) (scorecard.ResultPage, error)
	EvaluateScorecard(ctx *gofr.Context, name string) (scorecard.Evaluated, error)

	Connectors(ctx *gofr.Context) ([]integration.Connector, error)
	SyncRuns(ctx *gofr.Context, name string) (integration.RunPage, error)
	Sync(ctx *gofr.Context, name, mode string) (*integration.Run, error)
	CheckIntegration(ctx *gofr.Context, name string) (integration.CheckResult, error)
	IngestionTokens(ctx *gofr.Context, name string) ([]integration.Token, error)
	CreateIngestionToken(ctx *gofr.Context, name, tokenName string) (integration.CreatedToken, error)
	RevokeIngestionToken(ctx *gofr.Context, name, id string) error

	EntityActions(ctx *gofr.Context, blueprint, identifier string) ([]action.Available, error)
	RunAction(ctx *gofr.Context, name string, req action.RunRequest) (action.Run, error)
	Run(ctx *gofr.Context, id string) (action.Run, error)
	Runs(ctx *gofr.Context, req action.RunsRequest) (action.RunPage, error)
	CancelRun(ctx *gofr.Context, id string) (action.Run, error)
	RetryRun(ctx *gofr.Context, id string) (action.Run, error)
	Approvals(ctx *gofr.Context) ([]action.Run, error)
	SelfServiceCatalog(ctx *gofr.Context) (action.Catalog, error)
	RunWorkflow(ctx *gofr.Context, name string, req action.RunRequest) (action.Run, error)
}

// NewServices are the portal's own services, searching with searcher.
func NewServices(searcher search.Service) Services { return portal{searcher} }

type portal struct{ searcher search.Service }

func (p portal) Search(ctx *gofr.Context, text string) ([]search.Result, error) {
	return p.searcher.Search(ctx, text)
}

func (portal) Documents(kind string) (configdoc.Documents, bool) { return configdoc.DocumentsOf(kind) }

func (portal) Apply(ctx *gofr.Context, source string, dryRun bool) (configdoc.Applied, error) {
	return configdoc.Apply(ctx, source, dryRun)
}

func (portal) PublishMany(ctx *gofr.Context, documents []configdoc.DocumentRef, acknowledgeBreaking bool) ([]configdoc.Published, error) {
	return configdoc.PublishMany(ctx, documents, acknowledgeBreaking)
}

func (portal) ExportAll(ctx *gofr.Context) (string, error) { return configdoc.ExportAll(ctx) }

func (portal) Schema(ctx *gofr.Context, kind string) (json.RawMessage, error) {
	return configdoc.GetSchema(ctx, kind)
}

func (portal) Catalog(ctx *gofr.Context) ([]entity.CatalogEntry, error) { return entity.Catalog(ctx) }

func (portal) Entities(ctx *gofr.Context, blueprint string, req entity.ListRequest) (entity.Page, error) {
	return entity.List(ctx, blueprint, req)
}

func (portal) Entity(ctx *gofr.Context, blueprint, identifier string) (entity.Detail, error) {
	return entity.Get(ctx, blueprint, identifier)
}

func (portal) BlueprintUsage(ctx *gofr.Context, blueprint string) (integration.Usage, error) {
	return integration.BlueprintUsage(ctx, blueprint)
}

func (portal) Home(ctx *gofr.Context) ([]dashboard.HomeDashboard, error) { return dashboard.Home(ctx) }

func (portal) ViewDashboard(ctx *gofr.Context, name string, draft bool) (dashboard.HomeDashboard, error) {
	return dashboard.View(ctx, name, draft)
}

func (portal) DashboardData(ctx *gofr.Context, name string, inputs map[string][]string) (dashboard.PageData, error) {
	return dashboard.Data(ctx, name, inputs, false)
}

func (portal) EntityScorecards(ctx *gofr.Context, blueprint, identifier string) ([]scorecard.EntityScorecard, error) {
	return scorecard.ForEntity(ctx, blueprint, identifier)
}

func (portal) ScorecardSummary(ctx *gofr.Context, name string) (scorecard.SummaryView, error) {
	return scorecard.GetSummary(ctx, name)
}

func (portal) ScorecardResults(ctx *gofr.Context, name string, req scorecard.ResultsRequest) (scorecard.ResultPage, error) {
	return scorecard.Results(ctx, name, req)
}

func (portal) EvaluateScorecard(ctx *gofr.Context, name string) (scorecard.Evaluated, error) {
	return scorecard.EvaluateNow(ctx, name)
}

func (portal) Connectors(ctx *gofr.Context) ([]integration.Connector, error) {
	return integration.ListConnectors(ctx)
}

func (portal) SyncRuns(ctx *gofr.Context, name string) (integration.RunPage, error) {
	return integration.Runs(ctx, name, integration.RunsRequest{})
}

func (portal) CheckIntegration(ctx *gofr.Context, name string) (integration.CheckResult, error) {
	return integration.CheckNow(ctx, name)
}

func (portal) Sync(ctx *gofr.Context, name, mode string) (*integration.Run, error) {
	return integration.SyncNow(ctx, name, mode)
}

func (portal) IngestionTokens(ctx *gofr.Context, name string) ([]integration.Token, error) {
	return integration.ListTokens(ctx, name)
}

func (portal) CreateIngestionToken(ctx *gofr.Context, name, tokenName string) (integration.CreatedToken, error) {
	return integration.CreateToken(ctx, name, tokenName)
}

func (portal) RevokeIngestionToken(ctx *gofr.Context, name, id string) error {
	return integration.RevokeToken(ctx, name, id)
}

func (portal) EntityActions(ctx *gofr.Context, blueprint, identifier string) ([]action.Available, error) {
	return action.ForEntity(ctx, blueprint, identifier)
}

func (portal) RunAction(ctx *gofr.Context, name string, req action.RunRequest) (action.Run, error) {
	return action.Start(ctx, name, req, action.ViaMCP)
}

func (portal) Run(ctx *gofr.Context, id string) (action.Run, error) { return action.GetRun(ctx, id) }

func (portal) Runs(ctx *gofr.Context, req action.RunsRequest) (action.RunPage, error) {
	return action.Runs(ctx, req)
}

func (portal) CancelRun(ctx *gofr.Context, id string) (action.Run, error) {
	return action.CancelRun(ctx, id)
}

func (portal) RetryRun(ctx *gofr.Context, id string) (action.Run, error) {
	return action.RetryRun(ctx, id)
}

func (portal) Approvals(ctx *gofr.Context) ([]action.Run, error) { return action.Approvals(ctx) }

func (portal) SelfServiceCatalog(ctx *gofr.Context) (action.Catalog, error) {
	return action.SelfServiceCatalog(ctx)
}

func (portal) RunWorkflow(ctx *gofr.Context, name string, req action.RunRequest) (action.Run, error) {
	return action.StartWorkflow(ctx, name, req, action.ViaMCP)
}
