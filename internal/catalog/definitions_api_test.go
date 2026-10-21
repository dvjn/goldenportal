package catalog

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/definition"
)

func full(documents ...string) planRequest {
	return planRequest{Source: strings.Join(documents, "---\n")}
}

func (h *harness) planAndApply(as string, request any, message string) revisionResponse {
	h.t.Helper()
	planned := h.do(http.MethodPost, "/api/v1/definitions/plan", as, request)
	expectStatus(h.t, planned, http.StatusCreated)
	applied := h.do(http.MethodPost, "/api/v1/definitions/apply", as, applyRequest{PlanID: decode[planResponse](h.t, planned).ID, Message: message})
	expectStatus(h.t, applied, http.StatusCreated)

	return decode[revisionResponse](h.t, applied)
}

func TestOnlyModelersPlanAndApply(t *testing.T) {
	cases := []struct {
		name  string
		as    string
		plan  int
		apply int
	}{
		{"signed out", "", http.StatusUnauthorized, http.StatusUnauthorized},
		{"member", "cara", http.StatusForbidden, http.StatusForbidden},
		{"member api token", "gpt_viewer", http.StatusForbidden, http.StatusForbidden},
		{"maintainer", "bob", http.StatusCreated, http.StatusNotFound},
		{"maintainer api token", "gpt_ci", http.StatusCreated, http.StatusNotFound},
		{"admin", "alice", http.StatusCreated, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)

			expectStatus(t, h.do(http.MethodPost, "/api/v1/definitions/plan", tc.as, full(teamYAML)), tc.plan)
			expectStatus(t, h.do(http.MethodPost, "/api/v1/definitions/apply", tc.as, applyRequest{PlanID: "unknown"}), tc.apply)
		})
	}
}

func TestEveryMemberReadsDefinitions(t *testing.T) {
	h := newHarness(t)
	h.planAndApply("alice", full(teamYAML, serviceYAML), "")

	for _, path := range []string{"/api/v1/definitions", "/api/v1/definitions/schema", "/api/v1/definitions/revisions", "/api/v1/definitions/revisions/1"} {
		expectStatus(t, h.do(http.MethodGet, path, "cara", nil), http.StatusOK)
		expectStatus(t, h.do(http.MethodGet, path, "", nil), http.StatusUnauthorized)
	}
	listed := decode[struct {
		Revision  int `json:"revision"`
		Documents []struct {
			YAML string `json:"yaml"`
		} `json:"documents"`
	}](t, h.do(http.MethodGet, "/api/v1/definitions", "cara", nil))
	if listed.Revision != 1 || len(listed.Documents) != 2 || listed.Documents[0].YAML != serviceYAML {
		t.Fatalf("definitions = %+v, want revision 1 with the service YAML first", listed)
	}
}

func TestPlanThenApplyRecordsARevision(t *testing.T) {
	h := newHarness(t)
	h.definitions.impact = Impact{DeletedEntities: []EntityGroup{{Blueprint: "service", Count: 2}}}

	planned := h.do(http.MethodPost, "/api/v1/definitions/plan", "bob", full(teamYAML, serviceYAML))
	expectStatus(t, planned, http.StatusCreated)
	plan := decode[planResponse](t, planned)
	if plan.ID == "" || plan.Summary != (planSummary{Added: 2}) || !plan.ExpiresAt.Equal(at.Add(planLifetime)) {
		t.Fatalf("plan = %+v, want two added documents", plan)
	}
	if len(h.definitions.revisions) != 0 {
		t.Fatal("planning stored a revision")
	}

	applied := h.do(http.MethodPost, "/api/v1/definitions/apply", "bob", applyRequest{PlanID: plan.ID, Message: "  add the catalog  "})
	expectStatus(t, applied, http.StatusCreated)
	revision := decode[revisionResponse](t, applied)
	if revision.Number != 1 || revision.Author != "bob@example.test" || revision.Message != "add the catalog" {
		t.Fatalf("revision = %+v", revision)
	}
	entry := h.audit.entries[0]
	if entry.Action != audit.DefinitionsApplied || entry.Target.Name != "Revision 1" || entry.Details["added"] != 2 || entry.Details["deleted_entities"] != 2 {
		t.Fatalf("audit entry = %+v", entry)
	}
	expectStatus(t, h.do(http.MethodPost, "/api/v1/definitions/apply", "bob", applyRequest{PlanID: plan.ID}), http.StatusNotFound)
}

func TestPlanReportsProblemsWithTheirLines(t *testing.T) {
	h := newHarness(t)

	response := h.do(http.MethodPost, "/api/v1/definitions/plan", "bob", full(serviceYAML))

	expectStatus(t, response, http.StatusUnprocessableEntity)
	body := decodeError(t, response)
	if len(body.Error.Problems) != 1 || body.Error.Problems[0].Line != 15 || body.Error.Problems[0].Path != "relations.owner.target" {
		t.Fatalf("error = %+v, want the owner target at line 15", body.Error)
	}
	if body.Error.Message != "the definitions have a problem, fix it and plan again" {
		t.Fatalf("message = %q", body.Error.Message)
	}
}

func TestPartialPlanChangesOneDocument(t *testing.T) {
	h := newHarness(t)
	h.planAndApply("alice", full(teamYAML, serviceYAML), "")

	tier := strings.Replace(serviceYAML, "      - \"2\"\n", "      - \"2\"\n      - \"3\"\n", 1)
	planned := h.do(http.MethodPost, "/api/v1/definitions/plan", "alice", planRequest{Source: tier, Partial: true})
	expectStatus(t, planned, http.StatusCreated)
	plan := decode[planResponse](t, planned)
	if plan.Summary != (planSummary{Changed: 1}) || plan.Changes[0].Fields[0].Path != "properties.tier.enum" {
		t.Fatalf("plan = %+v, want the tier enum changed", plan)
	}

	removal := h.do(http.MethodPost, "/api/v1/definitions/plan", "alice", planRequest{
		Partial: true, Remove: []definition.Ref{{Kind: definition.KindBlueprint, Identifier: "team"}},
	})
	expectStatus(t, removal, http.StatusUnprocessableEntity)
	if problem := decodeError(t, removal).Error.Problems[0]; problem.Identifier != "service" || problem.Line != 0 {
		t.Fatalf("problem = %+v, want the service relation that would point nowhere", problem)
	}
}

func TestPlanWithoutChangesCannotBeApplied(t *testing.T) {
	h := newHarness(t)
	h.planAndApply("alice", full(teamYAML), "")

	plan := decode[planResponse](t, h.do(http.MethodPost, "/api/v1/definitions/plan", "alice", full(teamYAML)))

	if plan.ID != "" || len(plan.Changes) != 0 {
		t.Fatalf("plan = %+v, want no id and no changes", plan)
	}
}

func TestApplyRefusesStaleAndExpiredPlans(t *testing.T) {
	h := newHarness(t)
	first := decode[planResponse](t, h.do(http.MethodPost, "/api/v1/definitions/plan", "alice", full(teamYAML)))
	h.planAndApply("bob", full(teamYAML, serviceYAML), "")

	stale := h.do(http.MethodPost, "/api/v1/definitions/apply", "alice", applyRequest{PlanID: first.ID})
	expectStatus(t, stale, http.StatusConflict)

	later := decode[planResponse](t, h.do(http.MethodPost, "/api/v1/definitions/plan", "alice", full(teamYAML)))
	h.now = h.now.Add(planLifetime)
	expectStatus(t, h.do(http.MethodPost, "/api/v1/definitions/apply", "alice", applyRequest{PlanID: later.ID}), http.StatusNotFound)
}

func TestRollbackPlansTheOldSnapshot(t *testing.T) {
	h := newHarness(t)
	h.planAndApply("alice", full(teamYAML, serviceYAML), "")
	h.planAndApply("alice", full(teamYAML), "drop services")

	planned := h.do(http.MethodPost, "/api/v1/definitions/revisions/1/rollback", "bob", nil)
	expectStatus(t, planned, http.StatusCreated)
	plan := decode[planResponse](t, planned)
	if plan.RollbackOf != 1 || plan.Summary != (planSummary{Added: 1}) {
		t.Fatalf("plan = %+v, want the service blueprint back", plan)
	}
	expectStatus(t, h.do(http.MethodPost, "/api/v1/definitions/revisions/1/rollback", "cara", nil), http.StatusForbidden)
	expectStatus(t, h.do(http.MethodPost, "/api/v1/definitions/revisions/7/rollback", "bob", nil), http.StatusNotFound)

	applied := h.do(http.MethodPost, "/api/v1/definitions/apply", "bob", applyRequest{PlanID: plan.ID})
	expectStatus(t, applied, http.StatusCreated)
	if revision := decode[revisionResponse](t, applied); revision.Number != 3 || revision.RollbackOf != 1 {
		t.Fatalf("revision = %+v", revision)
	}
	if entry := h.audit.entries[2]; entry.Action != audit.DefinitionsRolledBack || entry.Details["rollback_of"] != 1 {
		t.Fatalf("audit entry = %+v", entry)
	}
}

func TestDiffComparesTwoRevisions(t *testing.T) {
	h := newHarness(t)
	h.planAndApply("alice", full(teamYAML), "")
	h.planAndApply("alice", full(teamYAML, serviceYAML), "")

	diff := decode[diffResponse](t, h.do(http.MethodGet, "/api/v1/definitions/diff?from=0&to=2", "cara", nil))
	if len(diff.Changes) != 2 {
		t.Fatalf("diff = %+v, want both blueprints added", diff)
	}
	diff = decode[diffResponse](t, h.do(http.MethodGet, "/api/v1/definitions/diff?from=2&to=1", "cara", nil))
	if len(diff.Changes) != 1 || diff.Changes[0].Action != definition.Removed {
		t.Fatalf("diff = %+v, want the service removed", diff)
	}
	expectStatus(t, h.do(http.MethodGet, "/api/v1/definitions/diff?from=1&to=9", "cara", nil), http.StatusNotFound)
}

func TestSchemaIsServedAsJSON(t *testing.T) {
	h := newHarness(t)

	schema := decode[map[string]json.RawMessage](t, h.do(http.MethodGet, "/api/v1/definitions/schema", "cara", nil))

	if _, ok := schema["$defs"]; !ok {
		t.Fatalf("schema = %v, want $defs", schema)
	}
}
