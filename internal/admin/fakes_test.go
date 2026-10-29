package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/httpx"
	"github.com/dvjn/goldport/internal/httpx/httpxtest"
	"github.com/dvjn/goldport/internal/token"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/logging"
)

var (
	testCookies = auth.CookieNames("admin test")
	at          = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

	alice = directory.User{ID: "u-1", UserName: "alice", Email: "alice@example.test", Name: "Alice Archer", Active: true}
	bob   = directory.User{ID: "u-2", UserName: "bob", Email: "bob@example.test", Name: "Bob Baker", Active: true}
	cara  = directory.User{ID: "u-3", UserName: "cara", Email: "cara@example.test", Name: "Cara Cole", Active: true}
	dan   = directory.User{ID: "u-4", UserName: "dan", Email: "dan@example.test", Name: "Dan Dale", Active: true}

	platform = directory.Group{ID: "g-1", DisplayName: "platform-team"}
	payments = directory.Group{ID: "g-2", DisplayName: "payments-team"}

	platformAdmin = access.Grant{
		ID: "r-1", Subject: access.Subject{Type: access.SubjectGroup, ID: "g-1"}, SubjectName: "platform-team",
		MemberCount: 1, Role: access.Admin, GrantedByEmail: "alice@example.test", CreatedAt: at, UpdatedAt: at,
	}
	paymentsMaintainer = access.Grant{
		ID: "r-2", Subject: access.Subject{Type: access.SubjectGroup, ID: "g-2"}, SubjectName: "payments-team",
		MemberCount: 1, Role: access.Maintainer, GrantedByEmail: "alice@example.test", CreatedAt: at, UpdatedAt: at,
	}
)

type setCall struct {
	subject access.Subject
	role    access.Role
	by      string
}

type memoryGrants struct {
	grants   []access.Grant
	carrying []string
	setErr   error
	previous access.Role
	sets     []setCall
}

func (m *memoryGrants) List(context.Context) ([]access.Grant, error) { return m.grants, nil }

func (m *memoryGrants) ForUser(_ context.Context, userID string) ([]access.Grant, error) {
	memberships := map[string][]string{"u-1": {"g-1"}, "u-2": {"g-2"}}
	var grants []access.Grant
	for _, grant := range m.grants {
		if grant.Subject.ID == userID || slices.Contains(memberships[userID], grant.Subject.ID) {
			grants = append(grants, grant)
		}
	}

	return grants, nil
}

func (m *memoryGrants) AdminCarrying(context.Context) ([]string, error) { return m.carrying, nil }

func (m *memoryGrants) Set(_ context.Context, subject access.Subject, role access.Role, by string) (access.Role, error) {
	if m.setErr != nil {
		return access.NoRole, m.setErr
	}
	m.sets = append(m.sets, setCall{subject: subject, role: role, by: by})

	return m.previous, nil
}

type memoryDirectory struct{}

func (memoryDirectory) users() []directory.User { return []directory.User{alice, bob, cara, dan} }

func (d memoryDirectory) UserByID(_ context.Context, id string) (directory.User, error) {
	for _, user := range d.users() {
		if user.ID == id {
			return user, nil
		}
	}

	return directory.User{}, directory.ErrNotFound
}

func (memoryDirectory) GroupByID(_ context.Context, id string) (directory.Group, error) {
	for _, group := range []directory.Group{platform, payments} {
		if group.ID == id {
			return group, nil
		}
	}

	return directory.Group{}, directory.ErrNotFound
}

func (d memoryDirectory) ListUsers(_ context.Context, _ directory.UserFilter, page directory.Page) ([]directory.User, int, error) {
	users := d.users()
	return users[:min(page.Limit, len(users))], len(users), nil
}

func (memoryDirectory) ListGroups(_ context.Context, _ directory.GroupFilter, page directory.Page) ([]directory.Group, int, error) {
	groups := []directory.Group{platform, payments}
	return groups[:min(page.Limit, len(groups))], len(groups), nil
}

func (memoryDirectory) GroupsOfUser(_ context.Context, userID string) ([]directory.Group, error) {
	return map[string][]directory.Group{"u-1": {platform}, "u-2": {payments}}[userID], nil
}

func (memoryDirectory) LastSignIn(context.Context, string) (time.Time, error) { return at, nil }

type memoryTokens struct {
	created []token.NewToken
	rotated []token.NewToken
	revoked []string
}

func (m *memoryTokens) Create(_ context.Context, newToken token.NewToken) (token.Token, string, error) {
	m.created = append(m.created, newToken)
	return token.Token{ID: "t-9", Kind: newToken.Kind, Name: newToken.Name, Role: newToken.Role, ExpiresAt: newToken.ExpiresAt}, "gpt_new", nil
}

func (m *memoryTokens) Rotate(_ context.Context, newToken token.NewToken) (token.Token, string, error) {
	m.rotated = append(m.rotated, newToken)
	return token.Token{ID: "t-8", Kind: newToken.Kind, Name: newToken.Name, CreatedAt: at}, "gpt_scim", nil
}

func (m *memoryTokens) List(context.Context, string) ([]token.Token, error) { return nil, nil }

func (m *memoryTokens) Revoke(_ context.Context, _, id string) (token.Token, error) {
	if id != "t-1" {
		return token.Token{}, token.ErrNotFound
	}
	m.revoked = append(m.revoked, id)

	return token.Token{ID: id, Name: "ci"}, nil
}

func (m *memoryTokens) LastUsed(context.Context, string) (time.Time, error) { return time.Time{}, nil }

// Find serves the bearer token of the authenticator: gpt_ci is a Maintainer.
func (m *memoryTokens) Find(_ context.Context, _, secret string) (token.Token, error) {
	if secret != "gpt_ci" {
		return token.Token{}, token.ErrNotFound
	}

	return token.Token{ID: "t-1", Kind: token.KindService, Name: "ci", Role: access.Maintainer}, nil
}

type memorySessions struct {
	revoked []string
}

// Lookup takes the session cookie value as the user ID.
func (m *memorySessions) Lookup(ctx context.Context, token string) (auth.Principal, error) {
	user, err := memoryDirectory{}.UserByID(ctx, token)
	if err != nil {
		return auth.Principal{}, auth.ErrNoSession
	}

	return auth.Principal{User: user, SessionID: "s-" + token}, nil
}

func (m *memorySessions) DeleteForUser(_ context.Context, userID string) error {
	m.revoked = append(m.revoked, userID)
	return nil
}

type memoryAudit struct {
	entries []audit.Entry
	queries []audit.Query
	facets  audit.Facets
}

func (m *memoryAudit) Record(_ context.Context, entry audit.Entry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func (m *memoryAudit) List(_ context.Context, query audit.Query) ([]audit.Entry, int, error) {
	m.queries = append(m.queries, query)
	return m.entries, query.Limit, nil
}

func (m *memoryAudit) Facets(context.Context) (audit.Facets, error) {
	return m.facets, nil
}

type harness struct {
	grants   *memoryGrants
	tokens   *memoryTokens
	sessions *memorySessions
	audit    *memoryAudit
	router   *httpxtest.Router
}

// newHarness serves the admin API with platform-team as the Admin grant
// that carries alice and payments-team as bob's Maintainer grant.
func newHarness() *harness {
	h := &harness{
		grants:   &memoryGrants{grants: []access.Grant{platformAdmin, paymentsMaintainer}, carrying: []string{"r-1"}},
		tokens:   &memoryTokens{},
		sessions: &memorySessions{},
		audit:    &memoryAudit{},
	}
	settings := Settings{
		Name:               "Acme",
		PublicURL:          "https://portal.example.test",
		IssuerURL:          "https://login.example.test/",
		ClientID:           "portal",
		SessionMaxAge:      10 * time.Hour,
		SessionIdleTimeout: 45 * time.Minute,
		BootstrapAdmins:    access.BootstrapAdmins{"cara@example.test"},
	}
	h.router = httpxtest.NewRouter(&container.Container{Logger: logging.NewMockLogger(logging.ERROR)})
	routes := httpx.NewRoutes(h.router)
	NewService(settings, h.grants, memoryDirectory{}, h.tokens, h.sessions, h.audit).Register(routes)
	authenticator := auth.NewAuthenticator(h.sessions, h.tokens, access.NewResolver(h.grants, settings.BootstrapAdmins), testCookies)
	h.router.Use(httpx.ExposeRaw, auth.CrossOriginProtection(), auth.Middleware(routes, authenticator))

	return h
}

// do sends a request as the user with the ID, or with the bearer token when
// as starts with gpt_.
func (h *harness) do(t *testing.T, method, path, as, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if strings.HasPrefix(as, token.Prefix) {
		request.Header.Set("Authorization", "Bearer "+as)
	} else {
		request.AddCookie(&http.Cookie{Name: testCookies.Session, Value: as})
	}
	recorder := httptest.NewRecorder()
	h.router.ServeHTTP(recorder, request)

	return recorder
}
