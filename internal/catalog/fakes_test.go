package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/apitoken"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/httpx"
	"github.com/dvjn/goldport/internal/httpx/httpxtest"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/logging"
)

var (
	testCookies = auth.CookieNames("catalog test")

	people = map[string]struct {
		user directory.User
		role access.Role
	}{
		"alice": {directory.User{ID: "u-1", Email: "alice@example.test", Name: "Alice Archer", Active: true}, access.Admin},
		"bob":   {directory.User{ID: "u-2", Email: "bob@example.test", Name: "Bob Baker", Active: true}, access.Maintainer},
		"cara":  {directory.User{ID: "u-3", Email: "cara@example.test", Name: "Cara Cole", Active: true}, access.Member},
	}
	machines = map[string]apitoken.Token{
		"gpt_ci":     {ID: "t-1", Kind: apitoken.KindAPI, Name: "ci", Role: access.Maintainer},
		"gpt_viewer": {ID: "t-2", Kind: apitoken.KindAPI, Name: "viewer", Role: access.Member},
	}
)

type memorySessions struct{}

// Lookup takes the session cookie value as a person's first name.
func (memorySessions) Lookup(_ context.Context, token string) (auth.Principal, error) {
	person, ok := people[token]
	if !ok {
		return auth.Principal{}, auth.ErrNoSession
	}

	return auth.Principal{User: person.user, SessionID: "s-" + token}, nil
}

type memoryTokens struct{}

func (memoryTokens) Find(_ context.Context, _, secret string) (apitoken.Token, error) {
	token, ok := machines[secret]
	if !ok {
		return apitoken.Token{}, apitoken.ErrNotFound
	}

	return token, nil
}

type fixedRoles struct{}

func (fixedRoles) Resolve(_ context.Context, user directory.User) (access.Resolution, error) {
	for _, person := range people {
		if person.user.ID == user.ID {
			return access.Resolution{Role: person.role}, nil
		}
	}

	return access.Resolution{}, nil
}

type memoryAudit struct {
	entries []audit.Entry
}

func (m *memoryAudit) Record(_ context.Context, entry audit.Entry) error {
	m.entries = append(m.entries, entry)
	return nil
}

type memoryDefinitions struct {
	set       definition.Set
	revisions []Revision
	impact    Impact
}

func (m *memoryDefinitions) number() int { return len(m.revisions) }

func (m *memoryDefinitions) Definitions(context.Context) (definition.Set, int, error) {
	return m.set, m.number(), nil
}

func (m *memoryDefinitions) Impact(context.Context, definition.Effects) (Impact, error) {
	return m.impact, nil
}

func (m *memoryDefinitions) Apply(_ context.Context, plan Plan, author audit.Actor, message string) (Revision, error) {
	if plan.Base != m.number() {
		return Revision{}, ErrStale
	}
	revision := Revision{
		Number: m.number() + 1, Author: author, Message: message, RollbackOf: plan.RollbackOf, CreatedAt: at,
		Snapshot: plan.Desired, Changes: plan.Changes, Impact: plan.Impact,
	}
	m.revisions = append(m.revisions, revision)
	m.set = plan.Desired

	return revision, nil
}

func (m *memoryDefinitions) Revisions(_ context.Context, page Page) ([]Revision, int, error) {
	newest := slices.Clone(m.revisions)
	slices.Reverse(newest)

	return newest[min(page.Offset, len(newest)):min(page.Offset+page.Limit, len(newest))], len(newest), nil
}

func (m *memoryDefinitions) Revision(_ context.Context, number int) (Revision, error) {
	if number < 1 || number > m.number() {
		return Revision{}, ErrNotFound
	}

	return m.revisions[number-1], nil
}

type harness struct {
	t           *testing.T
	definitions *memoryDefinitions
	audit       *memoryAudit
	now         time.Time
	router      *httpxtest.Router
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, definitions: &memoryDefinitions{}, audit: &memoryAudit{}, now: at}
	h.router = httpxtest.NewRouter(&container.Container{Logger: logging.NewMockLogger(logging.ERROR)})
	routes := httpx.NewRoutes(h.router)
	NewService(h.definitions, h.audit, func() time.Time { return h.now }).Register(routes)
	authenticator := auth.NewAuthenticator(memorySessions{}, memoryTokens{}, fixedRoles{}, testCookies)
	h.router.Use(httpx.ExposeRaw, auth.CrossOriginProtection(), auth.Middleware(routes, authenticator))

	return h
}

// do sends a request as the person with the first name, or with the bearer
// token when as starts with gpt_.
func (h *harness) do(method, path, as string, body any) *httptest.ResponseRecorder {
	h.t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		h.t.Fatal(err)
	}
	if body == nil {
		encoded = nil
	}
	request := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	if strings.HasPrefix(as, apitoken.Prefix) {
		request.Header.Set("Authorization", "Bearer "+as)
	} else {
		request.AddCookie(&http.Cookie{Name: testCookies.Session, Value: as})
	}
	recorder := httptest.NewRecorder()
	h.router.ServeHTTP(recorder, request)

	return recorder
}

func decode[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %s: %v", response.Body.String(), err)
	}

	return envelope.Data
}

type errorBody struct {
	Error struct {
		Message  string               `json:"message"`
		Problems []definition.Problem `json:"problems"`
		Fields   map[string]string    `json:"fields"`
	} `json:"error"`
}

func decodeError(t *testing.T, response *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", response.Body.String(), err)
	}

	return body
}

func expectStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d: %s", response.Code, status, response.Body.String())
	}
}
