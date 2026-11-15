package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/oauth"
)

// tool is one MCP tool and the scope a caller's token must carry to use
// it: portal:read to read, portal:run to run actions and portal:author to
// change configuration. Who may connect at all is the endpoint's to say,
// and the services check again on every call.
type tool struct {
	name        string
	description string
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
func define[Args any](name, description, scope string, readOnly, destructive bool, run func(bridge, *gofr.Context, Args) (any, error)) tool {
	return tool{
		name: name, description: description, scope: scope, readOnly: readOnly, destructive: destructive,
		register: func(s *mcp.Server, b bridge, t tool) {
			mcp.AddTool(s, &mcp.Tool{
				Name: t.name, Title: title(t.name), Description: t.description,
				Annotations: &mcp.ToolAnnotations{
					ReadOnlyHint: t.readOnly, DestructiveHint: &t.destructive,
					OpenWorldHint: boolPointer(t.name == "sync_connection" || t.name == "check_connection"),
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

// read, run and author define the three kinds of tool: reading, running
// actions as the caller, and changing configuration.
func read[Args any](name, description string, do func(bridge, *gofr.Context, Args) (any, error)) tool {
	return define(name, description, oauth.ScopeRead, true, false, do)
}

func run[Args any](name, description string, destructive bool, do func(bridge, *gofr.Context, Args) (any, error)) tool {
	return define(name, description, oauth.ScopeRun, false, destructive, do)
}

func author[Args any](name, description string, destructive bool, do func(bridge, *gofr.Context, Args) (any, error)) tool {
	return define(name, description, oauth.ScopeAuthor, false, destructive, do)
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

type (
	empty    struct{}
	kindArgs struct {
		Kind string `json:"kind" jsonschema:"Document collection: blueprints, connections, scorecards, dashboards, actions or workflows"`
	}
	documentArgs struct {
		Kind       string `json:"kind" jsonschema:"Document collection: blueprints, connections, scorecards, dashboards, actions or workflows"`
		Identifier string `json:"identifier"`
	}
	entityArgs struct {
		Blueprint  string `json:"blueprint"`
		Identifier string `json:"identifier" jsonschema:"The entity's identifier, which may contain slashes"`
	}
	identifierArgs struct {
		Identifier string `json:"identifier"`
	}
	searchArgs struct {
		Text string `json:"text" jsonschema:"Words in a title or identifier; empty lists blueprints, dashboards, scorecards and connections"`
	}
	catalogArgs struct {
		Blueprint string `json:"blueprint"`
		Tab       string `json:"tab,omitempty" jsonschema:"The tab to open, from tabs: one of the blueprint's own, entities, or scorecard-<identifier> for a scorecard's results. Omitted, the page's first tab, or entities when searching, filtering, sorting or paging"`
		tableArgs
	}
	scorecardArgs struct {
		Identifier string `json:"identifier" jsonschema:"The scorecard's identifier"`
		tableArgs
	}
	entityPageArgs struct {
		entityArgs
		Tab string `json:"tab,omitempty" jsonschema:"The tab to open, from tabs; omitted, the blueprint's overview tab when it has one"`
	}
	dashboardArgs struct {
		Identifier string              `json:"identifier,omitempty" jsonschema:"The dashboard's identifier; omitted, the one Home opens on"`
		Inputs     map[string][]string `json:"inputs,omitempty" jsonschema:"Values to set the dashboard's inputs to, by input identifier; an empty list clears one, and one left out takes its default"`
	}
)

// sharedTools read what both servers' callers need: the catalog's pages,
// as members see them and builders check their work in.
var sharedTools = []tool{
	read("search", "Find blueprints, entities, dashboards, scorecards and connections by title or identifier, closest first, as the portal's search box does.", func(b bridge, ctx *gofr.Context, a searchArgs) (any, error) {
		return b.services.Search(ctx, a.Text)
	}),
	read("list_blueprints", "Read the Catalog page: every kind of thing the catalog holds, as blueprints with their titles, descriptions and how many entities each has.", func(b bridge, ctx *gofr.Context, _ empty) (any, error) {
		return b.services.Catalog(ctx)
	}),
	read("get_catalog", "Read a blueprint's catalog page: the blueprint and what each of its properties means, its tabs, and the open tab: a tab of widgets with their numbers, the entities table (searched, filtered, sorted and paged), or a scorecard's results.", func(b bridge, ctx *gofr.Context, a catalogArgs) (any, error) {
		return readCatalog(b, ctx, a.Blueprint, a.Tab, a.tableArgs)
	}),
	withLinks(read("get_entity", "Read an entity's page: its properties and relations with titles, what relates to it, its scorecard results with each check, the actions the caller can run on it with their inputs, its latest runs, and its blueprint's tabs that apply to it, with one open showing its widgets' numbers.", func(b bridge, ctx *gofr.Context, a entityPageArgs) (any, error) {
		return readEntity(b, ctx, a.Blueprint, a.Identifier, a.Tab)
	})),
	read("get_scorecard", "Read a scorecard's results, as its tab on its blueprint's catalog page shows them: its tiers and checks, how many entities reach each tier and pass, fail or skip each check, and a page of entities' results, worst tier first unless sorted. Search and filter narrow both. evaluating is true while a publish's new version is still being evaluated, and evaluatedAt is when results were last evaluated.", func(b bridge, ctx *gofr.Context, a scorecardArgs) (any, error) {
		return readScorecard(b, ctx, a.Identifier, a.tableArgs)
	}),
	read("get_dashboard", "Read a dashboard whole, as Home shows it to the caller: its inputs and what each is set to, and every widget's title, what it measures and its numbers, or why they couldn't load.", func(b bridge, ctx *gofr.Context, a dashboardArgs) (any, error) {
		return readDashboard(b, ctx, a.Identifier, a.Inputs, false)
	}),
}

// document is the service for the document a tool names.
func (b bridge) document(a documentArgs) (configdoc.Documents, error) {
	if a.Identifier == "" {
		return nil, httpx.BadRequest("identify a document")
	}
	return b.documents(a.Kind)
}

func (a documentArgs) resourceLink() *mcp.ResourceLink { return documentLink(a.Kind, a.Identifier) }

func (a entityArgs) resourceLink() *mcp.ResourceLink {
	return link(entityURI(a.Blueprint, a.Identifier), a.Blueprint+"/"+a.Identifier, "application/json")
}
