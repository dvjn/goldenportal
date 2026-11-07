package action

import (
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// baseURL is the portal's public URL, which runs link to.
var baseURL string

// Register serves actions and their runs, and has this instance take on
// runs no instance holds. The handlers read the request and call the
// services, which MCP calls too.
func Register(app httpx.Routes, portalURL string) {
	baseURL = portalURL
	configdoc.Register(app, Kind)
	configdoc.Register(app, WorkflowDocs)
	app.GET("self-service", selfServiceCatalog)
	app.POST("workflows/{identifier}/runs", startWorkflow)
	app.GET("catalog/{blueprint}/actions", forEntity)
	app.GET("catalog/{blueprint}/row-actions", forEntities)
	app.POST("actions/{identifier}/runs", start)
	app.GET("runs", listRuns)
	app.GET("runs/{id}", getRun)
	app.POST("runs/{id}/cancel", cancel)
	app.POST("runs/{id}/retry", retry)
	app.POST("runs/{id}/resolve", resolve)
	app.POST("runs/{id}/decide", decide)
	app.GET("approvals", approvals)
	app.AddCronJob(SweepSchedule, "carry-on-action-runs", func(ctx *gofr.Context) {
		NewRunner(ctx.SQL, ctx.Container.Logger, baseURL).Sweep(ctx)
	})
}

func forEntity(ctx *gofr.Context) (any, error) {
	return httpx.Result(ForEntity(ctx, ctx.PathParam("blueprint"), ctx.Param("entity")))
}

// forEntities answers for each entity given as entity, which repeats. It
// reads the raw query, since identifiers come from other systems and may
// hold commas, which ctx.Params splits on.
func forEntities(ctx *gofr.Context) (any, error) {
	return httpx.Result(ForEntities(ctx, ctx.PathParam("blueprint"), httpx.Query(ctx)["entity"]))
}

func start(ctx *gofr.Context) (any, error) {
	var req RunRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.BadRequest("the request body is not valid JSON")
	}

	return httpx.Result(Start(ctx, ctx.PathParam("identifier"), req, ViaWeb))
}

func runsRequest(ctx *gofr.Context) (RunsRequest, error) {
	page, size, err := httpx.Paging(ctx.Param)
	if err != nil {
		return RunsRequest{}, err
	}

	return RunsRequest{
		Action: ctx.Param("action"), Workflow: ctx.Param("workflow"),
		Blueprint: ctx.Param("blueprint"), Entity: ctx.Param("entity"), Status: ctx.Param("status"), Search: ctx.Param("q"),
		Parent: ctx.Param("parent"),
		Mine:   ctx.Param("mine") == "true", Page: page, Size: size,
	}, nil
}

func listRuns(ctx *gofr.Context) (any, error) {
	req, err := runsRequest(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Result(Runs(ctx, req))
}

func getRun(ctx *gofr.Context) (any, error) { return httpx.Result(GetRun(ctx, ctx.PathParam("id"))) }

func cancel(ctx *gofr.Context) (any, error) { return httpx.Result(CancelRun(ctx, ctx.PathParam("id"))) }

func retry(ctx *gofr.Context) (any, error) { return httpx.Result(RetryRun(ctx, ctx.PathParam("id"))) }

type resolveRequest struct {
	Outcome string `json:"outcome"`
	Comment string `json:"comment"`
}

func resolve(ctx *gofr.Context) (any, error) {
	var req resolveRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.BadRequest("the request body is not valid JSON")
	}

	return httpx.Result(ResolveRun(ctx, ctx.PathParam("id"), req.Outcome, req.Comment))
}

type decideRequest struct {
	Decision string `json:"decision"`
	Comment  string `json:"comment"`
}

func decide(ctx *gofr.Context) (any, error) {
	var req decideRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.BadRequest("the request body is not valid JSON")
	}

	return httpx.Result(Decide(ctx, ctx.PathParam("id"), req.Decision, req.Comment))
}

func approvals(ctx *gofr.Context) (any, error) { return httpx.Result(Approvals(ctx)) }

func selfServiceCatalog(ctx *gofr.Context) (any, error) { return httpx.Result(SelfServiceCatalog(ctx)) }

func startWorkflow(ctx *gofr.Context) (any, error) {
	var req RunRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.BadRequest("the request body is not valid JSON")
	}

	return httpx.Result(StartWorkflow(ctx, ctx.PathParam("identifier"), req, ViaWeb))
}
