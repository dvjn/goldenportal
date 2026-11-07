package action

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// Run statuses. A queued run waits for an instance to take it on, and a
// running one is held by an instance while its lease lasts. An interrupted
// run lost its instance during a step that isn't safe to repeat, so a
// person decides how it ended.
const (
	RunQueued      = "queued"
	RunRunning     = "running"
	RunSucceeded   = "succeeded"
	RunFailed      = "failed"
	RunCancelled   = "cancelled"
	RunInterrupted = "interrupted"
	// RunWaiting is a run paused at an approval step, which no instance holds.
	RunWaiting  = "waiting"
	RunRejected = "rejected"
)

// Step statuses. A step that failed with onFailure: continue is failed too,
// and the run goes on.
const (
	StepPending     = "pending"
	StepRunning     = "running"
	StepDone        = "done"
	StepFailed      = "failed"
	StepInterrupted = "interrupted"
	StepWaiting     = "waiting"
)

// Approval decisions.
const (
	Approve = "approve"
	Reject  = "reject"
)

// Where a run was asked for.
const (
	ViaWeb = "web"
	ViaMCP = "mcp"
)

var ErrRunNotFound = errors.New("run not found")

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Run is one run of an action on one entity.
type Run struct {
	ID string `json:"id"`
	// Kind is the configdoc kind of what ran: action or workflow.
	Kind       string         `json:"kind"`
	Action     string         `json:"action"`
	Title      string         `json:"title"`
	Version    int            `json:"version"`
	Entity     EntityRef      `json:"entity"`
	Requester  Requester      `json:"requester"`
	Via        string         `json:"via"`
	Inputs     map[string]any `json:"inputs"`
	Status     string         `json:"status"`
	Summary    string         `json:"summary"`
	Links      []Link         `json:"links"`
	Outputs    map[string]any `json:"outputs"`
	Error      string         `json:"error,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	StartedAt  *time.Time     `json:"startedAt"`
	FinishedAt *time.Time     `json:"finishedAt"`
	ResolvedBy string         `json:"resolvedBy,omitempty"`
	Resolution string         `json:"resolution,omitempty"`
	Steps      []RunStep      `json:"steps,omitempty"`
	// CanDecide says the caller can approve or reject the step the run waits at.
	CanDecide   bool `json:"canDecide,omitempty"`
	documentID  string
	requesterID string
	entityID    string
	key         string
	// Parent is the workflow run that started it, from its step at parentPosition.
	Parent         string `json:"parent,omitempty"`
	parentPosition int
}

// EntityRef is the entity a run is on, as it was when the run was asked for.
type EntityRef struct {
	Blueprint  string `json:"blueprint"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

// Requester is who asked for a run, as they were then.
type Requester struct {
	ID     string   `json:"id"`
	Email  string   `json:"email"`
	Name   string   `json:"name"`
	Role   string   `json:"role"`
	Groups []string `json:"groups"`
}

// RunStep is how one step of a run went.
type RunStep struct {
	Position   int        `json:"position"`
	Step       string     `json:"step,omitempty"`
	Type       string     `json:"type"`
	Title      string     `json:"title"`
	Status     string     `json:"status"`
	Attempts   int        `json:"attempts"`
	Output     any        `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	Detail     any        `json:"detail,omitempty"`
	StartedAt  *time.Time `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	// DueAt is when a waiting step gives up.
	DueAt     *time.Time `json:"dueAt,omitempty"`
	Decisions []Decision `json:"decisions,omitempty"`
}

// Decision is someone's answer to an approval step.
type Decision struct {
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Decision  string    `json:"decision"`
	Comment   string    `json:"comment,omitempty"`
	DecidedAt time.Time `json:"decidedAt"`
	userID    string
}

// Finished reports whether a run is over, one way or another.
func (r Run) Finished() bool {
	return r.Status != RunQueued && r.Status != RunRunning && r.Status != RunWaiting
}

type Store struct{ db configdoc.DB }

func NewStore(db configdoc.DB) Store { return Store{db: db} }

// Create adds a queued run with its steps pending. A run asked for again
// with the same idempotency key comes back as it is, with created false.
func (s Store) Create(ctx context.Context, r Run, steps []Step) (id string, created bool, err error) {
	requester, _ := json.Marshal(r.Requester)
	inputs, _ := json.Marshal(r.Inputs)
	var key *string
	if k := strings.TrimSpace(r.key); k != "" {
		key = &k
	}
	var entityID, parent *string
	var parentPosition *int
	if r.entityID != "" {
		entityID = &r.entityID
	}
	if r.Parent != "" {
		parent, parentPosition = &r.Parent, &r.parentPosition
	}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO runs (document_id, version, title, entity_id, entity_blueprint, entity_identifier, entity_title,
			requested_by, requester, via, idempotency_key, inputs, status, parent_run_id, parent_position)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (document_id, requested_by, idempotency_key) DO NOTHING
		RETURNING id`,
		r.documentID, r.Version, r.Title, entityID, r.Entity.Blueprint, r.Entity.Identifier, r.Entity.Title,
		r.requesterID, requester, r.Via, key, inputs, RunQueued, parent, parentPosition).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = s.db.QueryRowContext(ctx, `SELECT id FROM runs WHERE document_id = $1 AND requested_by = $2 AND idempotency_key = $3`,
			r.documentID, r.requesterID, key).Scan(&id)
		return id, false, err
	}
	if err != nil {
		return "", false, err
	}
	for i, step := range steps {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO run_steps (run_id, position, step, type, title, status) VALUES ($1, $2, $3, $4, $5, $6)`,
			id, i, step.ID, step.Type, step.Title, StepPending); err != nil {
			return "", false, err
		}
	}

	return id, true, nil
}

const runColumns = `r.id, d.identifier, r.title, r.version, r.entity_blueprint, r.entity_identifier, r.entity_title,
	r.requester, r.via, r.inputs, r.status, r.summary, r.links, r.outputs, r.error,
	r.created_at, r.started_at, r.finished_at, coalesce(u.email, ''), r.resolution,
	r.document_id, coalesce(r.requested_by::text, ''), d.kind,
	coalesce(r.parent_run_id::text, ''), coalesce(r.parent_position, 0)`

const runFrom = ` FROM runs r
	JOIN config_documents d ON d.id = r.document_id
	LEFT JOIN users u ON u.id = r.resolved_by`

func scanRun(scan func(...any) error, extra ...any) (Run, error) {
	var r Run
	var requester, inputs, links, outputs []byte
	err := scan(append([]any{
		&r.ID, &r.Action, &r.Title, &r.Version, &r.Entity.Blueprint, &r.Entity.Identifier, &r.Entity.Title,
		&requester, &r.Via, &inputs, &r.Status, &r.Summary, &links, &outputs, &r.Error,
		&r.CreatedAt, &r.StartedAt, &r.FinishedAt, &r.ResolvedBy, &r.Resolution,
		&r.documentID, &r.requesterID, &r.Kind, &r.Parent, &r.parentPosition,
	}, extra...)...)
	if err != nil {
		return r, err
	}
	for _, field := range []struct {
		raw    []byte
		target any
	}{{requester, &r.Requester}, {inputs, &r.Inputs}, {links, &r.Links}, {outputs, &r.Outputs}} {
		if err := json.Unmarshal(field.raw, field.target); err != nil {
			return r, err
		}
	}

	return r, nil
}

// Get returns a run with its steps.
func (s Store) Get(ctx context.Context, id string) (Run, error) {
	r, err := scanRun(s.db.QueryRowContext(ctx, `SELECT `+runColumns+runFrom+` WHERE r.id::text = $1`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrRunNotFound
	}
	if err != nil {
		return Run{}, err
	}
	r.Steps, err = s.steps(ctx, id)

	return r, err
}

func (s Store) steps(ctx context.Context, runID string) ([]RunStep, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT position, step, type, title, status, attempts, output, error, detail, started_at, finished_at, due_at
		FROM run_steps WHERE run_id = $1 ORDER BY position`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	steps := []RunStep{}
	for rows.Next() {
		var step RunStep
		var output, detail []byte
		if err := rows.Scan(&step.Position, &step.Step, &step.Type, &step.Title, &step.Status, &step.Attempts,
			&output, &step.Error, &detail, &step.StartedAt, &step.FinishedAt, &step.DueAt); err != nil {
			return nil, err
		}
		if output != nil {
			_ = json.Unmarshal(output, &step.Output)
		}
		if detail != nil {
			_ = json.Unmarshal(detail, &step.Detail)
		}
		steps = append(steps, step)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	decisions, err := s.decisions(ctx, runID)
	for i := range steps {
		steps[i].Decisions = decisions[steps[i].Position]
	}

	return steps, err
}

func (s Store) decisions(ctx context.Context, runID string) (map[int][]Decision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT position, email, name, decision, comment, decided_at, coalesce(user_id::text, '')
		FROM run_approvals WHERE run_id = $1 ORDER BY decided_at`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int][]Decision{}
	for rows.Next() {
		var position int
		var d Decision
		if err := rows.Scan(&position, &d.Email, &d.Name, &d.Decision, &d.Comment, &d.DecidedAt, &d.userID); err != nil {
			return nil, err
		}
		out[position] = append(out[position], d)
	}

	return out, rows.Err()
}

// RunsQuery picks a page of runs, newest first: of one action, by one
// requester, on one blueprint's entities or one entity, as set.
type RunsQuery struct {
	DocumentID  string
	RequestedBy string
	Blueprint   string
	Entity      string
	Status      string
	// Kind is the configdoc kind of what ran, and Parent the run that started them.
	Kind   string
	Parent string
	// Search matches the action, the entity or the requester, ignoring case.
	Search     string
	Page, Size int
}

// RunPage is a page of runs, without their steps.
type RunPage = httpx.Page[Run]

func (s Store) List(ctx context.Context, q RunsQuery) (RunPage, error) {
	var where []string
	var args []any
	add := func(condition string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(condition, len(args)))
	}
	where = append(where, "true")
	if q.DocumentID != "" {
		add("r.document_id = $%d", q.DocumentID)
	}
	if q.RequestedBy != "" {
		add("r.requested_by = $%d", q.RequestedBy)
	}
	if q.Blueprint != "" {
		add("r.entity_blueprint = $%d", q.Blueprint)
	}
	if q.Entity != "" {
		add("r.entity_identifier = $%d", q.Entity)
	}
	if q.Kind != "" {
		add("d.kind = $%d", q.Kind)
	}
	if q.Parent != "" {
		add("r.parent_run_id = $%d", q.Parent)
	}
	if q.Search != "" {
		add(`(r.title ILIKE $%[1]d OR r.entity_title ILIKE $%[1]d OR r.entity_identifier ILIKE $%[1]d
			OR r.requester->>'email' ILIKE $%[1]d OR r.requester->>'name' ILIKE $%[1]d)`, "%"+likeEscaper.Replace(q.Search)+"%")
	}
	if q.Status != "" {
		add("r.status = ANY($%d)", pq.Array(strings.Split(q.Status, ",")))
	}
	args = append(args, q.Size, (q.Page-1)*q.Size)
	rows, err := s.db.QueryContext(ctx, `SELECT `+runColumns+`, count(*) OVER ()`+runFrom+`
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY r.created_at DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return RunPage{}, err
	}
	defer rows.Close()

	page := RunPage{Items: []Run{}}
	for rows.Next() {
		r, err := scanRun(rows.Scan, &page.Total)
		if err != nil {
			return RunPage{}, err
		}
		page.Items = append(page.Items, r)
	}

	return page, rows.Err()
}

// claim takes on a run for lease: one that's queued, or running on an
// instance whose lease ran out. With id empty it takes the oldest. ok is
// false when there's none to take.
func (s Store) claim(ctx context.Context, id string, lease time.Duration) (string, bool, error) {
	var claimed string
	err := s.db.QueryRowContext(ctx, `
		UPDATE runs SET status = 'running', lease_until = now() + $2 * interval '1 millisecond',
			started_at = coalesce(started_at, now())
		WHERE id = (
			SELECT id FROM runs
			WHERE (status = 'queued' OR (status = 'running' AND lease_until < now()))
				AND ($1 = '' OR id::text = $1)
			ORDER BY created_at
			LIMIT 1
			FOR UPDATE SKIP LOCKED)
		RETURNING id`, id, lease.Milliseconds()).Scan(&claimed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}

	return claimed, err == nil, err
}

// renew extends the lease on a run this instance holds. held is false once
// the run isn't running any more, as when someone cancels it.
func (s Store) renew(ctx context.Context, id string, lease time.Duration) (held bool, err error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE runs SET lease_until = now() + $2 * interval '1 millisecond'
		WHERE id = $1 AND status = 'running'`, id, lease.Milliseconds())
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()

	return n == 1, err
}

func (s Store) startStep(ctx context.Context, runID string, position int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE run_steps SET status = 'running', attempts = attempts + 1, started_at = now(), finished_at = NULL, error = ''
		WHERE run_id = $1 AND position = $2`, runID, position)

	return err
}

func (s Store) finishStep(ctx context.Context, runID string, position int, status string, output any, message string, detail any) error {
	var encodedOutput, encodedDetail []byte
	if output != nil {
		encodedOutput, _ = json.Marshal(output)
	}
	if detail != nil {
		encodedDetail, _ = json.Marshal(detail)
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE run_steps SET status = $3, output = $4, error = $5, detail = $6, finished_at = now()
		WHERE run_id = $1 AND position = $2`, runID, position, status, encodedOutput, message, encodedDetail)

	return err
}

// setOutput records what an output step says the run gave.
func (s Store) setOutput(ctx context.Context, runID, summary string, links []Link) error {
	encoded, _ := json.Marshal(links)
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET summary = $2, links = $3 WHERE id = $1`, runID, summary, encoded)

	return err
}

// finish ends a run this instance holds. It doesn't overwrite a run
// someone ended meanwhile, as by cancelling it.
func (s Store) finish(ctx context.Context, runID, status, message string, outputs map[string]any) error {
	encoded, _ := json.Marshal(outputs)
	if outputs == nil {
		encoded = []byte("{}")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE runs SET status = $2, error = $3, outputs = $4, finished_at = now(), lease_until = NULL
		WHERE id = $1 AND status = 'running'`, runID, status, message, encoded)

	return err
}

// cancel ends a run that hasn't finished. ok is false when it had.
func (s Store) cancel(ctx context.Context, runID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE runs SET status = 'cancelled', finished_at = now(), lease_until = NULL
		WHERE id = $1 AND status IN ('queued', 'running', 'waiting')`, runID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	// Cancelling a workflow cancels the runs it started that are still going.
	_, err = s.db.ExecContext(ctx, `
		UPDATE runs SET status = 'cancelled', finished_at = now(), lease_until = NULL
		WHERE parent_run_id = $1 AND status IN ('queued', 'running', 'waiting')`, runID)

	return err == nil, err
}

// requeue sends a failed or interrupted run back to the queue from its
// first step that isn't done, which starts again. With skip, that step
// counts as done, as when a person checked it worked.
func (s Store) requeue(ctx context.Context, runID string, from []string, skip bool, resolvedBy, resolution string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE runs SET status = 'queued', error = '', finished_at = NULL, lease_until = NULL,
			resolved_by = nullif($3, '')::uuid, resolution = $4
		WHERE id = $1 AND status = ANY($2)`, runID, pq.Array(from), resolvedBy, resolution)
	if err != nil {
		return false, err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	status := StepPending
	if skip {
		status = StepDone
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE run_steps SET status = $2, error = CASE WHEN $3 THEN '' ELSE error END
		WHERE run_id = $1 AND position = (
			SELECT min(position) FROM run_steps WHERE run_id = $1 AND status IN ('failed', 'interrupted', 'running'))`,
		runID, status, skip)

	return true, err
}

// resolveFailed ends an interrupted run as failed, as a person decided.
func (s Store) resolveFailed(ctx context.Context, runID, resolvedBy, resolution string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE runs SET status = 'failed', finished_at = now(), resolved_by = $2, resolution = $3
		WHERE id = $1 AND status = 'interrupted'`, runID, resolvedBy, resolution)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()

	return n == 1, err
}

// interrupt ends a run whose step was cut off by a lost instance, which
// isn't safe to repeat.
func (s Store) interrupt(ctx context.Context, runID string, position int) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE run_steps SET status = 'interrupted', finished_at = now(),
			error = 'The portal stopped while this step ran, so it may or may not have taken effect.'
		WHERE run_id = $1 AND position = $2`, runID, position); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE runs SET status = 'interrupted', finished_at = now(), lease_until = NULL WHERE id = $1 AND status = 'running'`, runID)

	return err
}

// source is an action's source at a version, which its runs follow.
func (s Store) source(ctx context.Context, documentID string, version int) (string, error) {
	var source string
	err := s.db.QueryRowContext(ctx, `SELECT source FROM config_versions WHERE document_id = $1 AND version = $2`,
		documentID, version).Scan(&source)

	return source, err
}

// wait pauses a run this instance holds at an approval step, until due, and
// lets go of it: no instance holds a waiting run.
func (s Store) wait(ctx context.Context, runID string, position int, detail any, due time.Time) error {
	encoded, _ := json.Marshal(detail)
	if _, err := s.db.ExecContext(ctx, `
		UPDATE run_steps SET status = 'waiting', detail = $3, due_at = $4, started_at = now(), attempts = attempts + 1
		WHERE run_id = $1 AND position = $2`, runID, position, encoded, due); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE runs SET status = 'waiting', lease_until = NULL WHERE id = $1 AND status = 'running'`, runID)

	return err
}

// ErrDecided means the person already decided this step.
var ErrDecided = errors.New("already decided")

// decide records a decision on the step a run waits at. One rejection ends
// the run; enough approvals send it back to the queue. resumed says the
// run can carry on.
func (s Store) decide(ctx context.Context, runID string, position, require int, d Decision) (resumed bool, err error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO run_approvals (run_id, position, user_id, email, name, decision, comment)
		SELECT $1, $2, nullif($3, '')::uuid, $4, $5, $6, $7
		WHERE EXISTS (SELECT 1 FROM run_steps WHERE run_id = $1 AND position = $2 AND status = 'waiting')
		ON CONFLICT (run_id, position, email) DO NOTHING`,
		runID, position, d.userID, d.Email, d.Name, d.Decision, d.Comment)
	if err != nil {
		return false, err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return false, cmp.Or(err, ErrDecided)
	}

	if d.Decision == Reject {
		message := "Rejected by " + d.Email
		if d.Comment != "" {
			message += ": " + d.Comment
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE run_steps SET status = 'failed', error = $3, finished_at = now()
			WHERE run_id = $1 AND position = $2 AND status = 'waiting'`, runID, position, message); err != nil {
			return false, err
		}
		_, err := s.db.ExecContext(ctx, `
			UPDATE runs SET status = 'rejected', error = $2, finished_at = now() WHERE id = $1 AND status = 'waiting'`,
			runID, message)
		return false, err
	}

	decisions, err := s.decisions(ctx, runID)
	if err != nil {
		return false, err
	}
	approvedBy, comments := []any{}, []any{}
	for _, decision := range decisions[position] {
		if decision.Decision == Approve {
			approvedBy = append(approvedBy, decision.Email)
			if decision.Comment != "" {
				comments = append(comments, decision.Comment)
			}
		}
	}
	if len(approvedBy) < require {
		return false, nil
	}
	output, _ := json.Marshal(map[string]any{"approvedBy": approvedBy, "comments": comments})
	result, err = s.db.ExecContext(ctx, `
		UPDATE run_steps SET status = 'done', output = $3, finished_at = now()
		WHERE run_id = $1 AND position = $2 AND status = 'waiting'`, runID, position, output)
	if err != nil {
		return false, err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE runs SET status = 'queued' WHERE id = $1 AND status = 'waiting'`, runID)

	return err == nil, err
}

// expire ends the runs whose approval nobody gave in time.
func (s Store) expire(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		WITH expired AS (
			UPDATE run_steps SET status = 'failed', finished_at = now(),
				error = 'Not enough people approved it in time.'
			WHERE status = 'waiting' AND due_at < now()
			RETURNING run_id
		)
		UPDATE runs SET status = 'failed', finished_at = now(), error = 'Not enough people approved it in time.'
		WHERE id IN (SELECT run_id FROM expired) AND status = 'waiting'`)

	return err
}

// children are the runs a step started, the latest on each entity, by
// entity identifier.
func (s Store) children(ctx context.Context, parent string, position int) (map[string]Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+runColumns+runFrom+`
		WHERE r.parent_run_id = $1 AND r.parent_position = $2
		ORDER BY r.created_at`, parent, position)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Run{}
	for rows.Next() {
		r, err := scanRun(rows.Scan)
		if err != nil {
			return nil, err
		}
		out[r.Entity.Identifier] = r
	}

	return out, rows.Err()
}

// waitChildren pauses a run this instance holds at an action step until
// the runs it started finish, and lets go of it.
func (s Store) waitChildren(ctx context.Context, runID string, position int) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE run_steps SET status = 'waiting', due_at = NULL WHERE run_id = $1 AND position = $2`, runID, position); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE runs SET status = 'waiting', lease_until = NULL WHERE id = $1 AND status = 'running'`, runID)

	return err
}

// endChildStep ends the action step a run waits at, and sends the run back
// to the queue, or ends it when the step failed. resumed says it can carry
// on. Only one caller gets to end it.
func (s Store) endChildStep(ctx context.Context, runID string, position int, output any, failure string, carryOn bool) (resumed bool, err error) {
	encoded, _ := json.Marshal(output)
	status := StepDone
	if failure != "" {
		status = StepFailed
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE run_steps SET status = $3, output = $4, error = $5, finished_at = now()
		WHERE run_id = $1 AND position = $2 AND status = 'waiting'`, runID, position, status, encoded, failure)
	if err != nil {
		return false, err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	if failure != "" && !carryOn {
		_, err = s.db.ExecContext(ctx, `
			UPDATE runs SET status = 'failed', error = $2, finished_at = now() WHERE id = $1 AND status = 'waiting'`, runID, failure)
		return false, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE runs SET status = 'queued' WHERE id = $1 AND status = 'waiting'`, runID)

	return err == nil, err
}

// waitingParents are runs waiting at an action step, for the sweep to check
// on in case a run they started ended without saying.
func (s Store) waitingParents(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT r.id FROM runs r JOIN run_steps st ON st.run_id = r.id
		WHERE r.status = 'waiting' AND st.status = 'waiting' AND st.type = 'action'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// setRefused records the entities an action step couldn't run its action
// on, and why, for when the step ends.
func (s Store) setRefused(ctx context.Context, runID string, position int, refused map[string]string) error {
	encoded, _ := json.Marshal(map[string]any{"refused": refused})
	_, err := s.db.ExecContext(ctx, `UPDATE run_steps SET detail = $3 WHERE run_id = $1 AND position = $2`, runID, position, encoded)

	return err
}
