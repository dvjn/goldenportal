// Package audit keeps the log of who signed in, who changed access, who
// changed the catalog's definitions and entities, who provisioned users and
// groups, and who synced, pushed to or gave tokens to integrations.
package audit

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr/container"
)

type Action string

const (
	SignedIn            Action = "auth.signed_in"
	SignedOut           Action = "auth.signed_out"
	SignInRefused       Action = "auth.sign_in_refused"
	GrantCreated        Action = "grant.created"
	GrantChanged        Action = "grant.changed"
	GrantRemoved        Action = "grant.removed"
	SessionsRevoked     Action = "sessions.revoked"
	SCIMTokenRotated    Action = "scim_token.rotated"
	ServiceTokenCreated Action = "service_token.created"
	ServiceTokenRevoked Action = "service_token.revoked"

	UserProvisioned   Action = "user.provisioned"
	UserUpdated       Action = "user.updated"
	UserDeprovisioned Action = "user.deprovisioned"
	GroupProvisioned  Action = "group.provisioned"
	GroupUpdated      Action = "group.updated"
	GroupDeleted      Action = "group.deleted"

	DefinitionsApplied    Action = "definitions.applied"
	DefinitionsRolledBack Action = "definitions.rolled_back"
	EntityCreated         Action = "entity.created"
	EntityUpdated         Action = "entity.updated"
	EntityDeleted         Action = "entity.deleted"

	IntegrationSynced  Action = "integration.synced"
	IntegrationPushed  Action = "integration.pushed"
	IngestTokenCreated Action = "ingest_token.created"
	IngestTokenRevoked Action = "ingest_token.revoked"
)

type ActorKind string

const (
	ActorUser         ActorKind = "user"
	ActorServiceToken ActorKind = "service_token"
	ActorIngestToken  ActorKind = "ingest_token"
	ActorSCIM         ActorKind = "scim"
	ActorSystem       ActorKind = "system"
)

// Actor is who did it, as they were at the time: a user, a token, the SCIM
// provisioner or the portal itself. ID is the user's or the token's ID and
// is empty for the provisioner, the portal and people who could not sign in.
type Actor struct {
	Kind  ActorKind
	ID    string
	Name  string
	Email string
}

var (
	SCIMProvisioner = Actor{Kind: ActorSCIM, Name: "SCIM provisioning"}
	Scheduler       = Actor{Kind: ActorSystem, Name: "Schedule"}
)

// UserID returns the ID of the user behind the actor, or an empty string
// when the actor is not a user.
func (a Actor) UserID() string {
	if a.Kind != ActorUser {
		return ""
	}

	return a.ID
}

// Label names the actor in one string: a user's email, otherwise the name.
func (a Actor) Label() string {
	return cmp.Or(a.Email, a.Name)
}

type ResourceKind string

const (
	ResourceUser         ResourceKind = "user"
	ResourceGroup        ResourceKind = "group"
	ResourceServiceToken ResourceKind = "service_token"
	ResourceSCIMToken    ResourceKind = "scim_token"
	ResourceRevision     ResourceKind = "definitions_revision"
	ResourceBlueprint    ResourceKind = "blueprint"
	ResourceEntity       ResourceKind = "entity"
	ResourceIntegration  ResourceKind = "integration"
	ResourceIngestToken  ResourceKind = "ingest_token"
)

// Resource is what it was done to, with the resource it belongs to when it
// has one, such as an entity's blueprint or an ingest token's integration.
// Names keep the resources' names at the time, so the entry stays readable
// after they are gone.
type Resource struct {
	Kind   ResourceKind
	ID     string
	Name   string
	Parent Parent
}

type Parent struct {
	Kind ResourceKind
	ID   string
	Name string
}

// Client is where the request came from. It is empty for what the portal
// does on its own.
type Client struct {
	IP        string
	UserAgent string
}

// Change is a field's value before and after an update.
type Change struct {
	Before any `json:"before"`
	After  any `json:"after"`
}

// Changes holds the fields an update changed, by field. Entries keep them
// under ChangesKey in their details.
type Changes map[string]Change

const ChangesKey = "changes"

type Entry struct {
	ID       int64
	At       time.Time
	Actor    Actor
	Action   Action
	Resource Resource
	Details  map[string]any
	Client   Client
}

type Page struct {
	Offset int
	Limit  int
}

// Query selects a page of entries. Search matches the actor, action,
// resource and client address in part, ignoring case; Actors take
// "kind:id" values; Actors, ActorKinds, Actions and ResourceKinds match any
// of their values; From and To bound when, each when set.
type Query struct {
	Page
	Search        string
	Actors        []string
	ActorKinds    []string
	Actions       []string
	ResourceKinds []string
	From          time.Time
	To            time.Time
	Ascending     bool
}

// Facets are the distinct values the log holds, for filters. Each actor
// carries the name and email of its latest entry.
type Facets struct {
	Actors        []Actor
	ActorKinds    []string
	Actions       []string
	ResourceKinds []string
}

type Log struct {
	db container.DB
}

func NewLog(db container.DB) *Log {
	return &Log{db: db}
}

const (
	queryRecord = `INSERT INTO audit_log (actor_kind, actor_id, actor_name, actor_email, action,
			resource_kind, resource_id, resource_name, resource_parent_kind, resource_parent_id, resource_parent_name,
			details, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`
	queryFilter = ` FROM audit_log
		WHERE ($1 = '' OR actor_name ILIKE $1 OR actor_email ILIKE $1 OR action ILIKE $1
				OR resource_kind ILIKE $1 OR resource_id ILIKE $1 OR resource_name ILIKE $1
				OR resource_parent_id ILIKE $1 OR resource_parent_name ILIKE $1 OR ip_address ILIKE $1)
			AND (cardinality($2::text[]) = 0 OR actor_kind || ':' || actor_id = ANY($2))
			AND (cardinality($3::text[]) = 0 OR actor_kind = ANY($3))
			AND (cardinality($4::text[]) = 0 OR action = ANY($4))
			AND (cardinality($5::text[]) = 0 OR resource_kind = ANY($5))
			AND ($6::timestamptz IS NULL OR at >= $6)
			AND ($7::timestamptz IS NULL OR at < $7)`
	queryCount = `SELECT COUNT(*)` + queryFilter
	queryList  = `SELECT id, at, actor_kind, actor_id, actor_name, actor_email, action,
			resource_kind, resource_id, resource_name, resource_parent_kind, resource_parent_id, resource_parent_name,
			details, ip_address, user_agent` + queryFilter + `
		ORDER BY at %[1]s, id %[1]s OFFSET $8 LIMIT $9`
	queryFacetActors = `SELECT actor_kind, actor_id, actor_name, actor_email FROM (
			SELECT DISTINCT ON (actor_kind, actor_id) actor_kind, actor_id, actor_name, actor_email
			FROM audit_log ORDER BY actor_kind, actor_id, at DESC
		) latest ORDER BY lower(COALESCE(NULLIF(actor_email, ''), actor_name))`
	queryFacets = `SELECT
			COALESCE((SELECT array_agg(DISTINCT actor_kind ORDER BY actor_kind) FROM audit_log), '{}'),
			COALESCE((SELECT array_agg(DISTINCT action ORDER BY action) FROM audit_log), '{}'),
			COALESCE((SELECT array_agg(DISTINCT resource_kind ORDER BY resource_kind) FROM audit_log WHERE resource_kind <> ''), '{}')`
)

// Record stores the entry. An entry without a client takes the one of the
// request the context carries.
func (l *Log) Record(ctx context.Context, entry Entry) error {
	details, err := json.Marshal(maps.Collect(maps.All(entry.Details)))
	if err != nil {
		return err
	}
	actor, resource, client := entry.Actor, entry.Resource, cmp.Or(entry.Client, clientOf(ctx))
	_, err = l.db.ExecContext(ctx, queryRecord, actor.Kind, actor.ID, actor.Name, actor.Email, entry.Action,
		resource.Kind, resource.ID, resource.Name, resource.Parent.Kind, resource.Parent.ID, resource.Parent.Name,
		string(details), client.IP, client.UserAgent)

	return err
}

// List returns a page of the entries the query selects, newest first unless
// it asks for oldest first, and the number of entries it selects.
func (l *Log) List(ctx context.Context, query Query) ([]Entry, int, error) {
	filter := []any{
		likePattern(query.Search), pq.Array(nonNil(query.Actors)), pq.Array(nonNil(query.ActorKinds)),
		pq.Array(nonNil(query.Actions)), pq.Array(nonNil(query.ResourceKinds)), nullTime(query.From), nullTime(query.To),
	}
	var total int
	if err := l.db.QueryRowContext(ctx, queryCount, filter...).Scan(&total); err != nil {
		return nil, 0, err
	}
	direction := "DESC"
	if query.Ascending {
		direction = "ASC"
	}
	rows, err := l.db.QueryContext(ctx, fmt.Sprintf(queryList, direction), append(filter, query.Offset, query.Limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries := []Entry{}
	for rows.Next() {
		var (
			entry   Entry
			details []byte
		)
		actor, resource, client := &entry.Actor, &entry.Resource, &entry.Client
		if err := rows.Scan(&entry.ID, &entry.At, &actor.Kind, &actor.ID, &actor.Name, &actor.Email, &entry.Action,
			&resource.Kind, &resource.ID, &resource.Name, &resource.Parent.Kind, &resource.Parent.ID, &resource.Parent.Name,
			&details, &client.IP, &client.UserAgent); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(details, &entry.Details); err != nil {
			return nil, 0, err
		}
		entries = append(entries, entry)
	}

	return entries, total, rows.Err()
}

// Facets returns the distinct actors, actor kinds, actions and resource
// kinds in the log.
func (l *Log) Facets(ctx context.Context) (Facets, error) {
	facets := Facets{Actors: []Actor{}}
	rows, err := l.db.QueryContext(ctx, queryFacetActors)
	if err != nil {
		return Facets{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var actor Actor
		if err := rows.Scan(&actor.Kind, &actor.ID, &actor.Name, &actor.Email); err != nil {
			return Facets{}, err
		}
		facets.Actors = append(facets.Actors, actor)
	}
	if err := rows.Err(); err != nil {
		return Facets{}, err
	}

	err = l.db.QueryRowContext(ctx, queryFacets).Scan(
		pq.Array(&facets.ActorKinds), pq.Array(&facets.Actions), pq.Array(&facets.ResourceKinds))
	facets.ActorKinds, facets.Actions, facets.ResourceKinds = nonNil(facets.ActorKinds), nonNil(facets.Actions), nonNil(facets.ResourceKinds)

	return facets, err
}

// clientOf reads the client of the request the context carries. Behind a
// proxy the first X-Forwarded-For address is the client's.
func clientOf(ctx context.Context) Client {
	request := httpx.RawRequest(ctx)
	if request == nil {
		return Client{}
	}
	forwarded, _, _ := strings.Cut(request.Header.Get("X-Forwarded-For"), ",")
	remote := request.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}

	return Client{
		IP:        cmp.Or(strings.TrimSpace(forwarded), strings.TrimSpace(request.Header.Get("X-Real-IP")), remote),
		UserAgent: request.UserAgent(),
	}
}

func likePattern(search string) string {
	search = strings.TrimSpace(search)
	if search == "" {
		return ""
	}

	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(search) + "%"
}

func nullTime(at time.Time) sql.NullTime {
	return sql.NullTime{Time: at, Valid: !at.IsZero()}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}

	return values
}
