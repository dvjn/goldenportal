package integration

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldport/internal/audit"
)

func (h *harness) seedOverview() {
	h.counts = fixedCounts{
		{Integration: "ci", Blueprint: "service", Count: 3},
		{Integration: "manual", Blueprint: "service", Count: 1},
		{Integration: "manual", Blueprint: "team", Count: 2},
	}
	h.runs.runs = []Run{
		{ID: 1, Integration: "ci", Trigger: TriggerPush, Actor: "pipelines", Kind: "service", Mode: modeUpsert, Status: StatusSucceeded, StartedAt: at, FinishedAt: at},
		{ID: 2, Integration: "gitlab", Trigger: TriggerManual, Actor: "bob@example.test", Status: StatusFailed, StartedAt: at, FinishedAt: at, Error: "could not reach GitLab"},
		{ID: 3, Integration: "ci", Trigger: TriggerPush, Actor: "pipelines", Kind: "service", Mode: modeSnapshot, Status: StatusPartial, StartedAt: at.Add(time.Minute), FinishedAt: at.Add(time.Minute), Warnings: []string{"kept 2"}},
	}
}

func TestListIntegrationsSummarisesEveryIntegration(t *testing.T) {
	h := newHarness(t)
	h.seedOverview()

	response := h.do(http.MethodGet, "/api/v1/integrations", "cara", nil)

	expectStatus(t, response, http.StatusOK)
	integrations := decode[integrationsResponse](t, response).Integrations
	if len(integrations) != 3 {
		t.Fatalf("integrations = %+v, want manual, ci and gitlab", integrations)
	}
	manual, ci, gitlab := integrations[0], integrations[1], integrations[2]
	if manual.Identifier != "manual" || !manual.Manual || manual.Pull || manual.EntityCount != 3 || manual.LastRun != nil {
		t.Errorf("manual = %+v, want the built-in integration with 3 entities", manual)
	}
	if ci.Identifier != "ci" || ci.Connector != "push" || ci.Pull || ci.EntityCount != 3 || ci.LastRun.ID != 3 || !ci.NextRunAt.IsZero() {
		t.Errorf("ci = %+v, want the push integration with its latest run", ci)
	}
	if gitlab.Identifier != "gitlab" || !gitlab.Pull || gitlab.Interval != "1h" || gitlab.LastRun.Status != StatusFailed ||
		!gitlab.NextRunAt.Equal(at.Add(time.Hour)) {
		t.Errorf("gitlab = %+v, want the failed pull integration due an hour after its run", gitlab)
	}
}

func TestGetIntegrationDescribesItsKindsEntitiesAndSecrets(t *testing.T) {
	h := newHarness(t)
	h.seedOverview()

	ci := decode[integrationDetail](t, h.do(http.MethodGet, "/api/v1/integrations/ci", "cara", nil))
	gitlab := decode[integrationDetail](t, h.do(http.MethodGet, "/api/v1/integrations/gitlab", "cara", nil))
	manual := decode[integrationDetail](t, h.do(http.MethodGet, "/api/v1/integrations/manual", "cara", nil))

	wantKinds := []kindView{
		{Kind: "service", Blueprint: "service", BlueprintTitle: "Service"},
		{Kind: "team", Blueprint: "team", BlueprintTitle: "Team", Canonical: true},
	}
	if !reflect.DeepEqual(ci.Kinds, wantKinds) || ci.IngestURL != "https://portal.example.test/api/v1/ingest/ci" ||
		!reflect.DeepEqual(ci.Entities, []blueprintCount{{Blueprint: "service", Title: "Service", Count: 3}}) {
		t.Errorf("ci = %+v, want its two kinds, its services and the ingest address", ci)
	}
	if !reflect.DeepEqual(gitlab.Secrets, []secretView{{Name: "token", Variable: "GITLAB_TOKEN"}}) || gitlab.IngestURL != "" {
		t.Errorf("gitlab = %+v, want its token secret and no ingest address", gitlab)
	}
	if manual.Title != "Manual" || len(manual.Entities) != 2 || len(manual.Kinds) != 0 {
		t.Errorf("manual = %+v, want its services and teams", manual)
	}
	expectStatus(t, h.do(http.MethodGet, "/api/v1/integrations/jira", "cara", nil), http.StatusNotFound)
}

func TestListRunsListsTheNewestFirst(t *testing.T) {
	h := newHarness(t)
	h.seedOverview()

	runs := decode[runsResponse](t, h.do(http.MethodGet, "/api/v1/integrations/ci/runs", "cara", nil)).Runs
	manual := decode[runsResponse](t, h.do(http.MethodGet, "/api/v1/integrations/manual/runs", "cara", nil)).Runs

	if len(runs) != 2 || runs[0].ID != 3 || runs[0].Mode != modeSnapshot || runs[0].Warnings[0] != "kept 2" || runs[1].ID != 1 {
		t.Fatalf("runs = %+v, want runs 3 and 1", runs)
	}
	if manual == nil || len(manual) != 0 {
		t.Fatalf("manual runs = %+v, want an empty list", manual)
	}
	expectStatus(t, h.do(http.MethodGet, "/api/v1/integrations/jira/runs", "cara", nil), http.StatusNotFound)
}

func TestIngestTokensAreCreatedListedAndRevokedPerIntegration(t *testing.T) {
	h := newHarness(t)

	response := h.do(http.MethodPost, "/api/v1/integrations/ci/tokens", "bob", map[string]any{"name": " deploys ", "expires_in_days": 30})

	expectStatus(t, response, http.StatusCreated)
	created := decode[createdToken](t, response)
	if !strings.HasPrefix(created.Secret, "gpi_") || created.Token.Name != "deploys" ||
		!created.Token.ExpiresAt.Equal(at.AddDate(0, 0, 30)) || created.IngestURL != "https://portal.example.test/api/v1/ingest/ci" {
		t.Fatalf("created = %+v, want a 30 day ingest token for ci", created)
	}
	if token := h.tokens[created.Secret]; token.Integration != "ci" || token.CreatedBy != "u-2" {
		t.Fatalf("stored token = %+v, want it scoped to ci and created by bob", token)
	}
	listed := decode[tokensResponse](t, h.do(http.MethodGet, "/api/v1/integrations/ci/tokens", "bob", nil)).Tokens
	if len(listed) != 2 {
		t.Fatalf("tokens = %+v, want pipelines and deploys", listed)
	}

	expectStatus(t, h.do(http.MethodDelete, "/api/v1/integrations/gitlab/tokens/"+created.Token.ID, "bob", nil), http.StatusConflict)
	expectStatus(t, h.do(http.MethodDelete, "/api/v1/integrations/ci/tokens/"+created.Token.ID, "bob", nil), http.StatusNoContent)
	expectStatus(t, h.do(http.MethodDelete, "/api/v1/integrations/ci/tokens/"+created.Token.ID, "bob", nil), http.StatusNotFound)

	bob := audit.Actor{UserID: "u-2", Label: "bob@example.test"}
	target := audit.Target{Kind: "ingest_token", ID: created.Token.ID, Name: "deploys"}
	want := []audit.Entry{
		{Actor: bob, Action: audit.IngestTokenCreated, Target: target, Details: map[string]any{"integration": "ci", "expires_at": at.AddDate(0, 0, 30)}},
		{Actor: bob, Action: audit.IngestTokenRevoked, Target: target, Details: map[string]any{"integration": "ci"}},
	}
	if !reflect.DeepEqual(h.audit.entries, want) {
		t.Fatalf("audit = %+v, want %+v", h.audit.entries, want)
	}
}

func TestIngestTokensRefuseWhatTheyCannotTake(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		as     string
		body   any
		status int
	}{
		{"member", http.MethodGet, "/api/v1/integrations/ci/tokens", "cara", nil, http.StatusForbidden},
		{"pull integration", http.MethodPost, "/api/v1/integrations/gitlab/tokens", "bob", map[string]any{"name": "x", "expires_in_days": 30}, http.StatusConflict},
		{"manual integration", http.MethodGet, "/api/v1/integrations/manual/tokens", "bob", nil, http.StatusNotFound},
		{"no name", http.MethodPost, "/api/v1/integrations/ci/tokens", "bob", map[string]any{"name": " ", "expires_in_days": 30}, http.StatusBadRequest},
		{"no expiry", http.MethodPost, "/api/v1/integrations/ci/tokens", "bob", map[string]any{"name": "deploys"}, http.StatusBadRequest},
		{"ingest token", http.MethodGet, "/api/v1/integrations/ci/tokens", "gpi_ci", nil, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)

			expectStatus(t, h.do(tc.method, tc.path, tc.as, tc.body), tc.status)
			if len(h.audit.entries) != 0 {
				t.Fatalf("audit = %+v, want none", h.audit.entries)
			}
		})
	}
}
