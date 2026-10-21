// Package apitoken stores the bearer tokens that machines call the portal
// with. Only a token's SHA-256 hash is stored.
package apitoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"gofr.dev/pkg/gofr/container"
)

const (
	KindSCIM = "scim"
	KindAPI  = "api"
)

// Prefix starts every token the portal generates, so a leaked token is easy
// to recognise.
const Prefix = "gpt_"

var ErrNotFound = errors.New("token not found")

// Token is a stored token. The zero time stands for an unset ExpiresAt or
// LastUsedAt.
type Token struct {
	ID             string
	Kind           string
	Name           string
	Role           access.Role
	CreatedBy      string
	CreatedByEmail string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	LastUsedAt     time.Time
}

type NewToken struct {
	Kind      string
	Name      string
	Role      access.Role
	CreatedBy string
	ExpiresAt time.Time
}

type Store struct {
	db container.DB
}

func NewStore(db container.DB) *Store {
	return &Store{db: db}
}

const tokenColumns = `t.id, t.kind, t.name, COALESCE(t.role, ''), COALESCE(t.created_by::text, ''), COALESCE(u.email, ''),
	t.created_at, t.expires_at, t.last_used_at`

const (
	queryFindToken = `WITH used AS (
			UPDATE api_tokens SET last_used_at = NOW()
			WHERE kind = $1 AND token_hash = $2 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > NOW())
			RETURNING *
		)
		SELECT ` + tokenColumns + ` FROM used t LEFT JOIN users u ON u.id = t.created_by`
	querySeedToken = `INSERT INTO api_tokens (kind, name, token_hash)
		SELECT $1, $2, $3 WHERE NOT EXISTS (SELECT 1 FROM api_tokens WHERE kind = $1)`
	queryCreateToken = `WITH created AS (
			INSERT INTO api_tokens (kind, name, token_hash, role, created_by, expires_at)
			VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, '')::uuid, $6)
			RETURNING *
		)
		SELECT ` + tokenColumns + ` FROM created t LEFT JOIN users u ON u.id = t.created_by`
	queryListTokens = `SELECT ` + tokenColumns + ` FROM api_tokens t LEFT JOIN users u ON u.id = t.created_by
		WHERE t.kind = $1 AND t.revoked_at IS NULL ORDER BY t.created_at DESC`
	queryRevokeToken = `WITH revoked AS (
			UPDATE api_tokens SET revoked_at = NOW()
			WHERE kind = $1 AND id::text = $2 AND revoked_at IS NULL
			RETURNING *
		)
		SELECT ` + tokenColumns + ` FROM revoked t LEFT JOIN users u ON u.id = t.created_by`
	queryRevokeOthers = `UPDATE api_tokens SET revoked_at = NOW() WHERE kind = $1 AND id <> $2 AND revoked_at IS NULL`
	queryLastUsed     = `SELECT MAX(last_used_at) FROM api_tokens WHERE kind = $1`
)

// Find returns the live token of the kind with the secret, and records its
// use.
func (s *Store) Find(ctx context.Context, kind, secret string) (Token, error) {
	return scanToken(s.db.QueryRowContext(ctx, queryFindToken, kind, hash(secret)))
}

// Verify reports whether the secret is a live token of the kind, and records
// its use.
func (s *Store) Verify(ctx context.Context, kind, secret string) (bool, error) {
	_, err := s.Find(ctx, kind, secret)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}

	return err == nil, err
}

// Seed stores the token only while no token of the kind exists, so a token
// from the configuration starts a fresh install and the portal manages every
// token after that. It reports whether it stored the token.
func (s *Store) Seed(ctx context.Context, kind, name, token string) (bool, error) {
	result, err := s.db.ExecContext(ctx, querySeedToken, kind, name, hash(token))
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()

	return inserted == 1, err
}

// Create stores a new token and returns it with its secret, which is never
// readable again.
func (s *Store) Create(ctx context.Context, token NewToken) (Token, string, error) {
	return create(ctx, s.db, token)
}

// Rotate creates a new token of the kind and revokes every other one.
func (s *Store) Rotate(ctx context.Context, token NewToken) (Token, string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Token{}, "", err
	}
	defer func() { _ = tx.Rollback() }()

	created, secret, err := create(ctx, tx, token)
	if err != nil {
		return Token{}, "", err
	}
	if _, err := tx.ExecContext(ctx, queryRevokeOthers, token.Kind, created.ID); err != nil {
		return Token{}, "", err
	}

	return created, secret, tx.Commit()
}

// List returns the tokens of the kind that are not revoked, newest first.
func (s *Store) List(ctx context.Context, kind string) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, queryListTokens, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := []Token{}
	for rows.Next() {
		token, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}

	return tokens, rows.Err()
}

func (s *Store) Revoke(ctx context.Context, kind, id string) (Token, error) {
	return scanToken(s.db.QueryRowContext(ctx, queryRevokeToken, kind, id))
}

// LastUsed returns when any token of the kind was last used, or the zero
// time.
func (s *Store) LastUsed(ctx context.Context, kind string) (time.Time, error) {
	var lastUsed sql.NullTime
	err := s.db.QueryRowContext(ctx, queryLastUsed, kind).Scan(&lastUsed)

	return lastUsed.Time, err
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func create(ctx context.Context, db rowQuerier, token NewToken) (Token, string, error) {
	secret := Prefix + rand.Text()
	expiresAt := sql.NullTime{Time: token.ExpiresAt, Valid: !token.ExpiresAt.IsZero()}
	created, err := scanToken(db.QueryRowContext(ctx, queryCreateToken,
		token.Kind, token.Name, hash(secret), token.Role, token.CreatedBy, expiresAt))

	return created, secret, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanToken(row scanner) (Token, error) {
	var (
		token                 Token
		expiresAt, lastUsedAt sql.NullTime
	)
	err := row.Scan(&token.ID, &token.Kind, &token.Name, &token.Role, &token.CreatedBy, &token.CreatedByEmail,
		&token.CreatedAt, &expiresAt, &lastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Token{}, ErrNotFound
	}
	token.ExpiresAt, token.LastUsedAt = expiresAt.Time, lastUsedAt.Time

	return token, err
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
