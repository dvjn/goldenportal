package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/action"
	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/oauth"
	"github.com/dvjn/goldenportal/internal/scorecard"
)

// tool is one MCP tool and who may use it: the role the caller needs now,
// and the scope their token must carry. Reading needs portal:read and
// authoring portal:author, which only builders can grant. A caller is only
// offered what they may use, and the services check again on every call.
type tool struct {
	name        string
	description string
	role        auth.Role
	scope       string
	readOnly    bool
	destructive bool
	// links adds a link to the resource the tool read, when its arguments
	// name one.
	links    bool
	register func(*mcp.Server, bridge, tool)
}

// linked arguments name a resource the tool reads.
type linked interface{ resourceLink() *mcp.ResourceLink }

// define makes a tool that calls services with its arguments.
func define[Args any](name, description string, role auth.Role, scope string, readOnly, destructive bool, run func(bridge, *gofr.Context, Args) (any, error)) tool {
	return tool{
		name: name, description: description, role: role, scope: scope, readOnly: readOnly, destructive: destructive,
		register: func(s *mcp.Server, b bridge, t tool) {
			mcp.AddTool(s, &mcp.Tool{
				Name: t.name, Title: title(t.name), Description: t.description,
				Annotations: &mcp.ToolAnnotations{
					ReadOnlyHint: t.readOnly, DestructiveHint: &t.destructive,
					OpenWorldHint: boolPointer(t.name == "sync_integration" || t.name == "check_integration"),
				},
			}, func(ctx context.Context, _ *mcp.CallToolRequest, a Args) (*mcp.CallToolResult, Output, error) {
				result, out, err := b.call(ctx, func(g *gofr.Context) (any, error) { return run(b, g, a) })
				if l, ok := any(a).(linked); ok && t.links && result == nil && err == nil {
					raw, _ := json.Marshal(out)
					result = &mcp.CallToolResult{
						Content:           []mcp.Content{&mcp.TextContent{Text: string(raw)}, l.resourceLink()},
						StructuredContent: out,
					}
				}
				return result, out, err
			})
		},
	}
}

// read and author define the two kinds of tool: reading, for anyone with
// the role, and authoring, which changes configuration or data.
func read[Args any](name, description string, role auth.Role, run func(bridge, *gofr.Context, Args) (any, error)) tool {
	return define(name, description, role, oauth.ScopeRead, true, false, run)
}

func author[Args any](name, description string, destructive bool, run func(bridge, *gofr.Context, Args) (any, error)) tool {
	return define(name, description, auth.RoleBuilder, oauth.ScopeAuthor, false, destructive, run)
}

// withLinks makes t link to the resource it read.
func withLinks(t tool) tool {
	t.links = true
	return t
}

func boolPointer(v bool) *bool { return &v }

// title is how clients show a tool: save_draft is "Save draft".
func title(name string) string {
	words := strings.ReplaceAll(name, "_", " ")
	return strings.ToUpper(words[:1]) + words[1:]
}

// newServer offers the caller the tools they may use, as of this request.
func newServer(b bridge, c caller, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "goldenportal", Title: "Golden Portal", Version: version}, &mcp.ServerOptions{
		Instructions: instructions(c), CompletionHandler: complete(b, c),
	})
	if slices.Contains(c.scopes, oauth.ScopeRead) {
		addResources(s, b)
	}
	for _, p := range prompts {
		if c.principal.Can(p.role) && slices.Contains(c.scopes, p.scope) {
			s.AddPrompt(p.prompt, p.handler(b))
		}
	}
	for _, t := range tools {
		if c.allows(t) {
			t.register(s, b, t)
		}
	}

	return s
}

type (
	empty    struct{}
	kindArgs struct {
		Kind string `json:"kind" jsonschema:"Document collection: blueprints, integrations, scorecards, dashboards, actions or workflows"`
	}
	documentArgs struct {
		Kind string `json:"kind" jsonschema:"Document collection: blueprints, integrations, scorecards, dashboards, actions or workflows"`
		Name string `json:"name"`
	}
	sourceArgs struct {
		Kind   string `json:"kind"`
		Source string `json:"source" jsonschema:"Complete YAML document with kind, metadata and spec"`
	}
	draftArgs struct {
		Kind   string `json:"kind"`
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	versionArgs struct {
		Kind    string `json:"kind"`
		Name    string `json:"name"`
		Version int    `json:"version"`
	}
	publishArgs struct {
		Kind                string `json:"kind"`
		Name                string `json:"name"`
		AcknowledgeBreaking bool   `json:"acknowledgeBreaking,omitempty"`
	}
	deleteArgs struct {
		Kind    string `json:"kind"`
		Name    string `json:"name"`
		Confirm string `json:"confirm" jsonschema:"Repeat the exact document identifier to confirm deletion"`
	}
	applyArgs struct {
		Source string `json:"source"`
		DryRun *bool  `json:"dryRun,omitempty" jsonschema:"Defaults to true; set false explicitly to save drafts"`
	}
	documentRef struct {
		Kind string `json:"kind" jsonschema:"Blueprint, Integration, Scorecard, Dashboard, Action or Workflow"`
		Name string `json:"name"`
	}
	syncArgs struct {
		Name string `json:"name"`
		Mode string `json:"mode,omitempty" jsonschema:"complete deletes what the sync doesn't see; incremental fetches what changed and deletes only what the source says was deleted. Left out, the integration's schedule decides"`
	}
	publishManyArgs struct {
		Documents           []documentRef `json:"documents"`
		AcknowledgeBreaking bool          `json:"acknowledgeBreaking,omitempty"`
	}
	entityArgs struct {
		Blueprint  string `json:"blueprint"`
		Identifier string `json:"identifier"`
	}
	queryArgs struct {
		Blueprint string `json:"blueprint"`
		Search    string `json:"search,omitempty"`
		Filter    string `json:"filter,omitempty" jsonschema:"Shared filter tree encoded as JSON"`
		Sort      string `json:"sort,omitempty"`
		Page      int    `json:"page,omitempty"`
		Size      int    `json:"size,omitempty" jsonschema:"Page size: 10, 20, 50 or 100; omit for the API default"`
	}
	nameArgs struct {
		Name string `json:"name"`
	}
	searchArgs struct {
		Text string `json:"text" jsonschema:"Words in a title or identifier; empty lists blueprints, dashboards, scorecards and integrations"`
	}
	resultsArgs struct {
		Name  string `json:"name"`
		Tiers string `json:"tiers,omitempty" jsonschema:"Comma-separated tier names, or none for entities that reached no tier"`
		Page  int    `json:"page,omitempty"`
		Size  int    `json:"size,omitempty"`
	}
	dashboardDataArgs struct {
		Name   string              `json:"name"`
		Inputs map[string][]string `json:"inputs,omitempty" jsonschema:"Values to set the dashboard's inputs to, by input identifier; an empty list clears one"`
	}
	tokenArgs struct {
		Integration string `json:"integration"`
		Name        string `json:"name" jsonschema:"What the token is for, such as the system that pushes with it"`
	}
	runActionArgs struct {
		Action         string         `json:"action"`
		Entity         string         `json:"entity" jsonschema:"Identifier of the entity to run it on, of the action's blueprint"`
		Inputs         map[string]any `json:"inputs,omitempty" jsonschema:"Values for the action's inputs, by input identifier, as list_entity_actions describes them"`
		IdempotencyKey string         `json:"idempotencyKey,omitempty" jsonschema:"Any unique text; asking again with the same key returns the first run instead of starting another"`
	}
	workflowArgs struct {
		Name           string         `json:"name" jsonschema:"The workflow's identifier, from list_self_service"`
		Inputs         map[string]any `json:"inputs,omitempty" jsonschema:"Values for its inputs, by input identifier"`
		IdempotencyKey string         `json:"idempotencyKey,omitempty" jsonschema:"Any unique text; asking again with the same key returns the first run instead of starting another"`
	}
	runArgs struct {
		ID string `json:"id" jsonschema:"The run's id"`
	}
	runsArgs struct {
		Action    string `json:"action,omitempty"`
		Blueprint string `json:"blueprint,omitempty" jsonschema:"With entity, the runs on one entity"`
		Entity    string `json:"entity,omitempty"`
		Status    string `json:"status,omitempty" jsonschema:"Comma-separated: queued, running, succeeded, failed, cancelled or interrupted"`
		Search    string `json:"search,omitempty" jsonschema:"Words in the action, entity or requester"`
		Mine      bool   `json:"mine,omitempty" jsonschema:"Only runs the caller asked for; members only ever see their own"`
		Page      int    `json:"page,omitempty"`
		Size      int    `json:"size,omitempty"`
	}
	revokeTokenArgs struct {
		Integration string `json:"integration"`
		ID          string `json:"id" jsonschema:"The token's id, from list_ingestion_tokens"`
	}
)

var tools = []tool{
	read("search", "Find blueprints, entities, dashboards, scorecards and integrations by title or identifier, closest first.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a searchArgs) (any, error) {
		return b.services.Search(ctx, a.Text)
	}),
	read("list_documents", "List configuration documents of a kind visible to the caller.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a kindArgs) (any, error) {
		d, err := b.documents(a.Kind)
		if err != nil {
			return nil, err
		}
		return d.List(ctx)
	}),
	withLinks(read("get_document", "Read a document's published configuration and, for builders, its draft.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a documentArgs) (any, error) {
		d, err := b.document(a)
		if err != nil {
			return nil, err
		}
		return d.Get(ctx, a.Name)
	})),
	read("list_versions", "List a configuration document's published versions.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a documentArgs) (any, error) {
		d, err := b.document(a)
		if err != nil {
			return nil, err
		}
		return d.Versions(ctx, a.Name)
	}),
	read("get_version", "Read the exact source of a numbered document version.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a versionArgs) (any, error) {
		d, err := b.document(documentArgs{a.Kind, a.Name})
		if err != nil {
			return nil, err
		}
		if a.Version < 1 {
			return nil, badArgs("version must be positive")
		}
		return d.Version(ctx, a.Name, a.Version)
	}),
	withLinks(read("export_document", "Export one published document as YAML.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a documentArgs) (any, error) {
		d, err := b.document(a)
		if err != nil {
			return nil, err
		}
		source, err := d.Export(ctx, a.Name, false)
		return yaml(source), err
	})),
	read("export_documents", "Export all published documents visible to the caller as multi-document YAML.", auth.RoleMember, func(b bridge, ctx *gofr.Context, _ empty) (any, error) {
		source, err := b.services.ExportAll(ctx)
		return yaml(source), err
	}),
	read("get_document_schema", "Read the JSON Schema of a document kind: every field, which are required, allowed values and what each means. Read it before writing a document of that kind.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a kindArgs) (any, error) {
		if _, err := b.documents(a.Kind); err != nil {
			return nil, err
		}
		return b.services.Schema(ctx, a.Kind)
	}),
	read("list_connectors", "Discover available connector types and their readable resources.", auth.RoleMember, func(b bridge, ctx *gofr.Context, _ empty) (any, error) {
		return b.services.Connectors(ctx)
	}),
	read("list_catalog", "List published blueprints with entity counts.", auth.RoleMember, func(b bridge, ctx *gofr.Context, _ empty) (any, error) {
		return b.services.Catalog(ctx)
	}),
	read("query_entities", "Search, filter, sort and page entities of a blueprint. Filter uses the shared JSON filter tree.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a queryArgs) (any, error) {
		return b.services.Entities(ctx, a.Blueprint, entity.ListRequest{Search: a.Search, Filter: a.Filter, Sort: a.Sort, Page: a.Page, Size: a.Size})
	}),
	withLinks(read("get_entity", "Read an entity, resolved properties and incoming relations. Identifiers may contain slashes.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a entityArgs) (any, error) {
		return b.services.Entity(ctx, a.Blueprint, a.Identifier)
	})),
	read("get_home", "Read the caller's visible dashboards, rendered text and resolved input defaults.", auth.RoleMember, func(b bridge, ctx *gofr.Context, _ empty) (any, error) {
		return b.services.Home(ctx)
	}),
	read("get_dashboard_data", "Read every widget's data on a dashboard the caller sees, with its inputs set as given or to their defaults.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a dashboardDataArgs) (any, error) {
		return b.services.DashboardData(ctx, a.Name, a.Inputs)
	}),
	read("get_entity_scorecards", "Read an entity's current scorecard results.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a entityArgs) (any, error) {
		return b.services.EntityScorecards(ctx, a.Blueprint, a.Identifier)
	}),
	read("get_scorecard_summary", "Read how many entities reached each of a scorecard's tiers and how each criterion fares.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a nameArgs) (any, error) {
		return b.services.ScorecardSummary(ctx, a.Name)
	}),
	read("list_scorecard_results", "Page a scorecard's entity results, worst tier first, optionally narrowed to some tiers.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a resultsArgs) (any, error) {
		req := scorecard.ResultsRequest{Page: a.Page, Size: a.Size}
		if a.Tiers != "" {
			req.Tiers = strings.Split(a.Tiers, ",")
		}
		return b.services.ScorecardResults(ctx, a.Name, req)
	}),
	read("get_blueprint_usage", "Read what uses a blueprint: integrations writing it, scorecards, dashboards and relations, with its entity count.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a nameArgs) (any, error) {
		return b.services.BlueprintUsage(ctx, a.Name)
	}),
	read("preview_impact", "Preview the impact of publishing a saved draft, including affected entities and breaking changes.", auth.RoleBuilder, func(b bridge, ctx *gofr.Context, a documentArgs) (any, error) {
		d, err := b.document(a)
		if err != nil {
			return nil, err
		}
		return d.Impact(ctx, a.Name)
	}),
	read("view_dashboard", "Read a dashboard as Home would show it to the caller, from its draft when draft is true.", auth.RoleBuilder, func(b bridge, ctx *gofr.Context, a struct {
		Name  string `json:"name"`
		Draft bool   `json:"draft,omitempty"`
	},
	) (any, error) {
		return b.services.ViewDashboard(ctx, a.Name, a.Draft)
	}),
	read("list_sync_runs", "Read recent integration runs and item errors.", auth.RoleBuilder, func(b bridge, ctx *gofr.Context, a nameArgs) (any, error) {
		return b.services.SyncRuns(ctx, a.Name)
	}),
	read("list_ingestion_tokens", "List an integration's ingestion tokens, by name and prefix; secrets are never shown again.", auth.RoleBuilder, func(b bridge, ctx *gofr.Context, a nameArgs) (any, error) {
		return b.services.IngestionTokens(ctx, a.Name)
	}),

	read("list_entity_actions", "List the actions the caller can run on an entity, with the inputs each takes.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a entityArgs) (any, error) {
		return b.services.EntityActions(ctx, a.Blueprint, a.Identifier)
	}),
	read("get_run", "Read an action run: its status, each step's outcome and output, and what it gave.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a runArgs) (any, error) {
		return b.services.Run(ctx, a.ID)
	}),
	read("list_runs", "Page action runs, newest first, of an action, on an entity or by the caller.", auth.RoleMember, func(b bridge, ctx *gofr.Context, a runsArgs) (any, error) {
		return b.services.Runs(ctx, action.RunsRequest{
			Action: a.Action, Blueprint: a.Blueprint, Entity: a.Entity, Status: a.Status, Search: a.Search, Mine: a.Mine, Page: a.Page, Size: a.Size,
		})
	}),
	define("run_action", "Run an action on an entity as the caller. It starts the run and returns it; poll get_run until it finishes. Approval steps wait for people in the portal.", auth.RoleMember, oauth.ScopeRun, false, false, func(b bridge, ctx *gofr.Context, a runActionArgs) (any, error) {
		return b.services.RunAction(ctx, a.Action, action.RunRequest{Entity: a.Entity, Inputs: a.Inputs, Key: a.IdempotencyKey})
	}),
	read("list_self_service", "List the Self-Service catalog: the workflows the caller can run, and the actions they can run on entities they pick, by group, with their inputs.", auth.RoleMember, func(b bridge, ctx *gofr.Context, _ empty) (any, error) {
		return b.services.SelfServiceCatalog(ctx)
	}),
	define("run_workflow", "Run a workflow as the caller, such as registering a service. It starts the run and returns it; poll get_run until it finishes. Approval steps wait for people in the portal.", auth.RoleMember, oauth.ScopeRun, false, false, func(b bridge, ctx *gofr.Context, a workflowArgs) (any, error) {
		return b.services.RunWorkflow(ctx, a.Name, action.RunRequest{Inputs: a.Inputs, Key: a.IdempotencyKey})
	}),
	define("cancel_run", "Cancel an action run that hasn't finished. A step already under way may still take effect.", auth.RoleMember, oauth.ScopeRun, false, true, func(b bridge, ctx *gofr.Context, a runArgs) (any, error) {
		return b.services.CancelRun(ctx, a.ID)
	}),
	define("retry_run", "Retry a failed or interrupted run from the step it stopped at, with the same inputs. It returns the run; poll get_run until it finishes.", auth.RoleMember, oauth.ScopeRun, false, false, func(b bridge, ctx *gofr.Context, a runArgs) (any, error) {
		return b.services.RetryRun(ctx, a.ID)
	}),
	read("list_approvals", "List the runs waiting on the caller's approval, oldest first, with what each step gave so far. Approving and rejecting happen only in the portal.", auth.RoleMember, func(b bridge, ctx *gofr.Context, _ empty) (any, error) {
		return b.services.Approvals(ctx)
	}),

	author("validate_document", "Validate a complete YAML document and return parsed configuration and structured issues, without saving.", false, func(b bridge, ctx *gofr.Context, a sourceArgs) (any, error) {
		d, err := b.documents(a.Kind)
		if err != nil {
			return nil, err
		}
		return d.Validate(ctx, a.Source)
	}),
	author("save_draft", "Create or update a document's YAML draft without publishing it.", false, func(b bridge, ctx *gofr.Context, a draftArgs) (any, error) {
		d, err := b.document(documentArgs{a.Kind, a.Name})
		if err != nil {
			return nil, err
		}
		return d.SaveDraft(ctx, a.Name, a.Source)
	}),
	author("discard_draft", "Discard a saved draft; an unpublished document is removed.", true, func(b bridge, ctx *gofr.Context, a documentArgs) (any, error) {
		d, err := b.document(a)
		if err != nil {
			return nil, err
		}
		return nil, d.DiscardDraft(ctx, a.Name)
	}),
	author("publish_document", "Publish a saved draft as a numbered version. Preview impact first; explicitly acknowledge breaking changes when applicable.", true, func(b bridge, ctx *gofr.Context, a publishArgs) (any, error) {
		d, err := b.document(documentArgs{a.Kind, a.Name})
		if err != nil {
			return nil, err
		}
		return d.Publish(ctx, a.Name, a.AcknowledgeBreaking)
	}),
	author("delete_document", "Delete a document and dependent data allowed by its API. Requires repeating its identifier in confirm.", true, func(b bridge, ctx *gofr.Context, a deleteArgs) (any, error) {
		d, err := b.document(documentArgs{a.Kind, a.Name})
		if err != nil {
			return nil, err
		}
		return nil, d.Delete(ctx, a.Name, a.Confirm)
	}),
	author("apply_documents", "Validate multi-document YAML as a set. Defaults to dryRun=true; explicitly set false to save drafts atomically.", false, func(b bridge, ctx *gofr.Context, a applyArgs) (any, error) {
		dry := true
		if a.DryRun != nil {
			dry = *a.DryRun
		}
		return b.services.Apply(ctx, a.Source, dry)
	}),
	author("publish_documents", "Publish multiple saved drafts atomically in dependency order after reviewing their impacts.", true, func(b bridge, ctx *gofr.Context, a publishManyArgs) (any, error) {
		refs := make([]configdoc.DocumentRef, 0, len(a.Documents))
		for _, d := range a.Documents {
			refs = append(refs, configdoc.DocumentRef{Kind: d.Kind, Name: d.Name})
		}
		return b.services.PublishMany(ctx, refs, a.AcknowledgeBreaking)
	}),
	author("sync_integration", "Sync a published integration that pulls now, completely or incrementally, using its declared mappings.", true, func(b bridge, ctx *gofr.Context, a syncArgs) (any, error) {
		return b.services.Sync(ctx, a.Name, a.Mode)
	}),
	author("check_integration", "Check a published integration's connector can reach its system with the current config and secrets, or the draft's when it isn't published.", false, func(b bridge, ctx *gofr.Context, a nameArgs) (any, error) {
		return b.services.CheckIntegration(ctx, a.Name)
	}),
	author("evaluate_scorecard", "Evaluate a published scorecard now and store its current results.", false, func(b bridge, ctx *gofr.Context, a nameArgs) (any, error) {
		return b.services.EvaluateScorecard(ctx, a.Name)
	}),
	author("create_ingestion_token", "Create an ingestion token for an integration that takes pushes. Its secret is in this response only; hand it to the pushing system now.", false, func(b bridge, ctx *gofr.Context, a tokenArgs) (any, error) {
		return b.services.CreateIngestionToken(ctx, a.Integration, a.Name)
	}),
	author("revoke_ingestion_token", "Revoke an integration's ingestion token; systems pushing with it stop being accepted.", true, func(b bridge, ctx *gofr.Context, a revokeTokenArgs) (any, error) {
		return nil, b.services.RevokeIngestionToken(ctx, a.Integration, a.ID)
	}),
}

// document is the service for the document a tool names.
func (b bridge) document(a documentArgs) (configdoc.Documents, error) {
	if a.Name == "" {
		return nil, badArgs("name a document")
	}
	return b.documents(a.Kind)
}

// badArgs says what's wrong with a tool's arguments.
func badArgs(message string) error {
	return httpx.Error{Code: http.StatusBadRequest, Message: message}
}

func (a documentArgs) resourceLink() *mcp.ResourceLink { return documentLink(a.Kind, a.Name) }

func (a entityArgs) resourceLink() *mcp.ResourceLink {
	return link(entityURI(a.Blueprint, a.Identifier), a.Blueprint+"/"+a.Identifier, "application/json")
}

const authoringHelp = `Documents are YAML with kind (Blueprint, Integration, Scorecard, Dashboard, Action or Workflow), metadata and spec. Every field, its allowed values and what it means are in each kind's JSON Schema: read portal://schemas/<kind> or get_document_schema before writing one. Names are immutable URL identifiers. Blueprints come first: integrations fill them with entities, scorecards grade those entities, dashboards summarize them and actions run steps on one entity at a time. Look at existing documents of the same kind for conventions.

Workflow: search or list/read existing documents; validate proposed YAML; save_draft or apply_documents(dryRun=true) then apply_documents(dryRun=false); preview_impact; publish_document or publish_documents in one atomic batch; inspect catalog/entities and sync runs. Validation returns issues with severity/path/line. An error blocks saving/publishing; warnings may refer to dependencies not published yet. Export includes only published configuration unless reading a draft explicitly through get_document.

Integrations map a connector's items, each one object of one of its resources or its deletion, into entities with JQ expressions; list_connectors says what each connector can do and reads. An http resource can be fetched from a url, pushed to the ingest API, or both, through the same mappings. A sync is complete, deleting what it didn't see, or incremental, fetching what changed since {{ $since }} and deleting only what's marked deleted; sync.complete sets how often a sync is complete. Secrets use {env: GP_SECRET_*}; never put secret values in configuration. Pushes need an ingestion token, which create_ingestion_token shows once, or a signature the integration checks; only a push with a token can be complete (?mode=complete).

An action's steps call URLs, write entities as an integration that doesn't pull, check the catalog, sync an integration that pulls after changing its source, and set what the run shows, reading $input, $entity, $me, $run and $step references. Say who can run it with run, and put an approval step before anything that removes or disrupts things. A Workflow takes the same steps but no blueprint, and its action steps run an action on many entities, each checked against whoever asked. Both show under their group in the Self-Service catalog.

Tools follow the caller's role and the access they granted this client: members browse and run actions, builders also author. Permissions are checked again on every call.`
