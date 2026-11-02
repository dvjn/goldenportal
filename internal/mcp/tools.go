package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/oauth"
)

// tool is one MCP tool and who may use it: the role the caller needs now,
// and the scope their token must carry. Reading needs portal:read and
// authoring portal:author, which only builders can grant. A caller is only
// offered what they may use, and the API checks again on every call.
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

// define makes a tool that turns its arguments into one API call.
func define[Args any](name, description string, role auth.Role, scope string, readOnly, destructive bool, build func(Args) (apiCall, error)) tool {
	return tool{
		name: name, description: description, role: role, scope: scope, readOnly: readOnly, destructive: destructive,
		register: func(s *mcp.Server, b bridge, t tool) {
			mcp.AddTool(s, &mcp.Tool{
				Name: t.name, Title: title(t.name), Description: t.description,
				Annotations: &mcp.ToolAnnotations{
					ReadOnlyHint: t.readOnly, DestructiveHint: &t.destructive,
					OpenWorldHint: boolPointer(t.name == "sync_integration"),
				},
			}, func(ctx context.Context, _ *mcp.CallToolRequest, a Args) (*mcp.CallToolResult, Output, error) {
				call, err := build(a)
				if err != nil {
					return nil, Output{}, err
				}
				result, out, err := b.call(ctx, t, call)
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
func read[Args any](name, description string, role auth.Role, build func(Args) (apiCall, error)) tool {
	return define(name, description, role, oauth.ScopeRead, true, false, build)
}

func author[Args any](name, description string, destructive bool, build func(Args) (apiCall, error)) tool {
	return define(name, description, auth.RoleBuilder, oauth.ScopeAuthor, false, destructive, build)
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
		Kind string `json:"kind" jsonschema:"Document collection: blueprints, integrations, scorecards or dashboards"`
	}
	documentArgs struct {
		Kind string `json:"kind" jsonschema:"Document collection: blueprints, integrations, scorecards or dashboards"`
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
		Kind string `json:"kind" jsonschema:"Blueprint, Integration, Scorecard or Dashboard"`
		Name string `json:"name"`
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
	revokeTokenArgs struct {
		Integration string `json:"integration"`
		ID          string `json:"id" jsonschema:"The token's id, from list_ingestion_tokens"`
	}
)

var tools = []tool{
	read("search", "Find blueprints, entities, dashboards, scorecards and integrations by title or identifier, closest first.", auth.RoleMember, func(a searchArgs) (apiCall, error) {
		return apiCall{method: http.MethodGet, path: "search", query: url.Values{"q": {a.Text}}}, nil
	}),
	read("list_documents", "List configuration documents of a kind visible to the caller.", auth.RoleMember, func(a kindArgs) (apiCall, error) {
		path, err := documentPath(a.Kind, "")
		return apiCall{method: http.MethodGet, path: path}, err
	}),
	withLinks(read("get_document", "Read a document's published configuration and, for builders, its draft.", auth.RoleMember, func(a documentArgs) (apiCall, error) {
		path, err := requiredDocument(a)
		return apiCall{method: http.MethodGet, path: path}, err
	})),
	read("list_versions", "List a configuration document's published versions.", auth.RoleMember, func(a documentArgs) (apiCall, error) {
		path, err := requiredDocument(a)
		return apiCall{method: http.MethodGet, path: path + "/versions"}, err
	}),
	read("get_version", "Read the exact source of a numbered document version.", auth.RoleMember, func(a versionArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{a.Kind, a.Name})
		if a.Version < 1 {
			return apiCall{}, fmt.Errorf("version must be positive")
		}
		return apiCall{method: http.MethodGet, path: path + "/versions/" + strconv.Itoa(a.Version)}, err
	}),
	withLinks(read("export_document", "Export one published document as YAML.", auth.RoleMember, func(a documentArgs) (apiCall, error) {
		path, err := requiredDocument(a)
		return apiCall{method: http.MethodGet, path: path + "/export"}, err
	})),
	read("export_documents", "Export all published documents visible to the caller as multi-document YAML.", auth.RoleMember, func(empty) (apiCall, error) {
		return apiCall{method: http.MethodGet, path: "documents/export"}, nil
	}),
	read("get_document_schema", "Read the JSON Schema of a document kind: every field, which are required, allowed values and what each means. Read it before writing a document of that kind.", auth.RoleMember, func(a kindArgs) (apiCall, error) {
		_, err := documentPath(a.Kind, "")
		return apiCall{method: http.MethodGet, path: "schemas/" + a.Kind}, err
	}),
	read("list_connectors", "Discover available connector types and their readable resources.", auth.RoleMember, func(empty) (apiCall, error) {
		return apiCall{method: http.MethodGet, path: "connectors"}, nil
	}),
	read("list_catalog", "List published blueprints with entity counts.", auth.RoleMember, func(empty) (apiCall, error) {
		return apiCall{method: http.MethodGet, path: "catalog"}, nil
	}),
	read("query_entities", "Search, filter, sort and page entities of a blueprint. Filter uses the shared JSON filter tree.", auth.RoleMember, func(a queryArgs) (apiCall, error) {
		if !configdoc.ValidIdentifier(a.Blueprint) {
			return apiCall{}, fmt.Errorf("choose a blueprint")
		}
		q := url.Values{}
		for k, v := range map[string]string{"q": a.Search, "filter": a.Filter, "sort": a.Sort} {
			if v != "" {
				q.Set(k, v)
			}
		}
		setPage(q, a.Page, a.Size)
		return apiCall{method: http.MethodGet, path: "catalog/" + a.Blueprint, query: q}, nil
	}),
	withLinks(read("get_entity", "Read an entity, resolved properties and incoming relations. Identifiers may contain slashes.", auth.RoleMember, entityCall)),
	read("get_home", "Read the caller's visible dashboards, rendered text and resolved input defaults.", auth.RoleMember, func(empty) (apiCall, error) {
		return apiCall{method: http.MethodGet, path: "home"}, nil
	}),
	read("get_dashboard_data", "Read every widget's data on a dashboard the caller sees, with its inputs set as given or to their defaults.", auth.RoleMember, func(a dashboardDataArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"dashboards", a.Name})
		q := url.Values{}
		for input, values := range a.Inputs {
			if len(values) == 0 {
				q.Add("input."+input, "")
			}
			for _, value := range values {
				q.Add("input."+input, value)
			}
		}
		return apiCall{method: http.MethodGet, path: path + "/data", query: q}, err
	}),
	read("get_entity_scorecards", "Read an entity's current scorecard results.", auth.RoleMember, func(a entityArgs) (apiCall, error) {
		if !configdoc.ValidIdentifier(a.Blueprint) {
			return apiCall{}, fmt.Errorf("choose a blueprint")
		}
		return apiCall{method: http.MethodGet, path: "catalog/" + a.Blueprint + "/scorecards", query: url.Values{"identifier": {a.Identifier}}}, nil
	}),
	read("get_scorecard_summary", "Read how many entities reached each of a scorecard's tiers and how each criterion fares.", auth.RoleMember, func(a nameArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"scorecards", a.Name})
		return apiCall{method: http.MethodGet, path: path + "/summary"}, err
	}),
	read("list_scorecard_results", "Page a scorecard's entity results, worst tier first, optionally narrowed to some tiers.", auth.RoleMember, func(a resultsArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"scorecards", a.Name})
		q := url.Values{}
		if a.Tiers != "" {
			q.Set("tier", a.Tiers)
		}
		setPage(q, a.Page, a.Size)
		return apiCall{method: http.MethodGet, path: path + "/results", query: q}, err
	}),
	read("get_blueprint_usage", "Read what uses a blueprint: integrations writing it, scorecards, dashboards and relations, with its entity count.", auth.RoleMember, func(a nameArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"blueprints", a.Name})
		return apiCall{method: http.MethodGet, path: path + "/usage"}, err
	}),
	read("preview_impact", "Preview the impact of publishing a saved draft, including affected entities and breaking changes.", auth.RoleBuilder, func(a documentArgs) (apiCall, error) {
		path, err := requiredDocument(a)
		return apiCall{method: http.MethodGet, path: path + "/draft/impact"}, err
	}),
	read("view_dashboard", "Read a dashboard as Home would show it to the caller, from its draft when draft is true.", auth.RoleBuilder, func(a struct {
		Name  string `json:"name"`
		Draft bool   `json:"draft,omitempty"`
	},
	) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"dashboards", a.Name})
		q := url.Values{}
		if a.Draft {
			q.Set("draft", "true")
		}
		return apiCall{method: http.MethodGet, path: path + "/view", query: q}, err
	}),
	read("list_sync_runs", "Read recent integration runs and item errors.", auth.RoleBuilder, func(a nameArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"integrations", a.Name})
		return apiCall{method: http.MethodGet, path: path + "/runs"}, err
	}),
	read("list_ingestion_tokens", "List a push integration's ingestion tokens, by name and prefix; secrets are never shown again.", auth.RoleBuilder, func(a nameArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"integrations", a.Name})
		return apiCall{method: http.MethodGet, path: path + "/tokens"}, err
	}),

	author("validate_document", "Validate a complete YAML document and return parsed configuration and structured issues, without saving.", false, func(a sourceArgs) (apiCall, error) {
		path, err := documentPath(a.Kind, "")
		return apiCall{method: http.MethodPost, path: path + "/validate", body: map[string]any{"source": a.Source}}, err
	}),
	author("save_draft", "Create or update a document's YAML draft without publishing it.", false, func(a draftArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{a.Kind, a.Name})
		return apiCall{method: http.MethodPut, path: path + "/draft", body: map[string]any{"source": a.Source}}, err
	}),
	author("discard_draft", "Discard a saved draft; an unpublished document is removed.", true, func(a documentArgs) (apiCall, error) {
		path, err := requiredDocument(a)
		return apiCall{method: http.MethodDelete, path: path + "/draft"}, err
	}),
	author("publish_document", "Publish a saved draft as a numbered version. Preview impact first; explicitly acknowledge breaking changes when applicable.", true, func(a publishArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{a.Kind, a.Name})
		return apiCall{method: http.MethodPost, path: path + "/draft/publish", body: map[string]any{"acknowledgeBreaking": a.AcknowledgeBreaking}}, err
	}),
	author("delete_document", "Delete a document and dependent data allowed by its API. Requires repeating its identifier in confirm.", true, func(a deleteArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{a.Kind, a.Name})
		if a.Confirm != a.Name {
			return apiCall{}, fmt.Errorf("confirm must repeat the document name")
		}
		return apiCall{method: http.MethodDelete, path: path, query: url.Values{"confirm": {a.Confirm}}}, err
	}),
	author("apply_documents", "Validate multi-document YAML as a set. Defaults to dryRun=true; explicitly set false to save drafts atomically.", false, func(a applyArgs) (apiCall, error) {
		dry := true
		if a.DryRun != nil {
			dry = *a.DryRun
		}
		return apiCall{method: http.MethodPost, path: "documents/apply", query: url.Values{"dryRun": {strconv.FormatBool(dry)}}, body: map[string]any{"source": a.Source}}, nil
	}),
	author("publish_documents", "Publish multiple saved drafts atomically in dependency order after reviewing their impacts.", true, func(a publishManyArgs) (apiCall, error) {
		if len(a.Documents) == 0 {
			return apiCall{}, fmt.Errorf("name at least one draft")
		}
		for _, d := range a.Documents {
			if !configdoc.ValidIdentifier(d.Name) {
				return apiCall{}, fmt.Errorf("invalid document name")
			}
			switch d.Kind {
			case "Blueprint", "Integration", "Scorecard", "Dashboard":
			default:
				return apiCall{}, fmt.Errorf("document kind must be Blueprint, Integration, Scorecard or Dashboard")
			}
		}
		return apiCall{method: http.MethodPost, path: "documents/publish", body: a}, nil
	}),
	author("sync_integration", "Run a published pull integration now using its declared mappings.", true, func(a nameArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"integrations", a.Name})
		return apiCall{method: http.MethodPost, path: path + "/sync"}, err
	}),
	author("evaluate_scorecard", "Evaluate a published scorecard now and store its current results.", false, func(a nameArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"scorecards", a.Name})
		return apiCall{method: http.MethodPost, path: path + "/evaluate"}, err
	}),
	author("create_ingestion_token", "Create an ingestion token for a push integration. Its secret is in this response only; hand it to the pushing system now.", false, func(a tokenArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"integrations", a.Integration})
		return apiCall{method: http.MethodPost, path: path + "/tokens", body: map[string]any{"name": a.Name}}, err
	}),
	author("revoke_ingestion_token", "Revoke a push integration's ingestion token; systems pushing with it stop being accepted.", true, func(a revokeTokenArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"integrations", a.Integration})
		if _, uuidErr := uuid.Parse(a.ID); uuidErr != nil {
			return apiCall{}, fmt.Errorf("id must be a token id from list_ingestion_tokens")
		}
		return apiCall{method: http.MethodDelete, path: path + "/tokens/" + a.ID}, err
	}),
}

func entityCall(a entityArgs) (apiCall, error) {
	if !configdoc.ValidIdentifier(a.Blueprint) || a.Identifier == "" {
		return apiCall{}, fmt.Errorf("choose a blueprint and entity identifier")
	}
	for _, segment := range strings.Split(a.Identifier, "/") {
		if segment == "." || segment == ".." || segment == "" || strings.Contains(segment, "\\") {
			return apiCall{}, fmt.Errorf("entity identifier cannot contain empty, dot or backslash path segments")
		}
	}
	return apiCall{method: http.MethodGet, path: "catalog/" + a.Blueprint + "/entities/" + a.Identifier}, nil
}

func (a documentArgs) resourceLink() *mcp.ResourceLink { return documentLink(a.Kind, a.Name) }

func (a entityArgs) resourceLink() *mcp.ResourceLink {
	return link(entityURI(a.Blueprint, a.Identifier), a.Blueprint+"/"+a.Identifier, "application/json")
}

func setPage(q url.Values, page, size int) {
	if page != 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if size != 0 {
		q.Set("size", strconv.Itoa(size))
	}
}

func requiredDocument(a documentArgs) (string, error) {
	if a.Name == "" {
		return "", fmt.Errorf("name a document")
	}
	return documentPath(a.Kind, a.Name)
}

const authoringHelp = `Documents are YAML with kind (Blueprint, Integration, Scorecard or Dashboard), metadata and spec. Every field, its allowed values and what it means are in each kind's JSON Schema: read portal://schemas/<kind> or get_document_schema before writing one. Names are immutable URL identifiers. Blueprints come first: integrations fill them with entities, scorecards grade those entities and dashboards summarize them. Look at existing documents of the same kind for conventions.

Workflow: search or list/read existing documents; validate proposed YAML; save_draft or apply_documents(dryRun=true) then apply_documents(dryRun=false); preview_impact; publish_document or publish_documents in one atomic batch; inspect catalog/entities and sync runs. Validation returns issues with severity/path/line. An error blocks saving/publishing; warnings may refer to dependencies not published yet. Export includes only published configuration unless reading a draft explicitly through get_document.

Integrations map what a connector reads into entities with JQ expressions; list_connectors says what each connector reads. Secrets use {env: GP_SECRET_*}; never put secret values in configuration. A push integration needs an ingestion token: create_ingestion_token shows its secret once.

Tools follow the caller's role and the access they granted this client: members browse, builders also author. The API checks permissions again on every call.`
