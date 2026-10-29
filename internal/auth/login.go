package auth

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"
	"golang.org/x/oauth2"
)

const loginAttemptMaxAge = 10 * time.Minute

// loginAttempt travels in the login cookie from the login redirect to the
// callback. It is useless without the browser that holds the cookie, so it is
// not signed.
type loginAttempt struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	ReturnTo string `json:"return_to"`
}

var (
	errSignInFailed   = httpx.Error{Code: http.StatusBadRequest, Message: "sign-in failed, start again from the portal"}
	errNotProvisioned = errors.New("user not provisioned")
)

func (s *Service) login(ctx *gofr.Context) (any, error) {
	attempt := loginAttempt{
		State:    rand.Text(),
		Nonce:    rand.Text(),
		Verifier: oauth2.GenerateVerifier(),
		ReturnTo: safeReturnTo(ctx.Param("return_to")),
	}
	encoded, err := json.Marshal(attempt)
	if err != nil {
		return nil, err
	}
	http.SetCookie(httpx.RawWriter(ctx), newCookie(s.config.Cookies.Login,
		base64.RawURLEncoding.EncodeToString(encoded), loginAttemptMaxAge))

	return response.Redirect{URL: s.provider.AuthCodeURL(attempt)}, nil
}

func (s *Service) callback(ctx *gofr.Context) (any, error) {
	http.SetCookie(httpx.RawWriter(ctx), expiredCookie(s.config.Cookies.Login))

	attempt, err := s.readLoginAttempt(ctx)
	if err != nil {
		ctx.Infof("login refused: %v", err)
		return nil, errSignInFailed
	}
	who, err := s.provider.Exchange(ctx, attempt, ctx.Param("code"))
	if err != nil {
		ctx.Infof("login refused: %v", err)
		return nil, errSignInFailed
	}

	user, matchedBy, err := s.findUser(ctx, who)
	if errors.Is(err, errNotProvisioned) {
		ctx.Infof("login refused: user not provisioned sub=%s email=%s matched_by=%s", who.Subject, who.Email, matchedBy)
		refused := audit.Entry{
			Actor:   audit.Actor{Kind: audit.ActorUser, Name: cmp.Or(who.Email, who.Subject), Email: who.Email},
			Action:  audit.SignInRefused,
			Details: map[string]any{"reason": "not provisioned", "subject": who.Subject, "matched_by": matchedBy},
		}
		if err := s.audit.Record(ctx, refused); err != nil {
			return nil, err
		}

		return response.Redirect{URL: "/not-provisioned"}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := s.directory.RecordSignIn(ctx, user.ID, who.Subject); err != nil {
		return nil, err
	}

	token, err := s.sessions.Create(ctx, user.ID, who.IDToken)
	if err != nil {
		return nil, err
	}
	signedIn := audit.Entry{
		Actor:    Principal{User: user}.Actor(),
		Action:   audit.SignedIn,
		Resource: audit.Resource{Kind: audit.ResourceUser, ID: user.ID, Name: cmp.Or(user.Email, user.UserName)},
		Details:  map[string]any{"matched_by": matchedBy},
	}
	if err := s.audit.Record(ctx, signedIn); err != nil {
		return nil, err
	}
	http.SetCookie(httpx.RawWriter(ctx), newCookie(s.config.Cookies.Session, token, s.config.SessionMaxAge))

	return response.Redirect{URL: attempt.ReturnTo}, nil
}

type logoutResponse struct {
	RedirectTo string `json:"redirect_to"`
}

func (s *Service) logout(ctx *gofr.Context) (any, error) {
	principal, _ := PrincipalFrom(ctx)
	idToken, err := s.sessions.Delete(ctx, principal.SessionID)
	if err != nil && !errors.Is(err, ErrNoSession) {
		return nil, err
	}
	if err := s.audit.Record(ctx, audit.Entry{Actor: principal.Actor(), Action: audit.SignedOut}); err != nil {
		return nil, err
	}
	http.SetCookie(httpx.RawWriter(ctx), expiredCookie(s.config.Cookies.Session))

	return logoutResponse{RedirectTo: s.provider.EndSessionURL(idToken)}, nil
}

// signout only sends the browser to the provider's logout, without a hint
// because there is no session. It answers a GET, so it changes no state.
func (s *Service) signout(*gofr.Context) (any, error) {
	return response.Redirect{URL: s.provider.EndSessionURL("")}, nil
}

func (s *Service) readLoginAttempt(ctx *gofr.Context) (loginAttempt, error) {
	cookie, err := httpx.RawRequest(ctx).Cookie(s.config.Cookies.Login)
	if err != nil {
		return loginAttempt{}, errors.New("the login cookie is missing")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return loginAttempt{}, errors.New("the login cookie is malformed")
	}
	var attempt loginAttempt
	if err := json.Unmarshal(decoded, &attempt); err != nil {
		return loginAttempt{}, errors.New("the login cookie is malformed")
	}
	if attempt.State == "" || attempt.State != ctx.Param("state") {
		return loginAttempt{}, errors.New("the login state does not match")
	}

	return attempt, nil
}

// findUser matches the signed-in identity to a provisioned, active user: by
// the subject seen at an earlier sign-in, then by the SCIM external ID, which
// Authentik sets to the same value as the subject, then by a unique active
// email for users created before SCIM ran. A bootstrap admin that SCIM has
// not provisioned yet gets a user row, which SCIM adopts later.
func (s *Service) findUser(ctx context.Context, who identity) (directory.User, string, error) {
	matchers := []struct {
		name string
		find func(context.Context, string) (directory.User, error)
		key  string
	}{
		{"oidc_subject", s.directory.UserByOIDCSubject, who.Subject},
		{"external_id", s.directory.UserByExternalID, who.Subject},
		{"email", s.directory.ActiveUserByEmail, who.Email},
		{"bootstrap_admin", func(ctx context.Context, email string) (directory.User, error) {
			return s.createBootstrapAdmin(ctx, email, who.Name)
		}, who.Email},
	}
	for _, matcher := range matchers {
		user, err := matcher.find(ctx, matcher.key)
		if errors.Is(err, directory.ErrNotFound) {
			continue
		}
		if err == nil && !user.Active {
			err = errNotProvisioned
		}

		return user, matcher.name, err
	}

	return directory.User{}, "none", errNotProvisioned
}

// createBootstrapAdmin never creates a second row for an email that a user
// already has, so a deactivated bootstrap admin stays out.
func (s *Service) createBootstrapAdmin(ctx context.Context, email, name string) (directory.User, error) {
	if !s.config.BootstrapAdmins.Contains(email) {
		return directory.User{}, directory.ErrNotFound
	}

	return s.directory.CreateUnprovisionedUser(ctx, email, name)
}

// safeReturnTo keeps the post-login redirect on this origin. Browsers read
// "//host" and "/\host" as another host.
func safeReturnTo(path string) string {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.HasPrefix(path, `/\`) {
		return "/"
	}

	return path
}
