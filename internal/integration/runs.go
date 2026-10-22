package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"gofr.dev/pkg/gofr/container"
)

type Trigger string

const (
	TriggerSchedule Trigger = "schedule"
	TriggerManual   Trigger = "manual"
	TriggerPush     Trigger = "push"
)

type Status string

const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusPartial   Status = "partial"
	StatusFailed    Status = "failed"
)

const (
	keptRuns         = 100
	maxWarnings      = 100
	maxMessageLength = 1000
)

// Counts are what a run did with records and entities. Skipped records made
// no entity.
type Counts struct {
	Fetched  int `json:"fetched"`
	Upserted int `json:"upserted"`
	Deleted  int `json:"deleted"`
	Skipped  int `json:"skipped"`
}

// Run is one sync of a pull integration or one push call. Kind and Mode
// describe a push; a sync reads every kind.
type Run struct {
	ID          int64
	Integration string
	Trigger     Trigger
	Actor       string
	Kind        string
	Mode        string
	Status      Status
	StartedAt   time.Time
	FinishedAt  time.Time
	Counts      Counts
	Warnings    []string
	Error       string
}

// finish settles the run's status: failed with an error, partial with
// warnings or skipped records, succeeded otherwise.
func (r *Run) finish(at time.Time, failure string) {
	r.FinishedAt = at
	r.Error = truncate(failure)
	switch {
	case failure != "":
		r.Status = StatusFailed
	case len(r.Warnings) > 0 || r.Counts.Skipped > 0:
		r.Status = StatusPartial
	default:
		r.Status = StatusSucceeded
	}
	if len(r.Warnings) > maxWarnings {
		r.Warnings = append(r.Warnings[:maxWarnings-1], fmt.Sprintf("and %d more warnings", len(r.Warnings)-maxWarnings+1))
	}
	for index, warning := range r.Warnings {
		r.Warnings[index] = truncate(warning)
	}
}

func truncate(text string) string {
	runes := []rune(text)
	if len(runes) <= maxMessageLength {
		return text
	}

	return string(runes[:maxMessageLength-1]) + "…"
}

type Runs struct {
	db container.DB
}

func NewRuns(db container.DB) *Runs {
	return &Runs{db: db}
}

const (
	runColumns = `id, integration, trigger, actor_label, kind, mode, status, started_at, finished_at,
		fetched, upserted, deleted, skipped, warnings, error`
	queryStartRun = `INSERT INTO sync_runs (integration, trigger, actor_label, kind, mode, status)
		VALUES ($1, $2, $3, $4, $5, 'running') RETURNING id, started_at`
	queryRecordRun = `INSERT INTO sync_runs (integration, trigger, actor_label, kind, mode, status, started_at, finished_at,
			fetched, upserted, deleted, skipped, warnings, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`
	queryFinishRun = `UPDATE sync_runs SET status = $2, finished_at = $3, fetched = $4, upserted = $5, deleted = $6,
			skipped = $7, warnings = $8, error = $9
		WHERE id = $1`
	queryPruneRuns = `DELETE FROM sync_runs WHERE integration = $1 AND id NOT IN (
			SELECT id FROM sync_runs WHERE integration = $1 ORDER BY started_at DESC, id DESC LIMIT $2
		)`
	queryInterruptRuns = `UPDATE sync_runs SET status = 'failed', finished_at = NOW(), error = $3
		WHERE integration = $1 AND id <> $2 AND status = 'running'`
	queryListRuns = `SELECT ` + runColumns + ` FROM sync_runs WHERE integration = $1
		ORDER BY started_at DESC, id DESC LIMIT $2`
	queryLatestRuns = `SELECT DISTINCT ON (integration) ` + runColumns + ` FROM sync_runs
		ORDER BY integration, started_at DESC, id DESC`
	queryLastPull = `SELECT MAX(started_at) FROM sync_runs WHERE integration = $1 AND trigger <> 'push'`
)

const interrupted = "the server stopped before the run finished"

// Start records a pull run as running. It marks any other run of the
// integration that is still running as interrupted, since the caller holds
// the integration's lock.
func (r *Runs) Start(ctx context.Context, run Run) (Run, error) {
	if err := r.db.QueryRowContext(ctx, queryStartRun, run.Integration, run.Trigger, run.Actor, run.Kind, run.Mode).
		Scan(&run.ID, &run.StartedAt); err != nil {
		return Run{}, err
	}
	run.Status = StatusRunning
	if _, err := r.db.ExecContext(ctx, queryInterruptRuns, run.Integration, run.ID, interrupted); err != nil {
		return Run{}, err
	}

	return run, nil
}

func (r *Runs) Finish(ctx context.Context, run Run) error {
	warnings, err := json.Marshal(nonNil(run.Warnings))
	if err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, queryFinishRun, run.ID, run.Status, run.FinishedAt, run.Counts.Fetched,
		run.Counts.Upserted, run.Counts.Deleted, run.Counts.Skipped, string(warnings), run.Error); err != nil {
		return err
	}

	return r.prune(ctx, run.Integration)
}

// Record stores a run that already finished, such as a push.
func (r *Runs) Record(ctx context.Context, run Run) (Run, error) {
	warnings, err := json.Marshal(nonNil(run.Warnings))
	if err != nil {
		return Run{}, err
	}
	if err := r.db.QueryRowContext(ctx, queryRecordRun, run.Integration, run.Trigger, run.Actor, run.Kind, run.Mode,
		run.Status, run.StartedAt, run.FinishedAt, run.Counts.Fetched, run.Counts.Upserted, run.Counts.Deleted,
		run.Counts.Skipped, string(warnings), run.Error).Scan(&run.ID); err != nil {
		return Run{}, err
	}

	return run, r.prune(ctx, run.Integration)
}

func (r *Runs) prune(ctx context.Context, integration string) error {
	_, err := r.db.ExecContext(ctx, queryPruneRuns, integration, keptRuns)
	return err
}

// List returns the integration's runs, newest first.
func (r *Runs) List(ctx context.Context, integration string, limit int) ([]Run, error) {
	return r.query(ctx, queryListRuns, integration, limit)
}

// Latest returns the newest run of every integration that has one.
func (r *Runs) Latest(ctx context.Context) (map[string]Run, error) {
	runs, err := r.query(ctx, queryLatestRuns)
	if err != nil {
		return nil, err
	}
	latest := map[string]Run{}
	for _, run := range runs {
		latest[run.Integration] = run
	}

	return latest, nil
}

// LastPull returns when the integration's last sync started, or the zero
// time.
func (r *Runs) LastPull(ctx context.Context, integration string) (time.Time, error) {
	var started sql.NullTime
	err := r.db.QueryRowContext(ctx, queryLastPull, integration).Scan(&started)

	return started.Time, err
}

func (r *Runs) query(ctx context.Context, query string, args ...any) ([]Run, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []Run{}
	for rows.Next() {
		var (
			run      Run
			finished sql.NullTime
			warnings []byte
		)
		if err := rows.Scan(&run.ID, &run.Integration, &run.Trigger, &run.Actor, &run.Kind, &run.Mode, &run.Status,
			&run.StartedAt, &finished, &run.Counts.Fetched, &run.Counts.Upserted, &run.Counts.Deleted,
			&run.Counts.Skipped, &warnings, &run.Error); err != nil {
			return nil, err
		}
		run.FinishedAt = finished.Time
		if err := json.Unmarshal(warnings, &run.Warnings); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}

	return runs, rows.Err()
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}

	return items
}
