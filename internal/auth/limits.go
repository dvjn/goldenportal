package auth

import (
	"context"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Limits fixed in this version, shown read-only in settings.
const (
	SessionIdleTimeout = sessionIdleTimeout
	SessionMaxAge      = sessionMaxAge
	TokenPrefix        = tokenPrefix
	MaxTokenDays       = maxTokenDays
)

const checkTimeout = 10 * time.Second

// RedirectURL is the callback address to register with the identity provider.
func (o *OIDC) RedirectURL() string {
	return strings.TrimRight(o.cfg.BaseURL, "/") + callbackPath
}

// Check fetches the identity provider's metadata afresh and returns the
// issuer it reports, so admins can confirm the settings without signing in.
func (o *OIDC) Check(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	provider, err := oidc.NewProvider(ctx, o.cfg.Issuer)
	if err != nil {
		return "", err
	}

	var metadata struct {
		Issuer string `json:"issuer"`
	}
	if err := provider.Claims(&metadata); err != nil {
		return "", err
	}

	return metadata.Issuer, nil
}
