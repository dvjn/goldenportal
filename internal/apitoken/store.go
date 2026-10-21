// Package apitoken stores the bearer tokens that machines call the portal
// with. Only a token's SHA-256 hash is stored.
package apitoken

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"

	"gofr.dev/pkg/gofr/container"
)

const KindSCIM = "scim"

type Store struct {
	db container.DB
}

func NewStore(db container.DB) *Store {
	return &Store{db: db}
}

const (
	queryVerifyToken = `UPDATE api_tokens SET last_used_at = NOW()
		WHERE kind = $1 AND token_hash = $2 AND revoked_at IS NULL
		RETURNING id`
	querySeedToken = `INSERT INTO api_tokens (kind, name, token_hash) VALUES ($1, $2, $3)
		ON CONFLICT (token_hash) DO NOTHING`
)

// Verify reports whether the token is a live token of the kind, and records
// its use.
func (s *Store) Verify(ctx context.Context, kind, token string) (bool, error) {
	var id string
	err := s.db.QueryRowContext(ctx, queryVerifyToken, kind, hash(token)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	return err == nil, err
}

// Seed stores the token unless a token with the same hash exists, revoked or
// not. It reports whether it stored the token.
func (s *Store) Seed(ctx context.Context, kind, name, token string) (bool, error) {
	result, err := s.db.ExecContext(ctx, querySeedToken, kind, name, hash(token))
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()

	return inserted == 1, err
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
