package mcp

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestEachEndpointSaysWhatItsFor(t *testing.T) {
	server := testServer(t, newFake())
	user := connect(t, server, userPath, "builder").InitializeResult()
	builder := connect(t, server, builderPath, "builder").InitializeResult()
	if !strings.Contains(user.Instructions, "run_action") || !strings.Contains(user.Instructions, "/api/mcp/builder") ||
		strings.Contains(user.Instructions, "portal://guide/authoring") {
		t.Errorf("user instructions:\n%s", user.Instructions)
	}
	if !strings.Contains(builder.Instructions, "portal://guide/authoring") || strings.Contains(builder.Instructions, "run_action") {
		t.Errorf("builder instructions:\n%s", builder.Instructions)
	}
	if user.ServerInfo.Name == builder.ServerInfo.Name {
		t.Errorf("both servers are %s", user.ServerInfo.Name)
	}
}

func TestUserServerOffersEntities(t *testing.T) {
	session := connect(t, testServer(t, newFake()), userPath, "member")
	ctx := context.Background()
	templates, err := session.ListResourceTemplates(ctx, nil)
	if err != nil || len(templates.ResourceTemplates) != 1 || templates.ResourceTemplates[0].URITemplate != entityTemplate {
		t.Errorf("templates: %+v, %v", templates, err)
	}
	entity, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "portal://entities/service/acme/payments"})
	if err != nil || !strings.Contains(entity.Contents[0].Text, `"acme/payments"`) {
		t.Errorf("entity with a slash: %+v, %v", entity, err)
	}
}

func TestResourcesReadAsTheCaller(t *testing.T) {
	session := connect(t, testServer(t, newFake()), builderPath, "builder-reader")
	ctx := context.Background()

	doc, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "portal://documents/blueprints/service"})
	if err != nil || doc.Contents[0].Text != "kind: Blueprint\n" || doc.Contents[0].MIMEType != "application/yaml" {
		t.Errorf("document: %+v, %v", doc, err)
	}
	entity, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "portal://entities/service/acme/payments"})
	if err != nil || !strings.Contains(entity.Contents[0].Text, `"acme/payments"`) {
		t.Errorf("entity with a slash: %+v, %v", entity, err)
	}
	for _, uri := range []string{"portal://documents/blueprints/missing", "portal://documents/widgets/service"} {
		_, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		var rpc *jsonrpc.Error
		if !errors.As(err, &rpc) || rpc.Code != jsonrpc.CodeInvalidParams {
			t.Errorf("%s: want not found as invalid params, got %v", uri, err)
		}
	}

	schema, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "portal://schemas/dashboards"})
	if err != nil || !strings.Contains(schema.Contents[0].Text, `"$schema"`) {
		t.Errorf("schema: %v", err)
	}
	resources, err := session.ListResources(ctx, nil)
	if err != nil || len(resources.Resources) != 1+len(documentKinds) {
		t.Errorf("resources: %+v, %v", resources, err)
	}

	templates, err := session.ListResourceTemplates(ctx, nil)
	if err != nil || len(templates.ResourceTemplates) != 2 {
		t.Errorf("templates: %+v, %v", templates, err)
	}
}

func TestCompletionSuggestsWhatTheCallerSees(t *testing.T) {
	session := connect(t, testServer(t, newFake()), builderPath, "builder-reader")
	ctx := context.Background()
	resource := &mcp.CompleteReference{Type: "ref/resource", URI: documentTemplate}
	for _, test := range []struct {
		ref      *mcp.CompleteReference
		argument string
		typed    string
		known    map[string]string
		want     []string
	}{
		{resource, "kind", "s", nil, []string{"scorecards"}},
		{resource, "identifier", "se", map[string]string{"kind": "blueprints"}, []string{"service"}},
		{resource, "identifier", "", map[string]string{"kind": "widgets"}, nil},
		{&mcp.CompleteReference{Type: "ref/resource", URI: entityTemplate}, "identifier", "pay", map[string]string{"blueprint": "service"}, []string{"payments/pay", "billing"}},
		{&mcp.CompleteReference{Type: "ref/prompt", Name: "explain_entity"}, "blueprint", "t", nil, []string{"team"}},
	} {
		params := &mcp.CompleteParams{Ref: test.ref, Argument: mcp.CompleteParamsArgument{Name: test.argument, Value: test.typed}}
		if test.known != nil {
			params.Context = &mcp.CompleteContext{Arguments: test.known}
		}
		got, err := session.Complete(ctx, params)
		if err != nil || !slices.Equal(got.Completion.Values, test.want) {
			t.Errorf("%s %q: %+v, %v", test.argument, test.typed, got, err)
		}
	}
}

// The user's server serves no documents, so it suggests nothing only
// they take.
func TestUserServerCompletesWhatItServes(t *testing.T) {
	session := connect(t, testServer(t, newFake()), userPath, "member")
	ctx := context.Background()
	resource := &mcp.CompleteReference{Type: "ref/resource", URI: documentTemplate}
	for _, test := range []struct {
		ref      *mcp.CompleteReference
		argument string
		known    map[string]string
		want     []string
	}{
		{resource, "kind", nil, nil},
		{resource, "identifier", map[string]string{"kind": "blueprints"}, nil},
		{&mcp.CompleteReference{Type: "ref/prompt", Name: "add_connection"}, "connector", nil, nil},
		{&mcp.CompleteReference{Type: "ref/prompt", Name: "explain_entity"}, "blueprint", nil, []string{"service", "team"}},
	} {
		params := &mcp.CompleteParams{Ref: test.ref, Argument: mcp.CompleteParamsArgument{Name: test.argument}}
		if test.known != nil {
			params.Context = &mcp.CompleteContext{Arguments: test.known}
		}
		got, err := session.Complete(ctx, params)
		if err != nil || !slices.Equal(got.Completion.Values, test.want) {
			t.Errorf("%s: %+v, %v", test.argument, got, err)
		}
	}
}

func TestPromptsFollowEndpointAndScope(t *testing.T) {
	server := testServer(t, newFake())
	reads := []string{"explain_entity", "scorecard_gaps"}
	for _, test := range []struct {
		path, token string
		want        []string
	}{
		{userPath, "member", reads},
		{userPath, "builder", reads},
		{builderPath, "builder-reader", nil},
		{builderPath, "builder", []string{"add_blueprint", "build_dashboard", "add_connection", "create_scorecard"}},
	} {
		token, want := test.path+" "+test.token, test.want
		result, err := connect(t, server, test.path, test.token).ListPrompts(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, p := range result.Prompts {
			names = append(names, p.Name)
		}
		slices.Sort(names)
		slices.Sort(want)
		if !slices.Equal(names, want) {
			t.Errorf("%s prompts = %v, want %v", token, names, want)
		}
	}
}

func TestPromptsCarryWhatTheTaskNeeds(t *testing.T) {
	server := testServer(t, newFake())
	ctx := context.Background()
	builder := connect(t, server, builderPath, "builder")

	blueprint, err := builder.GetPrompt(ctx, &mcp.GetPromptParams{Name: "add_blueprint", Arguments: map[string]string{"what": "teams"}})
	if err != nil {
		t.Fatal(err)
	}
	task := blueprint.Messages[0].Content.(*mcp.TextContent).Text
	guide := blueprint.Messages[1].Content.(*mcp.EmbeddedResource).Resource
	schema := blueprint.Messages[2].Content.(*mcp.EmbeddedResource).Resource
	if !strings.Contains(task, "for teams") || !strings.Contains(task, "publish only once I agree") || guide.URI != guideURI ||
		schema.URI != "portal://schemas/blueprints" || !strings.Contains(schema.Text, `"$schema"`) {
		t.Errorf("add_blueprint = %q, %+v, %+v", task, guide, schema.URI)
	}
	if _, err := builder.GetPrompt(ctx, &mcp.GetPromptParams{Name: "add_blueprint"}); err == nil {
		t.Error("a prompt ran without its required argument")
	}

	member := connect(t, server, userPath, "member")
	explained, err := member.GetPrompt(ctx, &mcp.GetPromptParams{Name: "explain_entity", Arguments: map[string]string{"blueprint": "service", "identifier": "payments"}})
	if err != nil || len(explained.Messages) != 3 ||
		explained.Messages[1].Content.(*mcp.EmbeddedResource).Resource.URI != "portal://entities/service/payments" ||
		!strings.Contains(explained.Messages[2].Content.(*mcp.TextContent).Text, "gold") {
		t.Errorf("explain_entity = %+v, %v", explained, err)
	}
	if _, err := member.GetPrompt(ctx, &mcp.GetPromptParams{Name: "scorecard_gaps", Arguments: map[string]string{"scorecard": "missing"}}); err == nil {
		t.Error("a missing scorecard was explained")
	}
	// The user's server serves no documents, so the scorecard comes inline.
	gaps, err := member.GetPrompt(ctx, &mcp.GetPromptParams{Name: "scorecard_gaps", Arguments: map[string]string{"scorecard": "ready"}})
	if err != nil || len(gaps.Messages) != 2 || !strings.Contains(gaps.Messages[1].Content.(*mcp.TextContent).Text, `"spec"`) {
		t.Errorf("scorecard_gaps = %+v, %v", gaps, err)
	}
}

func TestReadsLinkToTheirResource(t *testing.T) {
	session := connect(t, testServer(t, newFake()), builderPath, "builder-reader")
	result := call(t, session, "get_document", map[string]any{"kind": "blueprints", "identifier": "service"})
	if len(result.Content) != 2 {
		t.Fatalf("content = %+v", result.Content)
	}
	if l, ok := result.Content[1].(*mcp.ResourceLink); !ok || l.URI != "portal://documents/blueprints/service" {
		t.Errorf("link = %+v", result.Content[1])
	}
	if output(t, result).Status != http.StatusOK {
		t.Errorf("structured result lost: %+v", result.StructuredContent)
	}
	// A failed read links to nothing.
	missing := call(t, session, "get_document", map[string]any{"kind": "blueprints", "identifier": "missing"})
	if !missing.IsError || len(missing.Content) != 1 {
		t.Errorf("missing = %+v", missing)
	}
}

func TestLimiterAllowsBurstsThenWaits(t *testing.T) {
	l := NewLimiter()
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	for range requestBurst {
		if ok, _ := l.allow("ada"); !ok {
			t.Fatal("refused within the burst")
		}
	}
	ok, wait := l.allow("ada")
	if ok || wait <= 0 || wait > time.Second {
		t.Errorf("past the burst: %v, wait %v", ok, wait)
	}
	if ok, _ := l.allow("grace"); !ok {
		t.Error("one user's burst limited another")
	}
	now = now.Add(time.Second)
	if ok, _ := l.allow("ada"); !ok {
		t.Error("still refused after waiting")
	}
	now = now.Add(limiterIdle + time.Minute)
	l.allow("linus")
	if _, kept := l.users["ada"]; kept {
		t.Error("an idle user was kept")
	}
}

func TestTooManyRequestsSaysWhenToRetry(t *testing.T) {
	limits := NewLimiter()
	for range requestBurst {
		limits.allow("member")
	}
	srv := testServerWith(t, newFake(), limits)
	response := post(t, srv, userPath, "member", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil)
	if response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") != "1" {
		t.Errorf("limited: %d, Retry-After %q", response.StatusCode, response.Header.Get("Retry-After"))
	}
}
