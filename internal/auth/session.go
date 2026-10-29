package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/apitoken"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/directory"
	"gofr.dev/pkg/gofr/container"
)

var ErrNoSession = errors.New("no session")

// Principal is who is behind a request and the role they hold: a signed-in
// user with a session, or an API token, whose name stands in User.Name.
type Principal struct {
	User      directory.User
	SessionID string
	APIToken  apitoken.Token
	Access    access.Resolution
}

func (p Principal) Can(permission access.Permission) bool {
	return p.Access.Role.Can(permission)
}

var tokenActorKinds = map[string]audit.ActorKind{
	apitoken.KindAPI:    audit.ActorAPIToken,
	apitoken.KindIngest: audit.ActorIngestToken,
}

func (p Principal) Actor() audit.Actor {
	if kind, ok := tokenActorKinds[p.APIToken.Kind]; ok {
		return audit.Actor{Kind: kind, ID: p.APIToken.ID, Name: p.APIToken.Name}
	}

	return audit.Actor{Kind: audit.ActorUser, ID: p.User.ID, Name: p.User.Name, Email: p.User.Email}
}

type Sessions struct {
	db          container.DB
	maxAge      time.Duration
	idleTimeout time.Duration
}

func NewSessions(db container.DB, config Config) *Sessions {
	return &Sessions{db: db, maxAge: config.SessionMaxAge, idleTimeout: config.SessionIdleTimeout}
}

const (
	queryCreateSession = `INSERT INTO sessions (token_hash, user_id, id_token, expires_at)
		VALUES ($1, $2, $3, NOW() + make_interval(secs => $4))`
	queryLookupSession = `SELECT s.id, NOW() - s.last_seen_at > interval '60 seconds',
			u.id, COALESCE(u.external_id, ''), COALESCE(u.oidc_subject, ''), u.user_name, u.email, u.name,
			u.active, u.created_at, u.updated_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1
			AND s.expires_at > NOW()
			AND s.last_seen_at > NOW() - make_interval(secs => $2)
			AND u.active`
	queryTouchSession         = `UPDATE sessions SET last_seen_at = NOW() WHERE id = $1`
	queryDeleteSession        = `DELETE FROM sessions WHERE id = $1 RETURNING id_token`
	queryDeleteSessionsOfUser = `DELETE FROM sessions WHERE user_id = $1`
)

// Create stores a new session for the user and returns the token for the
// cookie. Only the token's hash is stored. The ID token from sign-in is kept
// as the id_token_hint for the provider's logout.
func (s *Sessions) Create(ctx context.Context, userID, idToken string) (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(secret)

	if _, err := s.db.ExecContext(ctx, queryCreateSession, hashToken(token), userID, idToken, s.maxAge.Seconds()); err != nil {
		return "", err
	}

	return token, nil
}

// Lookup finds the live session for a token and refreshes its last use at
// most once a minute.
func (s *Sessions) Lookup(ctx context.Context, token string) (Principal, error) {
	var (
		principal Principal
		stale     bool
	)
	user := &principal.User
	err := s.db.QueryRowContext(ctx, queryLookupSession, hashToken(token), s.idleTimeout.Seconds()).Scan(
		&principal.SessionID, &stale, &user.ID, &user.ExternalID, &user.OIDCSubject, &user.UserName,
		&user.Email, &user.Name, &user.Active, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrNoSession
	}
	if err != nil {
		return Principal{}, err
	}

	if stale {
		if _, err := s.db.ExecContext(ctx, queryTouchSession, principal.SessionID); err != nil {
			return Principal{}, err
		}
	}

	return principal, nil
}

// Delete removes the session and returns the ID token stored with it.
func (s *Sessions) Delete(ctx context.Context, id string) (string, error) {
	var idToken string
	err := s.db.QueryRowContext(ctx, queryDeleteSession, id).Scan(&idToken)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoSession
	}

	return idToken, err
}

func (s *Sessions) DeleteForUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, queryDeleteSessionsOfUser, userID)
	return err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
