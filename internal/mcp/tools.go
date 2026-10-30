package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type (
	empty    struct{}
	kindArgs struct {
		Kind string `json:"kind" jsonschema:"Document collection: blueprints, integrations, scorecards or dashboards"`
	}
	documentArgs struct {
		Kind string `json:"kind"`
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
)

func newServer(b bridge, p auth.Principal, write bool) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "goldenportal", Version: "1.0.0"}, &mcp.ServerOptions{Instructions: instructions})
	s.AddResource(&mcp.Resource{URI: "portal://authoring", Name: "authoring", MIMEType: "text/plain", Description: "Configuration envelope, authoring lifecycle and connector mapping conventions"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "portal://authoring", MIMEType: "text/plain", Text: authoringHelp}}}, nil
	})
	addTool(s, b, "list_documents", "List configuration documents of a kind visible to the caller.", true, false, func(a kindArgs) (apiCall, error) {
		path, err := documentPath(a.Kind, "")
		return apiCall{method: "GET", path: path}, err
	})
	addTool(s, b, "get_document", "Read a document's published configuration and, for builders, its draft.", true, false, func(a documentArgs) (apiCall, error) {
		path, err := requiredDocument(a)
		return apiCall{method: "GET", path: path}, err
	})
	addTool(s, b, "list_versions", "List a configuration document's published versions.", true, false, func(a documentArgs) (apiCall, error) {
		path, err := requiredDocument(a)
		return apiCall{method: "GET", path: path + "/versions"}, err
	})
	addTool(s, b, "get_version", "Read the exact source of a numbered document version.", true, false, func(a versionArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{a.Kind, a.Name})
		if a.Version < 1 {
			return apiCall{}, fmt.Errorf("version must be positive")
		}
		return apiCall{method: "GET", path: path + "/versions/" + strconv.Itoa(a.Version)}, err
	})
	addTool(s, b, "export_document", "Export one published document as YAML.", true, false, func(a documentArgs) (apiCall, error) {
		path, err := requiredDocument(a)
		return apiCall{method: "GET", path: path + "/export"}, err
	})
	addTool(s, b, "export_documents", "Export all published documents visible to the caller as multi-document YAML.", true, false, func(empty) (apiCall, error) { return apiCall{method: "GET", path: "/api/v1/export"}, nil })
	addTool(s, b, "list_connectors", "Discover available connector types and their readable resources.", true, false, func(empty) (apiCall, error) { return apiCall{method: "GET", path: "/api/v1/connectors"}, nil })
	addTool(s, b, "list_catalog", "List published blueprints with entity counts.", true, false, func(empty) (apiCall, error) { return apiCall{method: "GET", path: "/api/v1/catalog"}, nil })
	addTool(s, b, "query_entities", "Search, filter, sort and page entities of a blueprint. Filter uses the shared JSON filter tree.", true, false, func(a queryArgs) (apiCall, error) {
		if !configdoc.ValidIdentifier(a.Blueprint) {
			return apiCall{}, fmt.Errorf("choose a blueprint")
		}
		q := url.Values{"blueprint": {a.Blueprint}}
		for k, v := range map[string]string{"q": a.Search, "filter": a.Filter, "sort": a.Sort} {
			if v != "" {
				q.Set(k, v)
			}
		}
		if a.Page != 0 {
			q.Set("page", strconv.Itoa(a.Page))
		}
		if a.Size != 0 {
			q.Set("size", strconv.Itoa(a.Size))
		}
		return apiCall{method: "GET", path: "/api/v1/entities", query: q}, nil
	})
	addTool(s, b, "get_entity", "Read an entity, resolved properties and incoming relations. Identifiers may contain slashes.", true, false, func(a entityArgs) (apiCall, error) {
		if !configdoc.ValidIdentifier(a.Blueprint) || a.Identifier == "" {
			return apiCall{}, fmt.Errorf("choose a blueprint and entity identifier")
		}
		for _, segment := range strings.Split(a.Identifier, "/") {
			if segment == "." || segment == ".." || segment == "" || strings.Contains(segment, "\\") {
				return apiCall{}, fmt.Errorf("entity identifier cannot contain empty, dot or backslash path segments")
			}
		}
		return apiCall{method: "GET", path: "/api/v1/entities/" + a.Blueprint + "/" + a.Identifier}, nil
	})
	addTool(s, b, "get_home", "Read the caller's visible dashboards, rendered text and resolved filter defaults.", true, false, func(empty) (apiCall, error) { return apiCall{method: "GET", path: "/api/v1/home"}, nil })
	addTool(s, b, "get_entity_scorecards", "Read an entity's current scorecard results.", true, false, func(a entityArgs) (apiCall, error) {
		return apiCall{method: "GET", path: "/api/v1/entity-scorecards", query: url.Values{"blueprint": {a.Blueprint}, "identifier": {a.Identifier}}}, nil
	})
	if !p.Can(auth.RoleBuilder) {
		return s
	}
	addTool(s, b, "preview_impact", "Preview the impact of publishing a saved draft, including affected entities and breaking changes.", true, false, func(a documentArgs) (apiCall, error) {
		path, err := requiredDocument(a)
		return apiCall{method: "GET", path: path + "/draft/impact"}, err
	})
	addTool(s, b, "list_sync_runs", "Read recent integration runs and item errors.", true, false, func(a nameArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"integrations", a.Name})
		return apiCall{method: "GET", path: path + "/runs"}, err
	})
	if !write {
		return s
	}
	addTool(s, b, "validate_document", "Validate a complete YAML document and return parsed configuration and structured issues, without saving.", false, false, func(a sourceArgs) (apiCall, error) {
		path, err := documentPath(a.Kind, "")
		return apiCall{method: "POST", path: path + "/validate", body: map[string]any{"source": a.Source}}, err
	})
	addTool(s, b, "save_draft", "Create or update a document's YAML draft without publishing it.", false, false, func(a draftArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{a.Kind, a.Name})
		return apiCall{method: "PUT", path: path + "/draft", body: map[string]any{"source": a.Source}}, err
	})
	addTool(s, b, "discard_draft", "Discard a saved draft; an unpublished document is removed.", false, true, func(a documentArgs) (apiCall, error) {
		path, err := requiredDocument(a)
		return apiCall{method: "DELETE", path: path + "/draft"}, err
	})
	addTool(s, b, "publish_document", "Publish a saved draft as a numbered version. Preview impact first; explicitly acknowledge breaking changes when applicable.", false, true, func(a publishArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{a.Kind, a.Name})
		return apiCall{method: "POST", path: path + "/draft/publish", body: map[string]any{"acknowledgeBreaking": a.AcknowledgeBreaking}}, err
	})
	addTool(s, b, "delete_document", "Delete a document and dependent data allowed by its API. Requires repeating its identifier in confirm.", false, true, func(a deleteArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{a.Kind, a.Name})
		if a.Confirm != a.Name {
			return apiCall{}, fmt.Errorf("confirm must repeat the document name")
		}
		return apiCall{method: "DELETE", path: path, query: url.Values{"confirm": {a.Confirm}}}, err
	})
	addTool(s, b, "apply_documents", "Validate multi-document YAML as a set. Defaults to dryRun=true; explicitly set false to save drafts atomically.", false, false, func(a applyArgs) (apiCall, error) {
		dry := true
		if a.DryRun != nil {
			dry = *a.DryRun
		}
		return apiCall{method: "POST", path: "/api/v1/apply", query: url.Values{"dryRun": {strconv.FormatBool(dry)}}, body: map[string]any{"source": a.Source}}, nil
	})
	addTool(s, b, "publish_documents", "Publish multiple saved drafts atomically in dependency order after reviewing their impacts.", false, true, func(a publishManyArgs) (apiCall, error) {
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
		return apiCall{method: "POST", path: "/api/v1/publish", body: a}, nil
	})
	addTool(s, b, "sync_integration", "Run a published pull integration now using its declared mappings.", false, true, func(a nameArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"integrations", a.Name})
		return apiCall{method: "POST", path: path + "/sync"}, err
	})
	addTool(s, b, "evaluate_scorecard", "Evaluate a published scorecard now and store its current results.", false, false, func(a nameArgs) (apiCall, error) {
		path, err := requiredDocument(documentArgs{"scorecards", a.Name})
		return apiCall{method: "POST", path: path + "/evaluate"}, err
	})
	return s
}

func requiredDocument(a documentArgs) (string, error) {
	if a.Name == "" {
		return "", fmt.Errorf("name a document")
	}
	return documentPath(a.Kind, a.Name)
}

const authoringHelp = `Documents use kind: Blueprint|Integration|Scorecard|Dashboard, metadata: {name, title, description?, icon?}, spec: {...}. No apiVersion, labels or status. Names are immutable URL identifiers. A Blueprint has an ordered properties map; a property is stored (JSON Schema type/format), relation: <blueprint> with optional many, lookup: relation.property, or compute: <jq> with type. A Dashboard has ordered widgets and optional filters, visibility and defaultFor. Widgets: stat, bar, donut, table, scorecard, markdown, properties (entity pages only). Home widgets name a blueprint. Use the published documents and connector resources as examples; don't invent built-in schemas.

Workflow: list/read existing documents; validate proposed YAML; save_draft or apply_documents(dryRun=true) then apply_documents(dryRun=false); preview_impact; publish_document or publish_documents in one atomic batch; inspect catalog/entities and sync runs. Validation returns issues with severity/path/line. An error blocks saving/publishing; warnings may refer to dependencies not published yet. Export includes only published configuration unless reading a draft explicitly through get_document.

Integrations declare connector: {type, config}, optional sync: {interval}, and mappings: [{resource?, blueprint, items?, filter?, entity: {identifier, title?, properties}}]. Expressions are JQ. Push payloads and HTTP pages use items selectors; GitHub/GitLab resources supply one source object per mapping input (items defaults to .). Source objects preserve provider fields and children include _gp parent references. Secrets use {env: GP_SECRET_*}; never put secret values in configuration. Read list_connectors for resources. GitHub/GitLab maxRuns bounds recent CI snapshots per repository/project.

API permissions apply to every call. Builder/admin roles author configuration; members browse. Read-only API tokens can use read tools only. Dashboard visibility also filters member reads and exports. MCP transport POST is not permission to mutate data. Tool outputs contain HTTP status and structured data/error, or source for YAML exports.`
