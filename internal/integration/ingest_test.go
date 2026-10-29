package integration

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/catalog"
)

const servicesPath = "/api/v1/ingest/ci/service"

func threeServices(mode string) map[string]any {
	return map[string]any{
		"mode": mode,
		"entities": []map[string]any{
			{"name": "checkout", "language": "go", "tier": 1, "team": "payments"},
			{"name": "search", "language": "go", "tier": 2, "team": "platform"},
			{"name": "ledger", "language": "go", "tier": 1, "team": "payments"},
		},
	}
}

func TestIngestUpsertsTheMappedEntitiesAndRecordsTheRun(t *testing.T) {
	h := newHarness(t)

	response := h.do(http.MethodPost, servicesPath, "gpi_ci", threeServices("upsert"))

	expectStatus(t, response, http.StatusCreated)
	body := decode[ingestResponse](t, response)
	if body.Run != 1 || body.Status != StatusSucceeded || body.Upserted != 3 || len(body.Warnings) != 0 {
		t.Fatalf("response = %+v, want run 1 that upserted 3", body)
	}
	write := h.writer.writes[0]
	if write.Integration != "ci" || write.Sweep || len(write.Groups) != 1 || write.Groups[0].Blueprint != "service" ||
		len(write.Groups[0].Entities) != 3 || write.Groups[0].Entities[0].Identifier != "checkout" {
		t.Fatalf("write = %+v, want the three services of ci without a sweep", write)
	}
	want := Run{
		ID: 1, Integration: "ci", Trigger: TriggerPush, Actor: "pipelines", Kind: "service", Mode: modeUpsert,
		Status: StatusSucceeded, StartedAt: at, FinishedAt: at, Counts: Counts{Fetched: 3, Upserted: 3},
	}
	if !reflect.DeepEqual(h.runs.runs, []Run{want}) {
		t.Fatalf("runs = %+v, want %+v", h.runs.runs, want)
	}
	wantEntry := audit.Entry{
		Actor:    audit.Actor{Kind: audit.ActorIngestToken, ID: "t-1", Name: "pipelines"},
		Action:   audit.IntegrationPushed,
		Resource: audit.Resource{Kind: audit.ResourceIntegration, ID: "ci", Name: "CI"},
		Details: map[string]any{
			"run": int64(1), "trigger": TriggerPush, "kind": "service", "mode": modeUpsert,
			"status": StatusSucceeded, "upserted": 3, "deleted": 0,
		},
	}
	if !reflect.DeepEqual(h.audit.entries, []audit.Entry{wantEntry}) {
		t.Fatalf("audit = %+v, want %+v", h.audit.entries, wantEntry)
	}
}

func TestIngestSnapshotReportsTheGuardAsAWarning(t *testing.T) {
	h := newHarness(t)
	h.writer.result = catalog.WriteResult{
		Guarded: []catalog.GuardedSweep{{Blueprint: "service", Kept: 2, Written: 1, Existing: 3}},
		Dropped: []catalog.Link{{
			Relation: "owner",
			Source:   catalog.EntityName{Blueprint: "service", Identifier: "checkout"},
			Target:   catalog.EntityName{Blueprint: "team", Identifier: "payments"},
		}},
	}

	response := h.do(http.MethodPost, servicesPath, "gpi_ci", map[string]any{
		"mode": "snapshot", "force": false, "entities": []map[string]any{{"name": "checkout", "team": "payments"}},
	})

	expectStatus(t, response, http.StatusCreated)
	body := decode[ingestResponse](t, response)
	want := []string{
		"checkout: dropped the owner link to team payments, which does not exist",
		"kept 2 service entities that the snapshot leaves out, because it holds 1 of the 3 the integration had, " +
			"fewer than half; push the snapshot again with force set to delete them",
	}
	if body.Status != StatusPartial || !reflect.DeepEqual(body.Warnings, want) {
		t.Fatalf("response = %+v, want a partial run with %q", body, want)
	}
	if write := h.writer.writes[0]; !write.Sweep || write.Force {
		t.Fatalf("write = %+v, want an unforced sweep", write)
	}
	if run := h.runs.runs[0]; run.Mode != modeSnapshot || run.Status != StatusPartial || len(run.Warnings) != 2 {
		t.Fatalf("run = %+v, want a partial snapshot with two warnings", run)
	}
}

func TestIngestForcedSnapshotSweepsWithForce(t *testing.T) {
	h := newHarness(t)

	expectStatus(t, h.do(http.MethodPost, servicesPath, "gpi_ci", map[string]any{
		"mode": "snapshot", "force": true, "entities": []map[string]any{{"name": "checkout"}},
	}), http.StatusCreated)

	if write := h.writer.writes[0]; !write.Sweep || !write.Force {
		t.Fatalf("write = %+v, want a forced sweep", write)
	}
}

func TestIngestRefusesEntitiesAnotherIntegrationOwns(t *testing.T) {
	h := newHarness(t)
	h.writer.err = catalog.ConflictError{Conflicts: []catalog.Conflict{{Blueprint: "service", Identifier: "ledger", Integration: "manual"}}}

	response := h.do(http.MethodPost, servicesPath, "gpi_ci", threeServices("upsert"))

	expectStatus(t, response, http.StatusConflict)
	body := decodeError(t, response)
	if body.Error.Message != "ownership conflict: ledger is owned by the manual integration" ||
		!reflect.DeepEqual(body.Error.Conflicts, []catalog.Conflict{{Blueprint: "service", Identifier: "ledger", Integration: "manual"}}) {
		t.Fatalf("error = %+v, want the ledger conflict", body.Error)
	}
	if run := h.runs.runs[0]; run.Status != StatusFailed || run.Error != body.Error.Message {
		t.Fatalf("run = %+v, want a failed run with the conflict", run)
	}
}

func TestIngestRefusesTheWholePushWhenARecordHasAProblem(t *testing.T) {
	h := newHarness(t)

	response := h.do(http.MethodPost, servicesPath, "gpi_ci", map[string]any{
		"entities": []map[string]any{{"name": "checkout"}, {"name": "ledger", "tier": 7}},
	})

	expectStatus(t, response, http.StatusUnprocessableEntity)
	body := decodeError(t, response)
	if len(body.Error.Problems) != 1 || body.Error.Problems[0].Index != 1 || body.Error.Problems[0].Field != "properties.tier" {
		t.Fatalf("problems = %+v, want the tier of the second record", body.Error.Problems)
	}
	if len(h.writer.writes) != 0 {
		t.Fatalf("writes = %+v, want none", h.writer.writes)
	}
	run := h.runs.runs[0]
	if run.Status != StatusFailed || run.Mode != modeUpsert || len(run.Warnings) != 1 ||
		!strings.HasPrefix(run.Warnings[0], "service record 2 (ledger): properties.tier:") {
		t.Fatalf("run = %+v, want a failed run that lists the problem", run)
	}
	if len(h.audit.entries) != 0 {
		t.Fatalf("audit = %+v, want none for a push that wrote nothing", h.audit.entries)
	}
}

func TestIngestAcceptsOnlyTheIntegrationsOwnToken(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name   string
		as     string
		status int
	}{
		{"token of another integration", "gpi_gitlab", http.StatusForbidden},
		{"service token", "gpt_ci", http.StatusUnauthorized},
		{"maintainer session", "bob", http.StatusUnauthorized},
		{"unknown token", "gpi_forged", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectStatus(t, h.do(http.MethodPost, servicesPath, tc.as, threeServices("upsert")), tc.status)
		})
	}
	if len(h.writer.writes) != 0 || len(h.runs.runs) != 0 {
		t.Fatalf("writes = %v, runs = %v, want none", h.writer.writes, h.runs.runs)
	}
}

func TestIngestRefusesWhatItCannotReceive(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		as       string
		body     any
		status   int
		message  string
		recorded bool
	}{
		{"pull integration", "/api/v1/ingest/gitlab/group", "gpi_gitlab", threeServices("upsert"), http.StatusNotFound, "no push integration has that identifier", false},
		{"unknown kind", "/api/v1/ingest/ci/pipeline", "gpi_ci", threeServices("upsert"), http.StatusNotFound, `the CI integration receives no kind "pipeline", it receives service, team`, false},
		{"invalid JSON", servicesPath, "gpi_ci", "{", http.StatusBadRequest, "the request body is not valid JSON", true},
		{"unknown mode", servicesPath, "gpi_ci", threeServices("replace"), http.StatusBadRequest, "the mode must be upsert or snapshot", true},
		{"no entities", servicesPath, "gpi_ci", map[string]any{"entities": []any{}}, http.StatusBadRequest, "push 1 to 1000 entities at a time", true},
		{
			"too many entities", servicesPath, "gpi_ci",
			map[string]any{"entities": make([]map[string]any, 1001)},
			http.StatusBadRequest, "push 1 to 1000 entities at a time", true,
		},
		{
			"body over 4 MiB", servicesPath, "gpi_ci", `{"entities": [{"name": "` + strings.Repeat("a", 4<<20) + `"}]}`,
			http.StatusRequestEntityTooLarge, "the request body is larger than 4 MiB, push fewer entities at a time", true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)

			response := h.do(http.MethodPost, tc.path, tc.as, tc.body)

			expectStatus(t, response, tc.status)
			if got := decodeError(t, response).Error.Message; got != tc.message {
				t.Fatalf("message = %q, want %q", got, tc.message)
			}
			if recorded := len(h.runs.runs) == 1 && h.runs.runs[0].Error == tc.message; recorded != tc.recorded {
				t.Fatalf("runs = %+v, want a failed run recorded: %t", h.runs.runs, tc.recorded)
			}
		})
	}
}

func TestIngestHidesAnUnexpectedFailure(t *testing.T) {
	h := newHarness(t)
	h.writer.err = errors.New(`pq: relation "entities" does not exist`)

	response := h.do(http.MethodPost, servicesPath, "gpi_ci", threeServices("upsert"))

	expectStatus(t, response, http.StatusInternalServerError)
	if run := h.runs.runs[0]; run.Status != StatusFailed || run.Error != unexpectedFailure {
		t.Fatalf("run = %+v, want a failed run with the generic message", run)
	}
}

func TestIngestDeleteRemovesOnlyAnEntityTheIntegrationOwns(t *testing.T) {
	h := newHarness(t)
	h.writer.owned["ci/service/checkout"] = true

	expectStatus(t, h.do(http.MethodDelete, servicesPath+"/checkout", "gpi_ci", nil), http.StatusNoContent)
	response := h.do(http.MethodDelete, servicesPath+"/payments-api", "gpi_ci", nil)

	expectStatus(t, response, http.StatusNotFound)
	if got := decodeError(t, response).Error.Message; got != "the CI integration owns no Service with that identifier" {
		t.Fatalf("message = %q", got)
	}
	runs := h.runs.runs
	if len(runs) != 2 || runs[0].Mode != modeDelete || runs[0].Counts.Deleted != 1 || runs[0].Status != StatusSucceeded ||
		runs[1].Status != StatusFailed {
		t.Fatalf("runs = %+v, want a delete that succeeded and one that failed", runs)
	}
}
