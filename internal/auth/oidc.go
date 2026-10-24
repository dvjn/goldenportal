package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"gofr.dev/pkg/gofr/logging"
	"golang.org/x/oauth2"

	"github.com/dvjn/goldenportal/internal/audit"
)

const (
	signInPath   = "/api/v1/auth/login"
	callbackPath = "/api/v1/auth/callback"
	// stateCookie ties a callback to the browser that started the sign-in.
	stateCookie = "gp_sign_in"
)

type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	// BaseURL is the portal's public URL. The identity provider redirects to BaseURL + /api/v1/auth/callback.
	BaseURL string
	// GroupsClaim names the ID token claim that lists the user's groups.
	GroupsClaim string
	// SCIMManagesGroups turns off group sync from the claim: SCIM keeps groups instead.
	SCIMManagesGroups bool
	// BootstrapAdminEmail is made an admin when they sign in with that email verified.
	BootstrapAdminEmail string
	SecureCookies       bool
}

// OIDC signs people in with the authorization code flow and PKCE.
type OIDC struct {
	cfg    OIDCConfig
	logger logging.Logger

	mu       sync.Mutex
	provider *oidc.Provider
}

func NewOIDC(cfg OIDCConfig, logger logging.Logger) *OIDC {
	return &OIDC{cfg: cfg, logger: logger}
}

func (o *OIDC) Configured() bool {
	return o.cfg.Issuer != "" && o.cfg.ClientID != ""
}

// Routes serves the sign-in endpoints. They answer with redirects and set
// cookies, so they run as plain HTTP handlers instead of gofr handlers.
func (o *OIDC) Routes(store Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				switch r.URL.Path {
				case signInPath:
					o.signIn(w, r, store)
					return
				case callbackPath:
					o.callback(w, r, store)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// discover fetches the provider's metadata on first use, so the portal can
// start before the identity provider is reachable.
func (o *OIDC) discover(ctx context.Context) (*oidc.Provider, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.provider == nil {
		provider, err := oidc.NewProvider(ctx, o.cfg.Issuer)
		if err != nil {
			return nil, err
		}
		o.provider = provider
	}

	return o.provider, nil
}

func (o *OIDC) oauth(provider *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     o.cfg.ClientID,
		ClientSecret: o.cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  o.RedirectURL(),
		Scopes:       o.cfg.Scopes,
	}
}

func (o *OIDC) signIn(w http.ResponseWriter, r *http.Request, store Store) {
	if !o.Configured() {
		failSignIn(w, r, "Single sign-on is not set up. Set GP_OIDC_ISSUER, GP_OIDC_CLIENT_ID and GP_OIDC_CLIENT_SECRET.")
		return
	}

	provider, err := o.discover(r.Context())
	if err != nil {
		o.logger.Errorf("discover OIDC provider %s: %v", o.cfg.Issuer, err)
		failSignIn(w, r, "The identity provider can't be reached. Try again in a moment.")
		return
	}

	state, nonce, verifier := newSecret(), newSecret(), oauth2.GenerateVerifier()
	req := signInRequest{nonce: nonce, codeVerifier: verifier, redirect: safeRedirect(r.URL.Query().Get("redirect"))}
	if err := store.CreateSignInRequest(r.Context(), hashSecret(state), req); err != nil {
		o.logger.Errorf("store sign-in request: %v", err)
		failSignIn(w, r, "Signing in failed. Try again.")
		return
	}

	http.SetCookie(w, o.cookie(stateCookie, state, int(signInTimeout.Seconds())))
	http.Redirect(w, r, o.oauth(provider).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

type idClaims struct {
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
}

func (c idClaims) displayName() string {
	for _, name := range []string{c.Name, c.PreferredUsername, c.Email} {
		if name != "" {
			return name
		}
	}

	return "Unnamed user"
}

func (o *OIDC) callback(w http.ResponseWriter, r *http.Request, store Store) {
	ctx := r.Context()
	query := r.URL.Query()
	http.SetCookie(w, o.cookie(stateCookie, "", -1))

	if code := query.Get("error"); code != "" {
		message := query.Get("error_description")
		if message == "" {
			message = "The identity provider stopped the sign-in (" + code + ")."
		}
		failSignIn(w, r, message)
		return
	}

	state := query.Get("state")
	cookie, err := r.Cookie(stateCookie)
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		failSignIn(w, r, "The sign-in expired or was started in another browser. Sign in again.")
		return
	}

	req, err := store.TakeSignInRequest(ctx, hashSecret(state))
	if errors.Is(err, errNotFound) {
		failSignIn(w, r, "The sign-in expired or was started in another browser. Sign in again.")
		return
	}
	if err != nil {
		o.fail(w, r, "load sign-in request", err)
		return
	}

	provider, err := o.discover(ctx)
	if err != nil {
		o.fail(w, r, "discover OIDC provider", err)
		return
	}

	token, err := o.oauth(provider).Exchange(ctx, query.Get("code"), oauth2.VerifierOption(req.codeVerifier))
	if err != nil {
		o.fail(w, r, "exchange authorization code", err)
		return
	}
	rawIDToken, _ := token.Extra("id_token").(string)
	idToken, err := provider.Verifier(&oidc.Config{ClientID: o.cfg.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		o.fail(w, r, "verify ID token", err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(req.nonce)) != 1 {
		o.fail(w, r, "verify ID token", errors.New("nonce does not match"))
		return
	}

	var claims idClaims
	if err := idToken.Claims(&claims); err != nil {
		o.fail(w, r, "read ID token claims", err)
		return
	}
	groups, err := o.groups(idToken)
	if err != nil {
		o.fail(w, r, "read ID token groups", err)
		return
	}

	userID, err := store.UpsertUser(ctx, idToken.Issuer, idToken.Subject, claims.Email, claims.displayName(), claims.EmailVerified)
	if errors.Is(err, errDeactivated) {
		failSignIn(w, r, "Your account is deactivated. Ask an admin to reactivate it.")
		return
	}
	if err != nil {
		o.fail(w, r, "save user", err)
		return
	}

	if !o.cfg.SCIMManagesGroups {
		if err := store.SyncGroups(ctx, userID, groups); err != nil {
			o.fail(w, r, "sync groups", err)
			return
		}
	}

	if o.cfg.BootstrapAdminEmail != "" && strings.EqualFold(claims.Email, o.cfg.BootstrapAdminEmail) {
		if !claims.EmailVerified {
			o.logger.Warnf("%s signed in but the identity provider did not mark the email as verified, so they were not made an admin", claims.Email)
		} else {
			granted, err := store.GrantRole(ctx, userID, RoleAdmin)
			if err != nil {
				o.fail(w, r, "grant bootstrap admin", err)
				return
			}
			if granted {
				o.record(ctx, store, audit.Actor{Label: "GP_BOOTSTRAP_ADMIN_EMAIL"}, audit.BootstrapAdmin,
					audit.Target{Type: "user", ID: userID, Label: claims.Email}, map[string]any{"role": RoleAdmin})
			}
		}
	}

	secret := newSecret()
	if err := store.CreateSession(ctx, userID, hashSecret(secret)); err != nil {
		o.fail(w, r, "create session", err)
		return
	}

	o.record(ctx, store, audit.Actor{ID: userID, Label: claims.Email}, audit.SignIn,
		audit.Target{Type: "user", ID: userID, Label: claims.Email}, nil)

	http.SetCookie(w, o.cookie(sessionCookie, secret, int(sessionMaxAge.Seconds())))
	http.Redirect(w, r, req.redirect, http.StatusFound)
}

// record writes an audit event. A failure is logged but doesn't stop the sign-in.
func (o *OIDC) record(ctx context.Context, store Store, actor audit.Actor, action string, target audit.Target, details map[string]any) {
	if err := audit.Record(ctx, store.db, o.logger, actor, action, target, details); err != nil {
		o.logger.Errorf("record audit event %s: %v", action, err)
	}
}

// groups reads the configured groups claim. A missing claim means no groups.
func (o *OIDC) groups(idToken *oidc.IDToken) ([]string, error) {
	var all map[string]any
	if err := idToken.Claims(&all); err != nil {
		return nil, err
	}

	raw, _ := all[o.cfg.GroupsClaim].([]any)
	groups := make([]string, 0, len(raw))
	for _, value := range raw {
		if name, ok := value.(string); ok && name != "" {
			groups = append(groups, name)
		}
	}

	return groups, nil
}

func (o *OIDC) fail(w http.ResponseWriter, r *http.Request, step string, err error) {
	o.logger.Errorf("sign-in: %s: %v", step, err)
	failSignIn(w, r, "Signing in failed. Try again, and ask an admin to check the server log if it keeps failing.")
}

func failSignIn(w http.ResponseWriter, r *http.Request, message string) {
	http.Redirect(w, r, "/sign-in?error="+url.QueryEscape(message), http.StatusFound)
}

func (o *OIDC) cookie(name, value string, maxAge int) *http.Cookie {
	return newCookie(name, value, maxAge, o.cfg.SecureCookies)
}

func newCookie(name, value string, maxAge int, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// safeRedirect keeps post-sign-in redirects on this site.
func safeRedirect(target string) string {
	if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") && !strings.HasPrefix(target, `/\`) {
		return target
	}

	return "/"
}
