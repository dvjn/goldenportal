package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/oauth"
)

// Resources are what a user attaches to a conversation, such as a
// document's YAML. They only read, so they need portal:read, and every
// read calls services as the caller, like a tool.
const (
	guideURI            = "portal://guide/authoring"
	documentTemplate    = "portal://documents/{kind}/{name}"
	entityTemplate      = "portal://entities/{blueprint}/{+identifier}"
	documentURIPrefix   = "portal://documents/"
	entityURIPrefix     = "portal://entities/"
	maxCompletionValues = 20
)

var documentKinds = []string{"blueprints", "integrations", "scorecards", "dashboards", "actions", "workflows"}

func schemaURI(kind string) string { return "portal://schemas/" + kind }

func documentURI(kind, name string) string { return documentURIPrefix + kind + "/" + name }

func entityURI(blueprint, identifier string) string {
	return entityURIPrefix + blueprint + "/" + identifier
}

func addResources(s *mcp.Server, b bridge) {
	s.AddResource(&mcp.Resource{
		URI: guideURI, Name: "authoring", Title: "Authoring guide", MIMEType: "text/plain",
		Description: "How configuration documents fit together and the steps to change them safely",
	}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return textResult(guideURI, "text/plain", authoringHelp), nil
	})
	for _, kind := range documentKinds {
		uri := schemaURI(kind)
		schema, _ := configdoc.Schema(kind)
		s.AddResource(&mcp.Resource{
			URI: uri, Name: kind + "-schema", Title: "Schema of " + kind, MIMEType: "application/schema+json",
			Description: "Every field of a " + strings.TrimSuffix(kind, "s") + " document, which are required, allowed values and what each means",
		}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return textResult(uri, "application/schema+json", string(schema)), nil
		})
	}
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: documentTemplate, Name: "document", Title: "Document", MIMEType: "application/yaml",
		Description: "A blueprint, integration, scorecard, dashboard, action or workflow as published, in YAML",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		kind, name, _ := strings.Cut(strings.TrimPrefix(uri, documentURIPrefix), "/")
		d, err := b.document(documentArgs{kind, name})
		if err != nil {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		out := b.run(ctx, func(g *gofr.Context) (any, error) {
			source, err := d.Export(g, name, false)
			return yaml(source), err
		})
		if err := readError(uri, out); err != nil {
			return nil, err
		}
		return textResult(uri, "application/yaml", out.Source), nil
	})
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: entityTemplate, Name: "entity", Title: "Entity", MIMEType: "application/json",
		Description: "A catalog entity with its resolved properties and relations",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		blueprint, identifier, _ := strings.Cut(strings.TrimPrefix(uri, entityURIPrefix), "/")
		if blueprint == "" || identifier == "" {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		out := b.run(ctx, func(g *gofr.Context) (any, error) { return b.services.Entity(g, blueprint, identifier) })
		if err := readError(uri, out); err != nil {
			return nil, err
		}
		raw, err := json.MarshalIndent(out.Data, "", "  ")
		if err != nil {
			return nil, err
		}
		return textResult(uri, "application/json", string(raw)), nil
	})
}

func textResult(uri, mimeType, text string) *mcp.ReadResourceResult {
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: mimeType, Text: text}}}
}

// readError turns a failed read into the error the spec asks for:
// what the caller can't see is as missing as what doesn't exist.
func readError(uri string, out Output) error {
	switch {
	case out.Status < 400:
		return nil
	case out.Status == http.StatusNotFound || out.Status == http.StatusForbidden:
		return mcp.ResourceNotFoundError(uri)
	default:
		return &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: apiMessage(out)}
	}
}

func apiMessage(out Output) string {
	if e, ok := out.Error.(map[string]any); ok {
		if message, ok := e["message"].(string); ok {
			return message
		}
	}
	return http.StatusText(out.Status)
}

// complete suggests values for resource and prompt arguments from what the
// caller can see, so a client can offer them as the user types.
func complete(b bridge, c caller) func(context.Context, *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	return func(ctx context.Context, req *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
		values := []string{}
		if slices.Contains(c.scopes, oauth.ScopeRead) {
			var known map[string]string
			if req.Params.Context != nil {
				known = req.Params.Context.Arguments
			}
			values = b.suggest(ctx, req.Params.Argument.Name, req.Params.Argument.Value, known)
		}
		more := len(values) > maxCompletionValues
		if more {
			values = values[:maxCompletionValues]
		}
		return &mcp.CompleteResult{Completion: mcp.CompletionResultDetails{Values: values, HasMore: more}}, nil
	}
}

// suggest lists values for an argument, by its name, that start with
// typed. Arguments mean the same in every resource and prompt.
func (b bridge) suggest(ctx context.Context, argument, typed string, known map[string]string) []string {
	var candidates []string
	switch argument {
	case "kind":
		candidates = documentKinds
	case "name":
		candidates = b.identifiers(ctx, known["kind"])
	case "blueprint":
		candidates = b.identifiers(ctx, "blueprints")
	case "scorecard":
		candidates = b.identifiers(ctx, "scorecards")
	case "connector":
		connectors, _ := quietly(b, ctx, b.services.Connectors)
		for _, c := range connectors {
			candidates = append(candidates, c.ID)
		}
	case "identifier":
		// Entity search matches anywhere in a title or identifier, which is
		// what someone typing part of a name wants, so its results stand.
		page, _ := quietly(b, ctx, func(g *gofr.Context) (entity.Page, error) {
			return b.services.Entities(g, known["blueprint"], entity.ListRequest{Search: typed, Size: 20})
		})
		var values []string
		for _, e := range page.Entities {
			values = append(values, e.Identifier)
		}
		return values
	}
	var values []string
	for _, candidate := range candidates {
		if strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(typed)) {
			values = append(values, candidate)
		}
	}
	return values
}

func (b bridge) identifiers(ctx context.Context, kind string) []string {
	d, err := b.documents(kind)
	if err != nil {
		return nil
	}
	summaries, _ := quietly(b, ctx, d.List)
	var values []string
	for _, s := range summaries {
		values = append(values, s.Identifier)
	}
	return values
}

// quietly calls a service as the caller for a suggestion, which is
// nothing when the call fails.
func quietly[T any](b bridge, ctx context.Context, do func(*gofr.Context) (T, error)) (T, bool) {
	v, err := do(b.context(ctx))
	return v, err == nil
}

// link points a tool's result at the resource it read, so a client can
// attach it without another call.
func link(uri, name, mimeType string) *mcp.ResourceLink {
	return &mcp.ResourceLink{URI: uri, Name: name, MIMEType: mimeType}
}

func documentLink(kind, name string) *mcp.ResourceLink {
	return link(documentURI(kind, name), fmt.Sprintf("%s/%s", kind, name), "application/yaml")
}
