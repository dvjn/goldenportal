// Package audit records who changed what, for admins to review.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Actions recorded in the audit log.
const (
	SignIn         = "auth.sign_in"
	SignOut        = "auth.sign_out"
	TokenCreate    = "token.create"
	TokenRevoke    = "token.revoke"
	AccessGrant    = "access.grant"
	AccessRevoke   = "access.revoke"
	BootstrapAdmin = "access.bootstrap_admin"

	SCIMUserProvision  = "scim.user_provision"
	SCIMUserDeactivate = "scim.user_deactivate"
	SCIMUserReactivate = "scim.user_reactivate"
	SCIMGroupProvision = "scim.group_provision"
	SCIMGroupDelete    = "scim.group_delete"

	BlueprintDraftSave    = "blueprint.draft_save"
	BlueprintDraftDiscard = "blueprint.draft_discard"
	BlueprintPublish      = "blueprint.publish"
	BlueprintDelete       = "blueprint.delete"

	IntegrationDraftSave    = "integration.draft_save"
	IntegrationDraftDiscard = "integration.draft_discard"
	IntegrationPublish      = "integration.publish"
	IntegrationDelete       = "integration.delete"
	IngestionTokenCreate    = "integration.token_create"
	IngestionTokenRevoke    = "integration.token_revoke"
)

// Actions lists every action, for filters.
var Actions = []string{
	SignIn, SignOut, TokenCreate, TokenRevoke, AccessGrant, AccessRevoke, BootstrapAdmin,
	SCIMUserProvision, SCIMUserDeactivate, SCIMUserReactivate, SCIMGroupProvision, SCIMGroupDelete,
	BlueprintDraftSave, BlueprintDraftDiscard, BlueprintPublish, BlueprintDelete,
	IntegrationDraftSave, IntegrationDraftDiscard, IntegrationPublish, IntegrationDelete,
	IngestionTokenCreate, IngestionTokenRevoke,
}

type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Logger receives every event too, so the server log carries the audit trail.
type Logger interface {
	Infof(format string, args ...any)
}

// Actor is who did something. ID is empty for the system itself.
type Actor struct {
	ID    string `json:"id,omitempty"`
	Label string `json:"label"`
}

type Target struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Event struct {
	ID      int64          `json:"id"`
	At      time.Time      `json:"at"`
	Actor   Actor          `json:"actor"`
	Action  string         `json:"action"`
	Target  Target         `json:"target"`
	Details map[string]any `json:"details"`
}

// Record stores an event and writes it to the log.
func Record(ctx context.Context, db DB, logger Logger, actor Actor, action string, target Target, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return err
	}

	var actorID any
	if actor.ID != "" {
		actorID = actor.ID
	}

	logger.Infof("audit: %s %s %s %q %s", actor.Label, action, target.Type, target.Label, detailsJSON)

	_, err = db.ExecContext(ctx, `
		INSERT INTO audit_events (actor_id, actor_label, action, target_type, target_id, target_label, details)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		actorID, actor.Label, action, target.Type, target.ID, target.Label, detailsJSON)

	return err
}

// Query picks a page of audit events. Empty fields don't filter.
type Query struct {
	// Search matches the actor, the target or the action, ignoring case.
	Search  string
	Actions []string
	// From and To bound when events happened: From inclusive, To exclusive.
	From, To time.Time
	Page     int
	PageSize int
}

// Page is one page of events, newest first, and how many events match in all.
type Page struct {
	Events []Event `json:"events"`
	Total  int     `json:"total"`
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// List returns the page of events that match q.
func List(ctx context.Context, db DB, q Query) (Page, error) {
	var from, to any
	if !q.From.IsZero() {
		from = q.From
	}
	if !q.To.IsZero() {
		to = q.To
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, at, coalesce(actor_id::text, ''), actor_label, action,
			target_type, target_id, target_label, details, count(*) OVER ()
		FROM audit_events
		WHERE ($1 = '' OR actor_label ILIKE $1 OR target_label ILIKE $1 OR action ILIKE $1)
			AND (coalesce(cardinality($2::text[]), 0) = 0 OR action = ANY($2))
			AND ($3::timestamptz IS NULL OR at >= $3)
			AND ($4::timestamptz IS NULL OR at < $4)
		ORDER BY id DESC
		LIMIT $5 OFFSET $6`,
		search(q.Search), pq.Array(q.Actions), from, to, q.PageSize, (q.Page-1)*q.PageSize)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()

	page := Page{Events: []Event{}}
	for rows.Next() {
		var (
			e       Event
			details []byte
		)
		if err := rows.Scan(&e.ID, &e.At, &e.Actor.ID, &e.Actor.Label, &e.Action,
			&e.Target.Type, &e.Target.ID, &e.Target.Label, &details, &page.Total); err != nil {
			return Page{}, err
		}
		if err := json.Unmarshal(details, &e.Details); err != nil {
			return Page{}, err
		}
		page.Events = append(page.Events, e)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}

	// A page past the end has no rows to carry the total, so count separately.
	if len(page.Events) == 0 && q.Page > 1 {
		return countOnly(ctx, db, q)
	}

	return page, nil
}

func countOnly(ctx context.Context, db DB, q Query) (Page, error) {
	var from, to any
	if !q.From.IsZero() {
		from = q.From
	}
	if !q.To.IsZero() {
		to = q.To
	}

	rows, err := db.QueryContext(ctx, `
		SELECT count(*) FROM audit_events
		WHERE ($1 = '' OR actor_label ILIKE $1 OR target_label ILIKE $1 OR action ILIKE $1)
			AND (coalesce(cardinality($2::text[]), 0) = 0 OR action = ANY($2))
			AND ($3::timestamptz IS NULL OR at >= $3)
			AND ($4::timestamptz IS NULL OR at < $4)`,
		search(q.Search), pq.Array(q.Actions), from, to)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()

	page := Page{Events: []Event{}}
	if rows.Next() {
		if err := rows.Scan(&page.Total); err != nil {
			return Page{}, err
		}
	}

	return page, rows.Err()
}

// search turns free text into an ILIKE pattern, treating % and _ literally.
func search(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	return "%" + likeEscaper.Replace(text) + "%"
}
