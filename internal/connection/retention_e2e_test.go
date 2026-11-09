//go:build e2e

package connection

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/pgtest"
)

const (
	repositoryYAML = "kind: Blueprint\nmetadata: {identifier: repository, title: Repository}\nspec:\n  properties: {}\n"
	pipelineYAML   = "kind: Blueprint\nmetadata: {identifier: pipeline, title: Pipeline}\nspec:\n  properties:\n" +
		"    startedAt: {type: string, format: date-time}\n"
	retainedPipelineYAML = pipelineYAML + "  retention: {by: startedAt, keep: 30d}\n"
)

// repositorySources and pipelineSources read the fixture's projects as
// repositories, reconciled, and their pipelines, upserted.
const (
	repositorySources = `  sources:
    - connection: gitlab
      resource: projects
      entity: {identifier: .path_with_namespace}
`
	pipelineSources = `  sources:
    - connection: gitlab
      resource: pipelines
      mode: upsert
      entity:
        identifier: ._gp.project + "#" + (.id | tostring)
        properties: {startedAt: .created_at}
`
)

// connectionYAML is a connection to the fixture's instance; its blueprints'
// sources say what it reads.
func connectionYAML(instance string) string {
	return `kind: Connection
metadata: {identifier: gitlab, title: GitLab}
spec:
  connector:
    type: gitlab
    config:
      url: ` + instance + `
      token: {env: GP_SECRET_GITLAB_TOKEN}
      groups: [acme]
      projects: [octo/tools]
`
}

// syncFixture is the GitLab fixture, read by a published connection into
// a real database.
type syncFixture struct {
	t                        *testing.T
	db                       *gofrSQL.DB
	gitlab                   *gitlabFixture
	id                       string
	repositoryID, pipelineID string
}

func newSyncFixture(t *testing.T, pipelineBlueprint string) *syncFixture {
	t.Helper()
	db := pgtest.New(t)
	f := &syncFixture{t: t, db: db, gitlab: newGitLabFixture(t)}
	f.repositoryID = pgtest.Publish(t, db, "blueprint", "repository", repositoryYAML+repositorySources)
	f.pipelineID = pgtest.Publish(t, db, "blueprint", "pipeline", pipelineBlueprint+pipelineSources)
	f.id = pgtest.Publish(t, db, Name, "gitlab", connectionYAML(f.gitlab.url()))

	return f
}

func (f *syncFixture) sync() *Run {
	f.t.Helper()
	run, err := Sync(f.t.Context(), f.db, "gitlab", TriggerManual, ModeComplete)
	if err != nil {
		f.t.Fatal(err)
	}

	return run
}

func (f *syncFixture) count(blueprintID string) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.t.Context(), `SELECT count(*) FROM entities WHERE blueprint_id = $1`, blueprintID).Scan(&n); err != nil {
		f.t.Fatal(err)
	}

	return n
}

func (f *syncFixture) has(blueprintID, identifier string) bool {
	f.t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.t.Context(), `SELECT count(*) FROM entities WHERE blueprint_id = $1 AND identifier = $2`,
		blueprintID, identifier).Scan(&n); err != nil {
		f.t.Fatal(err)
	}

	return n == 1
}

// stray stores an entity the connection wrote from project 1 that GitLab
// no longer has.
func (f *syncFixture) stray(blueprintID, identifier string) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.t.Context(), `
		INSERT INTO entities (blueprint_id, identifier, title, connection_id, scope, created_at)
		VALUES ($1, $2, $2, $3, '1', now() - interval '1 hour')`, blueprintID, identifier, f.id); err != nil {
		f.t.Fatal(err)
	}
}

// readFrom is the updated_after each request for a project's pipelines
// asked with, "" for none.
func (f *syncFixture) readFrom(project string) []string {
	var out []string
	for _, query := range f.gitlab.sent(http.MethodGet, "/projects/"+project+"/pipelines") {
		q, _ := url.ParseQuery(query)
		if q.Get("source") == "" && q.Get("ref") == "" && q.Get("page") == "" {
			out = append(out, q.Get("updated_after"))
		}
	}

	return out
}

func cursorText(run *Run) string {
	return run.StartedAt.Add(-cursorOverlap).UTC().Format(time.RFC3339)
}

// TestUpsertNeverDeletes checks complete syncs delete what a reconcile
// source no longer reads, and never what an upsert source wrote, which
// they read only what changed of after the first.
func TestUpsertNeverDeletes(t *testing.T) {
	f := newSyncFixture(t, pipelineYAML)
	f.stray(f.repositoryID, "acme/gone")
	f.stray(f.pipelineID, "acme/api#99")

	first := f.sync()
	if got := f.readFrom("1"); len(got) != 1 || got[0] != "" {
		t.Fatalf("the first sync read pipelines from %v, want in full", got)
	}
	if len(first.ReadSince) != 0 {
		t.Errorf("the first sync says it read since %v, want in full", first.ReadSince)
	}
	second := f.sync()
	if got := f.readFrom("1"); len(got) != 2 || got[1] != cursorText(first) {
		t.Fatalf("pipelines were read from %v, want from a minute before the first sync started", got)
	}
	if since, ok := second.ReadSince[gitlabPipelines]; !ok || since.UTC().Format(time.RFC3339) != cursorText(first) {
		t.Errorf("the second sync says it read since %v", second.ReadSince)
	}
	f.sync()

	if f.has(f.repositoryID, "acme/gone") {
		t.Error("a repository GitLab no longer has outlived two complete syncs")
	}
	if !f.has(f.pipelineID, "acme/api#99") || f.count(f.pipelineID) != 13 {
		t.Errorf("pipelines = %d, has the stray one %v; want an upsert source to delete none", f.count(f.pipelineID), f.has(f.pipelineID, "acme/api#99"))
	}
	if second.Deleted != 1 {
		t.Errorf("the second sync deleted %d, want the repository it missed twice alone", second.Deleted)
	}
}

// TestFailedScopeKeepsItsCursor checks a project whose pipelines a sync
// couldn't read is read again from where the sync before left off, while
// the others carry on from the sync that read them.
func TestFailedScopeKeepsItsCursor(t *testing.T) {
	f := newSyncFixture(t, pipelineYAML)
	first := f.sync()
	f.gitlab.deny("/projects/2/pipelines")
	second := f.sync()
	if second.Error == "" {
		t.Fatal("the second sync didn't record project 2's failure")
	}
	f.gitlab.allow()
	f.sync()

	for project, want := range map[string]string{"1": cursorText(second), "2": cursorText(first), "3": cursorText(second)} {
		got := f.readFrom(project)
		if last := got[len(got)-1]; last != want {
			t.Errorf("the third sync read project %s's pipelines from %s, want %s", project, last, want)
		}
	}
}

// TestRetentionDeletesByItsProperty checks publishing retention fills each
// row's time from the property it names, the retention run deletes the
// rows that's older than kept, keeps those without it, and a write older
// than kept stores nothing.
func TestRetentionDeletesByItsProperty(t *testing.T) {
	f := newSyncFixture(t, pipelineYAML)
	now := time.Now()
	started := func(identifier string, ago time.Duration) Entry {
		e := entry("pipeline", identifier, gitlabPipelines, "1")
		if ago > 0 {
			e.Entity.Properties["startedAt"] = now.Add(-ago).UTC().Format(time.RFC3339)
		}
		return e
	}
	day := 24 * time.Hour
	run := &Run{Errors: []RunError{}}
	if err := writeBatched(t.Context(), f.db, f.id, []Entry{started("old", 40*day), started("new", day), started("unstarted", 0)}, run); err != nil {
		t.Fatal(err)
	}

	previous, _ := blueprint.Parse(pipelineYAML)
	next, _ := blueprint.Parse(retainedPipelineYAML)
	if _, err := f.db.ExecContext(t.Context(), `UPDATE config_versions SET source = $1 WHERE document_id = $2`, retainedPipelineYAML+pipelineSources, f.pipelineID); err != nil {
		t.Fatal(err)
	}
	if err := blueprint.Kind.Published(t.Context(), f.db, &previous, next); err != nil {
		t.Fatal(err)
	}
	var filled int
	if err := f.db.QueryRowContext(t.Context(), `SELECT count(occurred_at) FROM entities WHERE blueprint_id = $1`, f.pipelineID).Scan(&filled); err != nil || filled != 2 {
		t.Fatalf("filled %d, %v; want the two rows with startedAt", filled, err)
	}

	if deleted, err := entity.ExpireAll(t.Context(), f.db); err != nil || deleted != 1 {
		t.Fatalf("deleted %d, %v", deleted, err)
	}
	if f.has(f.pipelineID, "old") || !f.has(f.pipelineID, "new") || !f.has(f.pipelineID, "unstarted") {
		t.Error("want only the row started 40 days ago deleted")
	}

	run = &Run{Errors: []RunError{}}
	if err := writeBatched(t.Context(), f.db, f.id, []Entry{started("old", 40*day)}, run); err != nil {
		t.Fatal(err)
	}
	if f.has(f.pipelineID, "old") || run.expired != 1 {
		t.Errorf("a row older than kept was written, expired %d", run.expired)
	}

	var at sql.NullTime
	if err := f.db.QueryRowContext(t.Context(), `SELECT occurred_at FROM entities WHERE blueprint_id = $1 AND identifier = 'new'`,
		f.pipelineID).Scan(&at); err != nil || !at.Valid {
		t.Fatalf("new is kept by %v, %v", at, err)
	}
	if err := blueprint.Kind.Published(t.Context(), f.db, &next, previous); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRowContext(t.Context(), `SELECT count(occurred_at) FROM entities WHERE blueprint_id = $1`, f.pipelineID).Scan(&filled); err != nil || filled != 0 {
		t.Fatalf("dropping retention left %d rows with a time, %v", filled, err)
	}
}

// TestKeepingLongerReadsAgain checks a blueprint published to keep rows
// longer has the next sync read its upsert resources as far back as it now
// keeps them, rather than from where the last sync left off, and the sync
// after that from its cursor again.
func TestKeepingLongerReadsAgain(t *testing.T) {
	f := newSyncFixture(t, retainedPipelineYAML)
	first := f.sync()
	f.sync()
	if got := f.readFrom("1"); got[1] != cursorText(first) {
		t.Fatalf("the second sync read pipelines from %v, want from the first's cursor", got)
	}
	if f.has(f.pipelineID, "acme/api#11") {
		t.Fatal("a pipeline from 60 days ago was read while pipelines are kept 30d")
	}

	if _, err := f.db.ExecContext(t.Context(), `
		WITH v AS (INSERT INTO config_versions (document_id, version, source) VALUES ($1, 2, $2) RETURNING document_id)
		UPDATE config_documents SET published_version = 2 FROM v WHERE id = v.document_id`,
		f.pipelineID, strings.Replace(retainedPipelineYAML, "keep: 30d", "keep: 90d", 1)+pipelineSources); err != nil {
		t.Fatal(err)
	}
	third := f.sync()
	got := f.readFrom("1")
	from, err := time.Parse(time.RFC3339, got[2])
	if err != nil || time.Since(from) < 89*24*time.Hour {
		t.Fatalf("after keeping pipelines 90d, they were read from %q, want as far back as that", got[2])
	}
	if !f.has(f.pipelineID, "acme/api#11") {
		t.Error("the pipeline from 60 days ago wasn't read once pipelines are kept 90d")
	}
	f.sync()
	if got := f.readFrom("1"); got[3] != cursorText(third) {
		t.Errorf("the sync after read pipelines from %s, want from the third's cursor", got[3])
	}
}
