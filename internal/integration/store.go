package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

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

// Run is one ingestion: a push, or a sync of a pull integration.
type Run struct {
	ID         int64     `json:"id"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	// Trigger is push, schedule or manual.
	Trigger   string     `json:"trigger"`
	Token     string     `json:"token,omitempty"`
	Received  int        `json:"received"`
	Created   int        `json:"created"`
	Updated   int        `json:"updated"`
	Unchanged int        `json:"unchanged"`
	Deleted   int        `json:"deleted"`
	Failed    int        `json:"failed"`
	Errors    []RunError `json:"errors"`
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
		INSERT INTO ingestion_runs (integration_id, token_id, trigger, started_at, received, created, updated,
			unchanged, deleted, failed, errors)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, finished_at`,
		integrationID, nullable(tokenID), run.Trigger, run.StartedAt, run.Received, run.Created, run.Updated,
		run.Unchanged, run.Deleted, run.Failed, errorsJSON).Scan(&run.ID, &run.FinishedAt)
}

// Runs lists an integration's latest runs, newest first.
func (s Store) Runs(ctx context.Context, integrationID string, limit int) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.started_at, r.finished_at, r.trigger, coalesce(t.name, ''), r.received, r.created, r.updated,
			r.unchanged, r.deleted, r.failed, r.errors
		FROM ingestion_runs r LEFT JOIN ingestion_tokens t ON t.id = r.token_id
		WHERE r.integration_id = $1
		ORDER BY r.id DESC LIMIT $2`, integrationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []Run{}
	for rows.Next() {
		var (
			r      Run
			errors []byte
		)
		if err := rows.Scan(&r.ID, &r.StartedAt, &r.FinishedAt, &r.Trigger, &r.Token, &r.Received, &r.Created, &r.Updated,
			&r.Unchanged, &r.Deleted, &r.Failed, &errors); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(errors, &r.Errors); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}

	return runs, rows.Err()
}

func nullable(s string) any {
	if s == "" {
		return nil
	}

	return s
}

// LastSync is when an integration's last sync finished, or zero if it hasn't synced.
func (s Store) LastSync(ctx context.Context, identifier string) (time.Time, error) {
	var last sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT max(r.finished_at) FROM ingestion_runs r
		JOIN config_documents d ON d.id = r.integration_id
		WHERE d.kind = 'integration' AND d.identifier = $1 AND r.trigger <> 'push'`, identifier).Scan(&last)

	return last.Time, err
}
