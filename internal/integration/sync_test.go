package integration

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/catalog"
	"github.com/dvjn/goldport/internal/connector"
	"gofr.dev/pkg/gofr/container"
)

func (h *harness) startSync(due func(time.Time) bool) (Run, func(context.Context), error) {
	set := h.definitions.set
	gitlab, _ := set.Integration("gitlab")

	return h.syncer.Start(context.Background(), syncStart{set: set, integration: gitlab, trigger: TriggerManual, actor: "bob@example.test", due: due})
}

func (h *harness) sync() Run {
	h.t.Helper()
	run, perform, err := h.startSync(always)
	if err != nil {
		h.t.Fatal(err)
	}
	perform(context.Background())

	return h.runs.finished(h.t, run.ID)
}

func groups(paths ...any) connector.Records {
	var records []any
	for _, path := range paths {
		records = append(records, map[string]any{"path": path, "name": strings.ToUpper(path.(string)[:1]) + path.(string)[1:]})
	}

	return connector.Records{"group": records}
}

func TestSyncWritesTheMappedEntitiesAndSweepsTheRest(t *testing.T) {
	h := newHarness(t)
	h.gitlab.records = groups("payments", "platform")

	run := h.sync()

	if len(h.gitlab.requests) != 1 || !reflect.DeepEqual(h.gitlab.requests[0].Kinds, []string{"group"}) ||
		h.gitlab.requests[0].Secrets["token"] != "glpat-test" {
		t.Fatalf("requests = %+v, want one for groups with the token", h.gitlab.requests)
	}
	write := h.writer.writes[0]
	if write.Integration != "gitlab" || !write.Sweep || write.Force || len(write.Groups) != 1 ||
		write.Groups[0].Blueprint != "team" || len(write.Groups[0].Entities) != 2 || write.Groups[0].Entities[1].Title != "Platform" {
		t.Fatalf("write = %+v, want both teams of gitlab with a sweep", write)
	}
	want := Run{
		ID: 1, Integration: "gitlab", Trigger: TriggerManual, Actor: "bob@example.test", Status: StatusSucceeded,
		StartedAt: at, FinishedAt: at, Counts: Counts{Fetched: 2, Upserted: 2},
	}
	if !reflect.DeepEqual(run, want) {
		t.Fatalf("run = %+v, want %+v", run, want)
	}
}

func TestSyncSkipsRecordsThatMakeNoEntity(t *testing.T) {
	h := newHarness(t)
	h.gitlab.records = connector.Records{"group": []any{
		map[string]any{"path": "payments", "name": "Payments"},
		map[string]any{"path": 42},
	}}

	run := h.sync()

	if run.Status != StatusPartial || run.Counts != (Counts{Fetched: 2, Upserted: 1, Skipped: 1}) ||
		len(run.Warnings) != 1 || !strings.HasPrefix(run.Warnings[0], "group record 2: identifier:") {
		t.Fatalf("run = %+v, want a partial run that skipped the second group", run)
	}
}

func TestSyncReportsTheGuardedSweep(t *testing.T) {
	h := newHarness(t)
	h.gitlab.records = groups("payments")
	h.writer.result = catalog.WriteResult{Guarded: []catalog.GuardedSweep{{Blueprint: "team", Kept: 4, Written: 1, Existing: 5}}}

	run := h.sync()

	want := "kept 4 team entities that the source no longer has, because the sync read 1 of the 5 the integration had, " +
		"fewer than half; the next sync deletes them once it reads at least half"
	if run.Status != StatusPartial || !reflect.DeepEqual(run.Warnings, []string{want}) {
		t.Fatalf("run = %+v, want the guard warning", run)
	}
}

func TestSyncFailsWithAReasonPeopleCanActOn(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(h *harness)
		error   string
		fetched bool
	}{
		{
			name:    "missing secret",
			prepare: func(h *harness) { delete(h.env, "GITLAB_TOKEN") },
			error:   "the token secret reads the GITLAB_TOKEN environment variable, which is not set on the server",
		},
		{
			name: "unreachable source",
			prepare: func(h *harness) {
				h.gitlab.err = errors.New("could not reach GitLab at https://gitlab.example.test: no such host")
			},
			error:   "could not reach GitLab at https://gitlab.example.test: no such host",
			fetched: true,
		},
		{
			name: "entity another integration owns",
			prepare: func(h *harness) {
				h.writer.err = catalog.ConflictError{Conflicts: []catalog.Conflict{{Blueprint: "team", Identifier: "payments", Integration: "manual"}}}
			},
			error:   "ownership conflict: payments is owned by the manual integration",
			fetched: true,
		},
		{
			name:    "unexpected failure",
			prepare: func(h *harness) { h.writer.err = errors.New("pq: deadlock detected") },
			error:   unexpectedFailure,
			fetched: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.gitlab.records = groups("payments")
			tc.prepare(h)

			run := h.sync()

			if run.Status != StatusFailed || run.Error != tc.error || (len(h.gitlab.requests) == 1) != tc.fetched {
				t.Fatalf("run = %+v after %d fetches, want it failed with %q", run, len(h.gitlab.requests), tc.error)
			}
			if strings.Contains(run.Error, "glpat") {
				t.Fatalf("run error %q holds the secret", run.Error)
			}
		})
	}
}

func TestSyncRunsOneAtATimePerIntegration(t *testing.T) {
	h := newHarness(t)

	_, perform, err := h.startSync(always)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.startSync(always); !errors.Is(err, errSyncRunning) {
		t.Fatalf("second Start() error = %v, want errSyncRunning", err)
	}
	perform(context.Background())
	if _, _, err := h.startSync(always); err != nil {
		t.Fatalf("Start() after the run error = %v, want none", err)
	}
}

func TestSyncSkipsAnIntegrationAnotherInstanceHolds(t *testing.T) {
	h := newHarness(t)
	h.locker.elsewhere["gitlab"] = true

	if _, _, err := h.startSync(always); !errors.Is(err, errSyncRunning) {
		t.Fatalf("Start() error = %v, want errSyncRunning", err)
	}
	delete(h.locker.elsewhere, "gitlab")
	if _, _, err := h.startSync(always); err != nil {
		t.Fatalf("Start() once released error = %v, want none", err)
	}
}

func TestSyncChecksItIsStillDueOnceItHoldsTheLock(t *testing.T) {
	h := newHarness(t)

	_, _, err := h.startSync(func(time.Time) bool { return false })

	if !errors.Is(err, errNotDue) || len(h.locker.held) != 0 || len(h.runs.runs) != 0 {
		t.Fatalf("Start() error = %v with locks %v and runs %v, want errNotDue and nothing held", err, h.locker.held, h.runs.runs)
	}
	if _, _, err := h.startSync(always); err != nil {
		t.Fatalf("Start() after error = %v, want none", err)
	}
}

func TestPostgresLockerTakesATransactionLock(t *testing.T) {
	c, mocks := container.NewMockContainer(t)
	locker := NewPostgresLocker(c.SQL)
	db := mocks.SQL

	db.ExpectBegin()
	db.ExpectQuery(queryTryLock).WithArgs("gitlab").WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	db.ExpectRollback()
	db.ExpectBegin()
	db.ExpectQuery(queryTryLock).WithArgs("gitlab").WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(false))
	db.ExpectRollback()

	release, locked, err := locker.TryLock(context.Background(), "gitlab")
	if err != nil || !locked {
		t.Fatalf("TryLock() = %t, %v, want the lock", locked, err)
	}
	release()
	if _, locked, err := locker.TryLock(context.Background(), "gitlab"); err != nil || locked {
		t.Fatalf("second TryLock() = %t, %v, want no lock", locked, err)
	}
	if err := db.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func newTestScheduler(h *harness, now *time.Time) *Scheduler {
	scheduler := NewScheduler(h.definitions, h.runs, h.syncer, h.audit, func() time.Time { return *now }, &silentLogger{})
	scheduler.started = at
	scheduler.jitter = func() time.Duration { return 30 * time.Second }

	return scheduler
}

func TestSchedulerNextWaitsAnIntervalAndForItsFirstChance(t *testing.T) {
	h := newHarness(t)
	now := at
	scheduler := newTestScheduler(h, &now)
	gitlab, _ := h.definitions.set.Integration("gitlab")
	ci, _ := h.definitions.set.Integration("ci")

	cases := []struct {
		lastPull time.Time
		want     time.Time
	}{
		{time.Time{}, at.Add(30 * time.Second)},
		{at.Add(-2 * time.Hour), at.Add(30 * time.Second)},
		{at.Add(10 * time.Minute), at.Add(70 * time.Minute)},
	}
	for _, tc := range cases {
		if next, scheduled := scheduler.Next(gitlab, tc.lastPull); !scheduled || !next.Equal(tc.want) {
			t.Errorf("Next(last %s) = %s, %t, want %s", tc.lastPull, next, scheduled, tc.want)
		}
	}
	if _, scheduled := scheduler.Next(ci, time.Time{}); scheduled {
		t.Error("Next(ci) is scheduled, want a push integration never scheduled")
	}
}

func TestSchedulerSyncsTheIntegrationsThatAreDue(t *testing.T) {
	h := newHarness(t)
	h.gitlab.records = groups("payments")
	now := at.Add(10 * time.Second)
	scheduler := newTestScheduler(h, &now)

	scheduler.syncDue(context.Background())
	if len(h.runs.runs) != 0 {
		t.Fatalf("runs = %+v before the first chance, want none", h.runs.runs)
	}

	now = at.Add(time.Minute)
	scheduler.syncDue(context.Background())
	run := h.runs.finished(t, 1)
	if run.Integration != "gitlab" || run.Trigger != TriggerSchedule || run.Actor != audit.Scheduler.Label() || run.Status != StatusSucceeded {
		t.Fatalf("run = %+v, want a scheduled gitlab sync", run)
	}
	wantEntry := audit.Entry{
		Actor:    audit.Scheduler,
		Action:   audit.IntegrationSynced,
		Resource: audit.Resource{Kind: audit.ResourceIntegration, ID: "gitlab", Name: "GitLab"},
		Details:  map[string]any{"run": int64(1), "trigger": TriggerSchedule},
	}
	if !reflect.DeepEqual(h.audit.entries, []audit.Entry{wantEntry}) {
		t.Fatalf("audit = %+v, want %+v", h.audit.entries, wantEntry)
	}

	now = at.Add(30 * time.Minute)
	scheduler.syncDue(context.Background())
	if len(h.runs.runs) != 1 {
		t.Fatalf("runs = %+v within the interval, want one", h.runs.runs)
	}
}

func TestSyncNowStartsARunAndRecordsIt(t *testing.T) {
	h := newHarness(t)
	h.gitlab.records = groups("payments")
	h.gitlab.release = make(chan struct{})

	response := h.do(http.MethodPost, "/api/v1/integrations/gitlab/sync", "bob", nil)

	expectStatus(t, response, http.StatusCreated)
	started := decode[runView](t, response)
	if started.ID != 1 || started.Status != StatusRunning || started.Trigger != TriggerManual || started.Actor != "bob@example.test" {
		t.Fatalf("run = %+v, want a running manual run", started)
	}
	again := h.do(http.MethodPost, "/api/v1/integrations/gitlab/sync", "alice", nil)
	expectStatus(t, again, http.StatusConflict)
	close(h.gitlab.release)
	if run := h.runs.finished(t, 1); run.Status != StatusSucceeded {
		t.Fatalf("run = %+v, want it succeeded", run)
	}
	want := audit.Entry{
		Actor:    audit.Actor{Kind: audit.ActorUser, ID: "u-2", Name: "Bob Baker", Email: "bob@example.test"},
		Action:   audit.IntegrationSynced,
		Resource: audit.Resource{Kind: audit.ResourceIntegration, ID: "gitlab", Name: "GitLab"},
		Details:  map[string]any{"run": int64(1), "trigger": TriggerManual},
	}
	if !reflect.DeepEqual(h.audit.entries, []audit.Entry{want}) {
		t.Fatalf("audit = %+v, want %+v", h.audit.entries, want)
	}
}

func TestSyncNowRefusesWhatItCannotSync(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		as     string
		status int
	}{
		{"member", "/api/v1/integrations/gitlab/sync", "cara", http.StatusForbidden},
		{"push integration", "/api/v1/integrations/ci/sync", "bob", http.StatusConflict},
		{"unknown integration", "/api/v1/integrations/jira/sync", "bob", http.StatusNotFound},
		{"manual integration", "/api/v1/integrations/manual/sync", "bob", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)

			expectStatus(t, h.do(http.MethodPost, tc.path, tc.as, nil), tc.status)
			if len(h.runs.runs) != 0 || len(h.audit.entries) != 0 {
				t.Fatalf("runs = %v, audit = %v, want none", h.runs.runs, h.audit.entries)
			}
		})
	}
}
