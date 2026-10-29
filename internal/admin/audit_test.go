package admin

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/dvjn/goldport/internal/audit"
)

func TestListAuditShapesActorResourceAndClient(t *testing.T) {
	h := newHarness()
	h.audit.entries = []audit.Entry{
		{
			ID: 7, At: at,
			Actor:  audit.Actor{Kind: audit.ActorUser, ID: "u-1", Name: "Alice Archer", Email: "alice@example.test"},
			Action: audit.EntityUpdated,
			Resource: audit.Resource{
				Kind: audit.ResourceEntity, ID: "checkout", Name: "Checkout",
				Parent: audit.Parent{Kind: audit.ResourceBlueprint, ID: "service", Name: "Service"},
			},
			Client: audit.Client{IP: "203.0.113.7", UserAgent: "curl/8"},
		},
		{ID: 6, At: at, Actor: audit.Scheduler, Action: audit.IntegrationSynced, Resource: audit.Resource{Kind: audit.ResourceIntegration, ID: "gitlab"}},
	}

	response := h.do(t, http.MethodGet, "/api/v1/audit", "u-1", "")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	got := decode[auditResponse](t, response.Body.Bytes())
	wantFirst := auditEntry{
		ID: 7, At: at,
		Actor:  auditActor{Kind: audit.ActorUser, ID: "u-1", Name: "Alice Archer", Email: "alice@example.test"},
		Action: audit.EntityUpdated,
		Resource: auditResource{
			Kind: audit.ResourceEntity, ID: "checkout", Name: "Checkout",
			Parent: &auditParent{Kind: audit.ResourceBlueprint, ID: "service", Name: "Service"},
		},
		Client: auditClient{IP: "203.0.113.7", UserAgent: "curl/8"},
	}
	if len(got.Entries) != 2 || !reflect.DeepEqual(got.Entries[0], wantFirst) {
		t.Fatalf("entries = %+v, want first %+v", got.Entries, wantFirst)
	}
	if got.Entries[1].Resource.Parent != nil || got.Entries[1].Actor.Kind != audit.ActorSystem {
		t.Fatalf("second entry = %+v, want a system actor and no parent", got.Entries[1])
	}
}

func TestListAuditReadsTheFilters(t *testing.T) {
	h := newHarness()

	response := h.do(t, http.MethodGet,
		"/api/v1/audit?actor=user:u-1,api_token:t-1&actor_kind=user&action=entity.updated,entity.created&resource_kind=entity&q=check&sort=asc&limit=10",
		"u-1", "")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	query := h.audit.queries[0]
	if !reflect.DeepEqual(query.Actors, []string{"user:u-1", "api_token:t-1"}) ||
		!reflect.DeepEqual(query.ActorKinds, []string{"user"}) ||
		!reflect.DeepEqual(query.Actions, []string{"entity.updated", "entity.created"}) ||
		!reflect.DeepEqual(query.ResourceKinds, []string{"entity"}) ||
		query.Search != "check" || !query.Ascending || query.Limit != 10 {
		t.Fatalf("query = %+v", query)
	}
}

func TestAuditFacetsListActorsAndKinds(t *testing.T) {
	h := newHarness()
	h.audit.facets = audit.Facets{
		Actors:        []audit.Actor{{Kind: audit.ActorAPIToken, ID: "t-1", Name: "ci"}},
		ActorKinds:    []string{"api_token"},
		Actions:       []string{"entity.updated"},
		ResourceKinds: []string{"entity"},
	}

	response := h.do(t, http.MethodGet, "/api/v1/audit/facets", "u-1", "")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	got := decode[auditFacetsResponse](t, response.Body.Bytes())
	want := auditFacetsResponse{
		Actors:        []auditActor{{Kind: audit.ActorAPIToken, ID: "t-1", Name: "ci"}},
		ActorKinds:    []string{"api_token"},
		Actions:       []string{"entity.updated"},
		ResourceKinds: []string{"entity"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("facets = %+v, want %+v", got, want)
	}
}
