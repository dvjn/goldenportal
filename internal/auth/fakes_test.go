package auth

import (
	"context"
	"crypto/rand"
	"strings"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/httpx"
	"github.com/dvjn/goldport/internal/httpx/httpxtest"
	"github.com/dvjn/goldport/internal/token"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/logging"
)

var (
	testCookies         = CookieNames("GoldPort Test")
	testBootstrapAdmins = access.BootstrapAdmins{"boot@example.test"}
)

type memoryDirectory struct {
	users  []directory.User
	groups map[string][]directory.Group
}

func (d *memoryDirectory) find(match func(directory.User) bool) (directory.User, error) {
	for _, user := range d.users {
		if match(user) {
			return user, nil
		}
	}

	return directory.User{}, directory.ErrNotFound
}

func (d *memoryDirectory) UserByOIDCSubject(_ context.Context, subject string) (directory.User, error) {
	return d.find(func(u directory.User) bool { return subject != "" && u.OIDCSubject == subject })
}

func (d *memoryDirectory) UserByExternalID(_ context.Context, externalID string) (directory.User, error) {
	return d.find(func(u directory.User) bool { return externalID != "" && u.ExternalID == externalID })
}

func (d *memoryDirectory) ActiveUserByEmail(_ context.Context, email string) (directory.User, error) {
	return d.find(func(u directory.User) bool { return u.Active && u.Email != "" && strings.EqualFold(u.Email, email) })
}

func (d *memoryDirectory) RecordSignIn(_ context.Context, userID, subject string) error {
	for i := range d.users {
		if d.users[i].ID == userID {
			d.users[i].OIDCSubject = subject
		}
	}

	return nil
}

func (d *memoryDirectory) CreateUnprovisionedUser(_ context.Context, email, name string) (directory.User, error) {
	if _, err := d.find(func(u directory.User) bool { return strings.EqualFold(u.Email, email) }); err == nil {
		return directory.User{}, directory.ErrNotFound
	}
	user := directory.User{ID: "u-" + email, UserName: email, Email: email, Name: name, Active: true}
	d.users = append(d.users, user)

	return user, nil
}

func (d *memoryDirectory) GroupsOfUser(_ context.Context, userID string) ([]directory.Group, error) {
	return d.groups[userID], nil
}

// memorySessions uses the token as the session ID.
type memorySessions struct {
	directory      *memoryDirectory
	userByToken    map[string]string
	idTokenByToken map[string]string
	err            error
}

func newMemorySessions(users *memoryDirectory) *memorySessions {
	return &memorySessions{directory: users, userByToken: map[string]string{}, idTokenByToken: map[string]string{}}
}

func (m *memorySessions) Create(_ context.Context, userID, idToken string) (string, error) {
	token := rand.Text()
	m.userByToken[token] = userID
	m.idTokenByToken[token] = idToken

	return token, nil
}

func (m *memorySessions) Lookup(_ context.Context, token string) (Principal, error) {
	if m.err != nil {
		return Principal{}, m.err
	}
	user, err := m.directory.find(func(u directory.User) bool { return u.Active && u.ID == m.userByToken[token] })
	if err != nil {
		return Principal{}, ErrNoSession
	}

	return Principal{User: user, SessionID: token}, nil
}

func (m *memorySessions) Delete(_ context.Context, id string) (string, error) {
	idToken := m.idTokenByToken[id]
	delete(m.userByToken, id)
	delete(m.idTokenByToken, id)

	return idToken, nil
}

type memoryAudit struct {
	entries []audit.Entry
}

func (m *memoryAudit) Record(_ context.Context, entry audit.Entry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func (m *memoryAudit) actions() []audit.Action {
	actions := []audit.Action{}
	for _, entry := range m.entries {
		actions = append(actions, entry.Action)
	}

	return actions
}

// memoryTokens holds the tokens by secret.
type memoryTokens map[string]token.Token

func (m memoryTokens) Find(_ context.Context, kind, secret string) (token.Token, error) {
	found, ok := m[secret]
	if !ok || found.Kind != kind {
		return token.Token{}, token.ErrNotFound
	}

	return found, nil
}

var testTokens = memoryTokens{
	"gpt_ci": {ID: "t-1", Kind: token.KindService, Name: "ci", Role: access.Maintainer},
	"gpi_ci": {ID: "t-2", Kind: token.KindIngest, Name: "pipelines", Integration: "ci"},
}

// memoryGrants holds each user's grants by user ID.
type memoryGrants map[string][]access.Grant

func (m memoryGrants) ForUser(_ context.Context, userID string) ([]access.Grant, error) {
	return m[userID], nil
}

func newTestRouter(sessions sessionFinder, grants memoryGrants, register func(*httpx.Routes)) *httpxtest.Router {
	router := httpxtest.NewRouter(&container.Container{Logger: logging.NewMockLogger(logging.ERROR)})
	routes := httpx.NewRoutes(router)
	register(routes)
	authenticator := NewAuthenticator(sessions, testTokens, access.NewResolver(grants, testBootstrapAdmins), testCookies)
	router.Use(httpx.ExposeRaw, CrossOriginProtection(), Middleware(routes, authenticator))

	return router
}
