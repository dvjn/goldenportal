package oauth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/auth"
)

var errNotFound = errors.New("not found")

// lastUsedResolution keeps grants from being rewritten on every request.
const lastUsedResolution = time.Minute

type Store struct{ db auth.DB }

func NewStore(db auth.DB) Store { return Store{db: db} }

// Client is an application that registered itself.
type Client struct {
	ID           string
	ClientID     string
	Name         string
	RedirectURIs []string
}

// Grant is one user's consent for one client.
type Grant struct {
	ID       string
	UserID   string
	ClientID string
	Scopes   []string
}

// Code is an authorization code, until it's exchanged.
type Code struct {
	Grant       Grant
	RedirectURI string
	Challenge   string
	Resource    string
	Scopes      []string
}

func (s Store) CreateClient(ctx context.Context, clientID, name string, redirectURIs []string) (time.Time, error) {
	var created time.Time
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO oauth_clients (client_id, name, redirect_uris) VALUES ($1, $2, $3)
		RETURNING created_at`, clientID, name, pq.Array(redirectURIs)).Scan(&created)

	return created, err
}

func (s Store) Client(ctx context.Context, clientID string) (Client, error) {
	c := Client{ClientID: clientID}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, redirect_uris FROM oauth_clients WHERE client_id = $1`, clientID).
		Scan(&c.ID, &c.Name, pq.Array(&c.RedirectURIs))
	if errors.Is(err, sql.ErrNoRows) {
		return Client{}, errNotFound
	}

	return c, err
}

// Authorize records a user's consent for a client and returns a code for
// it. Consenting again replaces the scopes, so they're always what the
// user last agreed to.
func (s Store) Authorize(ctx context.Context, userID string, client Client, code Code, codeHash []byte) (string, error) {
	var grantID string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO oauth_grants (user_id, client_id, scopes) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, client_id) DO UPDATE SET scopes = EXCLUDED.scopes
		RETURNING id`, userID, client.ID, pq.Array(code.Scopes)).Scan(&grantID)
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO oauth_codes (code_hash, grant_id, redirect_uri, code_challenge, resource, scopes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		codeHash, grantID, code.RedirectURI, code.Challenge, code.Resource, pq.Array(code.Scopes),
		time.Now().Add(codeLifetime))

	return grantID, err
}

// TakeCode redeems an authorization code, which works once.
func (s Store) TakeCode(ctx context.Context, codeHash []byte) (Code, error) {
	var c Code
	err := s.db.QueryRowContext(ctx, `
		WITH taken AS (
			DELETE FROM oauth_codes WHERE code_hash = $1 RETURNING *
		)
		SELECT g.id, g.user_id, cl.client_id, t.redirect_uri, t.code_challenge, t.resource, t.scopes
		FROM taken t
		JOIN oauth_grants g ON g.id = t.grant_id
		JOIN oauth_clients cl ON cl.id = g.client_id
		WHERE t.expires_at > now()`, codeHash).
		Scan(&c.Grant.ID, &c.Grant.UserID, &c.Grant.ClientID, &c.RedirectURI, &c.Challenge, &c.Resource, pq.Array(&c.Scopes))
	if errors.Is(err, sql.ErrNoRows) {
		return Code{}, errNotFound
	}

	return c, err
}

// TakeRefresh redeems a refresh token, which works once.
func (s Store) TakeRefresh(ctx context.Context, tokenHash []byte) (Grant, error) {
	var g Grant
	err := s.db.QueryRowContext(ctx, `
		WITH taken AS (
			DELETE FROM oauth_tokens WHERE token_hash = $1 AND kind = 'refresh' RETURNING *
		)
		SELECT g.id, g.user_id, cl.client_id, t.scopes
		FROM taken t
		JOIN oauth_grants g ON g.id = t.grant_id
		JOIN oauth_clients cl ON cl.id = g.client_id
		WHERE t.expires_at > now()`, tokenHash).
		Scan(&g.ID, &g.UserID, &g.ClientID, pq.Array(&g.Scopes))
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, errNotFound
	}

	return g, err
}

// IssueTokens stores a new access and refresh token for a grant, unless
// its user has been deactivated. Expired tokens and codes go at the same
// time, so the tables don't grow.
func (s Store) IssueTokens(ctx context.Context, g Grant, resource string, accessHash, refreshHash []byte) error {
	var active bool
	err := s.db.QueryRowContext(ctx, `
		SELECT u.status = 'active' FROM oauth_grants g JOIN users u ON u.id = g.user_id WHERE g.id = $1`, g.ID).
		Scan(&active)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !active) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	now := time.Now()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO oauth_tokens (token_hash, grant_id, kind, resource, scopes, expires_at)
		VALUES ($1, $3, 'access', $4, $5, $6), ($2, $3, 'refresh', $4, $5, $7)`,
		accessHash, refreshHash, g.ID, resource, pq.Array(g.Scopes),
		now.Add(accessLifetime), now.Add(refreshLifetime)); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE expires_at < now()`); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM oauth_codes WHERE expires_at < now()`)

	return err
}

// RevokeToken ends a token the client holds. A refresh token takes the
// grant's other tokens with it.
func (s Store) RevokeToken(ctx context.Context, tokenHash []byte, clientID string) error {
	_, err := s.db.ExecContext(ctx, `
		WITH target AS (
			SELECT t.grant_id, t.kind FROM oauth_tokens t
			JOIN oauth_grants g ON g.id = t.grant_id
			JOIN oauth_clients cl ON cl.id = g.client_id
			WHERE t.token_hash = $1 AND ($2 = '' OR cl.client_id = $2)
		)
		DELETE FROM oauth_tokens t USING target
		WHERE t.token_hash = $1 OR (target.kind = 'refresh' AND t.grant_id = target.grant_id)`,
		tokenHash, clientID)

	return err
}

// ErrInvalidToken is a token that's unknown, expired, revoked, for another
// resource, or whose user can't sign in anymore.
var ErrInvalidToken = errors.New("the access token is invalid, expired or revoked")

// AccessPrincipal returns the user behind a live access token for
// resource, with the scopes it was granted. The role is the user's role
// now, not when they consented, so losing a role takes effect at once.
func (s Store) AccessPrincipal(ctx context.Context, token, resource string) (auth.Principal, []string, error) {
	tokenHash := hashSecret(token)
	var (
		p        auth.Principal
		scopes   []string
		grantID  string
		lastUsed sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.display_name, `+auth.EffectiveRoleSQL+`, `+auth.GroupNamesSQL+`,
			t.scopes, g.id, g.last_used_at
		FROM oauth_tokens t
		JOIN oauth_grants g ON g.id = t.grant_id
		JOIN users u ON u.id = g.user_id
		WHERE t.token_hash = $1 AND t.kind = 'access' AND t.resource = $2
			AND t.expires_at > now() AND u.status = 'active'`, tokenHash, resource).
		Scan(&p.ID, &p.Email, &p.DisplayName, &p.Role, pq.Array(&p.Groups), pq.Array(&scopes), &grantID, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.Principal{}, nil, ErrInvalidToken
	}
	if err != nil {
		return auth.Principal{}, nil, err
	}
	if !lastUsed.Valid || time.Since(lastUsed.Time) > lastUsedResolution {
		if _, err := s.db.ExecContext(ctx, `UPDATE oauth_grants SET last_used_at = now() WHERE id = $1`, grantID); err != nil {
			return auth.Principal{}, nil, err
		}
	}

	return p, scopes, nil
}

// AppGrant is a client a user has authorized, as their account lists it.
type AppGrant struct {
	ID         string     `json:"id"`
	Client     string     `json:"client"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

func (s Store) Grants(ctx context.Context, userID string) ([]AppGrant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id, cl.name, g.scopes, g.created_at, g.last_used_at
		FROM oauth_grants g JOIN oauth_clients cl ON cl.id = g.client_id
		WHERE g.user_id = $1
		ORDER BY g.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grants := []AppGrant{}
	for rows.Next() {
		var g AppGrant
		if err := rows.Scan(&g.ID, &g.Client, pq.Array(&g.Scopes), &g.CreatedAt, &g.LastUsedAt); err != nil {
			return nil, err
		}
		grants = append(grants, g)
	}

	return grants, rows.Err()
}

// RevokeGrant removes a user's consent for a client, with its codes and
// tokens, and returns the client's name.
func (s Store) RevokeGrant(ctx context.Context, userID, grantID string) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `
		WITH revoked AS (
			DELETE FROM oauth_grants WHERE id = $1 AND user_id = $2 RETURNING client_id
		)
		SELECT cl.name FROM revoked r JOIN oauth_clients cl ON cl.id = r.client_id`, grantID, userID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}

	return name, err
}
