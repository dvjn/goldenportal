package connection

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestGitLabPullNumbersAttempts checks a sync gives every attempt of each
// pipeline's jobs, retried ones included, oldest first across the project,
// each numbered, and each job's latest attempt with how many it took.
func TestGitLabPullNumbersAttempts(t *testing.T) {
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabProjectYAML("acme/api"), f.url())
	pulled, failed := pullGitLab(t, config, gitlabJobs, gitlabJobAttempts)
	if len(failed) != 0 {
		t.Fatalf("failed = %v", failed)
	}
	order := func(objects []map[string]any) string {
		ids := make([]string, len(objects))
		for i, o := range objects {
			ids[i] = idOf(o["id"])
		}
		return strings.Join(ids, ",")
	}
	if got := order(pulled[gitlabJobAttempts]); got != "110,120,130,131,132,140" {
		t.Errorf("attempts emitted in the order %s, want oldest first", got)
	}
	if got := order(pulled[gitlabJobs]); got != "110,120,130,132,140" {
		t.Errorf("jobs emitted in the order %s, want each job's latest attempt, oldest first", got)
	}
	numbered := map[string]string{
		"131": `{"attempt":1,"attempts":2,"failedAttempts":1,"flaky":true,"project":"acme/api","retried":true}`,
		"132": `{"attempt":2,"attempts":2,"failedAttempts":1,"flaky":true,"project":"acme/api","retried":false}`,
		"130": `{"attempt":1,"attempts":1,"failedAttempts":0,"flaky":false,"project":"acme/api","retried":false}`,
	}
	for _, resource := range []string{gitlabJobAttempts, gitlabJobs} {
		for _, attempt := range pulled[resource] {
			want, ok := numbered[idOf(attempt["id"])]
			if !ok {
				continue
			}
			gp := map[string]any{}
			for _, key := range []string{"project", "attempt", "retried", "attempts", "failedAttempts", "flaky"} {
				gp[key] = valueAt(attempt, parentKey, key)
			}
			if got, _ := json.Marshal(gp); string(got) != want {
				t.Errorf("%s %v carries %s, want %s", resource, attempt["id"], got, want)
			}
			if bad := nonJSON(attempt); bad != "" {
				t.Errorf("%s %v carries %s, which jq can't read", resource, attempt["id"], bad)
			}
		}
	}
	if q := f.sent(http.MethodGet, "/projects/1/pipelines/13/jobs"); len(q) != 2 || !strings.Contains(q[0], "include_retried=true") {
		t.Errorf("pipeline 13's jobs were asked for with %v, want its two pages once, retried ones included", q)
	}
}

// TestGitLabPipelineAlike checks a pipeline a sync reads, with its times
// from GraphQL, and one a webhook event reads, from REST, look alike, and
// so do their jobs' attempts and the runners those ran on.
func TestGitLabPipelineAlike(t *testing.T) {
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabProjectYAML("acme/api"), f.url())
	pulled, failed := pullGitLab(t, config, gitlabPipelines, gitlabJobAttempts, gitlabRunners)
	if len(failed) != 0 {
		t.Fatalf("failed = %v", failed)
	}
	items, err := receiveGitLab(t, config, "Pipeline Hook", `{"object_kind": "pipeline", "object_attributes": {"id": 13, "ref": "main"}, `+
		`"project": {"id": 1, "path_with_namespace": "acme/api", "default_branch": "main"}}`)
	if err != nil {
		t.Fatal(err)
	}
	received := map[string][]map[string]any{}
	for _, item := range items {
		received[item.Resource] = append(received[item.Resource], item.Object.(map[string]any))
	}
	of := func(objects []map[string]any, id string) map[string]any {
		for _, o := range objects {
			if idOf(o["id"]) == id {
				return o
			}
		}
		return nil
	}
	synced, event := of(pulled[gitlabPipelines], "13"), of(received[gitlabPipelines], "13")
	if synced == nil || !reflect.DeepEqual(synced, event) {
		t.Errorf("a sync gives pipeline 13 as\n%v\nand an event as\n%v", synced, event)
	}
	if synced["duration"] != float64(60) || synced["finished_at"] != gitlabSecond(gitlabT3) {
		t.Errorf("pipeline 13 = %v, want its duration and when it finished", synced)
	}
	for _, id := range []string{"130", "131", "132"} {
		if a, b := of(pulled[gitlabJobAttempts], id), of(received[gitlabJobAttempts], id); a == nil || !reflect.DeepEqual(a, b) {
			t.Errorf("a sync gives attempt %s as\n%v\nand an event as\n%v", id, a, b)
		}
	}
	if a, b := of(pulled[gitlabRunners], "1"), of(received[gitlabRunners], "1"); a == nil || !reflect.DeepEqual(a, b) {
		t.Errorf("a sync gives runner 1 as\n%v\nand an event as\n%v", a, b)
	}
}

// TestGitLabPipelineTimesWithoutGraphQL checks an instance without a
// GraphQL API has each pipeline's times read on its own, alike, and one
// GraphQL leaves out read on its own too, unless it's gone since it was
// listed, when it's left out.
func TestGitLabPipelineTimesWithoutGraphQL(t *testing.T) {
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabProjectYAML("acme/api"), f.url())
	pulled, _ := pullGitLab(t, config, gitlabPipelines)

	f.noGraphQL = true
	without, failed := pullGitLab(t, config, gitlabPipelines)
	if len(failed) != 0 || !reflect.DeepEqual(pulled[gitlabPipelines], without[gitlabPipelines]) {
		t.Errorf("without GraphQL, pipelines = %v, failed = %v; want them as with it, %v", without[gitlabPipelines], failed, pulled[gitlabPipelines])
	}
	if n := len(slices.DeleteFunc(f.asked(), func(r string) bool { return !strings.HasPrefix(r, "GET /projects/1/pipelines/1") })); n != 4 {
		t.Errorf("read %d pipelines on their own, want each of the 4", n)
	}

	f.noGraphQL = false
	f.answer("/api/graphql", func(w http.ResponseWriter) {
		f.reply(w, http.StatusOK, map[string]any{"data": map[string]any{"project": map[string]any{"pipelines": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false}, "nodes": []any{map[string]any{"id": "gid://gitlab/Ci::Pipeline/13", "duration": 60}},
		}}}})
	})
	f.answer("/projects/1/pipelines/12", gitlabNotFound)
	partly, failed := pullGitLab(t, config, gitlabPipelines)
	if len(failed) != 0 || gitlabField(partly[gitlabPipelines], "id") != "11,13,14" {
		t.Errorf("pipelines = %s, failed = %v; want the one gone since it was listed left out", gitlabField(partly[gitlabPipelines], "id"), failed)
	}
	for _, p := range partly[gitlabPipelines] {
		if p["user"] == nil && p["id"] != float64(13) {
			t.Errorf("pipeline %v = %v, want what GraphQL left out read on its own", p["id"], p)
		}
	}

	// Jobs read without what GraphQL says of them would lose which were
	// manual, so they fail with pipelines, to be read again.
	f.answer("/api/graphql", gitlabServerError, gitlabServerError, gitlabServerError, gitlabServerError)
	shortenGitLabWaits(t)
	if _, failed := pullGitLab(t, config, gitlabPipelines, gitlabJobs); failed[gitlabPipelines] == nil || failed[gitlabJobs] == nil {
		t.Errorf("failed = %v, want pipelines and jobs failed, without what GraphQL says of them", failed)
	}
}

// TestTwoSourcesReadOneResource checks one item of a resource two
// sources read makes an entity of each, and items that make one entity
// each make it, in turn.
func TestTwoSourcesReadOneResource(t *testing.T) {
	spec := withSources(t, `kind: Connection
metadata: {identifier: gitlab, title: GitLab}
spec:
  connector:
    type: gitlab
    config: {token: {env: GP_SECRET_GITLAB_TOKEN}, groups: [acme]}
`, `kind: Blueprint
metadata: {identifier: job-attempt, title: Job Attempt}
spec:
  properties:
    runner: {type: string}
  sources:
    - connection: gitlab
      resource: job_attempts
      mode: upsert
      entity: {identifier: .id, properties: {runner: .runner.id | values | tostring}}
`, `kind: Blueprint
metadata: {identifier: runner, title: Runner}
spec:
  properties:
    version: {type: string}
  sources:
    - connection: gitlab
      resource: job_attempts
      filter: .runner != null
      mode: upsert
      entity: {identifier: .runner.id, properties: {version: .runner_manager.version}}
`)
	attempt := func(id float64, version string) Item {
		object := map[string]any{"id": id, "runner": nil}
		if version != "" {
			object["runner"], object["runner_manager"] = map[string]any{"id": float64(7)}, map[string]any{"version": version}
		}
		return Item{Resource: gitlabJobAttempts, Object: object}
	}
	entries, got := gitlabMapped(t, spec, []Item{attempt(1, ""), attempt(2, "18.4.0"), attempt(3, "18.5.0")})
	if got != "job-attempt 1, job-attempt 2, runner 7, job-attempt 3, runner 7" {
		t.Fatalf("entries = %s", got)
	}
	if entries[0].Entity.Properties["runner"] != nil || entries[1].Entity.Properties["runner"] != "7" || entries[4].Entity.Properties["version"] != "18.5.0" {
		t.Errorf("entries = %+v", entries)
	}
}

func TestPullTimeout(t *testing.T) {
	for _, test := range []struct {
		interval string
		want     time.Duration
	}{
		{"1h", 50 * time.Minute},
		{"30m", 20 * time.Minute},
		{"15m", 7*time.Minute + 30*time.Second},
		{"1m", 30 * time.Second},
		{"", DefaultSyncInterval / 2},
	} {
		source := "kind: Connection\nmetadata: {identifier: tracker, title: Tracker}\nspec:\n" +
			"  connector:\n    type: http\n    config:\n      resources:\n        deploys: {url: 'https://tracker.example.com/deploys'}\n"
		if test.interval != "" {
			source += "  sync: {interval: " + test.interval + "}\n"
		}
		if got := mustParse(t, source).pullTimeout(); got != test.want {
			t.Errorf("syncing every %q, a sync stops after %s, want %s", test.interval, got, test.want)
		}
	}
}

// TestGitLabWaits checks what an attempt waited for before it queued: a
// job that isn't manual waited only for what came before it, and a manual
// one, once that had finished, for its play, which a retry finishing after
// the play doesn't move.
func TestGitLabWaits(t *testing.T) {
	at := func(seconds float64) string {
		return time.Date(2026, 11, 4, 6, 0, 0, 0, time.UTC).Add(time.Duration(seconds * float64(time.Second))).Format("2006-01-02T15:04:05.000Z")
	}
	attempt := func(id float64, name, stage string, started, queued, finished float64) map[string]any {
		a := map[string]any{
			"id": id, "name": name, "stage": stage, "created_at": at(0), "started_at": nil, "finished_at": nil, "queued_duration": nil,
			"user": map[string]any{"id": float64(1), "username": "ada", "name": "Ada", "state": "active"}, parentKey: map[string]any{},
		}
		if started > 0 {
			a["started_at"], a["queued_duration"] = at(started), queued
		}
		if finished > 0 {
			a["finished_at"] = at(finished)
		}
		return a
	}
	attempts := func() []map[string]any {
		played := attempt(6, "apply", "deploy", 400.5, 0.5, 500)
		played["user"] = map[string]any{"id": float64(2), "username": "grace", "name": "Grace", "state": "active"}
		return []map[string]any{
			attempt(1, "build", "build", 5, 2, 60),
			attempt(2, "test", "test", 62, 1, 120),
			// A retry of plan after apply was played, which apply didn't wait for.
			attempt(3, "plan", "build", 4, 1, 90),
			attempt(4, "release", "deploy", 600, 0, 610),
			attempt(5, "pending", "deploy", 0, 0, 0),
			played,
			attempt(7, "plan", "build", 450, 1, 700),
		}
	}
	manual := &gitlabManualPipeline{
		stages: []string{"build", "test", "deploy"},
		jobs:   map[string]gitlabManualJob{"4": {}, "5": {}, "6": {dag: true, needs: []string{"plan"}}},
	}
	got := attempts()
	gitlabWaits(got, manual)
	want := map[string]string{
		"1": `{"manual":false,"playWaitSeconds":0,"playedBy":null,"queuedAt":"2026-11-04T06:00:03.000Z","stageWaitSeconds":3,"waitSeconds":3}`,
		"2": `{"manual":false,"playWaitSeconds":0,"playedBy":null,"queuedAt":"2026-11-04T06:01:01.000Z","stageWaitSeconds":61,"waitSeconds":61}`,
		// Played once test, the last job of the stages before its own, had
		// finished.
		"4": `{"manual":true,"playWaitSeconds":480,"playedBy":{"id":1,"name":"Ada","username":"ada"},"queuedAt":"2026-11-04T06:10:00.000Z","stageWaitSeconds":120,"waitSeconds":600}`,
		"5": `{"manual":true,"playWaitSeconds":null,"playedBy":null,"queuedAt":null,"stageWaitSeconds":null,"waitSeconds":null}`,
		// Played once plan's first attempt, the job it needs, had finished.
		"6": `{"manual":true,"playWaitSeconds":310,"playedBy":{"id":2,"name":"Grace","username":"grace"},"queuedAt":"2026-11-04T06:06:40.000Z","stageWaitSeconds":90,"waitSeconds":400}`,
	}
	for _, a := range got {
		if w, ok := want[idOf(a["id"])]; ok {
			if gp, _ := json.Marshal(a[parentKey]); string(gp) != w {
				t.Errorf("attempt %v carries\n%s, want\n%s", a["id"], gp, w)
			}
		}
	}

	unknown := attempts()
	gitlabWaits(unknown, nil)
	if gp, _ := json.Marshal(unknown[3][parentKey]); string(gp) != `{"manual":null,"playWaitSeconds":null,"playedBy":null,`+
		`"queuedAt":"2026-11-04T06:10:00.000Z","stageWaitSeconds":null,"waitSeconds":600}` {
		t.Errorf("without its manual jobs, attempt 4 carries %s, want its wait unsplit", gp)
	}
}

// TestGitLabPullManualJobs checks a sync says which attempts were manual
// jobs, who played them and how long they waited, from the pipeline times
// GraphQL gives at no request more, or with a query for those, a request
// for up to twenty pipelines, when the sync reads no pipelines, and alike
// from a webhook event.
func TestGitLabPullManualJobs(t *testing.T) {
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabProjectYAML("acme/api"), f.url())
	of := func(objects []map[string]any, id string) map[string]any {
		for _, o := range objects {
			if idOf(o["id"]) == id {
				return o
			}
		}
		return nil
	}
	graphql := func() int { return len(f.sent(http.MethodPost, "/api/graphql")) }

	before := graphql()
	withPipelines, failed := pullGitLab(t, config, gitlabPipelines, gitlabJobAttempts)
	if len(failed) != 0 || graphql()-before != 2+1 {
		t.Fatalf("failed = %v, asked GraphQL %d times, want once for each page of each kind of pipeline", failed, graphql()-before)
	}
	played := of(withPipelines[gitlabJobAttempts], "110")
	want := `{"manual":true,"playWaitSeconds":569.75,"playedBy":{"id":2,"name":"Grace","username":"grace"},` +
		`"queuedAt":"` + gitlabLater(gitlabT0, 10*time.Minute-30250*time.Millisecond) + `","stageWaitSeconds":0,"waitSeconds":569.75}`
	gp := map[string]any{}
	for _, key := range []string{"queuedAt", "waitSeconds", "manual", "playWaitSeconds", "stageWaitSeconds", "playedBy"} {
		gp[key] = valueAt(played, parentKey, key)
	}
	if got, _ := json.Marshal(gp); string(got) != want {
		t.Errorf("attempt 110 carries\n%s, want\n%s", got, want)
	}
	if valueAt(of(withPipelines[gitlabJobAttempts], "130"), parentKey, "manual") != false {
		t.Errorf("attempt 130 = %v, want it not manual", of(withPipelines[gitlabJobAttempts], "130"))
	}

	before = graphql()
	alone, failed := pullGitLab(t, config, gitlabJobAttempts)
	if len(failed) != 0 || graphql()-before != 1 {
		t.Errorf("failed = %v, asked GraphQL %d times, want once for the pipelines' manual jobs", failed, graphql()-before)
	}
	if a := of(alone[gitlabJobAttempts], "110"); !reflect.DeepEqual(a, played) {
		t.Errorf("without pipelines, attempt 110 =\n%v\nwant\n%v", a, played)
	}

	items, err := receiveGitLab(t, config, "Pipeline Hook", `{"object_kind": "pipeline", "object_attributes": {"id": 11, "ref": "main"}, `+
		`"project": {"id": 1, "path_with_namespace": "acme/api", "default_branch": "main"}}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Resource == gitlabJobAttempts && !reflect.DeepEqual(item.Object, played) {
			t.Errorf("an event gives attempt 110 as\n%v\nwant\n%v", item.Object, played)
		}
	}

	f.noGraphQL = true
	without, failed := pullGitLab(t, config, gitlabJobAttempts)
	if a := of(without[gitlabJobAttempts], "110"); len(failed) != 0 || valueAt(a, parentKey, "manual") != nil || valueAt(a, parentKey, "waitSeconds") != 569.75 {
		t.Errorf("without GraphQL, attempt 110 = %v, failed = %v; want its wait, not known to be manual", a, failed)
	}
}
