// Package audit keeps the log of who signed in, who changed access, who
// changed the catalog's definitions and entities, and who synced or gave
// tokens to integrations.
package audit

import (
	"context"
	"encoding/json"
	"maps"
	"time"

	"gofr.dev/pkg/gofr/container"
)

type Action string

const (
	SignedIn         Action = "auth.signed_in"
	SignedOut        Action = "auth.signed_out"
	SignInRefused    Action = "auth.sign_in_refused"
	GrantCreated     Action = "grant.created"
	GrantChanged     Action = "grant.changed"
	GrantRemoved     Action = "grant.removed"
	SessionsRevoked  Action = "sessions.revoked"
	SCIMTokenRotated Action = "scim_token.rotated"
	APITokenCreated  Action = "api_token.created"
	APITokenRevoked  Action = "api_token.revoked"

	DefinitionsApplied    Action = "definitions.applied"
	DefinitionsRolledBack Action = "definitions.rolled_back"
	EntityCreated         Action = "entity.created"
	EntityUpdated         Action = "entity.updated"
	EntityDeleted         Action = "entity.deleted"

	IntegrationSynced  Action = "integration.synced"
	IngestTokenCreated Action = "ingest_token.created"
	IngestTokenRevoked Action = "ingest_token.revoked"
)

// Actor is who did it. UserID is empty for API tokens and for people who
// could not sign in; Label always names them.
type Actor struct {
	UserID string
	Label  string
}

// Target is what it was done to. Name keeps the target's name at the time,
// so the entry stays readable after the target is gone.
type Target struct {
	Kind string
	ID   string
	Name string
}

type Entry struct {
	ID      int64
	At      time.Time
	Actor   Actor
	Action  Action
	Target  Target
	Details map[string]any
}

type Page struct {
	Offset int
	Limit  int
}

type Log struct {
	db container.DB
}

func NewLog(db container.DB) *Log {
	return &Log{db: db}
}

const (
	queryRecord = `INSERT INTO audit_log (actor_user_id, actor_label, action, target_kind, target_id, target_name, details)
		VALUES (NULLIF($1, '')::uuid, $2, $3, $4, $5, $6, $7)`
	queryCount = `SELECT COUNT(*) FROM audit_log`
	queryList  = `SELECT id, at, COALESCE(actor_user_id::text, ''), actor_label, action,
			target_kind, target_id, target_name, details
		FROM audit_log ORDER BY at DESC, id DESC OFFSET $1 LIMIT $2`
)

func (l *Log) Record(ctx context.Context, entry Entry) error {
	details, err := json.Marshal(maps.Collect(maps.All(entry.Details)))
	if err != nil {
		return err
	}
	_, err = l.db.ExecContext(ctx, queryRecord, entry.Actor.UserID, entry.Actor.Label, entry.Action,
		entry.Target.Kind, entry.Target.ID, entry.Target.Name, string(details))

	return err
}

// List returns a page of entries, newest first, and the number of entries.
func (l *Log) List(ctx context.Context, page Page) ([]Entry, int, error) {
	var total int
	if err := l.db.QueryRowContext(ctx, queryCount).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := l.db.QueryContext(ctx, queryList, page.Offset, page.Limit)
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
		if err := rows.Scan(&entry.ID, &entry.At, &entry.Actor.UserID, &entry.Actor.Label, &entry.Action,
			&entry.Target.Kind, &entry.Target.ID, &entry.Target.Name, &details); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(details, &entry.Details); err != nil {
			return nil, 0, err
		}
		entries = append(entries, entry)
	}

	return entries, total, rows.Err()
}
