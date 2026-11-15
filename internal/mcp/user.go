package mcp

import (
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/action"
	"github.com/dvjn/goldenportal/internal/oauth"
)

// userServer serves members what the portal's pages show them, a tool to
// a page, and runs actions and workflows as them.
var userServer = server{
	endpoint: oauth.UserMCP, name: "goldenportal", title: "Golden Portal",
	instructions: userInstructions,
	tools:        append(append([]tool{}, sharedTools...), userTools...),
	prompts:      readingPrompts,
	resources:    addEntityResources,
}

const userInstructions = `Golden Portal is the organization's software catalog. Blueprints define kinds of things, such as services, teams or repositories; entities are the things themselves, kept up to date by connections. Scorecards grade entities against standards in tiers, and dashboards summarize them.

Each tool reads one of the portal's pages whole, as the user would see it. list_dashboards lists Home's dashboards and get_dashboard reads one with every widget's numbers. list_blueprints is the Catalog page; get_catalog reads a blueprint's catalog page, whose tabs hold its widgets, its entities to search, filter, sort and page, and its scorecards' results; get_scorecard reads one scorecard's results directly. get_entity reads an entity's page: its properties, relations, scorecards, the actions the user can run on it, its tabs and its latest runs. When you don't know where something is, find it with search. Answer from the portal's data, name the entities you mean by title and identifier, and say when the catalog doesn't hold something rather than guessing.

Actions do things to one entity, such as restarting a service; get_entity lists those the user can run, with their inputs. list_self_service is the Self-Service page: workflows, such as registering a service, and actions to run on entities the user picks. Run one with run_action or run_workflow only when the user asks, then follow it with get_run until it finishes and report what it gave. list_runs is the Runs page. A run that hasn't finished can be cancelled with cancel_run, and a failed one retried with retry_run, when the user asks. list_approvals lists runs waiting on the user's approval; tell them, since only they can approve, in the portal.

Changing the portal's configuration, such as blueprints, scorecards and dashboards, is for builders, through the portal's builder MCP server at /api/mcp/builder.`

type (
	runActionArgs struct {
		Identifier     string         `json:"identifier" jsonschema:"The action's identifier, from get_entity or list_self_service"`
		Entity         string         `json:"entity" jsonschema:"Identifier of the entity to run it on, of the action's blueprint"`
		Inputs         map[string]any `json:"inputs,omitempty" jsonschema:"Values for the action's inputs, by input identifier, as get_entity or list_self_service describes them"`
		IdempotencyKey string         `json:"idempotencyKey,omitempty" jsonschema:"Any unique text; asking again with the same key returns the first run instead of starting another"`
	}
	workflowArgs struct {
		Identifier     string         `json:"identifier" jsonschema:"The workflow's identifier, from list_self_service"`
		Inputs         map[string]any `json:"inputs,omitempty" jsonschema:"Values for its inputs, by input identifier"`
		IdempotencyKey string         `json:"idempotencyKey,omitempty" jsonschema:"Any unique text; asking again with the same key returns the first run instead of starting another"`
	}
	runArgs struct {
		ID string `json:"id" jsonschema:"The run's id"`
	}
	runsArgs struct {
		Action    string `json:"action,omitempty"`
		Workflow  string `json:"workflow,omitempty" jsonschema:"The runs of one workflow"`
		Parent    string `json:"parent,omitempty" jsonschema:"The runs a workflow run started, by its id"`
		Blueprint string `json:"blueprint,omitempty" jsonschema:"With entity, the runs on one entity"`
		Entity    string `json:"entity,omitempty"`
		Status    string `json:"status,omitempty" jsonschema:"Comma-separated: queued, running, waiting, succeeded, failed, rejected, cancelled or interrupted"`
		Search    string `json:"search,omitempty" jsonschema:"Words in the action, entity or requester"`
		Everyone  bool   `json:"everyone,omitempty" jsonschema:"Everyone's runs instead of the caller's own, for builders; members only ever see their own"`
		Page      int    `json:"page,omitempty"`
		Size      int    `json:"size,omitempty" jsonschema:"Page size: 10, 20, 50 or 100; omit for the default"`
	}
)

var userTools = []tool{
	read("list_dashboards", "List Home's dashboards, in Home's order: each one's title, description, group and inputs with their defaults, and which Home opens on. Read one with get_dashboard.", func(b bridge, ctx *gofr.Context, _ empty) (any, error) {
		return listDashboards(b, ctx)
	}),
	read("list_self_service", "Read the Self-Service page: the workflows the caller can run, and the actions they can run on entities they pick, by group, with their inputs.", func(b bridge, ctx *gofr.Context, _ empty) (any, error) {
		return b.services.SelfServiceCatalog(ctx)
	}),
	run("run_action", "Run an action on an entity as the caller. It starts the run and returns it; poll get_run until it finishes. Approval steps wait for people in the portal.", false, func(b bridge, ctx *gofr.Context, a runActionArgs) (any, error) {
		return b.services.RunAction(ctx, a.Identifier, action.RunRequest{Entity: a.Entity, Inputs: a.Inputs, Key: a.IdempotencyKey})
	}),
	run("run_workflow", "Run a workflow as the caller, such as registering a service. It starts the run and returns it; poll get_run until it finishes. Approval steps wait for people in the portal.", false, func(b bridge, ctx *gofr.Context, a workflowArgs) (any, error) {
		return b.services.RunWorkflow(ctx, a.Identifier, action.RunRequest{Inputs: a.Inputs, Key: a.IdempotencyKey})
	}),
	read("get_run", "Read a run's page: its status, each step's outcome and output, and what it gave.", func(b bridge, ctx *gofr.Context, a runArgs) (any, error) {
		return b.services.Run(ctx, a.ID)
	}),
	read("list_runs", "Read the Runs page: the caller's action and workflow runs, or for builders everyone's, newest first, narrowed to an action or workflow, an entity or a status.", func(b bridge, ctx *gofr.Context, a runsArgs) (any, error) {
		return b.services.Runs(ctx, action.RunsRequest{
			Action: a.Action, Workflow: a.Workflow, Parent: a.Parent, Blueprint: a.Blueprint, Entity: a.Entity, Status: a.Status, Search: a.Search, Mine: !a.Everyone, Page: a.Page, Size: a.Size,
		})
	}),
	run("cancel_run", "Cancel a run that hasn't finished. A step already under way may still take effect.", true, func(b bridge, ctx *gofr.Context, a runArgs) (any, error) {
		return b.services.CancelRun(ctx, a.ID)
	}),
	run("retry_run", "Retry a failed or interrupted run from the step it stopped at, with the same inputs. It returns the run; poll get_run until it finishes.", false, func(b bridge, ctx *gofr.Context, a runArgs) (any, error) {
		return b.services.RetryRun(ctx, a.ID)
	}),
	read("list_approvals", "Read the Approvals page: the runs waiting on the caller's approval, oldest first, with what each step gave so far. Approving and rejecting happen only in the portal.", func(b bridge, ctx *gofr.Context, _ empty) (any, error) {
		return b.services.Approvals(ctx)
	}),
}
