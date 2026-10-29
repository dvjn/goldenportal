package admin

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

const (
	defaultAuditLimit = 50
	maxAuditLimit     = 200
)

type auditActor struct {
	Kind  audit.ActorKind `json:"kind"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Email string          `json:"email"`
}

func newAuditActor(actor audit.Actor) auditActor {
	return auditActor{Kind: actor.Kind, ID: actor.ID, Name: actor.Name, Email: actor.Email}
}

type auditParent struct {
	Kind audit.ResourceKind `json:"kind"`
	ID   string             `json:"id"`
	Name string             `json:"name"`
}

type auditResource struct {
	Kind   audit.ResourceKind `json:"kind"`
	ID     string             `json:"id"`
	Name   string             `json:"name"`
	Parent *auditParent       `json:"parent,omitempty"`
}

func newAuditResource(resource audit.Resource) auditResource {
	response := auditResource{Kind: resource.Kind, ID: resource.ID, Name: resource.Name}
	if parent := resource.Parent; parent != (audit.Parent{}) {
		response.Parent = &auditParent{Kind: parent.Kind, ID: parent.ID, Name: parent.Name}
	}

	return response
}

type auditClient struct {
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
}

type auditEntry struct {
	ID       int64          `json:"id"`
	At       time.Time      `json:"at"`
	Actor    auditActor     `json:"actor"`
	Action   audit.Action   `json:"action"`
	Resource auditResource  `json:"resource"`
	Details  map[string]any `json:"details"`
	Client   auditClient    `json:"client"`
}

type auditResponse struct {
	Entries []auditEntry `json:"entries"`
	Offset  int          `json:"offset"`
	Limit   int          `json:"limit"`
	Total   int          `json:"total"`
}

func (s *Service) listAudit(ctx *gofr.Context) (any, error) {
	query, err := auditQuery(ctx)
	if err != nil {
		return nil, err
	}
	entries, total, err := s.audit.List(ctx, query)
	if err != nil {
		return nil, err
	}
	page := query.Page

	response := auditResponse{Entries: make([]auditEntry, 0, len(entries)), Offset: page.Offset, Limit: page.Limit, Total: total}
	for _, entry := range entries {
		response.Entries = append(response.Entries, auditEntry{
			ID:       entry.ID,
			At:       entry.At,
			Actor:    newAuditActor(entry.Actor),
			Action:   entry.Action,
			Resource: newAuditResource(entry.Resource),
			Details:  entry.Details,
			Client:   auditClient{IP: entry.Client.IP, UserAgent: entry.Client.UserAgent},
		})
	}

	return response, nil
}

// auditQuery reads the filters of the audit list. Actor (as kind:id),
// actor_kind, action and resource_kind take one value or several separated
// by commas; from and to take a time or a date, and to includes the whole
// of a date.
func auditQuery(ctx *gofr.Context) (audit.Query, error) {
	from, err := auditTime(ctx.Param("from"), 0)
	if err != nil {
		return audit.Query{}, err
	}
	to, err := auditTime(ctx.Param("to"), 24*time.Hour)
	if err != nil {
		return audit.Query{}, err
	}

	return audit.Query{
		Page: audit.Page{
			Offset: clamp(ctx.Param("offset"), 0, 0, math.MaxInt),
			Limit:  clamp(ctx.Param("limit"), defaultAuditLimit, 1, maxAuditLimit),
		},
		Search:        ctx.Param("q"),
		Actors:        listParam(ctx.Param("actor")),
		ActorKinds:    listParam(ctx.Param("actor_kind")),
		Actions:       listParam(ctx.Param("action")),
		ResourceKinds: listParam(ctx.Param("resource_kind")),
		From:          from,
		To:            to,
		Ascending:     ctx.Param("sort") == "asc",
	}, nil
}

// auditTime reads a time, or a date with dateOffset added to its start.
func auditTime(value string, dateOffset time.Duration) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if at, err := time.Parse(time.RFC3339, value); err == nil {
		return at, nil
	}
	date, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return time.Time{}, httpx.Error{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("give from and to as a date such as 2026-10-22 or a time such as 2026-10-22T09:00:00Z, not %q", value),
		}
	}

	return date.Add(dateOffset), nil
}

func listParam(value string) []string {
	var values []string
	for item := range strings.SplitSeq(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}

	return values
}

type auditFacetsResponse struct {
	Actors        []auditActor `json:"actors"`
	ActorKinds    []string     `json:"actor_kinds"`
	Actions       []string     `json:"actions"`
	ResourceKinds []string     `json:"resource_kinds"`
}

func (s *Service) auditFacets(ctx *gofr.Context) (any, error) {
	facets, err := s.audit.Facets(ctx)
	if err != nil {
		return nil, err
	}
	actors := make([]auditActor, 0, len(facets.Actors))
	for _, actor := range facets.Actors {
		actors = append(actors, newAuditActor(actor))
	}

	return auditFacetsResponse{
		Actors: actors, ActorKinds: facets.ActorKinds, Actions: facets.Actions, ResourceKinds: facets.ResourceKinds,
	}, nil
}

// clamp parses a query value, falls back when it is not a number and keeps
// it within the bounds.
func clamp(value string, fallback, lowest, highest int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		parsed = fallback
	}

	return min(max(parsed, lowest), highest)
}
