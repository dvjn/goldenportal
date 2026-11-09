//go:build e2e

package connection

import (
	"strings"
	"testing"
)

// publish publishes source as the connection's next version.
func (f *syncFixture) publish(source string) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.t.Context(), `
		WITH v AS (
			INSERT INTO config_versions (document_id, version, source)
			SELECT id, published_version + 1, $2 FROM config_documents WHERE id = $1 RETURNING document_id, version
		)
		UPDATE config_documents d SET published_version = v.version FROM v WHERE d.id = v.document_id`, f.id, source); err != nil {
		f.t.Fatal(err)
	}
}

func (f *syncFixture) syncAsUsual() *Run {
	f.t.Helper()
	run, err := Sync(f.t.Context(), f.db, "gitlab", TriggerSchedule, "")
	if err != nil {
		f.t.Fatal(err)
	}

	return run
}

// TestSchedulePublishKeepsCursors checks publishing only a new schedule
// leaves the next sync incremental and reading from where the last left
// off, while publishing a changed source reads its resource afresh, and a
// changed reconcile source makes the next sync complete.
func TestSchedulePublishKeepsCursors(t *testing.T) {
	f := newSyncFixture(t, pipelineYAML)
	source := connectionYAML(f.gitlab.url())
	if _, err := f.db.ExecContext(t.Context(), `UPDATE config_versions SET source = $1 WHERE document_id = $2`,
		source+"  sync: {interval: 15m, complete: 24h}\n", f.id); err != nil {
		t.Fatal(err)
	}
	first := f.syncAsUsual()
	if first.Mode != ModeComplete {
		t.Fatalf("the first sync was %s, want complete", first.Mode)
	}

	f.publish(strings.Replace(source, "title: GitLab}", "title: Git, description: Code}", 1) + "  sync: {interval: 1h, complete: 24h}\n")
	second := f.syncAsUsual()
	if second.Mode != ModeIncremental {
		t.Errorf("the sync after publishing a new schedule was %s, want incremental", second.Mode)
	}
	if got := f.readFrom("1"); len(got) != 2 || got[1] != cursorText(first) {
		t.Fatalf("after publishing a new schedule, pipelines were read from %v, want from the first sync's cursor", got)
	}

	changed := strings.Replace(pipelineYAML+pipelineSources, "properties: {startedAt: .created_at}", "properties: {startedAt: .started_at}", 1)
	publishVersion(t, f, f.pipelineID, changed)
	if third := f.syncAsUsual(); third.Mode != ModeIncremental {
		t.Errorf("the sync after changing an upsert source was %s, want incremental", third.Mode)
	}
	if got := f.readFrom("1"); len(got) != 3 || got[2] != "" {
		t.Fatalf("after changing the pipelines source, they were read from %v, want in full", got)
	}

	publishVersion(t, f, f.repositoryID, strings.Replace(repositoryYAML+repositorySources,
		"entity: {identifier: .path_with_namespace}", "entity: {identifier: .path_with_namespace, title: .name}", 1))
	if fourth := f.syncAsUsual(); fourth.Mode != ModeComplete {
		t.Errorf("the sync after changing a reconcile source was %s, want complete", fourth.Mode)
	}
}
