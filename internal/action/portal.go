package action

import (
	"context"
	"time"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/integration"
)

// The Portal connector reads actions and their recent runs, so the catalog
// and dashboards can count them. Whatever it reads, every member can see,
// so runs leave out their inputs, outputs and errors.
func init() {
	integration.AddPortalResource("actions", portalActions)
	integration.AddPortalResource("runs", portalRuns)
}

const (
	// portalRunDays and portalRunLimit bound the runs the Portal connector reads.
	portalRunDays  = 30
	portalRunLimit = 1000
)

type portalAction struct {
	Identifier    string     `json:"identifier"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Blueprint     string     `json:"blueprint"`
	Destructive   bool       `json:"destructive"`
	Version       int        `json:"version"`
	Steps         int        `json:"steps"`
	Approvals     int        `json:"approvals"`
	Runs          int        `json:"runs"`
	LastRunAt     *time.Time `json:"lastRunAt"`
	LastRunStatus *string    `json:"lastRunStatus"`
}

type portalRun struct {
	ID         string     `json:"id"`
	Action     string     `json:"action"`
	Title      string     `json:"title"`
	Blueprint  string     `json:"blueprint"`
	Entity     string     `json:"entity"`
	EntityName string     `json:"entityTitle"`
	Requester  string     `json:"requester"`
	Via        string     `json:"via"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	// Seconds is how long it took, once it's finished.
	Seconds *float64 `json:"seconds"`
}

// portalActions are the published actions, with how often each ran in the
// last portalRunDays days and how its last run went.
func portalActions(ctx context.Context, db configdoc.DB) (any, error) {
	store := configdoc.NewStore(db, Name)
	records, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	type stats struct {
		runs   int
		at     *time.Time
		status *string
	}
	byAction := map[string]stats{}
	rows, err := db.QueryContext(ctx, `
		SELECT d.identifier, count(*),
			max(r.created_at),
			(array_agg(r.status ORDER BY r.created_at DESC))[1]
		FROM runs r JOIN config_documents d ON d.id = r.document_id
		WHERE d.kind = 'action' AND r.created_at > now() - $1 * interval '1 day'
		GROUP BY d.identifier`, portalRunDays)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var s stats
		if err := rows.Scan(&name, &s.runs, &s.at, &s.status); err != nil {
			return nil, err
		}
		byAction[name] = s
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	actions := []portalAction{}
	for _, record := range records {
		if !record.Published() {
			continue
		}
		spec, _ := Parse(record.PublishedSource)
		a := portalAction{
			Identifier: record.Identifier, Title: spec.Title, Description: spec.Description,
			Blueprint: spec.Blueprint, Destructive: spec.Destructive, Version: record.PublishedVersion,
			Steps: len(spec.Steps),
		}
		for _, step := range spec.Steps {
			if step.Approval != nil {
				a.Approvals++
			}
		}
		s := byAction[record.Identifier]
		a.Runs, a.LastRunAt, a.LastRunStatus = s.runs, s.at, s.status
		actions = append(actions, a)
	}

	return actions, nil
}

// portalRuns are the runs of the last portalRunDays days, newest first.
func portalRuns(ctx context.Context, db configdoc.DB) (any, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT r.id, d.identifier, r.title, r.entity_blueprint, r.entity_identifier, r.entity_title,
			coalesce(r.requester->>'email', ''), r.via, r.status, r.created_at, r.finished_at,
			extract(epoch FROM r.finished_at - coalesce(r.started_at, r.created_at))::float8
		FROM runs r JOIN config_documents d ON d.id = r.document_id
		WHERE d.kind = 'action' AND r.created_at > now() - $1 * interval '1 day'
		ORDER BY r.created_at DESC
		LIMIT $2`, portalRunDays, portalRunLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []portalRun{}
	for rows.Next() {
		var r portalRun
		if err := rows.Scan(&r.ID, &r.Action, &r.Title, &r.Blueprint, &r.Entity, &r.EntityName,
			&r.Requester, &r.Via, &r.Status, &r.CreatedAt, &r.FinishedAt, &r.Seconds); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}

	return runs, rows.Err()
}
