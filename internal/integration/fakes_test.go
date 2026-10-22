package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/apitoken"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/catalog"
	"github.com/dvjn/goldport/internal/connector"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/httpx"
	"github.com/dvjn/goldport/internal/httpx/httpxtest"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/logging"
)

var (
	at          = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	testCookies = auth.CookieNames("integration test")

	people = map[string]struct {
		user directory.User
		role access.Role
	}{
		"alice": {directory.User{ID: "u-1", Email: "alice@example.test", Name: "Alice Archer", Active: true}, access.Admin},
		"bob":   {directory.User{ID: "u-2", Email: "bob@example.test", Name: "Bob Baker", Active: true}, access.Maintainer},
		"cara":  {directory.User{ID: "u-3", Email: "cara@example.test", Name: "Cara Cole", Active: true}, access.Member},
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

type fixedRoles struct{}

func (fixedRoles) Resolve(_ context.Context, user directory.User) (access.Resolution, error) {
	for _, person := range people {
		if person.user.ID == user.ID {
			return access.Resolution{Role: person.role}, nil
		}
	}

	return access.Resolution{}, nil
}

// memoryTokens holds tokens by secret.
type memoryTokens map[string]apitoken.Token

func (m memoryTokens) Find(_ context.Context, kind, secret string) (apitoken.Token, error) {
	token, ok := m[secret]
	if !ok || token.Kind != kind {
		return apitoken.Token{}, apitoken.ErrNotFound
	}

	return token, nil
}

func (m memoryTokens) Create(_ context.Context, token apitoken.NewToken) (apitoken.Token, string, error) {
	created := apitoken.Token{
		ID: fmt.Sprintf("t-%d", len(m)+1), Kind: token.Kind, Name: token.Name, Integration: token.Integration,
		CreatedBy: token.CreatedBy, CreatedByEmail: "bob@example.test", CreatedAt: at, ExpiresAt: token.ExpiresAt,
	}
	secret := apitoken.IngestPrefix + created.ID
	m[secret] = created

	return created, secret, nil
}

func (m memoryTokens) ListIngest(_ context.Context, integration string) ([]apitoken.Token, error) {
	var tokens []apitoken.Token
	for _, secret := range slices.Sorted(maps.Keys(m)) {
		if token := m[secret]; token.Kind == apitoken.KindIngest && token.Integration == integration {
			tokens = append(tokens, token)
		}
	}

	return tokens, nil
}

func (m memoryTokens) RevokeIngest(_ context.Context, integration, id string) (apitoken.Token, error) {
	for secret, token := range m {
		if token.Kind == apitoken.KindIngest && token.Integration == integration && token.ID == id {
			delete(m, secret)
			return token, nil
		}
	}

	return apitoken.Token{}, apitoken.ErrNotFound
}

type memoryDefinitions struct {
	set definition.Set
}

func (m *memoryDefinitions) Definitions(context.Context) (definition.Set, int, error) {
	return m.set, 1, nil
}

// recordingWriter keeps the writes it receives and answers with result or
// err.
type recordingWriter struct {
	writes  []catalog.OwnedWrite
	deletes []string
	owned   map[string]bool
	result  catalog.WriteResult
	err     error
}

func (r *recordingWriter) WriteOwned(_ context.Context, write catalog.OwnedWrite) (catalog.WriteResult, error) {
	r.writes = append(r.writes, write)
	if r.err != nil {
		return catalog.WriteResult{}, r.err
	}
	result := r.result
	for _, group := range write.Groups {
		result.Upserted += len(group.Entities)
	}

	return result, nil
}

func (r *recordingWriter) DeleteOwned(_ context.Context, integration, blueprint, identifier string) (bool, error) {
	name := integration + "/" + blueprint + "/" + identifier
	r.deletes = append(r.deletes, name)

	return r.owned[name], nil
}

// memoryRuns guards its runs, since a sync records its end from its own
// goroutine.
type memoryRuns struct {
	mu   sync.Mutex
	runs []Run
}

func (m *memoryRuns) Start(_ context.Context, run Run) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run.ID = int64(len(m.runs) + 1)
	run.Status = StatusRunning
	m.runs = append(m.runs, run)

	return run, nil
}

func (m *memoryRuns) Finish(_ context.Context, run Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[run.ID-1] = run

	return nil
}

func (m *memoryRuns) Record(_ context.Context, run Run) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run.ID = int64(len(m.runs) + 1)
	m.runs = append(m.runs, run)

	return run, nil
}

// finished waits for the run to end and returns it.
func (m *memoryRuns) finished(t *testing.T, id int64) Run {
	t.Helper()
	for range 200 {
		m.mu.Lock()
		run := m.runs[id-1]
		m.mu.Unlock()
		if run.Status != StatusRunning {
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %d is still running", id)

	return Run{}
}

func (m *memoryRuns) List(_ context.Context, integration string, limit int) ([]Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var runs []Run
	for _, run := range slices.Backward(m.runs) {
		if run.Integration == integration && len(runs) < limit {
			runs = append(runs, run)
		}
	}

	return runs, nil
}

func (m *memoryRuns) Latest(context.Context) (map[string]Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	latest := map[string]Run{}
	for _, run := range m.runs {
		latest[run.Integration] = run
	}

	return latest, nil
}

func (m *memoryRuns) LastPull(_ context.Context, integration string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var last time.Time
	for _, run := range m.runs {
		if run.Integration == integration && run.Trigger != TriggerPush {
			last = run.StartedAt
		}
	}

	return last, nil
}

type memoryAudit struct {
	entries []audit.Entry
}

func (m *memoryAudit) Record(_ context.Context, entry audit.Entry) error {
	m.entries = append(m.entries, entry)
	return nil
}

// fakeConnector answers every fetch with records or err, and counts the
// fetches. Release, when set, holds each fetch until it is closed.
type fakeConnector struct {
	records  connector.Records
	err      error
	requests []connector.Request
	release  chan struct{}
}

func (f *fakeConnector) Kinds() []string { return definition.ConnectorGitLab.Kinds() }

func (f *fakeConnector) Fetch(_ context.Context, request connector.Request) (connector.Records, error) {
	f.requests = append(f.requests, request)
	if f.release != nil {
		<-f.release
	}

	return f.records, f.err
}

// memoryLocker holds the integrations that another instance has locked.
type memoryLocker struct {
	mu        sync.Mutex
	elsewhere map[string]bool
	held      map[string]bool
}

func newMemoryLocker() *memoryLocker {
	return &memoryLocker{elsewhere: map[string]bool{}, held: map[string]bool{}}
}

func (m *memoryLocker) TryLock(_ context.Context, integration string) (func(), bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.elsewhere[integration] || m.held[integration] {
		return nil, false, nil
	}
	m.held[integration] = true

	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.held, integration)
	}, true, nil
}

type silentLogger struct {
	errors []string
}

func (l *silentLogger) Errorf(format string, args ...any) {
	l.errors = append(l.errors, fmt.Sprintf(format, args...))
}

type harness struct {
	t           *testing.T
	definitions *memoryDefinitions
	writer      *recordingWriter
	runs        *memoryRuns
	tokens      memoryTokens
	audit       *memoryAudit
	gitlab      *fakeConnector
	locker      *memoryLocker
	env         map[string]string
	counts      fixedCounts
	syncer      *Syncer
	scheduler   *Scheduler
	router      *httpxtest.Router
}

type fixedCounts []catalog.OwnedCount

func (f *fixedCounts) OwnedCounts(context.Context) ([]catalog.OwnedCount, error) {
	return *f, nil
}

func newHarness(t *testing.T) *harness {
	h := &harness{
		t:           t,
		definitions: &memoryDefinitions{set: acmeSet(t)},
		writer:      &recordingWriter{owned: map[string]bool{}},
		runs:        &memoryRuns{},
		tokens: memoryTokens{
			"gpi_ci":     {ID: "t-1", Kind: apitoken.KindIngest, Name: "pipelines", Integration: "ci"},
			"gpi_gitlab": {ID: "t-2", Kind: apitoken.KindIngest, Name: "stale", Integration: "gitlab"},
			"gpt_ci":     {ID: "t-3", Kind: apitoken.KindAPI, Name: "ci", Role: access.Maintainer},
		},
		audit:  &memoryAudit{},
		gitlab: &fakeConnector{records: connector.Records{}},
		locker: newMemoryLocker(),
		env:    map[string]string{"GITLAB_TOKEN": "glpat-test"},
	}
	h.syncer = NewSyncer(SyncerConfig{
		Entities:   h.writer,
		Runs:       h.runs,
		Connectors: map[definition.Connector]connector.Connector{definition.ConnectorGitLab: h.gitlab},
		Locker:     h.locker,
		Getenv:     func(name string) string { return h.env[name] },
		Now:        func() time.Time { return at },
		Logger:     &silentLogger{},
	})
	h.scheduler = NewScheduler(h.definitions, h.runs, h.syncer, func() time.Time { return at }, &silentLogger{})
	h.scheduler.jitter = func() time.Duration { return 30 * time.Second }
	h.router = httpxtest.NewRouter(&container.Container{Logger: logging.NewMockLogger(logging.FATAL)})
	routes := httpx.NewRoutes(h.router)
	NewService(ServiceConfig{
		Definitions: h.definitions,
		Entities:    h.writer,
		Counts:      &h.counts,
		Runs:        h.runs,
		Syncer:      h.syncer,
		Scheduler:   h.scheduler,
		Tokens:      h.tokens,
		Audit:       h.audit,
		PublicURL:   "https://portal.example.test/",
		Now:         func() time.Time { return at },
	}).Register(routes)
	authenticator := auth.NewAuthenticator(memorySessions{}, h.tokens, fixedRoles{}, testCookies)
	h.router.Use(httpx.ExposeRaw, auth.CrossOriginProtection(), auth.Middleware(routes, authenticator))

	return h
}

// do sends a request as the person with the first name, or with the bearer
// token when as starts with a token prefix. A string body goes as it is.
func (h *harness) do(method, path, as string, body any) *httptest.ResponseRecorder {
	h.t.Helper()
	text, isText := body.(string)
	if !isText && body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		text = string(encoded)
	}
	request := httptest.NewRequest(method, path, strings.NewReader(text))
	request.Header.Set("Content-Type", "application/json")
	if strings.HasPrefix(as, apitoken.Prefix) || strings.HasPrefix(as, apitoken.IngestPrefix) {
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
		Message   string             `json:"message"`
		Problems  []Problem          `json:"problems"`
		Conflicts []catalog.Conflict `json:"conflicts"`
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
