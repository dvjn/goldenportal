package migrations

import (
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/action"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

func TestRewriteConnectionsKind(t *testing.T) {
	got, err := rewriteConnections(`kind: Integration
metadata: {identifier: portal, title: Portal}
spec:
  connector: {type: portal}
  mappings:
    - resource: integrations
      blueprint: connection
      entity: {identifier: .identifier, title: .title}
`)
	if err != nil {
		t.Fatal(err)
	}
	// Mappings moved to blueprints' sources later, so this reads the text.
	if !strings.HasPrefix(got, "kind: Connection\n") || !strings.Contains(got, "- resource: connections\n") {
		t.Errorf("got\n%s", got)
	}
}

func TestRewriteConnectionsLeavesOtherResources(t *testing.T) {
	got, err := rewriteConnections(`kind: Integration
metadata: {identifier: web, title: Web}
spec:
  connector:
    type: http
    config:
      resources:
        integrations: {url: "https://example.com"}
`)
	if err != nil || !strings.HasPrefix(got, "kind: Connection\n") || !strings.Contains(got, "integrations:") {
		t.Errorf("got %q: %v", got, err)
	}
}

func TestRewriteConnectionsSteps(t *testing.T) {
	got, err := rewriteConnections(`kind: Workflow
metadata: {identifier: onboard, title: Onboard}
spec:
  group: Operate
  run: {roles: [member]}
  steps:
    - id: add
      entity:
        operation: upsert
        integration: incidents
        blueprint: incident
        identifier: a
    - sync:
        integration: registry
        mode: incremental
`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "integration") {
		t.Errorf("got\n%s", got)
	}
	if _, issues := action.ParseWorkflow(got); configdoc.HasErrors(issues) {
		t.Errorf("issues %+v in\n%s", issues, got)
	}
}

func TestRewriteConnectionsUnchanged(t *testing.T) {
	for _, text := range []string{
		"kind: Connection\nmetadata: {identifier: a, title: A}\nspec: {}\n",
		"kind: Blueprint\nmetadata: {identifier: a, title: A}\n",
		"not: [valid",
	} {
		got, err := rewriteConnections(text)
		if err != nil || got != text {
			t.Errorf("rewrote %q to %q: %v", text, got, err)
		}
	}
}
