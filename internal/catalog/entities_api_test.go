package catalog

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/definition"
)

const ciYAML = `kind: Integration
identifier: ci
title: CI
connector: push
`

const servicesPath = "/api/v1/catalog/blueprints/service/entities"

// newCatalog serves a catalog of teams, where a team has a parent team, and
// services owned by a team, with the payments team and the ledger service
// that the CI integration pushed.
func newCatalog(t *testing.T) *harness {
	h := newHarness(t)
	team := strings.Replace(teamYAML, "properties:", "relations:\n  parent:\n    title: Parent team\n    target: team\nproperties:", 1)
	h.planAndApply("alice", full(team, strings.Replace(serviceYAML, "    target: team\n", "    target: team\n    inverseTitle: Services\n", 1), ciYAML), "")
	h.entities.put(Entity{Blueprint: "team", Identifier: "payments", Title: "Payments", Integration: definition.Manual, Properties: map[string]any{}})
	h.entities.put(Entity{Blueprint: "team", Identifier: "platform", Title: "Platform", Integration: definition.Manual, Properties: map[string]any{}})
	h.entities.put(Entity{Blueprint: "service", Identifier: "ledger", Title: "Ledger", Integration: "ci", Properties: map[string]any{"tier": "1"}})
	h.audit.entries = nil

	return h
}

func paymentsAPI() entityRequest {
	return entityRequest{
		Identifier: "payments-api",
		Title:      "Payments API",
		Properties: map[string]any{"tier": "2"},
		Relations:  map[string][]string{"owner": {"payments"}},
	}
}

func TestEntityWritesNeedCatalogWrite(t *testing.T) {
	cases := []struct {
		name   string
		as     string
		status int
	}{
		{"signed out", "", http.StatusUnauthorized},
		{"member", "cara", http.StatusForbidden},
		{"member api token", "gpt_viewer", http.StatusForbidden},
		{"maintainer", "bob", http.StatusCreated},
		{"maintainer api token", "gpt_ci", http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newCatalog(t)

			expectStatus(t, h.do(http.MethodPost, servicesPath, tc.as, paymentsAPI()), tc.status)
		})
	}
}

func TestMembersReadTheCatalog(t *testing.T) {
	h := newCatalog(t)

	blueprints := decode[blueprintsResponse](t, h.do(http.MethodGet, "/api/v1/catalog/blueprints", "cara", nil))
	if len(blueprints.Blueprints) != 2 || blueprints.Blueprints[0].Identifier != "service" || blueprints.Blueprints[0].EntityCount != 1 {
		t.Fatalf("blueprints = %+v", blueprints)
	}
	team := decode[blueprintView](t, h.do(http.MethodGet, "/api/v1/catalog/blueprints/team", "cara", nil))
	wantInverses := []inverseView{
		{Source: "service", SourceTitle: "Service", Relation: "owner", Title: "Services"},
		{Source: "team", SourceTitle: "Team", Relation: "parent", Title: "Team (Parent team)"},
	}
	if !reflect.DeepEqual(team.Inverses, wantInverses) || !reflect.DeepEqual(team.Columns, []string{"email", "parent"}) {
		t.Fatalf("team = %+v", team)
	}
	expectStatus(t, h.do(http.MethodGet, "/api/v1/catalog/blueprints/group", "cara", nil), http.StatusNotFound)
	expectStatus(t, h.do(http.MethodGet, servicesPath+"/ledger", "cara", nil), http.StatusOK)
	expectStatus(t, h.do(http.MethodGet, servicesPath+"/ledger", "", nil), http.StatusUnauthorized)
}

func TestCreateEntityLinksItAndRecordsIt(t *testing.T) {
	h := newCatalog(t)

	response := h.do(http.MethodPost, servicesPath, "bob", paymentsAPI())

	expectStatus(t, response, http.StatusCreated)
	created := decode[entityResponse](t, response)
	if created.Integration != (integrationView{Identifier: "manual", Title: "Manual", Manual: true}) {
		t.Fatalf("integration = %+v", created.Integration)
	}
	if owner := created.Relations[0]; owner.Key != "owner" || len(owner.Entities) != 1 || owner.Entities[0].Title != "Payments" {
		t.Fatalf("relations = %+v", created.Relations)
	}
	entry := h.audit.entries[0]
	if entry.Action != audit.EntityCreated || entry.Target.ID != "service/payments-api" || entry.Target.Name != "Payments API" {
		t.Fatalf("audit entry = %+v", entry)
	}

	team := decode[entityResponse](t, h.do(http.MethodGet, "/api/v1/catalog/blueprints/team/entities/payments", "cara", nil))
	if len(team.Incoming) != 1 || team.Incoming[0].Title != "Services" || team.Incoming[0].Entities[0].Identifier != "payments-api" {
		t.Fatalf("incoming = %+v, want the service under Services", team.Incoming)
	}
	expectStatus(t, h.do(http.MethodPost, servicesPath, "bob", paymentsAPI()), http.StatusConflict)
}

func TestCreateEntityReportsInvalidFields(t *testing.T) {
	h := newCatalog(t)

	response := h.do(http.MethodPost, servicesPath, "bob", entityRequest{
		Identifier: "payments api",
		Title:      " ",
		Properties: map[string]any{"tier": "9", "homepage": "https://example.test"},
		Relations:  map[string][]string{"owner": {"payments", "platform"}, "team": {"payments"}},
	})

	expectStatus(t, response, http.StatusUnprocessableEntity)
	fields := decodeError(t, response).Error.Fields
	for _, field := range []string{"identifier", "title", "properties.tier", "properties.homepage", "relations.owner", "relations.team"} {
		if fields[field] == "" {
			t.Errorf("fields = %v, want a problem with %s", fields, field)
		}
	}
	if fields["relations.owner"] != "Owner links to one Team" {
		t.Errorf("owner problem = %q", fields["relations.owner"])
	}

	missing := paymentsAPI()
	missing.Relations["owner"] = []string{"billing"}
	response = h.do(http.MethodPost, servicesPath, "bob", missing)
	expectStatus(t, response, http.StatusUnprocessableEntity)
	if problem := decodeError(t, response).Error.Fields["relations.owner"]; problem != "no Team has the identifier billing" {
		t.Fatalf("owner problem = %q", problem)
	}
}

func TestLinksWithinABlueprintCannotLoop(t *testing.T) {
	h := newCatalog(t)
	teamsPath := "/api/v1/catalog/blueprints/team/entities"
	update := func(identifier, title, parent string) *entityPathResponse {
		response := h.do(http.MethodPut, teamsPath+"/"+identifier, "bob", entityRequest{Title: title, Relations: map[string][]string{"parent": {parent}}})
		return &entityPathResponse{code: response.Code, fields: decodeError(t, response).Error.Fields}
	}

	if self := update("payments", "Payments", "payments"); self.code != http.StatusUnprocessableEntity || self.fields["relations.parent"] != "an entity cannot link to itself" {
		t.Fatalf("self link = %+v", self)
	}
	if first := update("payments", "Payments", "platform"); first.code != http.StatusOK {
		t.Fatalf("payments under platform = %+v", first)
	}
	loop := update("platform", "Platform", "payments")
	if loop.code != http.StatusUnprocessableEntity || !strings.Contains(loop.fields["relations.parent"], "cannot form a loop") {
		t.Fatalf("loop = %+v", loop)
	}
}

type entityPathResponse struct {
	code   int
	fields map[string]string
}

func TestEntitiesOfAnIntegrationAreReadOnly(t *testing.T) {
	h := newCatalog(t)

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		response := h.do(method, servicesPath+"/ledger", "bob", entityRequest{Title: "Ledger"})
		expectStatus(t, response, http.StatusConflict)
		if message := decodeError(t, response).Error.Message; message != "the CI integration manages this entity, change it at its source" {
			t.Fatalf("%s message = %q", method, message)
		}
	}
	if len(h.audit.entries) != 0 {
		t.Fatalf("audit = %+v, want nothing recorded", h.audit.entries)
	}
}

func TestUpdateAndDeleteAManualEntity(t *testing.T) {
	h := newCatalog(t)
	h.do(http.MethodPost, servicesPath, "bob", paymentsAPI())

	edited := paymentsAPI()
	edited.Identifier = "ignored"
	edited.Title = "Payments gateway"
	edited.Relations = map[string][]string{"owner": {"platform"}}
	response := h.do(http.MethodPut, servicesPath+"/payments-api", "alice", edited)

	expectStatus(t, response, http.StatusOK)
	updated := decode[entityResponse](t, response)
	if updated.Identifier != "payments-api" || updated.Title != "Payments gateway" {
		t.Fatalf("updated = %+v", updated)
	}
	if entry := h.audit.entries[1]; entry.Action != audit.EntityUpdated || !reflect.DeepEqual(entry.Details["changed"], []string{"title", "owner"}) {
		t.Fatalf("audit entry = %+v", entry)
	}

	expectStatus(t, h.do(http.MethodDelete, servicesPath+"/payments-api", "bob", nil), http.StatusNoContent)
	if entry := h.audit.entries[2]; entry.Action != audit.EntityDeleted || entry.Target.Name != "Payments gateway" {
		t.Fatalf("audit entry = %+v", entry)
	}
	expectStatus(t, h.do(http.MethodDelete, servicesPath+"/payments-api", "bob", nil), http.StatusNotFound)
}

func TestListEntitiesSearchesAndCountsIntegrations(t *testing.T) {
	h := newCatalog(t)
	h.do(http.MethodPost, servicesPath, "bob", paymentsAPI())

	listed := decode[entitiesResponse](t, h.do(http.MethodGet, servicesPath+"?q=pay", "cara", nil))
	if listed.Total != 1 || listed.Entities[0].Identifier != "payments-api" || listed.Limit != defaultEntityLimit {
		t.Fatalf("search = %+v", listed)
	}
	wantCounts := []integrationCount{
		{integrationView: integrationView{Identifier: "ci", Title: "CI"}, Count: 1},
		{integrationView: integrationView{Identifier: "manual", Title: "Manual", Manual: true}, Count: 1},
	}
	if !reflect.DeepEqual(listed.Integrations, wantCounts) {
		t.Fatalf("integrations = %+v", listed.Integrations)
	}
	byCI := decode[entitiesResponse](t, h.do(http.MethodGet, servicesPath+"?integration=ci", "cara", nil))
	if byCI.Total != 1 || byCI.Entities[0].Integration.Title != "CI" {
		t.Fatalf("filter = %+v", byCI)
	}
}
