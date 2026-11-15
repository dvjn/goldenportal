package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/oauth"
	"github.com/dvjn/goldenportal/internal/scorecard"
)

// prompt is a task a user starts, such as a slash command, offered like
// tools: to those whose token allows what it asks the agent to do.
type prompt struct {
	prompt  *mcp.Prompt
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
		scope:  oauth.ScopeAuthor,
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
		scope:  oauth.ScopeRead,
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

var authoringPrompts = []prompt{
	authoring("add_blueprint", "Add a blueprint", "Model a new kind of thing in the catalog", "blueprints",
		[]*mcp.PromptArgument{argument("what", "What it catalogs, such as services or teams", true)},
		func(a args) string {
			return fmt.Sprintf("Add a blueprint to the catalog for %s.\n\n"+
				"First see what exists with search and list_blueprints, so it relates to existing blueprints instead of repeating them. "+
				"Propose its properties and relations and agree them with me before writing it.", a["what"])
		}),
	authoring("add_connection", "Add a connection", "Bring entities into the catalog from a source", "connections",
		[]*mcp.PromptArgument{
			argument("connector", "The connector to bring entities in with", true),
			argument("what", "What to bring in, such as repositories or deploys", false),
		},
		func(a args) string {
			task := "Add a connection with the " + a["connector"] + " connector"
			if a["what"] != "" {
				task += " that brings in " + a["what"]
			}
			return task + ".\n\n" +
				"Read list_connectors for what it can do and its resources. " +
				"Ask whether the source is fetched, sends its changes, or both, and whether a sync should be complete every time or incremental between complete ones. " +
				"Secrets go in GP_SECRET_* environment variables, never in the document. " +
				"Once it's published, offer to add sources to existing blueprints that map its resources into their entities, proposing any blueprints that are missing; until then it reads nothing. " +
				"A connection that takes pushes needs an ingestion token or a signature: offer to create a token, and check_connection once it's published."
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
				"Read the blueprint and its entities with get_catalog first, so criteria use real properties and values entities can actually meet. " +
				"Each check and when is a filter, as get_catalog takes them; logic a filter can't express belongs in a computed property on the blueprint, which the check then reads. " +
				"Propose tiers and criteria and agree them with me before writing it."
		}),
	authoring("build_dashboard", "Build a dashboard", "Show what matters on a dashboard", "dashboards",
		[]*mcp.PromptArgument{argument("goal", "What it shows, and for whom", true)},
		func(a args) string {
			return "Build a dashboard that shows " + a["goal"] + ".\n\n" +
				"See which blueprints and scorecards hold the data with list_blueprints and search. " +
				"Widgets never load entities to work on: every number is one the database works out, a count, distinct, sum, avg, min, max, percentile or share, and min or max of a date shows its age. " +
				"A table column shows a path, formats its value with jq in format, or shows whether each row meets a filter; a row link reads the row's paths, such as {{ url }}. " +
				"Logic a filter can't express belongs in a computed property on the blueprint. " +
				"Before publishing, check it with preview_dashboard."
		}),
}

var readingPrompts = []prompt{
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
			summary := b.run(ctx, func(g *gofr.Context) (any, error) {
				return b.services.ScorecardSummary(g, name, scorecard.SummaryRequest{})
			})
			if summary.Status >= 400 {
				return nil, invalidParams("scorecard " + name + ": " + apiMessage(summary))
			}
			// Inline, since its tiers and criteria come with it and no
			// resource of this server holds them.
			raw, _ := json.MarshalIndent(summary.Data, "", "  ")
			messages := []*mcp.PromptMessage{
				text(fmt.Sprintf("Explain where entities fall short on the %s scorecard: which criteria fail most, "+
					"and the changes that would lift the most entities a tier. Use get_scorecard for examples.", name)),
				text("The scorecard, and how entities fare on it now:\n" + string(raw)),
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
