package mcp

import (
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/oauth"
)

// builderServer serves builders shaping the portal: modelling blueprints
// and their sources, connecting systems, and authoring scorecards and
// dashboards, through drafts they check before publishing.
var builderServer = server{
	endpoint: oauth.BuilderMCP, name: "goldenportal-builder", title: "Golden Portal Builder", documents: true,
	instructions: builderInstructions,
	tools:        append(append([]tool{}, sharedTools...), builderTools...),
	prompts:      authoringPrompts,
	resources:    addBuilderResources,
}

const builderInstructions = `Golden Portal is the organization's software catalog. Blueprints define kinds of things, such as services, teams or repositories; entities are the things themselves, which connections bring in from other systems through each blueprint's sources. Scorecards grade entities against standards in tiers, dashboards summarize them, and actions and workflows run steps for people.

You help a builder change this configuration. Every part of it is a YAML document: read portal://guide/authoring and the kind's schema with get_document_schema before writing one, see what exists with list_documents and get_document, and list_connectors for what connections can read. Configure connections first, then blueprints and their sources, then scorecards and dashboards, and never invent blueprints that don't exist. Validate each document with validate_document, save it as a draft, preview_impact, and publish only when the builder agrees; apply_documents validates or saves many at once, and publish_documents publishes drafts together in dependency order.

Check the work as members will see it: get_catalog and get_entity show what sources brought in, get_scorecard a scorecard's results, get_dashboard a published dashboard and preview_dashboard a draft. check_connection tests a connection's access, sync_connection pulls now and list_sync_runs says how syncs went; get_blueprint_usage shows what depends on a blueprint. evaluate_scorecard re-evaluates a scorecard now.`

type (
	sourceArgs struct {
		Kind   string `json:"kind"`
		Source string `json:"source" jsonschema:"Complete YAML document with kind, metadata and spec"`
	}
	draftArgs struct {
		Kind       string `json:"kind"`
		Identifier string `json:"identifier"`
		Source     string `json:"source"`
	}
	versionArgs struct {
		Kind       string `json:"kind"`
		Identifier string `json:"identifier"`
		Version    int    `json:"version"`
	}
	publishArgs struct {
		Kind                string `json:"kind"`
		Identifier          string `json:"identifier"`
		AcknowledgeBreaking bool   `json:"acknowledgeBreaking,omitempty"`
	}
	deleteArgs struct {
		Kind       string `json:"kind"`
		Identifier string `json:"identifier"`
		Confirm    string `json:"confirm" jsonschema:"Repeat the exact document identifier to confirm deletion"`
	}
	applyArgs struct {
		Source string `json:"source"`
		DryRun *bool  `json:"dryRun,omitempty" jsonschema:"Defaults to true; set false explicitly to save drafts"`
	}
	documentRef struct {
		Kind       string `json:"kind" jsonschema:"Blueprint, Connection, Scorecard, Dashboard, Action or Workflow"`
		Identifier string `json:"identifier"`
	}
	syncArgs struct {
		Identifier string `json:"identifier"`
		Mode       string `json:"mode,omitempty" jsonschema:"complete deletes what neither it nor the complete run before it saw; incremental fetches what changed and deletes only what the source says was deleted. Left out, the connection's schedule decides"`
	}
	publishManyArgs struct {
		Documents           []documentRef `json:"documents"`
		AcknowledgeBreaking bool          `json:"acknowledgeBreaking,omitempty"`
	}
	tokenArgs struct {
		Identifier string `json:"identifier" jsonschema:"The connection's identifier"`
		Label      string `json:"name" jsonschema:"What the token is for, such as the system that pushes with it"`
	}
	revokeTokenArgs struct {
		Identifier string `json:"identifier" jsonschema:"The connection's identifier"`
		ID         string `json:"id" jsonschema:"The token's id, from list_ingestion_tokens"`
	}
)

const reevaluates = "Publishing a scorecard, or a blueprint one scores, re-evaluates its results shortly after rather than with the publish: get_scorecard says evaluating: true until a published scorecard's results are for its new version, and evaluatedAt when they were last evaluated."

var builderTools = []tool{
	read("list_documents", "List configuration documents of a kind, each with whether it's published and has a draft.", func(b bridge, ctx *gofr.Context, a kindArgs) (any, error) {
		d, err := b.documents(a.Kind)
		if err != nil {
			return nil, err
		}
		return d.List(ctx)
	}),
	withLinks(read("get_document", "Read a document's published configuration and its draft.", func(b bridge, ctx *gofr.Context, a documentArgs) (any, error) {
		d, err := b.document(a)
		if err != nil {
			return nil, err
		}
		return d.Get(ctx, a.Identifier)
	})),
	read("get_document_schema", "Read the JSON Schema of a document kind: every field, which are required, allowed values and what each means. Read it before writing a document of that kind.", func(b bridge, ctx *gofr.Context, a kindArgs) (any, error) {
		if _, err := b.documents(a.Kind); err != nil {
			return nil, err
		}
		return b.services.Schema(ctx, a.Kind)
	}),
	read("list_versions", "List a configuration document's published versions.", func(b bridge, ctx *gofr.Context, a documentArgs) (any, error) {
		d, err := b.document(a)
		if err != nil {
			return nil, err
		}
		return d.Versions(ctx, a.Identifier)
	}),
	read("get_version", "Read the exact source of a numbered document version.", func(b bridge, ctx *gofr.Context, a versionArgs) (any, error) {
		d, err := b.document(documentArgs{a.Kind, a.Identifier})
		if err != nil {
			return nil, err
		}
		if a.Version < 1 {
			return nil, httpx.BadRequest("version must be positive")
		}
		return d.Version(ctx, a.Identifier, a.Version)
	}),
	withLinks(read("export_document", "Export one published document as YAML.", func(b bridge, ctx *gofr.Context, a documentArgs) (any, error) {
		d, err := b.document(a)
		if err != nil {
			return nil, err
		}
		source, err := d.Export(ctx, a.Identifier, false)
		return yaml(source), err
	})),
	read("export_documents", "Export every published document as multi-document YAML.", func(b bridge, ctx *gofr.Context, _ empty) (any, error) {
		source, err := b.services.ExportAll(ctx)
		return yaml(source), err
	}),
	read("list_connectors", "Discover available connector types and the resources each reads.", func(b bridge, ctx *gofr.Context, _ empty) (any, error) {
		return b.services.Connectors(ctx)
	}),
	read("get_blueprint_usage", "Read what uses a blueprint: its sources with each connection's last run, connections writing it, scorecards, dashboards and relations, with its entity count.", func(b bridge, ctx *gofr.Context, a identifierArgs) (any, error) {
		return b.services.BlueprintUsage(ctx, a.Identifier)
	}),
	read("preview_impact", "Preview the impact of publishing a saved draft, including affected entities and breaking changes.", func(b bridge, ctx *gofr.Context, a documentArgs) (any, error) {
		d, err := b.document(a)
		if err != nil {
			return nil, err
		}
		return d.Impact(ctx, a.Identifier)
	}),
	read("preview_dashboard", "Read a dashboard's draft as get_dashboard reads a published one: its inputs, and every widget's title, what it measures and its numbers, or why they couldn't load.", func(b bridge, ctx *gofr.Context, a dashboardArgs) (any, error) {
		if a.Identifier == "" {
			return nil, httpx.BadRequest("identify the dashboard")
		}
		return readDashboard(b, ctx, a.Identifier, a.Inputs, true)
	}),
	read("list_sync_runs", "Read a connection's recent sync runs and item errors.", func(b bridge, ctx *gofr.Context, a identifierArgs) (any, error) {
		return b.services.SyncRuns(ctx, a.Identifier)
	}),
	read("list_ingestion_tokens", "List a connection's ingestion tokens, by name and prefix; secrets are never shown again.", func(b bridge, ctx *gofr.Context, a identifierArgs) (any, error) {
		return b.services.IngestionTokens(ctx, a.Identifier)
	}),

	author("validate_document", "Validate a complete YAML document and return parsed configuration and structured issues, without saving.", false, func(b bridge, ctx *gofr.Context, a sourceArgs) (any, error) {
		d, err := b.documents(a.Kind)
		if err != nil {
			return nil, err
		}
		return d.Validate(ctx, a.Source)
	}),
	author("save_draft", "Create or update a document's YAML draft without publishing it.", false, func(b bridge, ctx *gofr.Context, a draftArgs) (any, error) {
		d, err := b.document(documentArgs{a.Kind, a.Identifier})
		if err != nil {
			return nil, err
		}
		return d.SaveDraft(ctx, a.Identifier, a.Source)
	}),
	author("discard_draft", "Discard a saved draft; an unpublished document is removed.", true, func(b bridge, ctx *gofr.Context, a documentArgs) (any, error) {
		d, err := b.document(a)
		if err != nil {
			return nil, err
		}
		return nil, d.DiscardDraft(ctx, a.Identifier)
	}),
	author("publish_document", "Publish a saved draft as a numbered version. Preview impact first; explicitly acknowledge breaking changes when applicable. "+reevaluates, true, func(b bridge, ctx *gofr.Context, a publishArgs) (any, error) {
		d, err := b.document(documentArgs{a.Kind, a.Identifier})
		if err != nil {
			return nil, err
		}
		return d.Publish(ctx, a.Identifier, a.AcknowledgeBreaking)
	}),
	author("delete_document", "Delete a document and dependent data allowed by its API. Requires repeating its identifier in confirm.", true, func(b bridge, ctx *gofr.Context, a deleteArgs) (any, error) {
		d, err := b.document(documentArgs{a.Kind, a.Identifier})
		if err != nil {
			return nil, err
		}
		return nil, d.Delete(ctx, a.Identifier, a.Confirm)
	}),
	author("apply_documents", "Validate multi-document YAML as a set. Defaults to dryRun=true; explicitly set false to save drafts atomically.", false, func(b bridge, ctx *gofr.Context, a applyArgs) (any, error) {
		dry := true
		if a.DryRun != nil {
			dry = *a.DryRun
		}
		return b.services.Apply(ctx, a.Source, dry)
	}),
	author("publish_documents", "Publish multiple saved drafts atomically in dependency order after reviewing their impacts. "+reevaluates, true, func(b bridge, ctx *gofr.Context, a publishManyArgs) (any, error) {
		refs := make([]configdoc.DocumentRef, 0, len(a.Documents))
		for _, d := range a.Documents {
			refs = append(refs, configdoc.DocumentRef{Kind: d.Kind, Identifier: d.Identifier})
		}
		return b.services.PublishMany(ctx, refs, a.AcknowledgeBreaking)
	}),
	author("sync_connection", "Start syncing a published connection that pulls now, completely or incrementally, reading what blueprints' sources map. It returns the run as soon as it starts, with finishedAt null; read its outcome later with list_sync_runs, which lists the newest runs first: the run with the same id has finished once finishedAt is set.", true, func(b bridge, ctx *gofr.Context, a syncArgs) (any, error) {
		return b.services.Sync(ctx, a.Identifier, a.Mode)
	}),
	author("check_connection", "Check a published connection's connector can reach its system with the current config and secrets, or the draft's when it isn't published.", false, func(b bridge, ctx *gofr.Context, a identifierArgs) (any, error) {
		return b.services.CheckConnection(ctx, a.Identifier)
	}),
	author("evaluate_scorecard", "Evaluate a published scorecard now and store its current results, after any evaluation of it already under way. Scorecards are otherwise evaluated on a schedule, hourly by default, and shortly after a publish.", false, func(b bridge, ctx *gofr.Context, a identifierArgs) (any, error) {
		return b.services.EvaluateScorecard(ctx, a.Identifier)
	}),
	author("create_ingestion_token", "Create an ingestion token for a connection that takes pushes. Its secret is in this response only; hand it to the pushing system now.", false, func(b bridge, ctx *gofr.Context, a tokenArgs) (any, error) {
		return b.services.CreateIngestionToken(ctx, a.Identifier, a.Label)
	}),
	author("revoke_ingestion_token", "Revoke a connection's ingestion token; systems pushing with it stop being accepted.", true, func(b bridge, ctx *gofr.Context, a revokeTokenArgs) (any, error) {
		return nil, b.services.RevokeIngestionToken(ctx, a.Identifier, a.ID)
	}),
}

const authoringHelp = `Documents are YAML with kind (Blueprint, Connection, Scorecard, Dashboard, Action or Workflow), metadata and spec. Every field, its allowed values and what it means are in each kind's JSON Schema: read portal://schemas/<kind> or get_document_schema before writing one. Some definitions carry examples to start from, such as a blueprint source of each resource a connector reads. A document's metadata.identifier is immutable and is its URL identifier. Connections come first, then blueprints, whose sources fill them with entities from connections; scorecards grade those entities, dashboards summarize them and actions run steps on one entity at a time. Look at existing documents of the same kind for conventions.

Workflow: search or list/read existing documents; validate proposed YAML; save_draft or apply_documents(dryRun=true) then apply_documents(dryRun=false); preview_impact; publish_document or publish_documents in one atomic batch; check the catalog with get_catalog and get_entity, and sync runs with list_sync_runs. Validation returns issues with severity/path/line. An error blocks saving/publishing; warnings may refer to dependencies not published yet. Export includes only published configuration unless reading a draft explicitly through get_document.

A connection's connector gives items, each one object of one of its resources or its deletion; each blueprint's sources map a connection's resource into its entities with JQ expressions; list_connectors says what each connector can do and which resources it reads. An http resource can be fetched from a url, pushed to the ingest API, or both, through the same sources. A sync is complete, deleting what neither it nor the complete run before it saw, or incremental, fetching what changed since {{ $since }} and deleting only what's marked deleted; sync.complete sets how often a sync is complete, for connectors that can fetch only what changed. Secrets use {env: GP_SECRET_*}; never put secret values in configuration. Pushes need an ingestion token, which create_ingestion_token shows once, or a signature the connection checks; only a push with a token can be complete (?mode=complete).

An action's steps call URLs, write entities as a connection that doesn't pull, check the catalog, sync a connection that pulls after changing its source, and set what the run shows, reading $input, $entity, $me, $run and $step references. Say who can run it with run, and put an approval step before anything that removes or disrupts things. A Workflow takes the same steps but no blueprint, and its action steps run an action on many entities, each checked against whoever asked. Both show under their group in the Self-Service catalog.

This server is for builders, and changing configuration needs the access the builder granted this client. Members use the portal's other MCP server, which reads its pages and runs actions. Permissions are checked again on every call.`
