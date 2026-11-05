package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

var ErrTokenNotFound = errors.New("ingestion token not found")

type Store struct{ db configdoc.DB }

func NewStore(db configdoc.DB) Store { return Store{db: db} }

// Token is an ingestion token, without its secret.
type Token struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedBy  string     `json:"createdBy"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

func (s Store) CreateToken(ctx context.Context, integrationID, name, prefix string, secretHash []byte, userID string) (Token, error) {
	t := Token{Name: name, Prefix: prefix}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO ingestion_tokens (integration_id, name, prefix, secret_hash, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, coalesce((SELECT email FROM users WHERE id = $5), '')`,
		integrationID, name, prefix, secretHash, userID).Scan(&t.ID, &t.CreatedAt, &t.CreatedBy)

	return t, err
}

// Tokens lists an integration's tokens that haven't been revoked, newest first.
func (s Store) Tokens(ctx context.Context, integrationID string) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, t.prefix, coalesce(u.email, ''), t.created_at, t.last_used_at
		FROM ingestion_tokens t LEFT JOIN users u ON u.id = t.created_by
		WHERE t.integration_id = $1 AND t.revoked_at IS NULL
		ORDER BY t.created_at DESC`, integrationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := []Token{}
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &t.CreatedBy, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}

	return tokens, rows.Err()
}

func (s Store) RevokeToken(ctx context.Context, integrationID, tokenID string) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `
		UPDATE ingestion_tokens SET revoked_at = now()
		WHERE id = $1 AND integration_id = $2 AND revoked_at IS NULL
		RETURNING name`, tokenID, integrationID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrTokenNotFound
	}

	return name, err
}

// TokenIntegration finds the integration a token writes as, and marks the token used.
func (s Store) TokenIntegration(ctx context.Context, secretHash []byte) (tokenID, integration string, err error) {
	err = s.db.QueryRowContext(ctx, `
		UPDATE ingestion_tokens t SET last_used_at = now()
		FROM config_documents d
		WHERE d.id = t.integration_id AND t.secret_hash = $1 AND t.revoked_at IS NULL
		RETURNING t.id, d.identifier`, secretHash).Scan(&tokenID, &integration)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrTokenNotFound
	}

	return tokenID, integration, err
}

// Run is one ingestion: a push, a sync of an integration that pulls, or an
// action's write.
type Run struct {
	ID        int64     `json:"id"`
	StartedAt time.Time `json:"startedAt"`
	// FinishedAt is nil while a sync is running.
	FinishedAt *time.Time `json:"finishedAt"`
	// Trigger is push, schedule, manual or action.
	Trigger string `json:"trigger"`
	// Mode is complete when the run deleted what neither it nor the
	// complete run before it saw, or incremental when it only deleted what
	// it was told to.
	Mode  string `json:"mode"`
	Token string `json:"token,omitempty"`
	// ActionRun and Action are the action run behind an action trigger and its title.
	ActionRun string     `json:"actionRun,omitempty"`
	Action    string     `json:"action,omitempty"`
	Received  int        `json:"received"`
	Created   int        `json:"created"`
	Updated   int        `json:"updated"`
	Unchanged int        `json:"unchanged"`
	Deleted   int        `json:"deleted"`
	Failed    int        `json:"failed"`
	Errors    []RunError `json:"errors"`
	// Error is why a sync couldn't fetch some or all of its resources. It
	// deleted nothing those resources write.
	Error string `json:"error,omitempty"`
	// Warning is what went wrong without failing anything it read, such as
	// a webhook a sync couldn't set up.
	Warning string `json:"warning,omitempty"`
}

// RunError is why an item wasn't written.
type RunError struct {
	Index      int      `json:"index"`
	Blueprint  string   `json:"blueprint,omitempty"`
	Identifier string   `json:"identifier,omitempty"`
	Problems   []string `json:"problems"`
}

// MaxRunErrors caps the errors kept per run, so a bad batch stays small.
const MaxRunErrors = 100

func (s Store) RecordRun(ctx context.Context, integrationID, tokenID string, run *Run) error {
	errorsJSON, _ := json.Marshal(run.Errors)

	return s.db.QueryRowContext(ctx, `
		INSERT INTO ingestion_runs (integration_id, token_id, trigger, mode, started_at, received, created, updated,
			unchanged, deleted, failed, errors, error, warning, action_run_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id, finished_at`,
		integrationID, nullable(tokenID), run.Trigger, run.Mode, run.StartedAt, run.Received, run.Created, run.Updated,
		run.Unchanged, run.Deleted, run.Failed, errorsJSON, run.Error, run.Warning, nullable(run.ActionRun)).Scan(&run.ID, &run.FinishedAt)
}

// startRun records a sync as running, so it shows before it finishes.
func (s Store) startRun(ctx context.Context, integrationID string, run *Run) error {
	return s.db.QueryRowContext(ctx, `
		INSERT INTO ingestion_runs (integration_id, trigger, mode, started_at, received, created, updated,
			unchanged, deleted, failed, action_run_id, finished_at)
		VALUES ($1, $2, $3, $4, 0, 0, 0, 0, 0, 0, $5, NULL)
		RETURNING id`,
		integrationID, run.Trigger, run.Mode, run.StartedAt, nullable(run.ActionRun)).Scan(&run.ID)
}

// progress records a running sync's counts so far, in the transaction that
// wrote them.
func (s Store) progress(ctx context.Context, run *Run) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE ingestion_runs SET received = $2, created = $3, updated = $4, unchanged = $5, deleted = $6, failed = $7
		WHERE id = $1`, run.ID, run.Received, run.Created, run.Updated, run.Unchanged, run.Deleted, run.Failed)

	return err
}

// finishRun records how a running sync ended.
func (s Store) finishRun(ctx context.Context, run *Run) error {
	errorsJSON, _ := json.Marshal(run.Errors)

	return s.db.QueryRowContext(ctx, `
		UPDATE ingestion_runs SET received = $2, created = $3, updated = $4, unchanged = $5, deleted = $6, failed = $7,
			errors = $8, error = $9, warning = $10, finished_at = now()
		WHERE id = $1
		RETURNING finished_at`,
		run.ID, run.Received, run.Created, run.Updated, run.Unchanged, run.Deleted, run.Failed,
		errorsJSON, run.Error, run.Warning).Scan(&run.FinishedAt)
}

// ErrInterrupted is why a sync the portal stopped during never finished.
var ErrInterrupted = errors.New("interrupted: the portal stopped before the sync finished")

// finishInterruptedSQL fails the running syncs no instance holds the lock of,
// so a sync that's still going on another instance is left alone.
const finishInterruptedSQL = `
	WITH running AS MATERIALIZED (SELECT id, integration_id FROM ingestion_runs WHERE finished_at IS NULL)
	UPDATE ingestion_runs r SET finished_at = now(), error = $1
	FROM running
	WHERE r.id = running.id AND pg_try_advisory_xact_lock(hashtext('integration-sync:' || running.integration_id))`

// finishInterrupted fails the syncs a stopped instance left running, so
// they don't show as running for ever.
func (s Store) finishInterrupted(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, finishInterruptedSQL, ErrInterrupted.Error())

	return err
}

// RunQuery picks a page of an integration's runs. Empty fields don't filter.
type RunQuery struct {
	// Search matches the pushing token's name, the action's title or the run's error, ignoring case.
	Search   string
	Triggers []string
	Outcomes []string
	Page     int
	PageSize int
}

// RunPage is one page of runs, newest first, and how many runs match in all.
type RunPage struct {
	Runs  []Run `json:"runs"`
	Total int   `json:"total"`
}

// Run outcomes: a running run hasn't finished, a failed run fetched nothing,
// and a run with warnings skipped some items, or warned of something else
// that went wrong.
const (
	OutcomeRunning   = "running"
	OutcomeSucceeded = "succeeded"
	OutcomeWarnings  = "warnings"
	OutcomeFailed    = "failed"
)

const runFilter = `
	FROM ingestion_runs r LEFT JOIN ingestion_tokens t ON t.id = r.token_id
		LEFT JOIN runs a ON a.id = r.action_run_id
	WHERE r.integration_id = $1
		AND ($2 = '' OR coalesce(t.name, '') ILIKE $2 OR coalesce(a.title, '') ILIKE $2 OR r.error ILIKE $2 OR r.warning ILIKE $2)
		AND (coalesce(cardinality($3::text[]), 0) = 0 OR r.trigger = ANY($3))
		AND (coalesce(cardinality($4::text[]), 0) = 0 OR CASE
			WHEN r.finished_at IS NULL THEN 'running' WHEN r.error <> '' THEN 'failed' WHEN r.failed > 0 OR r.warning <> '' THEN 'warnings' ELSE 'succeeded' END = ANY($4))`

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Runs returns the page of an integration's runs that match q.
func (s Store) Runs(ctx context.Context, integrationID string, q RunQuery) (RunPage, error) {
	search := ""
	if q.Search != "" {
		search = "%" + likeEscaper.Replace(q.Search) + "%"
	}
	args := []any{integrationID, search, pq.Array(q.Triggers), pq.Array(q.Outcomes)}

	page := RunPage{Runs: []Run{}}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*)`+runFilter, args...).Scan(&page.Total); err != nil {
		return RunPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.started_at, r.finished_at, r.trigger, r.mode, coalesce(t.name, ''), coalesce(r.action_run_id::text, ''),
			coalesce(a.title, ''), r.received, r.created, r.updated, r.unchanged, r.deleted, r.failed, r.errors, r.error, r.warning`+runFilter+`
		ORDER BY r.id DESC LIMIT $5 OFFSET $6`, append(args, q.PageSize, (q.Page-1)*q.PageSize)...)
	if err != nil {
		return RunPage{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			r      Run
			errors []byte
		)
		if err := rows.Scan(&r.ID, &r.StartedAt, &r.FinishedAt, &r.Trigger, &r.Mode, &r.Token, &r.ActionRun, &r.Action, &r.Received,
			&r.Created, &r.Updated, &r.Unchanged, &r.Deleted, &r.Failed, &errors, &r.Error, &r.Warning); err != nil {
			return RunPage{}, err
		}
		if err := json.Unmarshal(errors, &r.Errors); err != nil {
			return RunPage{}, err
		}
		page.Runs = append(page.Runs, r)
	}

	return page, rows.Err()
}

func nullable(s string) any {
	if s == "" {
		return nil
	}

	return s
}

// LastSync is when an integration's last sync finished, or zero if it hasn't
// synced. max skips a running sync's missing end.
func (s Store) LastSync(ctx context.Context, identifier string) (time.Time, error) {
	var last sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT max(r.finished_at) FROM ingestion_runs r
		JOIN config_documents d ON d.id = r.integration_id
		WHERE d.kind = 'integration' AND d.identifier = $1 AND r.trigger IN ('schedule', 'manual')`, identifier).Scan(&last)

	return last.Time, err
}

// syncHistory is what a sync needs to choose its mode and what to fetch.
type syncHistory struct {
	// lastComplete started the last complete sync, and lastSucceeded the
	// last sync that fetched every resource. Either is zero if there's none.
	lastComplete, lastSucceeded time.Time
	// published is when the integration's version was published.
	published time.Time
}

const syncHistorySQL = `
		SELECT
			(SELECT max(started_at) FROM ingestion_runs
				WHERE integration_id = $1 AND trigger IN ('schedule', 'manual') AND mode = 'complete' AND error = ''
					AND finished_at IS NOT NULL),
			(SELECT max(started_at) FROM ingestion_runs
				WHERE integration_id = $1 AND trigger IN ('schedule', 'manual') AND error = '' AND finished_at IS NOT NULL),
			(SELECT v.published_at FROM config_versions v JOIN config_documents d
				ON d.id = v.document_id AND v.version = d.published_version WHERE d.id = $1)`

func (s Store) syncHistory(ctx context.Context, integrationID string) (syncHistory, error) {
	var (
		h                         syncHistory
		lastComplete, lastSucceed sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, syncHistorySQL, integrationID).Scan(&lastComplete, &lastSucceed, &h.published)
	h.lastComplete, h.lastSucceeded = lastComplete.Time, lastSucceed.Time

	return h, err
}
