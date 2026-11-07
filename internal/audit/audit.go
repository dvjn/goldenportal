// Package audit records who changed what, for admins to review.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/httpx"
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
	AppAuthorize   = "app.authorize"
	AppRevoke      = "app.revoke"

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

	ScorecardDraftSave    = "scorecard.draft_save"
	ScorecardDraftDiscard = "scorecard.draft_discard"
	ScorecardPublish      = "scorecard.publish"
	ScorecardDelete       = "scorecard.delete"

	ActionDraftSave    = "action.draft_save"
	ActionDraftDiscard = "action.draft_discard"
	ActionPublish      = "action.publish"
	ActionDelete       = "action.delete"
	ActionRun          = "action.run"
	ActionRunCancel    = "action.run_cancel"
	ActionRunRetry     = "action.run_retry"
	ActionRunResolve   = "action.run_resolve"
	ActionRunApprove   = "action.run_approve"
	ActionRunReject    = "action.run_reject"

	WorkflowDraftSave    = "workflow.draft_save"
	WorkflowDraftDiscard = "workflow.draft_discard"
	WorkflowPublish      = "workflow.publish"
	WorkflowDelete       = "workflow.delete"
)

// Actions lists every action, for filters.
var Actions = []string{
	// TokenCreate and TokenRevoke are personal API tokens, which only past
	// events hold.
	SignIn, SignOut, TokenCreate, TokenRevoke, AccessGrant, AccessRevoke, BootstrapAdmin,
	AppAuthorize, AppRevoke,
	SCIMUserProvision, SCIMUserDeactivate, SCIMUserReactivate, SCIMGroupProvision, SCIMGroupDelete,
	BlueprintDraftSave, BlueprintDraftDiscard, BlueprintPublish, BlueprintDelete,
	IntegrationDraftSave, IntegrationDraftDiscard, IntegrationPublish, IntegrationDelete,
	IngestionTokenCreate, IngestionTokenRevoke,
	ScorecardDraftSave, ScorecardDraftDiscard, ScorecardPublish, ScorecardDelete,
	ActionDraftSave, ActionDraftDiscard, ActionPublish, ActionDelete,
	ActionRun, ActionRunCancel, ActionRunRetry, ActionRunResolve, ActionRunApprove, ActionRunReject,
	WorkflowDraftSave, WorkflowDraftDiscard, WorkflowPublish, WorkflowDelete,
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
	// Name is the person's current name, when the actor is a user, so
	// events name people the way access does.
	Name string `json:"name,omitempty"`
}

type Target struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Event struct {
	ID         int64          `json:"id"`
	OccurredAt time.Time      `json:"occurredAt"`
	Actor      Actor          `json:"actor"`
	Action     string         `json:"action"`
	Target     Target         `json:"target"`
	Details    map[string]any `json:"details"`
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
type Page = httpx.Page[Event]

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
		SELECT a.id, a.at, coalesce(a.actor_id::text, ''), a.actor_label, coalesce(u.display_name, ''),
			a.action, a.target_type, a.target_id, a.target_label, a.details, count(*) OVER ()
		FROM audit_events a
		LEFT JOIN users u ON u.id = a.actor_id
		WHERE ($1 = '' OR actor_label ILIKE $1 OR u.display_name ILIKE $1 OR target_label ILIKE $1 OR action ILIKE $1)
			AND (coalesce(cardinality($2::text[]), 0) = 0 OR action = ANY($2))
			AND ($3::timestamptz IS NULL OR at >= $3)
			AND ($4::timestamptz IS NULL OR at < $4)
		ORDER BY a.id DESC
		LIMIT $5 OFFSET $6`,
		search(q.Search), pq.Array(q.Actions), from, to, q.PageSize, (q.Page-1)*q.PageSize)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()

	page := Page{Items: []Event{}}
	for rows.Next() {
		var (
			e       Event
			details []byte
		)
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.Actor.ID, &e.Actor.Label, &e.Actor.Name, &e.Action,
			&e.Target.Type, &e.Target.ID, &e.Target.Label, &details, &page.Total); err != nil {
			return Page{}, err
		}
		if err := json.Unmarshal(details, &e.Details); err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, e)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}

	// A page past the end has no rows to carry the total, so count separately.
	if len(page.Items) == 0 && q.Page > 1 {
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
		SELECT count(*) FROM audit_events a
		LEFT JOIN users u ON u.id = a.actor_id
		WHERE ($1 = '' OR actor_label ILIKE $1 OR u.display_name ILIKE $1 OR target_label ILIKE $1 OR action ILIKE $1)
			AND (coalesce(cardinality($2::text[]), 0) = 0 OR action = ANY($2))
			AND ($3::timestamptz IS NULL OR at >= $3)
			AND ($4::timestamptz IS NULL OR at < $4)`,
		search(q.Search), pq.Array(q.Actions), from, to)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()

	page := Page{Items: []Event{}}
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
