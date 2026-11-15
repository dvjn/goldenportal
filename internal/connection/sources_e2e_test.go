//go:build e2e

package connection

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/httpx/httpxtest"
	"github.com/dvjn/goldenportal/internal/pgtest"
)

const (
	sourcedRepositoryYAML = repositoryYAML + repositorySources
	sourcedPipelineYAML   = pipelineYAML + pipelineSources
)

// newSourcesFixture is the GitLab fixture read by a connection through the
// sources of its blueprints.
func newSourcesFixture(t *testing.T) *syncFixture {
	t.Helper()
	db := pgtest.New(t)
	f := &syncFixture{t: t, db: db, gitlab: newGitLabFixture(t)}
	f.repositoryID = pgtest.Publish(t, db, "blueprint", "repository", sourcedRepositoryYAML)
	f.pipelineID = pgtest.Publish(t, db, "blueprint", "pipeline", sourcedPipelineYAML)
	f.id = pgtest.Publish(t, db, Name, "gitlab", sourcelessYAML(f.gitlab.url()))

	return f
}

// sourcelessYAML is connectionYAML syncing incrementally.
func sourcelessYAML(instance string) string {
	return connectionYAML(instance) + "  sync: {interval: 15m, complete: 24h}\n"
}

// publishVersion publishes source as the next version of document id.
func publishVersion(t *testing.T, f *syncFixture, id, source string) {
	t.Helper()
	if _, err := f.db.ExecContext(t.Context(), `
		WITH v AS (
			INSERT INTO config_versions (document_id, version, source)
			SELECT id, published_version + 1, $2 FROM config_documents WHERE id = $1 RETURNING document_id, version
		)
		UPDATE config_documents d SET published_version = v.version FROM v WHERE d.id = v.document_id`, id, source); err != nil {
		t.Fatal(err)
	}
}

// TestSourcesSync checks a connection writes what its
// blueprints' sources map, and that a blueprint version changing an upsert
// source re-reads only its resource while one changing a reconcile source
// makes the next sync complete.
func TestSourcesSync(t *testing.T) {
	f := newSourcesFixture(t)
	first := f.syncAsUsual()
	if first.Mode != ModeComplete || f.count(f.repositoryID) == 0 || f.count(f.pipelineID) == 0 {
		t.Fatalf("the first sync was %s and wrote %d repositories and %d pipelines", first.Mode, f.count(f.repositoryID), f.count(f.pipelineID))
	}

	if second := f.syncAsUsual(); second.Mode != ModeIncremental {
		t.Fatalf("the second sync was %s, want incremental", second.Mode)
	}
	if got := f.readFrom("1"); len(got) != 2 || got[1] != cursorText(first) {
		t.Fatalf("pipelines were read from %v, want from the first sync's cursor", got)
	}

	publishVersion(t, f, f.pipelineID, strings.Replace(sourcedPipelineYAML, "startedAt: .created_at}", "startedAt: .started_at}", 1))
	if third := f.syncAsUsual(); third.Mode != ModeIncremental {
		t.Errorf("the sync after changing an upsert source was %s, want incremental", third.Mode)
	}
	if got := f.readFrom("1"); len(got) != 3 || got[2] != "" {
		t.Fatalf("after changing the pipelines source, they were read from %v, want in full", got)
	}

	publishVersion(t, f, f.repositoryID, strings.Replace(sourcedRepositoryYAML,
		"entity: {identifier: .path_with_namespace}", "entity: {identifier: .path_with_namespace, title: .name}", 1))
	if fourth := f.syncAsUsual(); fourth.Mode != ModeComplete {
		t.Errorf("the sync after changing a reconcile source was %s, want complete", fourth.Mode)
	}
}

// TestSourcesReadingNothing checks a connection no source reads isn't synced.
func TestSourcesReadingNothing(t *testing.T) {
	db := pgtest.New(t)
	gitlab := newGitLabFixture(t)
	pgtest.Publish(t, db, Name, "gitlab", sourcelessYAML(gitlab.url()))
	if _, err := Sync(t.Context(), db, "gitlab", TriggerSchedule, ""); !errors.Is(err, ErrReadsNothing) {
		t.Fatalf("Sync() error = %v, want ErrReadsNothing", err)
	}
}

// TestCheckSources checks a blueprint's sources against the published
// connections.
func TestCheckSources(t *testing.T) {
	f := newSourcesFixture(t)
	pgtest.Publish(t, f.db, Name, "solo", strings.Replace(strings.Replace(sourcelessYAML(f.gitlab.url()),
		"identifier: gitlab", "identifier: solo", 1), "      groups: [acme]\n", "", 1))
	for _, test := range []struct {
		name, connection, resource, severity, want string
	}{
		{"offered", "gitlab", "projects", "", ""},
		{"no such connection", "nope", "projects", configdoc.SeverityWarning, `no connection "nope" is published yet, so this source reads nothing until it is`},
		{"not offered", "gitlab", "tokens", configdoc.SeverityError, "resource must be one of "},
		{"can't read as set up", "solo", "members", configdoc.SeverityError, "members are read from groups, so list one in groups"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := strings.Replace(strings.Replace(sourcedRepositoryYAML, "connection: gitlab", "connection: "+test.connection, 1),
				"resource: projects", "resource: "+test.resource, 1)
			bp, _ := blueprint.Parse(source)
			issues, err := CheckSources(t.Context(), f.db, bp)
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if len(issues) != 0 {
					t.Fatalf("issues = %+v, want none", issues)
				}
				return
			}
			if len(issues) != 1 || issues[0].Severity != test.severity || !strings.HasPrefix(issues[0].Message, test.want) {
				t.Fatalf("issues = %+v, want a %s %q", issues, test.severity, test.want)
			}
		})
	}
}

// TestConnectionKeepsSourcesReadable checks a connection can't stop
// offering what a blueprint's source reads, nor be deleted while sources
// read it.
func TestConnectionKeepsSourcesReadable(t *testing.T) {
	f := newSourcesFixture(t)
	tracker := strings.Replace(sourcedYAML, "identifier: tracker", "identifier: gitlab", 1)
	issues, err := Kind.Check(t.Context(), f.db, mustParse(t, tracker))
	if err != nil {
		t.Fatal(err)
	}
	var messages []string
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	want := "pipeline's sources read pipelines, which the http connector doesn't offer"
	if !strings.Contains(strings.Join(messages, "\n"), want) {
		t.Fatalf("issues = %v, want %q", messages, want)
	}

	err = deletable(t.Context(), f.db, "gitlab")
	var refused httpx.Error
	if !errors.As(err, &refused) || refused.Code != http.StatusConflict ||
		refused.Message != "remove the sources that read it from pipeline and repository first" {
		t.Fatalf("deletable() = %v, want a 409", err)
	}
}

// TestApplyConnectionWithBlueprint checks a connection and a blueprint whose
// sources read it apply together, each seeing the other as pending.
func TestApplyConnectionWithBlueprint(t *testing.T) {
	configdoc.Register(httpxtest.New(), Kind)
	blueprint.Register(httpxtest.New(), []blueprint.Checker{CheckSources}, nil)
	ctx, cancel := builderContext(pgtest.New(t))
	defer cancel()

	applied, err := configdoc.Apply(ctx, sourcelessYAML("https://gitlab.example.com")+"---\n"+sourcedRepositoryYAML, true)
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Valid || len(applied.Documents) != 2 {
		t.Fatalf("applied = %+v", applied)
	}
	for _, document := range applied.Documents {
		if len(document.Issues) != 0 {
			t.Errorf("%s %s: issues = %+v, want none", document.Kind, document.Identifier, document.Issues)
		}
	}
}
