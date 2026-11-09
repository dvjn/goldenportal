package migrations

import (
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

const pipelineBlueprintSource = `kind: Blueprint
metadata: {identifier: pipeline, title: Pipeline}
spec:
  properties:
    status: {type: string}
    repo: {type: string}
    startedAt: {type: string, format: date-time}
  # Pipelines older than a month go.
  retention: {keep: 30d, by: startedAt}
`

const repositoryBlueprintSource = `kind: Blueprint
metadata: {identifier: repository, title: Repository}
spec:
  properties:
    url: {type: string}
`

const gitlabSource = `kind: Connection
metadata: {identifier: gitlab, title: GitLab}
spec:
  connector:
    type: gitlab
    config: {url: https://gitlab.com, token: {env: GP_SECRET_GITLAB_TOKEN}, groups: [a]}
  mappings:
    # Every pipeline, as it runs.
    - resource: pipelines
      mode: upsert
      blueprint: pipeline
      entity:
        identifier: .id
        title: .ref # the branch
        properties: {status: .status, repo: ._gp.project}
    - {resource: projects, blueprint: repository, entity: {identifier: .path_with_namespace, properties: {url: .web_url}}}
  sync: {interval: 1h, complete: 24h}
`

const deployTrackerSource = `kind: Connection
metadata: {identifier: deploy-tracker, title: Deploy Tracker}
spec:
  connector: {type: http}
  mappings:
    - resource: pipelines
      mode: upsert
      blueprint: pipeline
      entity: {identifier: .id, properties: {status: .state}}
`

func published(identifier string, versions ...string) *document {
	d := &document{identifier: identifier, versions: map[int]string{}, loadedVersions: map[int]string{}}
	for i, v := range versions {
		d.versions[i+1], d.loadedVersions[i+1] = v, v
		d.published = i + 1
	}

	return d
}

func (d *document) withDraft(source string) *document {
	d.draft, d.hasDraft, d.loadedDraft, d.hadDraft = source, true, source, true
	return d
}

func move(t *testing.T, connections, blueprints []*document) {
	t.Helper()
	if err := moveMappings(connections, blueprints); err != nil {
		t.Fatal(err)
	}
}

// parseSources parses a migrated blueprint, failing on any error.
func parseSources(t *testing.T, source string) []blueprint.Source {
	t.Helper()
	spec, issues := blueprint.Parse(source)
	if configdoc.HasErrors(issues) {
		t.Fatalf("issues %+v in\n%s", issues, source)
	}

	return spec.Sources
}

func TestMoveMappingsPublished(t *testing.T) {
	gitlab := published("gitlab", gitlabSource, gitlabSource)
	pipeline := published("pipeline", pipelineBlueprintSource, pipelineBlueprintSource)
	repository := published("repository", repositoryBlueprintSource)
	move(t, []*document{gitlab}, []*document{pipeline, repository})

	for version, source := range gitlab.versions {
		if strings.Contains(source, "mappings") || !strings.Contains(source, "sync:") {
			t.Errorf("gitlab version %d is\n%s", version, source)
		}
	}
	for version, source := range pipeline.versions {
		sources := parseSources(t, source)
		if len(sources) != 1 || sources[0].Connection != "gitlab" || sources[0].Resource != "pipelines" || sources[0].Mode != "upsert" {
			t.Errorf("pipeline version %d has sources %+v", version, sources)
		}
	}
	if pipeline.hasDraft || repository.hasDraft {
		t.Error("a blueprint got a draft though nothing was drafted")
	}
	got := repository.versions[1]
	if !strings.Contains(got, "  sources:\n    - {connection: gitlab, resource: projects, entity: {identifier: .path_with_namespace, properties: {url: .web_url}}}\n") {
		t.Errorf("the flow style mapping became\n%s", got)
	}
}

func TestMoveMappingsKeepsCommentsAndOrder(t *testing.T) {
	gitlab := published("gitlab", gitlabSource)
	pipeline := published("pipeline", pipelineBlueprintSource)
	move(t, []*document{gitlab}, []*document{pipeline, published("repository", repositoryBlueprintSource)})

	got := pipeline.versions[1]
	want := `  sources:
    # Every pipeline, as it runs.
    - connection: gitlab
      resource: pipelines
      mode: upsert
      entity:
        identifier: .id
        title: .ref # the branch
`
	if !strings.Contains(got, want) {
		t.Errorf("got\n%s", got)
	}
	if p, s, r := strings.Index(got, "properties:"), strings.Index(got, "sources:"), strings.Index(got, "# Pipelines older"); p >= s || s >= r {
		t.Errorf("sources aren't between properties and retention's comment in\n%s", got)
	}
}

func TestMoveMappingsTwoConnectionsIntoOneBlueprint(t *testing.T) {
	pipeline := published("pipeline", pipelineBlueprintSource)
	move(t, []*document{published("deploy-tracker", deployTrackerSource), published("gitlab", gitlabSource)},
		[]*document{pipeline, published("repository", repositoryBlueprintSource)})

	sources := parseSources(t, pipeline.versions[1])
	if len(sources) != 2 || sources[0].Connection != "deploy-tracker" || sources[1].Connection != "gitlab" {
		t.Errorf("sources %+v", sources)
	}
}

func TestMoveMappingsDifferingConnectionDraft(t *testing.T) {
	draft := strings.Replace(gitlabSource, "status: .status", "status: .detailed_status", 1)
	gitlab := published("gitlab", gitlabSource).withDraft(draft)
	pipeline := published("pipeline", pipelineBlueprintSource)
	repository := published("repository", repositoryBlueprintSource)
	move(t, []*document{gitlab}, []*document{pipeline, repository})

	if strings.Contains(gitlab.draft, "mappings") || !strings.Contains(gitlab.draft, "sync:") {
		t.Errorf("gitlab's draft is\n%s", gitlab.draft)
	}
	if strings.Contains(pipeline.versions[1], "detailed_status") {
		t.Errorf("the published pipeline reads the draft mapping:\n%s", pipeline.versions[1])
	}
	if !pipeline.hasDraft || !strings.Contains(pipeline.draft, "status: .detailed_status") {
		t.Errorf("pipeline's draft is\n%s", pipeline.draft)
	}
	parseSources(t, pipeline.draft)
	if repository.hasDraft {
		t.Errorf("repository got a draft though its sources didn't change:\n%s", repository.draft)
	}
}

func TestMoveMappingsExistingBlueprintDraft(t *testing.T) {
	draftBlueprint := strings.Replace(pipelineBlueprintSource, "title: Pipeline}", "title: Pipelines}", 1)
	pipeline := published("pipeline", pipelineBlueprintSource).withDraft(draftBlueprint)
	draft := strings.Replace(gitlabSource, "status: .status", "status: .detailed_status", 1)
	move(t, []*document{published("gitlab", gitlabSource).withDraft(draft)},
		[]*document{pipeline, published("repository", repositoryBlueprintSource)})

	if !strings.Contains(pipeline.draft, "title: Pipelines}") || !strings.Contains(pipeline.draft, "status: .detailed_status") {
		t.Errorf("pipeline's draft is\n%s", pipeline.draft)
	}
}

func TestMoveMappingsDraftOnlyConnection(t *testing.T) {
	tracker := &document{identifier: "deploy-tracker", versions: map[int]string{}, loadedVersions: map[int]string{}}
	tracker = tracker.withDraft(deployTrackerSource)
	pipeline := published("pipeline", pipelineBlueprintSource)
	move(t, []*document{tracker}, []*document{pipeline})

	if pipeline.versions[1] != pipelineBlueprintSource {
		t.Errorf("a never published connection changed the published pipeline:\n%s", pipeline.versions[1])
	}
	sources := parseSources(t, pipeline.draft)
	if len(sources) != 1 || sources[0].Connection != "deploy-tracker" {
		t.Errorf("pipeline's draft sources %+v", sources)
	}
	if strings.Contains(tracker.draft, "mappings") {
		t.Errorf("the connection's draft is\n%s", tracker.draft)
	}
}

func TestMoveMappingsMissingBlueprint(t *testing.T) {
	err := moveMappings([]*document{published("gitlab", gitlabSource)}, []*document{published("pipeline", pipelineBlueprintSource)})
	if err == nil || !strings.Contains(err.Error(), "connection gitlab maps blueprint repository") {
		t.Errorf("err = %v", err)
	}
}

func TestMoveMappingsWithoutProperties(t *testing.T) {
	bare := "kind: Blueprint\nmetadata: {identifier: pipeline, title: Pipeline}\nspec:\n  icon: rocket\n"
	pipeline := published("pipeline", bare)
	move(t, []*document{published("deploy-tracker", deployTrackerSource)}, []*document{pipeline})

	if !strings.HasPrefix(pipeline.versions[1], "kind: Blueprint\nmetadata: {identifier: pipeline, title: Pipeline}\nspec:\n  icon: rocket\n  sources:\n") {
		t.Errorf("got\n%s", pipeline.versions[1])
	}
}
