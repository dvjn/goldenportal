package action

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/filter"
)

const restartYAML = `
kind: Action
metadata:
  name: restart
  title: Restart
  icon: rotate-cw
spec:
  blueprint: service
  scope: {property: lifecycle, op: eq, value: production}
  run:
    roles: [builder]
    filter: {property: owner.members, op: contains, value: $me}
  inputs:
    reason: {type: string, required: true, minLength: 3}
    notify: {relation: team}
  steps:
    - id: restart
      http:
        method: POST
        url: https://ops.example.com/services/{{ $entity }}/restart
        headers:
          Authorization: {env: GP_SECRET_OPS}
          X-Requested-By: $me.email
        body: {reason: $input.reason, run: $run.id, by: $me}
        success: [200, 202]
        output:
          job: .body.job
      timeout: 2m
      retry: {attempts: 2, backoff: 10s}
    - check:
        blueprint: service
        filter: {property: identifier, op: eq, value: $entity}
        expect: {count: 1}
        message: "{{ $entity.title }} is gone"
    - entity:
        operation: upsert
        integration: self-service
        blueprint: service
        identifier: $entity
        properties: {lastRestart: $step.restart.job}
      onFailure: continue
    - output:
        summary: "Restarted {{ $entity.title }} as job {{ $step.restart.job }}"
        links:
          Job: https://ops.example.com/jobs/{{ $step.restart.job }}
  outputs:
    job: $step.restart.job
`

func TestParseAction(t *testing.T) {
	spec, issues := Parse(restartYAML)
	if len(issues) > 0 {
		t.Fatalf("issues = %+v", issues)
	}
	if spec.Blueprint != "service" || len(spec.Inputs) != 2 || len(spec.Steps) != 4 {
		t.Fatalf("spec = %+v", spec)
	}
	h := spec.Steps[0].HTTP
	if h.Method != "POST" || h.Headers["Authorization"].Env != "GP_SECRET_OPS" || time.Duration(spec.Steps[0].Timeout) != 2*time.Minute ||
		spec.Steps[0].Retry.Attempts != 2 || !reflect.DeepEqual(h.Success, []int{200, 202}) {
		t.Fatalf("http step = %+v", spec.Steps[0])
	}
	if spec.Steps[0].Title != "POST ops.example.com" || spec.Steps[2].Title != "Upsert service" || !spec.Steps[2].Continue {
		t.Fatalf("titles = %q, %q", spec.Steps[0].Title, spec.Steps[2].Title)
	}
	if notify, _ := spec.Input("notify"); notify.Relation != "team" {
		t.Fatalf("notify = %+v", notify)
	}
}

func TestParseActionProblems(t *testing.T) {
	head := "kind: Action\nmetadata: {name: a, title: A}\nspec:\n  blueprint: service\n  run: {roles: [member]}\n"
	tests := []struct{ name, source, want string }{
		{"no run", "kind: Action\nmetadata: {name: a, title: A}\nspec:\n  blueprint: service\n  steps: [{output: {summary: hi}}]\n", "say who can run it"},
		{"no steps", head, "add the steps"},
		{"two types", head + "  steps: [{output: {summary: hi}, check: {blueprint: s, expect: {count: 0}, message: m}}]\n", "a step does one thing"},
		{"unknown input", head + "  steps: [{output: {summary: '{{ $input.nope }}'}}]\n", "$input.nope names no input"},
		{"later step", head + "  steps:\n    - output: {summary: $step.b.x}\n    - id: b\n      output: {summary: hi}\n", "names no earlier step"},
		{"typo root", head + "  steps: [{output: {summary: $inputs.x}}]\n", "$inputs is not a reference"},
		{"bad template", head + "  steps: [{output: {summary: '{{ hello }}'}}]\n", "must hold a reference"},
		{"step in scope", head + "  scope: {property: identifier, op: eq, value: $input.x}\n  steps: [{output: {summary: hi}}]\n", "can't be used here"},
		{"retry on portal step", head + "  steps: [{output: {summary: hi}, retry: {attempts: 1, backoff: 1s}}]\n", "only http steps retry"},
		{"get with body", head + "  steps: [{http: {method: GET, url: 'https://a.example', body: {a: 1}}}]\n", "GET requests have no body"},
		{"plain env", head + "  steps: [{http: {method: POST, url: 'https://a.example', headers: {A: {env: DB_PASSWORD}}}}]\n", "starting with GP_SECRET_"},
		{"bad jq", head + "  steps: [{http: {method: POST, url: 'https://a.example', output: {a: '.a |'}}}]\n", "not valid jq"},
		{"delete with properties", head + "  steps: [{entity: {operation: delete, integration: i, blueprint: s, identifier: $entity, properties: {a: 1}}}]\n", "deleting takes no properties"},
		{"long timeout", head + "  steps: [{http: {method: POST, url: 'https://a.example'}, timeout: 1h}]\n", "at most 10m0s"},
		{"approval without require", head + "  steps: [{approval: {approvers: {roles: [admin]}, timeout: 1h}}]\n", "require must be"},
		{"approval with timeout", head + "  steps: [{approval: {approvers: {roles: [admin]}, require: 1, timeout: 1h}, timeout: 1m}]\n", "approval steps take no timeout"},
		{"review both", head + "  steps: [{approval: {approvers: {roles: [admin]}, require: 1, timeout: 1h, review: [{markdown: a, json: b}]}}]\n", "markdown or json, not both"},
		{"no expectation", head + "  steps: [{check: {blueprint: s, expect: {}, message: m}}]\n", "expect takes count, min or max"},
		{"sync without mode", head + "  steps: [{sync: {integration: registry}}]\n", "mode must be incremental"},
		{"sync no resources", head + "  steps: [{sync: {integration: registry, resources: [], mode: complete}}]\n", "resources must list"},
		{"sync retries", head + "  steps: [{sync: {integration: registry, mode: complete}, retry: {attempts: 1, backoff: 1s}}]\n", "only http steps retry"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, issues := Parse(test.source)
			var messages []string
			for _, issue := range issues {
				messages = append(messages, issue.Message)
			}
			if !strings.Contains(strings.Join(messages, "\n"), test.want) {
				t.Fatalf("issues = %v, want %q", messages, test.want)
			}
		})
	}
}

const applyYAML = `
kind: Action
metadata: {name: apply, title: Apply}
spec:
  blueprint: service
  run: {roles: [member]}
  steps:
    - id: plan
      http: {method: POST, url: https://ops.example.com/plan}
    - id: approve
      approval:
        approvers:
          groups: [platform]
          filter: {property: owner.members, op: contains, value: $me}
        require: 2
        timeout: 24h
        message: "Apply {{ $step.plan.body.count }} changes?"
        review:
          - {title: Plan, markdown: $step.plan.body.summary}
          - {title: Changes, json: $step.plan.body.changes}
    - http: {method: POST, url: https://ops.example.com/apply, body: {by: $step.approve.approvedBy}}
`

func TestParseSync(t *testing.T) {
	head := "kind: Action\nmetadata: {name: a, title: A}\nspec:\n  blueprint: service\n  run: {roles: [member]}\n"
	spec, issues := Parse(head + "  steps:\n    - sync: {integration: registry, resources: [services], mode: incremental}\n      timeout: 2m\n")
	if len(issues) > 0 {
		t.Fatalf("issues = %+v", issues)
	}
	step := spec.Steps[0]
	want := SyncStep{Integration: "registry", Resources: []string{"services"}, Mode: "incremental"}
	if step.Type != StepSync || step.Title != "Sync registry" || !reflect.DeepEqual(*step.Sync, want) || time.Duration(step.Timeout) != 2*time.Minute {
		t.Fatalf("step = %+v, sync = %+v", step, step.Sync)
	}
}

func TestParseApproval(t *testing.T) {
	spec, issues := Parse(applyYAML)
	if len(issues) > 0 {
		t.Fatalf("issues = %+v", issues)
	}
	a := spec.Steps[1].Approval
	if a.Require != 2 || time.Duration(a.Timeout) != 24*time.Hour || len(a.Review) != 2 || a.Review[1].JSON != "$step.plan.body.changes" ||
		a.Approvers.Filter == nil || spec.Steps[1].Title != "Approval" {
		t.Fatalf("approval = %+v", a)
	}
}

func TestResolve(t *testing.T) {
	scope := Scope{
		Inputs: map[string]any{"reason": "deploy stuck", "teams": []any{"core", "web"}},
		Entity: map[string]any{"identifier": "payments", "title": "Payments", "tier": 1.0},
		Me:     map[string]any{"email": "ada@example.com", "groups": []any{"eng"}},
		Run:    map[string]any{"id": "r1", "url": "https://portal.example.com/runs/r1"},
		Steps:  map[string]any{"create": map[string]any{"body": map[string]any{"ids": []any{"x", "y"}}}},
	}
	got := scope.Resolve(map[string]any{
		"who":    "$me",
		"tier":   "$entity.tier",
		"teams":  "$input.teams",
		"second": "$step.create.body.ids.1",
		"text":   "{{ $entity.title }} for {{ $me.email }}: {{ $input.teams }} {{ $input.missing }}",
		"plain":  "$5 off",
	})
	want := map[string]any{
		"who": "ada@example.com", "tier": 1.0, "teams": []any{"core", "web"}, "second": "y",
		"text": `Payments for ada@example.com: ["core","web"] `, "plain": "$5 off",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v", got)
	}

	f := scope.ResolveFilter(&filter.Filter{And: []filter.Filter{
		{Property: "owner", Op: filter.OpIn, ValuesFrom: "$input.teams"},
		{Property: "identifier", Op: filter.OpEq, Value: "$entity"},
	}})
	if !reflect.DeepEqual(f.And[0].Values, []any{"core", "web"}) || f.And[1].Value != "payments" {
		t.Fatalf("ResolveFilter() = %+v", f)
	}
}

func TestValidateInputs(t *testing.T) {
	spec, issues := Parse(restartYAML)
	if configdoc.HasErrors(issues) {
		t.Fatal(issues)
	}
	_, problems := blueprint.ValidateInputs(spec.Inputs, map[string]any{"reason": "no", "notify": 3.0, "extra": true})
	if len(problems["reason"]) != 1 || len(problems["notify"]) != 1 || len(problems[""]) != 1 {
		t.Fatalf("problems = %v", problems)
	}
	clean, problems := blueprint.ValidateInputs(spec.Inputs, map[string]any{"reason": "deploy stuck", "notify": "core"})
	if len(problems) != 0 || clean["notify"] != "core" {
		t.Fatalf("clean = %v, problems = %v", clean, problems)
	}
}

func TestHTTPStep(t *testing.T) {
	var got struct {
		method, auth, key string
		body              map[string]any
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.auth, got.key = r.Method, r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		if strings.HasSuffix(r.URL.Path, "/missing/restart") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "no such service"}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"job": "j-42"}`))
	}))
	defer server.Close()
	t.Setenv("GP_SECRET_OPS", "Bearer s3cret")

	spec, issues := Parse(strings.ReplaceAll(restartYAML, "https://ops.example.com", server.URL))
	if configdoc.HasErrors(issues) {
		t.Fatal(issues)
	}
	run := Run{ID: "r1"}
	scope := Scope{
		Inputs: map[string]any{"reason": "stuck"}, Entity: map[string]any{"identifier": "payments"},
		Me: map[string]any{"email": "ada@example.com"}, Run: map[string]any{"id": "r1"}, Steps: map[string]any{},
	}
	rn := Runner{}
	resp, _, err := rn.call(context.Background(), run, 0, spec.Steps[0], scope)
	if err != nil {
		t.Fatal(err)
	}
	output, err := httpOutput(context.Background(), spec.Steps[0].HTTP, resp)
	if err != nil || !reflect.DeepEqual(output, map[string]any{"job": "j-42"}) {
		t.Fatalf("output = %v, %v", output, err)
	}
	if got.method != "POST" || got.auth != "Bearer s3cret" || got.key != "r1-0" ||
		!reflect.DeepEqual(got.body, map[string]any{"reason": "stuck", "run": "r1", "by": "ada@example.com"}) {
		t.Fatalf("request = %+v", got)
	}

	scope.Entity["identifier"] = "missing"
	resp, _, err = rn.call(context.Background(), run, 0, spec.Steps[0], scope)
	if err != nil || succeeded(spec.Steps[0].HTTP.Success, resp.status) {
		t.Fatalf("missing = %+v, %v", resp, err)
	}
}

const restartManyYAML = `
kind: Workflow
metadata: {name: restart-many, title: Restart many}
spec:
  group: Operations
  run: {roles: [member]}
  inputs:
    services: {relation: service, many: true, required: true}
    reason: {type: string}
  steps:
    - id: restart
      action:
        name: restart
        entities: $input.services
        inputs: {reason: $input.reason}
        maxFailures: 1
    - output: {summary: "{{ $step.restart.succeeded }} restarted"}
`

func TestParseWorkflow(t *testing.T) {
	spec, issues := ParseWorkflow(restartManyYAML)
	if len(issues) > 0 {
		t.Fatalf("issues = %+v", issues)
	}
	a := spec.Steps[0].Action
	if !spec.IsWorkflow() || spec.Group != "Operations" || a.Name != "restart" || a.Entities != "$input.services" ||
		a.MaxFailures != 1 || spec.Steps[0].Title != "Run restart" {
		t.Fatalf("spec = %+v", spec)
	}
	if parsed, _ := parseAny(restartManyYAML); !parsed.IsWorkflow() {
		t.Fatal("parseAny didn't read it as a workflow")
	}

	head := "kind: Workflow\nmetadata: {name: a, title: A}\nspec:\n  group: G\n  run: {roles: [member]}\n"
	for want, source := range map[string]string{
		"$entity can't be used here":     head + "  steps: [{output: {summary: $entity}}]\n",
		"unknown field \"blueprint\"":    head + "  blueprint: service\n  steps: [{output: {summary: hi}}]\n",
		"roles and groups":               strings.Replace(head, "{roles: [member]}", "{filter: {property: a, op: exists}}", 1) + "  steps: [{output: {summary: hi}}]\n",
		"add the entities":               head + "  steps: [{action: {name: restart}}]\n",
		"add the group":                  strings.Replace(head, "  group: G\n", "", 1) + "  steps: [{output: {summary: hi}}]\n",
		"only workflows run actions":     "kind: Action\nmetadata: {name: a, title: A}\nspec:\n  blueprint: s\n  run: {roles: [member]}\n  steps: [{action: {name: b, entities: x}}]\n",
		"approvers are roles and groups": head + "  steps: [{approval: {approvers: {filter: {property: a, op: exists}}, require: 1, timeout: 1h}}]\n",
		"action steps take no retry":     head + "  steps: [{action: {name: b, entities: x}, retry: {attempts: 1, backoff: 1s}}]\n",
	} {
		_, issues := parse(source, regexp.MustCompile(`kind: (\w+)`).FindStringSubmatch(source)[1])
		var messages []string
		for _, issue := range issues {
			messages = append(messages, issue.Message)
		}
		if !strings.Contains(strings.Join(messages, "\n"), want) {
			t.Errorf("issues = %v, want %q", messages, want)
		}
	}
}

func TestIdentifiers(t *testing.T) {
	if got := identifiers([]any{"a", "b", "a", 3.0, ""}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("list = %v", got)
	}
	if got := identifiers("a"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("one = %v", got)
	}
	if got := identifiers(nil); len(got) != 0 {
		t.Errorf("none = %v", got)
	}
}
