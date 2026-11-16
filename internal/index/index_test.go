package index

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"gofr.dev/pkg/gofr/config"

	"github.com/dvjn/goldenportal/internal/action"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/dashboard"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/scorecard"
)

const (
	jobID      = "11111111-1111-4111-8111-111111111111"
	pipelineID = "22222222-2222-4222-8222-222222222222"
)

const jobYAML = `kind: Blueprint
metadata: {identifier: job, title: Job}
spec:
  properties:
    name: {type: string}
    status: {type: string}
    seconds: {type: number}
    startedAt: {type: string, format: date-time}
    finishedAt: {type: string, format: date-time}
    pipeline: {relation: pipeline}
    tags: {type: array, items: {type: string}}
  retention: {by: finishedAt, keep: 30d}
  catalog:
    columns: [name, tags, startedAt, finishedAt]
    tabs:
      speed:
        widgets:
          daily: {type: trend, title: Daily, date: startedAt}
  entity:
    queries:
      same: {filter: {property: pipeline, op: eq, value: $entity}}
    tabs:
      retries:
        when: {property: seconds, op: gt, value: 1}
        widgets:
          slowest:
            type: table
            title: Slowest
            query: same
            sort: seconds
`

const pipelineYAML = `kind: Blueprint
metadata: {identifier: pipeline, title: Pipeline}
spec:
  properties:
    ref: {type: string}
    meta: {type: object}
    triggeredAt: {type: string, format: date-time}
`

const dashboardYAML = `kind: Dashboard
metadata: {identifier: ci, title: CI}
spec:
  inputs:
    mine:
      type: relation
      relation: pipeline
      multiple: true
      default:
        query:
          blueprint: pipeline
          filter: {property: triggeredAt, op: within, value: 1d}
  queries:
    recent:
      blueprint: job
      filter: {and: [{property: pipeline.triggeredAt, op: within, value: 7d}, {property: pipeline, op: in, values: $input.mine}]}
  widgets:
    count:
      type: stat
      title: Jobs
      value: {query: recent, share: {property: seconds, op: gt, value: 600}}
    latest:
      type: table
      title: Latest
      query: recent
      sort: -startedAt
      link: {query: recent, filter: {property: startedAt, op: exists}}
    retried:
      type: table
      title: Retried
      query: recent
      columns: [{path: name}, {title: Recent, filter: {property: startedAt, op: within, value: 1d}}]
      sort: -Recent
    oldest:
      type: stat
      title: Oldest
      value: {query: recent, min: startedAt}
    byStatus:
      type: bar
      title: By status
      query: recent
      groupBy: status
      measure: {filter: {property: seconds, op: lt, value: 10}}
`

const scorecardYAML = `kind: Scorecard
metadata: {identifier: speed, title: Speed}
spec:
  blueprint: job
  filter: {property: startedAt, op: within, value: 30d}
  tiers:
    - name: fast
      criteria:
        quick: {check: {property: seconds, op: lt, value: 60}, when: {property: finishedAt, op: exists}}
`

const actionYAML = `kind: Action
metadata: {identifier: retry, title: Retry}
spec:
  blueprint: job
  scope: {property: seconds, op: gte, value: 0}
  run: {roles: [builder], filter: {property: startedAt, op: exists}}
  steps:
    - id: busy
      check:
        blueprint: job
        filter: {property: startedAt, op: within, value: 1h}
        expect: {max: 100}
        message: Too busy
`

func mustParse[S any](t *testing.T, parse func(string) (S, []configdoc.Issue), source string) S {
	t.Helper()
	spec, issues := parse(source)
	if configdoc.HasErrors(issues) {
		t.Fatalf("issues = %+v", issues)
	}

	return spec
}

func testConfig(t *testing.T) configuration {
	t.Helper()
	job := mustParse(t, blueprint.Parse, jobYAML)
	pipeline := mustParse(t, blueprint.Parse, pipelineYAML)
	job.ID, pipeline.ID = jobID, pipelineID

	return configuration{
		Blueprints: map[string]blueprint.Spec{"job": job, "pipeline": pipeline},
		Scorecards: map[string]scorecard.Spec{"speed": mustParse(t, scorecard.Parse, scorecardYAML)},
		Dashboards: []dashboard.Spec{mustParse(t, dashboard.Parse, dashboardYAML)},
		Actions:    []action.Spec{mustParse(t, action.Parse, actionYAML)},
	}
}

const (
	startedAt   = "gp_time((properties -> 'startedAt' #>> '{}'))"
	seconds     = "(CASE WHEN jsonb_typeof(properties -> 'seconds') = 'number' THEN (properties -> 'seconds')::numeric END)"
	triggeredAt = "gp_time((properties -> 'triggeredAt' #>> '{}'))"
)

func TestUsesComeFromEachKindOfDocument(t *testing.T) {
	var got []string
	for _, use := range derive(testConfig(t)) {
		got = append(got, use.Reason+" | "+use.Use+" "+use.Blueprint+" "+use.Expression)
	}
	slices.Sort(got)
	want := []string{
		"action retry step busy | range job " + startedAt,
		"blueprint job catalog column startedAt | sort job " + startedAt,
		"blueprint job catalog tab speed widget daily date | range job " + startedAt,
		"blueprint job entity tab retries widget slowest sort | sort job " + seconds,
		"blueprint pipeline catalog column triggeredAt | sort pipeline " + triggeredAt,
		"dashboard ci input mine | range pipeline " + triggeredAt,
		"dashboard ci widget byStatus | range pipeline " + triggeredAt,
		"dashboard ci widget byStatus | relation job relations ->> 'pipeline'",
		"dashboard ci widget byStatus measure | range job " + seconds,
		"dashboard ci widget count | range pipeline " + triggeredAt,
		"dashboard ci widget count | relation job relations ->> 'pipeline'",
		"dashboard ci widget count share | range job " + seconds,
		"dashboard ci widget latest | range pipeline " + triggeredAt,
		"dashboard ci widget latest | relation job relations ->> 'pipeline'",
		"dashboard ci widget latest link | range pipeline " + triggeredAt,
		"dashboard ci widget latest link | relation job relations ->> 'pipeline'",
		"dashboard ci widget latest sort | sort job " + startedAt,
		"dashboard ci widget oldest | range pipeline " + triggeredAt,
		"dashboard ci widget oldest | relation job relations ->> 'pipeline'",
		"dashboard ci widget retried | range pipeline " + triggeredAt,
		"dashboard ci widget retried | relation job relations ->> 'pipeline'",
		// A sort by a condition column has no key; its filter is a look at the rows.
		"dashboard ci widget retried column Recent | range job " + startedAt,
		"scorecard speed filter | range job " + startedAt,
	}
	// Catalog columns nothing else reads, name, ref and updatedAt, are left out.
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("uses =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestPlanMergesUsesOfOneKey(t *testing.T) {
	build, skipped := plan(derive(testConfig(t)), map[string]string{"job": jobID, "pipeline": pipelineID}, defaults)
	if len(skipped) > 0 {
		t.Errorf("skipped = %+v", skipped)
	}
	got := map[string]planned{}
	for _, ix := range build {
		got[ix.Blueprint+" "+ix.Expression] = ix
		if !strings.HasPrefix(ix.Name, prefix) || ix.Name != name(ix.BlueprintID, ix.Expression) {
			t.Errorf("name = %s", ix.Name)
		}
	}
	// A sort either way is descending, which also serves ranges on its key.
	started, ok := got["job ("+startedAt+") DESC NULLS LAST, title, identifier"]
	if !ok || started.Weight != 1+2+2+2+1+1 || len(started.Reasons) != 6 {
		t.Errorf("startedAt = %+v", started)
	}
	if want := `CREATE INDEX CONCURRENTLY "` + started.Name + `" ON entities ((` + startedAt + `) DESC NULLS LAST, title, identifier) WHERE blueprint_id = '` + jobID + `'`; started.create() != want {
		t.Errorf("create() = %s", started.create())
	}
	// Only ascending sorts keep the index ascending.
	if _, ok := got["job ("+seconds+") ASC NULLS FIRST, title, identifier"]; !ok {
		t.Errorf("no ascending index on seconds in %v", slices.Sorted(maps.Keys(got)))
	}
	for _, want := range []string{"job (relations ->> 'pipeline')", "pipeline (" + triggeredAt + ") DESC NULLS LAST, title, identifier"} {
		if _, ok := got[want]; !ok {
			t.Errorf("no index %s", want)
		}
	}
	if len(build) != 4 {
		t.Errorf("built %d indexes", len(build))
	}
}

func TestPlanOnlyIndexesLargeBlueprints(t *testing.T) {
	build, _ := plan(derive(testConfig(t)), map[string]string{"pipeline": pipelineID}, defaults)
	for _, ix := range build {
		if ix.Blueprint != "pipeline" || ix.BlueprintID != pipelineID {
			t.Errorf("index on %s", ix.Blueprint)
		}
	}
	if len(build) != 1 {
		t.Errorf("built %d indexes", len(build))
	}
}

func TestPlanKeepsToTheCaps(t *testing.T) {
	uses := []keyUse{
		{IndexKey: entity.IndexKey{Blueprint: "job", Expression: "a", Use: entity.UseRange}, Reason: "r", Weight: 1},
		{IndexKey: entity.IndexKey{Blueprint: "job", Expression: "b", Use: entity.UseRange}, Reason: "r", Weight: 3},
		{IndexKey: entity.IndexKey{Blueprint: "job", Expression: "c", Use: entity.UseRange}, Reason: "r", Weight: 2},
		{IndexKey: entity.IndexKey{Blueprint: "pipeline", Expression: "d", Use: entity.UseRange}, Reason: "r", Weight: 4},
		{IndexKey: entity.IndexKey{Blueprint: "pipeline", Expression: "e", Use: entity.UseRange}, Reason: "r", Weight: 1},
	}
	build, skipped := plan(uses, map[string]string{"job": jobID, "pipeline": pipelineID}, Settings{MinRows: 1, PerBlueprint: 2, Total: 3})
	names := func(indexes []planned) (out []string) {
		for _, ix := range indexes {
			out = append(out, ix.Expression+": "+ix.Skipped)
		}
		return out
	}
	if want := []string{"(d): ", "(b): ", "(c): "}; !slices.Equal(names(build), want) {
		t.Errorf("built %v, want %v", names(build), want)
	}
	want := []string{
		"(a): job has 2 indexes, the most a blueprint gets",
		"(e): there are 3 indexes, the most there can be",
	}
	if !slices.Equal(names(skipped), want) {
		t.Errorf("skipped %v, want %v", names(skipped), want)
	}
}

func TestSettings(t *testing.T) {
	s, err := SettingsFrom(config.NewMockConfig(nil))
	if err != nil || s != (Settings{MinRows: 50000, PerBlueprint: 8, Total: 64}) || s.off() {
		t.Errorf("defaults = %+v, %v", s, err)
	}
	s, err = SettingsFrom(config.NewMockConfig(map[string]string{TotalEnv: "0", MinRowsEnv: "20000"}))
	if err != nil || !s.off() || s.MinRows != 20000 {
		t.Errorf("settings = %+v, %v, want indexes off", s, err)
	}
	for env, raw := range map[string]string{MinRowsEnv: "0", PerBlueprintEnv: "-1", TotalEnv: "many"} {
		if _, err := SettingsFrom(config.NewMockConfig(map[string]string{env: raw})); err == nil {
			t.Errorf("%s=%s was taken", env, raw)
		}
	}
}
