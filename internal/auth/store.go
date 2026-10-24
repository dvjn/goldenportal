package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"

	"github.com/lib/pq"
)

const (
	sessionIdleTimeout = 8 * time.Hour
	sessionMaxAge      = 7 * 24 * time.Hour
	// lastSeenResolution limits how often a busy session writes its last-seen time.
	lastSeenResolution = time.Minute
	signInTimeout      = 10 * time.Minute
)

// EffectiveRoleSQL selects the highest role bound to user u directly or
// through one of their groups; everyone is at least a member.
const EffectiveRoleSQL = `coalesce((
	SELECT rb.role FROM role_bindings rb
	WHERE (rb.subject_type = 'user' AND rb.subject_id = u.id)
		OR (rb.subject_type = 'group' AND rb.subject_id IN (
			SELECT gm.group_id FROM group_members gm WHERE gm.user_id = u.id))
	ORDER BY CASE rb.role WHEN 'admin' THEN 3 WHEN 'builder' THEN 2 ELSE 1 END DESC
	LIMIT 1
), 'member')`

var (
	errNotFound    = errors.New("not found")
	errDeactivated = errors.New("user is deactivated")
)

// DB is the part of the gofr SQL datasource the store uses.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type Store struct{ db DB }

func NewStore(db DB) Store { return Store{db: db} }

func newSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)

	return base64.RawURLEncoding.EncodeToString(b)
}

func hashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

type signInRequest struct {
	nonce        string
	codeVerifier string
	redirect     string
}

// CreateSignInRequest stores a sign-in in progress and clears out stale ones.
func (s Store) CreateSignInRequest(ctx context.Context, stateHash []byte, req signInRequest) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sign_in_requests WHERE expires_at < now()`); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sign_in_requests (state_hash, nonce, code_verifier, redirect, expires_at)
		VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5))`,
		stateHash, req.nonce, req.codeVerifier, req.redirect, signInTimeout.Seconds())

	return err
}

// TakeSignInRequest returns a live sign-in request and deletes it, so it can finish only once.
func (s Store) TakeSignInRequest(ctx context.Context, stateHash []byte) (signInRequest, error) {
	var req signInRequest
	err := s.db.QueryRowContext(ctx, `
		DELETE FROM sign_in_requests
		WHERE state_hash = $1 AND expires_at > now()
		RETURNING nonce, code_verifier, redirect`, stateHash).
		Scan(&req.nonce, &req.codeVerifier, &req.redirect)
	if errors.Is(err, sql.ErrNoRows) {
		return signInRequest{}, errNotFound
	}

	return req, err
}

// UpsertUser creates or refreshes the user an identity provider vouched for.
func (s Store) UpsertUser(ctx context.Context, issuer, subject, email, displayName string) (string, error) {
	var id, status string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO users (oidc_issuer, oidc_subject, email, display_name)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (oidc_issuer, oidc_subject)
		DO UPDATE SET email = EXCLUDED.email, display_name = EXCLUDED.display_name
		RETURNING id, status`, issuer, subject, email, displayName).Scan(&id, &status)
	if err != nil {
		return "", err
	}
	if status != "active" {
		return "", errDeactivated
	}

	return id, nil
}

// SyncGroups makes the user's group memberships match the identity provider's
// list, creating groups the portal hasn't seen before.
func (s Store) SyncGroups(ctx context.Context, userID string, names []string) error {
	groups := pq.Array(names)

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO groups (name) SELECT unnest($1::text[])
		ON CONFLICT (name) DO NOTHING`, groups); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM group_members WHERE user_id = $1
			AND group_id NOT IN (SELECT id FROM groups WHERE name = ANY($2))`, userID, groups); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO group_members (group_id, user_id)
		SELECT id, $1 FROM groups WHERE name = ANY($2)
		ON CONFLICT DO NOTHING`, userID, groups)

	return err
}

// GrantRole binds role to the user and reports whether they didn't have it yet.
func (s Store) GrantRole(ctx context.Context, userID string, role Role) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO role_bindings (subject_type, subject_id, role)
		VALUES ('user', $1, $2)
		ON CONFLICT DO NOTHING`, userID, string(role))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()

	return n > 0, err
}

// CreateSession stores a new session for userID and clears out expired ones.
func (s Store) CreateSession(ctx context.Context, userID string, sessionHash []byte) error {
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE expires_at < now() OR last_seen_at < now() - make_interval(secs => $1)`,
		sessionIdleTimeout.Seconds()); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id_hash, user_id, expires_at)
		VALUES ($1, $2, now() + make_interval(secs => $3))`,
		sessionHash, userID, sessionMaxAge.Seconds()); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, userID)

	return err
}

// SessionPrincipal returns the user behind a live session and keeps it alive.
func (s Store) SessionPrincipal(ctx context.Context, sessionHash []byte) (Principal, error) {
	var (
		p        Principal
		lastSeen time.Time
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.display_name, s.last_seen_at, `+EffectiveRoleSQL+`
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.id_hash = $1
			AND s.expires_at > now()
			AND s.last_seen_at > now() - make_interval(secs => $2)
			AND u.status = 'active'`,
		sessionHash, sessionIdleTimeout.Seconds()).
		Scan(&p.ID, &p.Email, &p.DisplayName, &lastSeen, &p.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, errNotFound
	}
	if err != nil {
		return Principal{}, err
	}

	if time.Since(lastSeen) > lastSeenResolution {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE sessions SET last_seen_at = now() WHERE id_hash = $1`, sessionHash); err != nil {
			return Principal{}, err
		}
	}

	return p, nil
}

func (s Store) DeleteSession(ctx context.Context, sessionHash []byte) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = $1`, sessionHash)
	return err
}
