package audit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gofr.dev/pkg/gofr/container"

	"github.com/dvjn/goldport/internal/httpx"
)

func newLog(t *testing.T) (*Log, sqlmock.Sqlmock) {
	t.Helper()
	c, mocks := container.NewMockContainer(t)

	return NewLog(c.SQL), mocks.SQL
}

func contextOfRequest(t *testing.T, request *http.Request) context.Context {
	t.Helper()
	var ctx context.Context
	httpx.ExposeRaw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
	})).ServeHTTP(httptest.NewRecorder(), request)

	return ctx
}

func TestRecordStoresTheEntry(t *testing.T) {
	log, db := newLog(t)
	db.ExpectExec(queryRecord).
		WithArgs(ActorUser, "u-1", "Alice", "alice@example.test", GrantCreated,
			ResourceGroup, "g-1", "platform-team", "", "", "",
			`{"role":"admin"}`, "", "").
		WillReturnResult(sqlmock.NewResult(1, 1))
	db.ExpectExec(queryRecord).
		WithArgs(ActorUser, "", "alice@example.test", "alice@example.test", SignedOut,
			"", "", "", "", "", "",
			`{}`, "", "").
		WillReturnResult(sqlmock.NewResult(2, 1))

	err := log.Record(context.Background(), Entry{
		Actor:    Actor{Kind: ActorUser, ID: "u-1", Name: "Alice", Email: "alice@example.test"},
		Action:   GrantCreated,
		Resource: Resource{Kind: ResourceGroup, ID: "g-1", Name: "platform-team"},
		Details:  map[string]any{"role": "admin"},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	err = log.Record(context.Background(), Entry{
		Actor:  Actor{Kind: ActorUser, Name: "alice@example.test", Email: "alice@example.test"},
		Action: SignedOut,
	})
	if err != nil {
		t.Fatalf("Record() without details error = %v", err)
	}
}

func TestRecordStoresTheResourceParent(t *testing.T) {
	log, db := newLog(t)
	db.ExpectExec(queryRecord).
		WithArgs(ActorSystem, "", "Schedule", "", EntityUpdated,
			ResourceEntity, "web", "Web", ResourceBlueprint, "service", "Service",
			`{}`, "", "").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := log.Record(context.Background(), Entry{
		Actor:  Scheduler,
		Action: EntityUpdated,
		Resource: Resource{
			Kind: ResourceEntity, ID: "web", Name: "Web",
			Parent: Parent{Kind: ResourceBlueprint, ID: "service", Name: "Service"},
		},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
}

func TestRecordFillsTheClientFromTheRequest(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		wantIP     string
	}{
		{
			name:       "first forwarded address",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.7, 10.0.0.1", "X-Real-IP": "198.51.100.2"},
			remoteAddr: "10.0.0.9:4000",
			wantIP:     "203.0.113.7",
		},
		{
			name:       "real address",
			headers:    map[string]string{"X-Real-IP": "198.51.100.2"},
			remoteAddr: "10.0.0.9:4000",
			wantIP:     "198.51.100.2",
		},
		{
			name:       "remote host",
			remoteAddr: "10.0.0.9:4000",
			wantIP:     "10.0.0.9",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, db := newLog(t)
			db.ExpectExec(queryRecord).
				WithArgs(ActorUser, "u-1", "Alice", "", SignedIn,
					"", "", "", "", "", "",
					`{}`, tt.wantIP, "goldport-test/1.0").
				WillReturnResult(sqlmock.NewResult(1, 1))
			request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
			request.RemoteAddr = tt.remoteAddr
			request.Header.Set("User-Agent", "goldport-test/1.0")
			for name, value := range tt.headers {
				request.Header.Set(name, value)
			}

			err := log.Record(contextOfRequest(t, request), Entry{
				Actor:  Actor{Kind: ActorUser, ID: "u-1", Name: "Alice"},
				Action: SignedIn,
			})
			if err != nil {
				t.Fatalf("Record() error = %v", err)
			}
		})
	}
}

func TestRecordKeepsTheClientTheEntryCarries(t *testing.T) {
	log, db := newLog(t)
	db.ExpectExec(queryRecord).
		WithArgs(ActorUser, "", "Alice", "", SignInRefused,
			"", "", "", "", "", "",
			`{}`, "192.0.2.1", "curl/8").
		WillReturnResult(sqlmock.NewResult(1, 1))
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	request.Header.Set("X-Forwarded-For", "203.0.113.7")

	err := log.Record(contextOfRequest(t, request), Entry{
		Actor:  Actor{Kind: ActorUser, Name: "Alice"},
		Action: SignInRefused,
		Client: Client{IP: "192.0.2.1", UserAgent: "curl/8"},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
}

func TestActorUserIDAndLabel(t *testing.T) {
	tests := []struct {
		name      string
		actor     Actor
		wantID    string
		wantLabel string
	}{
		{"user", Actor{Kind: ActorUser, ID: "u-1", Name: "Alice", Email: "alice@example.test"}, "u-1", "alice@example.test"},
		{"user without email", Actor{Kind: ActorUser, ID: "u-1", Name: "Alice"}, "u-1", "Alice"},
		{"service token", Actor{Kind: ActorServiceToken, ID: "t-1", Name: "ci"}, "", "ci"},
		{"scheduler", Scheduler, "", "Schedule"},
		{"provisioner", SCIMProvisioner, "", "SCIM provisioning"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.actor.UserID(); got != tt.wantID {
				t.Errorf("UserID() = %q, want %q", got, tt.wantID)
			}
			if got := tt.actor.Label(); got != tt.wantLabel {
				t.Errorf("Label() = %q, want %q", got, tt.wantLabel)
			}
		})
	}
}

func TestListReturnsAPageNewestFirst(t *testing.T) {
	log, db := newLog(t)
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	db.ExpectQuery(queryCount).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	db.ExpectQuery(fmt.Sprintf(queryList, "DESC")).WillReturnRows(sqlmock.NewRows([]string{
		"id", "at", "actor_kind", "actor_id", "actor_name", "actor_email", "action",
		"resource_kind", "resource_id", "resource_name",
		"resource_parent_kind", "resource_parent_id", "resource_parent_name",
		"details", "ip_address", "user_agent",
	}).AddRow(2, at, ActorServiceToken, "t-1", "ci", "", ServiceTokenRevoked,
		ResourceServiceToken, "t-1", "ci", "", "", "",
		[]byte(`{"role":"member"}`), "203.0.113.7", "curl/8"))

	entries, total, err := log.List(context.Background(), Query{Page: Page{Offset: 1, Limit: 1}})
	if err != nil || total != 3 || len(entries) != 1 {
		t.Fatalf("List() = %v, %d, %v", entries, total, err)
	}
	want := Entry{
		ID: 2, At: at, Actor: Actor{Kind: ActorServiceToken, ID: "t-1", Name: "ci"}, Action: ServiceTokenRevoked,
		Resource: Resource{Kind: ResourceServiceToken, ID: "t-1", Name: "ci"},
		Details:  map[string]any{"role": "member"},
		Client:   Client{IP: "203.0.113.7", UserAgent: "curl/8"},
	}
	if !reflect.DeepEqual(entries[0], want) {
		t.Fatalf("List()[0] = %+v, want %+v", entries[0], want)
	}
}

func TestFacetsReturnsTheActorsAndTheKinds(t *testing.T) {
	log, db := newLog(t)
	db.ExpectQuery(queryFacetActors).WillReturnRows(sqlmock.NewRows([]string{
		"actor_kind", "actor_id", "actor_name", "actor_email",
	}).
		AddRow(ActorServiceToken, "t-1", "ci", "").
		AddRow(ActorUser, "u-1", "Alice", "alice@example.test"))
	db.ExpectQuery(queryFacets).WillReturnRows(sqlmock.NewRows([]string{"actor_kinds", "actions", "resource_kinds"}).
		AddRow(`{service_token,user}`, `{auth.signed_in,grant.created}`, `{group,user}`))

	facets, err := log.Facets(context.Background())
	if err != nil {
		t.Fatalf("Facets() error = %v", err)
	}
	want := Facets{
		Actors: []Actor{
			{Kind: ActorServiceToken, ID: "t-1", Name: "ci"},
			{Kind: ActorUser, ID: "u-1", Name: "Alice", Email: "alice@example.test"},
		},
		ActorKinds:    []string{"service_token", "user"},
		Actions:       []string{"auth.signed_in", "grant.created"},
		ResourceKinds: []string{"group", "user"},
	}
	if !reflect.DeepEqual(facets, want) {
		t.Fatalf("Facets() = %+v, want %+v", facets, want)
	}
}

func TestFacetsOfAnEmptyLogAreEmptyLists(t *testing.T) {
	log, db := newLog(t)
	db.ExpectQuery(queryFacetActors).WillReturnRows(sqlmock.NewRows([]string{
		"actor_kind", "actor_id", "actor_name", "actor_email",
	}))
	db.ExpectQuery(queryFacets).WillReturnRows(sqlmock.NewRows([]string{"actor_kinds", "actions", "resource_kinds"}).
		AddRow(`{}`, `{}`, `{}`))

	facets, err := log.Facets(context.Background())
	if err != nil {
		t.Fatalf("Facets() error = %v", err)
	}
	want := Facets{Actors: []Actor{}, ActorKinds: []string{}, Actions: []string{}, ResourceKinds: []string{}}
	if !reflect.DeepEqual(facets, want) {
		t.Fatalf("Facets() = %+v, want %+v", facets, want)
	}
}
