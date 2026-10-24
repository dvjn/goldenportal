package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/audit"
	"github.com/dvjn/goldenportal/internal/httpx"
)

const (
	// tokenPrefix marks personal API tokens, so secret scanners can recognise them.
	tokenPrefix        = "gpp_"
	tokenDisplayLength = len(tokenPrefix) + 8
	defaultTokenDays   = 90
	maxTokenDays       = 365
	maxTokenNameLength = 100
)

type APIToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	ReadOnly   bool       `json:"readOnly"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

const tokenColumns = `id, name, prefix, read_only, expires_at, last_used_at, created_at`

func scanToken(row interface{ Scan(...any) error }) (APIToken, error) {
	var t APIToken
	err := row.Scan(&t.ID, &t.Name, &t.Prefix, &t.ReadOnly, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)

	return t, err
}

func (s Store) CreateToken(ctx context.Context, userID, name, prefix string, secretHash []byte, readOnly bool, ttl time.Duration) (APIToken, error) {
	return scanToken(s.db.QueryRowContext(ctx, `
		INSERT INTO api_tokens (user_id, name, prefix, secret_hash, read_only, expires_at)
		VALUES ($1, $2, $3, $4, $5, now() + make_interval(secs => $6))
		RETURNING `+tokenColumns, userID, name, prefix, secretHash, readOnly, ttl.Seconds()))
}

// ListTokens returns a user's tokens that haven't been revoked, newest first.
func (s Store) ListTokens(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+tokenColumns+` FROM api_tokens
		WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := []APIToken{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}

	return tokens, rows.Err()
}

// RevokeToken revokes one of the user's tokens and returns its name.
func (s Store) RevokeToken(ctx context.Context, userID, tokenID string) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `
		UPDATE api_tokens SET revoked_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
		RETURNING name`, tokenID, userID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}

	return name, err
}

// TokenPrincipal returns the user behind a live API token and whether the token is read-only.
func (s Store) TokenPrincipal(ctx context.Context, secretHash []byte) (Principal, bool, error) {
	var (
		p        Principal
		readOnly bool
		lastUsed sql.NullTime
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.display_name, t.read_only, t.last_used_at, `+EffectiveRoleSQL+`
		FROM api_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.secret_hash = $1
			AND t.revoked_at IS NULL
			AND t.expires_at > now()
			AND u.status = 'active'`, secretHash).
		Scan(&p.ID, &p.Email, &p.DisplayName, &readOnly, &lastUsed, &p.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, false, errNotFound
	}
	if err != nil {
		return Principal{}, false, err
	}

	if !lastUsed.Valid || time.Since(lastUsed.Time) > lastSeenResolution {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE api_tokens SET last_used_at = now() WHERE secret_hash = $1`, secretHash); err != nil {
			return Principal{}, false, err
		}
	}

	return p, readOnly, nil
}

// requireSession stops API tokens from managing tokens, so a leaked token
// can't mint longer-lived ones.
func requireSession(ctx *gofr.Context) (Principal, error) {
	info, _ := authFromContext(ctx)
	if info.viaToken {
		return Principal{}, httpx.Error{Code: http.StatusForbidden, Message: "manage API tokens from the portal, not with a token"}
	}

	return info.principal, nil
}

func (h Handlers) listTokens(ctx *gofr.Context) (any, error) {
	p, err := requireSession(ctx)
	if err != nil {
		return nil, err
	}

	return NewStore(ctx.SQL).ListTokens(ctx, p.ID)
}

type createTokenRequest struct {
	Name          string `json:"name"`
	ReadOnly      bool   `json:"readOnly"`
	ExpiresInDays int    `json:"expiresInDays"`
}

type createdToken struct {
	Token APIToken `json:"token"`
	// Secret is shown once; only its hash is stored.
	Secret string `json:"secret"`
}

func (h Handlers) createToken(ctx *gofr.Context) (any, error) {
	p, err := requireSession(ctx)
	if err != nil {
		return nil, err
	}

	var req createTokenRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.Error{Code: http.StatusBadRequest, Message: "the request body is not valid JSON"}
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.ExpiresInDays == 0 {
		req.ExpiresInDays = defaultTokenDays
	}
	switch {
	case req.Name == "" || len(req.Name) > maxTokenNameLength:
		return nil, httpx.Error{Code: http.StatusBadRequest, Message: "give the token a name of up to 100 characters"}
	case req.ExpiresInDays < 1 || req.ExpiresInDays > maxTokenDays:
		return nil, httpx.Error{Code: http.StatusBadRequest, Message: "choose an expiry between 1 and 365 days"}
	}

	secret := tokenPrefix + newSecret()
	token, err := NewStore(ctx.SQL).CreateToken(ctx, p.ID, req.Name, secret[:tokenDisplayLength],
		hashSecret(secret), req.ReadOnly, time.Duration(req.ExpiresInDays)*24*time.Hour)
	if err != nil {
		return nil, err
	}

	Record(ctx, p, audit.TokenCreate, audit.Target{Type: "token", ID: token.ID, Label: token.Name},
		map[string]any{"readOnly": token.ReadOnly, "expiresAt": token.ExpiresAt})

	return createdToken{Token: token, Secret: secret}, nil
}

func (h Handlers) revokeToken(ctx *gofr.Context) (any, error) {
	p, err := requireSession(ctx)
	if err != nil {
		return nil, err
	}

	notFound := httpx.Error{Code: http.StatusNotFound, Message: "no such API token"}
	id := ctx.PathParam("id")
	if _, err := uuid.Parse(id); err != nil {
		return nil, notFound
	}

	name, err := NewStore(ctx.SQL).RevokeToken(ctx, p.ID, id)
	if errors.Is(err, errNotFound) {
		return nil, notFound
	}
	if err != nil {
		return nil, err
	}

	Record(ctx, p, audit.TokenRevoke, audit.Target{Type: "token", ID: id, Label: name}, nil)

	return nil, nil
}
