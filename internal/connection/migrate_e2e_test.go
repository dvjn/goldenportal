//go:build e2e

package connection

import (
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/pgtest"
)

// sourcesMigration is the migration that moves mappings into blueprint
// sources.
const sourcesMigration = 20261110140000

const mappedConnectionYAML = `kind: Connection
metadata: {identifier: gitlab, title: GitLab}
spec:
  connector:
    type: gitlab
    config:
      url: https://gitlab.example.com
      token: {env: GP_SECRET_GITLAB_TOKEN}
      groups: [acme]
  sync: {interval: 15m, complete: 24h}
  mappings:
    - resource: projects
      blueprint: repository
      entity: {identifier: .path_with_namespace}
    - resource: pipelines
      mode: upsert
      blueprint: pipeline
      entity:
        identifier: ._gp.project + "#" + (.id | tostring)
        properties: {startedAt: .created_at}
`

// TestMigrationKeepsCursors checks that moving a connection's mappings
// into its blueprints' sources reads nothing differently: the first sync
// after it is incremental and reads from where the last left off.
func TestMigrationKeepsCursors(t *testing.T) {
	db := pgtest.NewBefore(t, sourcesMigration)
	ctx := t.Context()
	now := time.Now().Truncate(time.Second)
	t0, t1, t2, t3 := now.Add(-10*24*time.Hour), now.Add(-5*24*time.Hour), now.Add(-2*time.Hour), now.Add(-time.Hour)

	repositoryID := pgtest.Publish(t, db, "blueprint", "repository", repositoryYAML)
	pipelineID := pgtest.Publish(t, db, "blueprint", "pipeline", pipelineYAML)
	id := pgtest.Publish(t, db, Name, "gitlab", mappedConnectionYAML)
	for _, step := range []struct {
		query string
		args  []any
	}{
		{`UPDATE config_versions SET published_at = $2 WHERE document_id = ANY($1)`, []any{[]string{repositoryID, pipelineID}, t0}},
		{`UPDATE config_versions SET published_at = $2 WHERE document_id = $1`, []any{id, t1}},
		{`INSERT INTO ingestion_runs (connection_id, trigger, mode, started_at, finished_at, received, created, updated, unchanged, deleted, failed)
			VALUES ($1, 'schedule', 'complete', $2, $2, 0, 0, 0, 0, 0, 0)`, []any{id, t2}},
		{
			`INSERT INTO sync_cursors (connection_id, resource, scope, read_at) VALUES ($1, 'projects', '1', $2), ($1, 'pipelines', '1', $2)`,
			[]any{id, t3},
		},
	} {
		if _, err := db.ExecContext(ctx, step.query, step.args...); err != nil {
			t.Fatal(err)
		}
	}

	pgtest.MigrateRest(t, db, sourcesMigration)

	published, err := blueprint.Published(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for _, bp := range []string{"repository", "pipeline"} {
		if sources := published[bp].Sources; len(sources) != 1 || sources[0].Connection != "gitlab" {
			t.Errorf("%s has sources %+v", bp, sources)
		}
	}
	record, err := configdoc.NewStore(db, Name).Get(ctx, "gitlab")
	if err != nil {
		t.Fatal(err)
	}
	spec, issues := Parse(record.PublishedSource)
	if configdoc.HasErrors(issues) {
		t.Fatalf("gitlab doesn't parse, as if it still had mappings: %+v", issues)
	}
	spec.AddSources(published)

	history, err := NewStore(db).syncHistory(ctx, id, "gitlab")
	if err != nil {
		t.Fatal(err)
	}
	for name, at := range map[string]time.Time{
		"all": history.changes.all, "complete": history.changes.complete,
		"projects": history.changes.since("projects"), "pipelines": history.changes.since("pipelines"),
	} {
		if !at.Before(t2) {
			t.Errorf("read changes say %s changed at %v, after the last complete run at %v", name, at, t2)
		}
	}
	if mode := spec.usualMode(history, now); mode != ModeIncremental {
		t.Errorf("the first sync after the migration is %s, want incremental", mode)
	}

	stored, err := NewStore(db).cursors(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cursors := spec.syncCursors(ModeIncremental, []string{"projects", "pipelines"}, stored, published, nil, history.changes, now)
	for _, resource := range []string{"projects", "pipelines"} {
		if got := cursors[resource].Scopes["1"]; !got.Equal(t3.Add(-cursorOverlap)) {
			t.Errorf("%s reads from %v, want from the stored cursor %v", resource, got, t3)
		}
	}
}
