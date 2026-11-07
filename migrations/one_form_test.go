package migrations

import (
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/action"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/dashboard"
)

const oldDashboard = `kind: Dashboard
metadata:
  name: overview
  title: Overview
spec:
  visibility: {roles: admin}
  defaultFor: {groups: payments}
  inputs:
    tier:
      type: enum
      values: [critical, standard]
      default: critical
    team:
      type: relation
      relation: team
      multiple: true
      default:
        query: {blueprint: team}
  widgets:
    count:
      type: stat
      title: Services
      value: {query: {blueprint: service}}
      link: {query: {blueprint: service, filter: {property: tier, op: eq, value: critical}}}
    list:
      type: table
      title: Services
      query: {blueprint: service}
      columns: [tier, {title: Owner, path: owner.title}]
    shared:
      type: bar
      title: Shared
      query: named
      groupBy: tier
    readiness:
      type: scorecard
      title: Readiness
      scorecard: s
      query: {filter: {property: tier, op: eq, value: critical}}
`

func TestRewriteOneFormDashboard(t *testing.T) {
	got, err := rewriteOneForm(oldDashboard)
	if err != nil {
		t.Fatal(err)
	}
	spec, issues := dashboard.Parse(strings.Replace(got, "query: named", "query: list", 1))
	for _, issue := range issues {
		// The scorecard widget's query has no blueprint to name, so it stays.
		if issue.Severity == configdoc.SeverityError && issue.Path != "spec.widgets.readiness.query" {
			t.Errorf("issue %+v in\n%s", issue, got)
		}
	}
	if spec.Identifier != "overview" {
		t.Errorf("identifier = %q in\n%s", spec.Identifier, got)
	}
	if spec.Inputs[0].Default[0] != "critical" || len(spec.Queries) != 3 {
		t.Errorf("inputs %+v, queries %+v", spec.Inputs, spec.Queries)
	}
	for _, name := range []string{"countValue", "countLink", "list"} {
		if !strings.Contains(got, "\n    "+name+":") {
			t.Errorf("no query %s in\n%s", name, got)
		}
	}
	if strings.Contains(got, "name: overview") || !strings.Contains(got, "{path: tier}") || !strings.Contains(got, "roles: [admin]") {
		t.Errorf("got\n%s", got)
	}

	again, err := rewriteOneForm(got)
	if err != nil || again != got {
		t.Errorf("not idempotent: %v\n%s", err, again)
	}
}

func TestRewriteOneFormBlueprint(t *testing.T) {
	got, err := rewriteOneForm(`kind: Blueprint
metadata: {name: service, title: Service}
spec:
  properties:
    tier: {type: string}
  entity:
    queries:
      owned: {blueprint: service}
    tabs:
      overview:
        widgets:
          owned:
            type: table
            title: Owned
            query: {filter: {property: tier, op: eq, value: critical}}
            columns: [tier]
`)
	if err != nil {
		t.Fatal(err)
	}
	spec, issues := blueprint.Parse(got)
	if configdoc.HasErrors(issues) {
		t.Fatalf("issues %+v in\n%s", issues, got)
	}
	if spec.Identifier != "service" || len(spec.Entity.Queries) != 2 || spec.Entity.Queries[1].Name != "owned2" {
		t.Errorf("got\n%s", got)
	}
}

func TestRewriteOneFormAction(t *testing.T) {
	got, err := rewriteOneForm(`kind: Action
metadata: {name: restart, title: Restart}
spec:
  blueprint: service
  run: {roles: member, groups: payments}
  steps:
    - approval: {approvers: {roles: admin}, require: 1, timeout: 24h}
    - output: {summary: done}
`)
	if err != nil {
		t.Fatal(err)
	}
	spec, issues := action.Parse(got)
	if configdoc.HasErrors(issues) {
		t.Fatalf("issues %+v in\n%s", issues, got)
	}
	if spec.Identifier != "restart" || len(spec.Run.Roles) != 1 || len(spec.Run.Groups) != 1 {
		t.Errorf("got\n%s", got)
	}
}

func TestRewriteOneFormDurations(t *testing.T) {
	got, err := rewriteOneForm(`kind: Action
metadata: {identifier: deploy, title: Deploy}
spec:
  steps:
    - http: {method: GET, url: "https://example.com"}
      timeout: 1h30m
      retry: {attempts: 2, backoff: 1500ms}
    - approval: {approvers: {roles: [admin]}, require: 1, timeout: 24h}
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"timeout: 90m", "backoff: 2s", "timeout: 24h"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	got, err = rewriteOneForm("kind: Integration\nmetadata: {identifier: a, title: A}\nspec:\n  sync: {interval: 1m30s, complete: 48h0m0s}\n")
	if err != nil || !strings.Contains(got, "interval: 90s") || !strings.Contains(got, "complete: 2d") {
		t.Errorf("got %q: %v", got, err)
	}
}

func TestRewriteOneFormLeavesTheRest(t *testing.T) {
	for _, in := range []string{
		"{",
		"kind: Integration\nmetadata: {identifier: a, title: A}\nspec:\n  connector: {type: portal}\n  mappings:\n    - blueprint: user\n      entity: {groups: .groups}\n",
		"kind: Blueprint\nmetadata: {identifier: a, title: A}\nspec: {}\n",
	} {
		got, err := rewriteOneForm(in)
		if err != nil || got != in {
			t.Errorf("rewrote %q to %q: %v", in, got, err)
		}
	}
}
