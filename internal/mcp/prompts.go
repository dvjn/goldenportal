package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/oauth"
)

// prompt is a task a user starts, such as a slash command, offered like
// tools: to those whose role and token allow what it asks the agent to do.
type prompt struct {
	prompt  *mcp.Prompt
	role    auth.Role
	scope   string
	handler func(bridge) mcp.PromptHandler
}

// args are a prompt's arguments, as the user gave them.
type args map[string]string

func argument(name, description string, required bool) *mcp.PromptArgument {
	return &mcp.PromptArgument{Name: name, Description: description, Required: required}
}

// authoring is a prompt for building configuration: the task, with the
// guide and the kind's schema attached, and the steps that keep publishing in the user's hands.
func authoring(name, title, description, kind string, arguments []*mcp.PromptArgument, task func(args) string) prompt {
	return prompt{
		prompt: &mcp.Prompt{Name: name, Title: title, Description: description, Arguments: arguments},
		role:   auth.RoleBuilder, scope: oauth.ScopeAuthor,
		handler: func(bridge) mcp.PromptHandler {
			schema, _ := configdoc.Schema(kind)
			return func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				a, err := required(req, arguments)
				if err != nil {
					return nil, err
				}
				return &mcp.GetPromptResult{Description: description, Messages: []*mcp.PromptMessage{
					text(task(a) + "\n\n" + authoringSteps),
					embed(guideURI, "text/plain", authoringHelp),
					embed(schemaURI(kind), "application/schema+json", string(schema)),
				}}, nil
			}
		},
	}
}

const authoringSteps = `Validate each document with validate_document and fix every error, then save_draft and preview_impact. Show me the YAML and the impact, and publish only once I agree.`

// reading is a prompt about what's in the catalog, for anyone who can read it.
func reading(name, title, description string, arguments []*mcp.PromptArgument, build func(context.Context, bridge, args) ([]*mcp.PromptMessage, error)) prompt {
	return prompt{
		prompt: &mcp.Prompt{Name: name, Title: title, Description: description, Arguments: arguments},
		role:   auth.RoleMember, scope: oauth.ScopeRead,
		handler: func(b bridge) mcp.PromptHandler {
			return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				a, err := required(req, arguments)
				if err != nil {
					return nil, err
				}
				messages, err := build(ctx, b, a)
				if err != nil {
					return nil, err
				}
				return &mcp.GetPromptResult{Description: description, Messages: messages}, nil
			}
		},
	}
}

var prompts = []prompt{
	authoring("add_blueprint", "Add a blueprint", "Model a new kind of thing in the catalog", "blueprints",
		[]*mcp.PromptArgument{argument("what", "What it catalogs, such as services or teams", true)},
		func(a args) string {
			return fmt.Sprintf("Add a blueprint to the catalog for %s.\n\n"+
				"First see what exists with search and list_catalog, so it relates to existing blueprints instead of repeating them. "+
				"Propose its properties and relations and agree them with me before writing it.", a["what"])
		}),
	authoring("connect_integration", "Connect an integration", "Bring entities into the catalog from a source", "integrations",
		[]*mcp.PromptArgument{
			argument("connector", "The connector to bring entities in with", true),
			argument("what", "What to bring in, such as repositories or deploys", false),
		},
		func(a args) string {
			task := "Connect an integration with the " + a["connector"] + " connector"
			if a["what"] != "" {
				task += " that brings in " + a["what"]
			}
			return task + ".\n\n" +
				"Read list_connectors for its configuration and resources, and map into existing blueprints, proposing any that are missing first. " +
				"Secrets go in GP_SECRET_* environment variables, never in the document. " +
				"A push integration needs an ingestion token once it's published: offer to create one."
		}),
	authoring("create_scorecard", "Create a scorecard", "Grade a blueprint's entities against standards", "scorecards",
		[]*mcp.PromptArgument{
			argument("blueprint", "The blueprint whose entities it grades", true),
			argument("goal", "What it measures, such as production readiness", false),
		},
		func(a args) string {
			task := "Create a scorecard for " + a["blueprint"] + " entities"
			if a["goal"] != "" {
				task += " that measures " + a["goal"]
			}
			return task + ".\n\n" +
				"Read the blueprint and query_entities first, so criteria use real properties and values entities can actually meet. " +
				"Propose tiers and criteria and agree them with me before writing it."
		}),
	authoring("build_dashboard", "Build a dashboard", "Show what matters on a dashboard", "dashboards",
		[]*mcp.PromptArgument{argument("goal", "What it shows, and for whom", true)},
		func(a args) string {
			return "Build a dashboard that shows " + a["goal"] + ".\n\n" +
				"See which blueprints and scorecards hold the data with list_catalog and search. " +
				"Before publishing, check it with view_dashboard and draft set."
		}),

	reading("explain_entity", "Explain an entity", "What an entity is, what it relates to and where it falls short",
		[]*mcp.PromptArgument{
			argument("blueprint", "The entity's blueprint", true),
			argument("identifier", "The entity's identifier", true),
		},
		func(ctx context.Context, b bridge, a args) ([]*mcp.PromptMessage, error) {
			entity, err := b.fetchJSON(ctx, entityArgs{a["blueprint"], a["identifier"]})
			if err != nil {
				return nil, err
			}
			scorecards := b.run(ctx, func(g *gofr.Context) (any, error) {
				return b.services.EntityScorecards(g, a["blueprint"], a["identifier"])
			})
			messages := []*mcp.PromptMessage{
				text(fmt.Sprintf("Explain %s %s: what it is, what it relates to, and where it falls short on its scorecards, "+
					"with what would fix each gap.", a["blueprint"], a["identifier"])),
				embed(entityURI(a["blueprint"], a["identifier"]), "application/json", entity),
			}
			if scorecards.Status < 400 {
				raw, _ := json.MarshalIndent(scorecards.Data, "", "  ")
				messages = append(messages, text("Its scorecard results:\n"+string(raw)))
			}
			return messages, nil
		}),
	reading("scorecard_gaps", "Find scorecard gaps", "Where entities fall short on a scorecard, and what would help most",
		[]*mcp.PromptArgument{argument("scorecard", "The scorecard to look into", true)},
		func(ctx context.Context, b bridge, a args) ([]*mcp.PromptMessage, error) {
			name := a["scorecard"]
			d, err := b.document(documentArgs{"scorecards", name})
			if err != nil {
				return nil, invalidParams(err.Error())
			}
			document := b.run(ctx, func(g *gofr.Context) (any, error) {
				source, err := d.Export(g, name, false)
				return yaml(source), err
			})
			if document.Status >= 400 {
				return nil, invalidParams("scorecard " + name + ": " + apiMessage(document))
			}
			summary := b.run(ctx, func(g *gofr.Context) (any, error) { return b.services.ScorecardSummary(g, name) })
			messages := []*mcp.PromptMessage{
				text(fmt.Sprintf("Explain where entities fall short on the %s scorecard: which criteria fail most, "+
					"and the changes that would lift the most entities a tier. Use list_scorecard_results for examples.", a["scorecard"])),
				embed(documentURI("scorecards", a["scorecard"]), "application/yaml", document.Source),
			}
			if summary.Status < 400 {
				raw, _ := json.MarshalIndent(summary.Data, "", "  ")
				messages = append(messages, text("How entities fare now:\n"+string(raw)))
			}
			return messages, nil
		}),
}

// required reads a prompt's arguments, refusing the request when one it
// needs is missing.
func required(req *mcp.GetPromptRequest, arguments []*mcp.PromptArgument) (args, error) {
	a := args{}
	for _, argument := range arguments {
		value := strings.TrimSpace(req.Params.Arguments[argument.Name])
		if value == "" && argument.Required {
			return nil, invalidParams("missing argument " + argument.Name)
		}
		a[argument.Name] = value
	}
	return a, nil
}

// fetchJSON reads an entity as indented JSON, for embedding.
func (b bridge) fetchJSON(ctx context.Context, a entityArgs) (string, error) {
	out := b.run(ctx, func(g *gofr.Context) (any, error) { return b.services.Entity(g, a.Blueprint, a.Identifier) })
	if out.Status >= 400 {
		return "", invalidParams(a.Blueprint + " " + a.Identifier + ": " + apiMessage(out))
	}
	raw, err := json.MarshalIndent(out.Data, "", "  ")
	return string(raw), err
}

func invalidParams(message string) error {
	return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: message}
}

func text(s string) *mcp.PromptMessage {
	return &mcp.PromptMessage{Role: "user", Content: &mcp.TextContent{Text: s}}
}

func embed(uri, mimeType, s string) *mcp.PromptMessage {
	return &mcp.PromptMessage{Role: "user", Content: &mcp.EmbeddedResource{
		Resource: &mcp.ResourceContents{URI: uri, MIMEType: mimeType, Text: s},
	}}
}
