//go:build e2e

package integration

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/pgtest"
)

// The blueprints CI runs are written to: job attempts, related to their
// pipeline, repository and runner, and runners, each kept a month.
const (
	jobAttemptYAML = `kind: Blueprint
metadata: {identifier: job-attempt, title: Job Attempt}
spec:
  properties:
    pipeline: {relation: pipeline, required: true}
    repo: {relation: repository, required: true}
    runner: {relation: runner}
    attempt: {type: number}
    retried: {type: boolean}
    triggeredAt: {type: string, format: date-time}
  retention: {by: triggeredAt, keep: 30d}
`
	runnerYAML = `kind: Blueprint
metadata: {identifier: runner, title: Runner}
spec:
  properties:
    version: {type: string}
    lastJobAt: {type: string, format: date-time}
  retention: {by: lastJobAt, keep: 30d}
`
)

// jobsYAML reads the fixture's pipelines, their jobs' attempts, and the
// runners those ran on.
func jobsYAML(instance string) string {
	return `kind: Integration
metadata: {identifier: gitlab, title: GitLab}
spec:
  connector:
    type: gitlab
    config:
      url: ` + instance + `
      token: {env: GP_SECRET_GITLAB_TOKEN}
      groups: [acme]
      projects: [octo/tools]
  sync: {interval: 1h}
  mappings:
    - resource: projects
      blueprint: repository
      entity: {identifier: .path_with_namespace}
    - resource: pipelines
      blueprint: pipeline
      mode: upsert
      entity:
        identifier: ._gp.project + "#" + (.id | tostring)
        properties: {startedAt: .created_at}
    - resource: job_attempts
      blueprint: job-attempt
      mode: upsert
      entity:
        identifier: ._gp.project + "#" + (.id | tostring)
        properties:
          pipeline: ._gp.project + "#" + (.pipeline.id | tostring)
          repo: ._gp.project
          runner: .runner.id | values | tostring
          attempt: ._gp.attempt
          retried: ._gp.retried
          triggeredAt: .pipeline.created_at
    - resource: runners
      blueprint: runner
      mode: upsert
      entity:
        identifier: .id
        title: ._gp.title
        properties:
          version: .version
          lastJobAt: ._gp.lastJobAt
`
}

func newJobsFixture(t *testing.T) (*syncFixture, map[string]string) {
	t.Helper()
	db := pgtest.New(t)
	f := &syncFixture{t: t, db: db, gitlab: newGitLabFixture(t)}
	ids := map[string]string{
		"repository": pgtest.Publish(t, db, "blueprint", "repository", repositoryYAML),
		"pipeline":   pgtest.Publish(t, db, "blueprint", "pipeline", retainedPipelineYAML),
		"runner":     pgtest.Publish(t, db, "blueprint", "runner", runnerYAML),
	}
	ids["job-attempt"] = pgtest.Publish(t, db, "blueprint", "job-attempt", jobAttemptYAML)
	f.id = pgtest.Publish(t, db, Name, "gitlab", jobsYAML(f.gitlab.url()))

	return f, ids
}

// TestSyncWritesJobAttempts checks a sync writes every attempt of the
// jobs of the pipelines it reads, related to their pipeline, repository
// and runner, and the runners they ran on, and the next sync reads only the
// pipelines changed since.
func TestSyncWritesJobAttempts(t *testing.T) {
	f, ids := newJobsFixture(t)
	first := f.sync()
	if first.Error != "" || first.Failed != 0 {
		t.Fatalf("sync failed: %s %+v", first.Error, first.Errors)
	}
	// Pipeline 11 is older than attempts are kept, so it isn't read.
	if n := f.count(ids["job-attempt"]); n != 3*5 {
		t.Errorf("job attempts = %d, want 5 of each of the 3 projects", n)
	}
	var relations, attempt string
	if err := f.db.QueryRowContext(t.Context(), `SELECT relations::text, properties->>'attempt' || ' ' || (properties->>'retried')
		FROM entities WHERE blueprint_id = $1 AND identifier = 'acme/api#131'`, ids["job-attempt"]).Scan(&relations, &attempt); err != nil {
		t.Fatal(err)
	}
	if relations != `{"repo": "acme/api", "runner": "1", "pipeline": "acme/api#13"}` || attempt != "1 true" {
		t.Errorf("attempt 131 relates to %s, numbered %s; want its pipeline, repository and runner, first and retried", relations, attempt)
	}
	// Six attempts, two in each project, ran on runner 1, read once.
	var version, title string
	if err := f.db.QueryRowContext(t.Context(), `SELECT properties->>'version', title FROM entities WHERE blueprint_id = $1 AND identifier = '1'`,
		ids["runner"]).Scan(&version, &title); err != nil || version != "18.6.0" || title != "runner 1" || f.count(ids["runner"]) != 1 {
		t.Errorf("runners = %d, runner 1 is %q with version %q, %v; want it as read on its own", f.count(ids["runner"]), title, version, err)
	}
	if n := len(f.gitlab.sent(http.MethodGet, "/runners/1")); n != 1 {
		t.Errorf("runner 1 was read %d times, want once", n)
	}

	jobPages := func() int {
		n := 0
		for _, r := range f.gitlab.asked() {
			if strings.Contains(r, "/jobs?") {
				n++
			}
		}
		return n
	}
	read := jobPages()
	second := f.sync()
	if second.Error != "" || second.Created != 0 || second.Updated != 0 {
		t.Errorf("the second sync created %d and updated %d, %s; want nothing new", second.Created, second.Updated, second.Error)
	}
	if got := f.readFrom("1"); got[len(got)-1] != cursorText(first) {
		t.Errorf("the second sync read pipelines from %v, want from the first's cursor", got)
	}
	if jobPages() != read {
		t.Errorf("the second sync read %d pages of jobs, want none, as no pipeline changed since the first", jobPages()-read)
	}
}

// TestSyncFailsRunnersAlone checks a runner GitLab fails to answer about
// fails runners alone, whose cursors stay where they were, so the next
// sync reads the attempts on it again, and writes it then, while a runner
// GitLab refuses is written as its newest attempt found it.
func TestSyncFailsRunnersAlone(t *testing.T) {
	f, ids := newJobsFixture(t)
	shortenGitLabWaits(t)
	for range 5 {
		f.gitlab.answer("/runners/1", gitlabServerError)
	}
	first := f.sync()
	if !strings.Contains(first.Error, "runners") || f.count(ids["runner"]) != 0 || f.count(ids["job-attempt"]) != 15 {
		t.Fatalf("error %q, runners %d, attempts %d; want runners failed alone", first.Error, f.count(ids["runner"]), f.count(ids["job-attempt"]))
	}
	cursors := func(resource string) int {
		var n int
		if err := f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM sync_cursors WHERE integration_id = $1 AND resource = $2`,
			f.id, resource).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if cursors("runners") != 0 || cursors("job_attempts") != 3 {
		t.Errorf("cursors: %d of runners, %d of job attempts; want none of runners and each project's of attempts", cursors("runners"), cursors("job_attempts"))
	}

	f.gitlab.deny("/runners/1")
	second := f.sync()
	var version string
	if err := f.db.QueryRowContext(t.Context(), `SELECT properties->>'version' FROM entities WHERE blueprint_id = $1 AND identifier = '1'`,
		ids["runner"]).Scan(&version); err != nil || second.Error != "" || version != "18.5.0" || cursors("runners") != 3 {
		t.Errorf("error %q, runner 1 version %q, %v, %d cursors of runners; want it as its newest attempt found it", second.Error, version, err, cursors("runners"))
	}
}

// TestManyItemsWriteOneEntity checks items about one entity write it in
// turn: the last of a batch, then a later batch answered no earlier, but
// never one the system answered before what's stored.
func TestManyItemsWriteOneEntity(t *testing.T) {
	f, ids := newJobsFixture(t)
	// Systems date their answers to the second.
	at := time.Now().UTC().Truncate(time.Second)
	runner := func(version string, sourceAt time.Time) Entry {
		e := entry("runner", "7", gitlabJobAttempts, "1")
		e.Entity.Properties = map[string]any{"version": version, "lastJobAt": at.Format(time.RFC3339)}
		e.SourceAt = sourceAt
		return e
	}
	version := func() string {
		var v string
		if err := f.db.QueryRowContext(t.Context(), `SELECT properties->>'version' FROM entities WHERE blueprint_id = $1 AND identifier = '7'`,
			ids["runner"]).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	write := func(entries ...Entry) *Run {
		run := &Run{Errors: []RunError{}}
		if err := writeBatched(t.Context(), f.db, f.id, entries, run); err != nil {
			t.Fatal(err)
		}
		return run
	}

	if run := write(runner("1", at), runner("2", at), runner("3", at)); version() != "3" || run.Created != 1 || run.Unchanged != 2 {
		t.Errorf("version %s, run %+v; want the batch's last written once", version(), run)
	}
	if write(runner("4", at)); version() != "4" {
		t.Errorf("version %s, want a later batch answered as early written", version())
	}
	if write(runner("5", at.Add(-time.Minute))); version() != "4" {
		t.Errorf("version %s, want one answered before what's stored left out", version())
	}
}

// TestSyncStopsAtItsDeadline checks a sync that runs out of time stops,
// says so, keeps what it read, and leaves the cursors of what it didn't
// read where they were, so the next sync reads it.
func TestSyncStopsAtItsDeadline(t *testing.T) {
	f, ids := newJobsFixture(t)
	first := f.sync()
	f.gitlab.answer("/projects/2/pipelines", func(w http.ResponseWriter) {
		time.Sleep(time.Second)
		gitlabServerError(w)
	})

	s, err := startSync(t.Context(), f.db, "gitlab", Run{Trigger: TriggerManual, Mode: ModeIncremental}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.timeout = 300 * time.Millisecond
	run, err := s.finish(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.Warning, "The sync stopped reading after 1s, ahead of the next") || !strings.Contains(run.Error, "acme/platform/web") {
		t.Errorf("warning %q, error %q; want the deadline and the project it didn't read", run.Warning, run.Error)
	}
	cursor := func(scope string) time.Time {
		var at sql.NullTime
		if err := f.db.QueryRowContext(t.Context(), `SELECT read_at FROM sync_cursors WHERE integration_id = $1 AND resource = 'job_attempts' AND scope = $2`,
			f.id, scope).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at.Time
	}
	if !cursor("2").Equal(first.StartedAt) || !cursor("1").Equal(run.StartedAt) {
		t.Errorf("cursors are %s for project 2 and %s for project 1, want the first sync's and this one's", cursor("2"), cursor("1"))
	}
	if f.count(ids["job-attempt"]) != 15 {
		t.Errorf("job attempts = %d, want those read kept", f.count(ids["job-attempt"]))
	}
}
