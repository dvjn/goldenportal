package integration

import (
	"context"
	"errors"
	"maps"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// What GitLab says of CI runs beyond its lists: a pipeline's times and who
// started it, and every attempt of its jobs, each numbered.

// gitlabPipelineTimeFields are what a pipeline carries beyond its list
// fields, which GitLab gives only of one pipeline on its own, or through
// GraphQL: its duration and time queued in seconds, downstream pipelines
// included, when it started and finished, and the user who started it.
var gitlabPipelineTimeFields = []string{"duration", "queued_duration", "started_at", "finished_at", "user"}

// gitlabSecond is a time GitLab gives, to the second, as its GraphQL API
// gives times, so a pipeline read through either looks alike.
func gitlabSecond(value any) any {
	text, _ := value.(string)
	at, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return value
	}

	return at.UTC().Truncate(time.Second).Format(time.RFC3339)
}

// gitlabPipelineTimes are the time fields of a pipeline GitLab's REST API
// gave on its own.
func gitlabPipelineTimes(pipeline map[string]any) map[string]any {
	return map[string]any{
		"duration": pipeline["duration"], "queued_duration": pipeline["queued_duration"],
		"started_at": gitlabSecond(pipeline["started_at"]), "finished_at": gitlabSecond(pipeline["finished_at"]), "user": gitlabUser(pipeline["user"]),
	}
}

// gitlabUser is who a REST object names, as GraphQL gives a user, or nil.
func gitlabUser(value any) any {
	u, ok := value.(map[string]any)
	if !ok {
		return nil
	}

	return map[string]any{"id": u["id"], "username": u["username"], "name": u["name"]}
}

// gitlabManualFields are a pipeline's stages in order and its manual jobs,
// retried ones included, each with whether it waits for its stage or for
// the jobs it needs, which says when it could first be played. REST
// doesn't say a job was manual once it has been played.
const gitlabManualFields = `stages { nodes { name } }
        manualJobs: jobs(whenExecuted: ["manual"], retried: null, first: 100) {
          pageInfo { hasNextPage }
          nodes { id schedulingType needs { nodes { name } } }
        }`

// gitlabPipelineTimesQuery pages through a project's pipelines changed
// since a time, a hundred a request, where REST would take a request a
// pipeline, with their manual jobs if asked. Child pipelines are listed
// only when asked for alone, as in REST.
func gitlabPipelineTimesQuery(manual bool) string {
	more := ""
	if manual {
		more = "\n        " + gitlabManualFields
	}

	return `query($path: ID!, $since: Time, $source: String, $after: String) {
  project(fullPath: $path) {
    pipelines(first: 100, after: $after, updatedAfter: $since, source: $source) {
      pageInfo { hasNextPage endCursor }
      nodes { id duration queuedDuration startedAt finishedAt user { id username name }` + more + ` }
    }
  }
}`
}

// gitlabManualQuery reads the manual jobs of up to twenty pipelines by ID,
// as many as GitLab takes at once.
const gitlabManualQuery = `query($path: ID!, $ids: [ID!]) {
  project(fullPath: $path) {
    pipelines(ids: $ids, first: 20) {
      nodes { id ` + gitlabManualFields + ` }
    }
  }
}`

// gitlabManualNode is what GraphQL says of a pipeline's manual jobs.
type gitlabManualNode struct {
	Stages *struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"stages"`
	ManualJobs *struct {
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
		Nodes []struct {
			ID             string `json:"id"`
			SchedulingType string `json:"schedulingType"`
			Needs          struct {
				Nodes []struct {
					Name string `json:"name"`
				} `json:"nodes"`
			} `json:"needs"`
		} `json:"nodes"`
	} `json:"manualJobs"`
}

// gitlabManualPipeline is what a pipeline's manual jobs waited for before
// they could be played: its stages in order and, by job ID, its manual
// jobs.
type gitlabManualPipeline struct {
	stages []string
	jobs   map[string]gitlabManualJob
}

// gitlabManualJob is a manual job that waited for the jobs it needs, or,
// without needs, for every job of the stages before its own.
type gitlabManualJob struct {
	dag   bool
	needs []string
}

// gitlabManual are pipelines' manual jobs, by pipeline ID, for those
// GraphQL gave all of.
type gitlabManual map[string]*gitlabManualPipeline

// add notes what GraphQL said of a pipeline's manual jobs, unless it gave
// only some of them.
func (m gitlabManual) add(id string, node gitlabManualNode) {
	if node.Stages == nil || node.ManualJobs == nil || node.ManualJobs.PageInfo.HasNextPage {
		return
	}
	pipeline := &gitlabManualPipeline{jobs: map[string]gitlabManualJob{}}
	for _, stage := range node.Stages.Nodes {
		pipeline.stages = append(pipeline.stages, stage.Name)
	}
	for _, job := range node.ManualJobs.Nodes {
		manual := gitlabManualJob{dag: job.SchedulingType == "dag"}
		for _, need := range job.Needs.Nodes {
			manual.needs = append(manual.needs, need.Name)
		}
		pipeline.jobs[idOf(gitlabNumericID(job.ID))] = manual
	}
	m[id] = pipeline
}

// gitlabManualJobs reads the manual jobs of the project's pipelines with
// ids. An instance without a GraphQL API, or one that refuses the query,
// as an older version would, says nothing of them.
func gitlabManualJobs(ctx context.Context, client *gitlabClient, fullPath string, ids []string) (gitlabManual, error) {
	manual := gitlabManual{}
	for batch := range slices.Chunk(ids, 20) {
		global := make([]string, len(batch))
		for i, id := range batch {
			global[i] = "gid://gitlab/Ci::Pipeline/" + id
		}
		var data struct {
			Project *struct {
				Pipelines struct {
					Nodes []struct {
						ID string `json:"id"`
						gitlabManualNode
					} `json:"nodes"`
				} `json:"pipelines"`
			} `json:"project"`
		}
		err := client.graphql(ctx, gitlabManualQuery, map[string]any{"path": fullPath, "ids": global}, &data)
		if apiGone(err) || errors.Is(err, errGraphQLRefused) {
			return manual, nil
		}
		if err != nil {
			return nil, err
		}
		if data.Project == nil {
			return manual, nil
		}
		for _, node := range data.Project.Pipelines.Nodes {
			manual.add(idOf(gitlabNumericID(node.ID)), node.gitlabManualNode)
		}
	}

	return manual, nil
}

// gitlabNumericID is the number at the end of a GraphQL global ID, such as
// gid://gitlab/Ci::Pipeline/42, as REST gives it.
func gitlabNumericID(global string) any {
	id, err := strconv.ParseFloat(global[strings.LastIndex(global, "/")+1:], 64)
	if err != nil {
		return nil
	}

	return id
}

// gitlabWithTimes puts their time fields on pipelines, listed in the
// project at fullPath since a time, children those GitLab lists only when
// asked for alone, and gives those still there, with the manual jobs of
// those GraphQL gave, which costs no request more. GraphQL gives them in
// pages; any it doesn't, as when the instance has no GraphQL API, are read
// on their own, and those gone since they were listed are left out. An
// instance that refuses to say which jobs are manual is asked for the
// times alone.
func gitlabWithTimes(ctx context.Context, client *gitlabClient, base, fullPath string, since time.Time,
	pipelines []map[string]any, children bool,
) ([]map[string]any, gitlabManual, error) {
	manual, askManual := gitlabManual{}, true
	missing := map[string]map[string]any{}
	for _, p := range pipelines {
		missing[idOf(p["id"])] = p
	}
	variables := map[string]any{"path": fullPath, "since": nil, "source": nil, "after": nil}
	if !since.IsZero() {
		variables["since"] = since.UTC().Format(time.RFC3339)
	}
	if children {
		variables["source"] = "parent_pipeline"
	}
	for len(missing) > 0 {
		var data struct {
			Project *struct {
				Pipelines struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						ID             string   `json:"id"`
						Duration       *float64 `json:"duration"`
						QueuedDuration *float64 `json:"queuedDuration"`
						StartedAt      *string  `json:"startedAt"`
						FinishedAt     *string  `json:"finishedAt"`
						User           *struct {
							ID       string `json:"id"`
							Username string `json:"username"`
							Name     string `json:"name"`
						} `json:"user"`
						gitlabManualNode
					} `json:"nodes"`
				} `json:"pipelines"`
			} `json:"project"`
		}
		err := client.graphql(ctx, gitlabPipelineTimesQuery(askManual), variables, &data)
		if askManual && errors.Is(err, errGraphQLRefused) {
			askManual = false
			continue
		}
		if apiGone(err) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		if data.Project == nil {
			break
		}
		for _, node := range data.Project.Pipelines.Nodes {
			id := idOf(gitlabNumericID(node.ID))
			pipeline := missing[id]
			if pipeline == nil {
				continue
			}
			// Mappings' jq takes JSON's types only, so values go in as they
			// would have come from JSON.
			var user any
			if node.User != nil {
				user = map[string]any{"id": gitlabNumericID(node.User.ID), "username": node.User.Username, "name": node.User.Name}
			}
			times := map[string]any{"duration": nil, "queued_duration": nil, "started_at": nil, "finished_at": nil, "user": user}
			if node.Duration != nil {
				times["duration"] = *node.Duration
			}
			if node.QueuedDuration != nil {
				times["queued_duration"] = *node.QueuedDuration
			}
			if node.StartedAt != nil {
				times["started_at"] = gitlabSecond(*node.StartedAt)
			}
			if node.FinishedAt != nil {
				times["finished_at"] = gitlabSecond(*node.FinishedAt)
			}
			maps.Copy(pipeline, times)
			manual.add(id, node.gitlabManualNode)
			delete(missing, id)
		}
		page := data.Project.Pipelines.PageInfo
		if !page.HasNextPage {
			break
		}
		variables["after"] = page.EndCursor
	}

	gone := map[string]bool{}
	for _, id := range slices.Sorted(maps.Keys(missing)) {
		var one map[string]any
		_, err := client.get(ctx, base+"pipelines/"+url.PathEscape(id), &one)
		switch {
		case apiGone(err):
			gone[id] = true
		case err != nil:
			return nil, nil, err
		default:
			maps.Copy(missing[id], gitlabPipelineTimes(one))
		}
	}

	return slices.DeleteFunc(pipelines, func(p map[string]any) bool { return gone[idOf(p["id"])] }), manual, nil
}

// gitlabAttempts reads every attempt of a pipeline's jobs, retried ones
// included, oldest first, each numbered under _gp: attempt, which try of
// its job it is, counting from 1; retried, whether a later one replaced
// it; attempts, how many its job had; failedAttempts, how many of those
// failed; and flaky, whether its job failed and then passed when retried.
// A job is its name in its pipeline, which a retry keeps. GitLab's lists
// carry none of these.
func gitlabAttempts(ctx context.Context, client *gitlabClient, base, pipeline, fullPath string) ([]map[string]any, error) {
	var attempts []map[string]any
	err := client.list(ctx, base+"pipelines/"+pipeline+"/jobs?include_retried=true", func(page []map[string]any) (bool, error) {
		attempts = append(attempts, page...)
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	gitlabOldestFirst(attempts)
	byJob := map[string][]map[string]any{}
	for _, a := range attempts {
		name, _ := a["name"].(string)
		byJob[name] = append(byJob[name], a)
	}
	for _, tries := range byJob {
		failed := 0
		for _, a := range tries {
			if a["status"] == "failed" {
				failed++
			}
		}
		flaky := failed > 0 && tries[len(tries)-1]["status"] == "success"
		for i, a := range tries {
			a[parentKey] = map[string]any{
				"project": fullPath, "attempt": float64(i + 1), "retried": i < len(tries)-1,
				"attempts": float64(len(tries)), "failedAttempts": float64(failed), "flaky": flaky,
			}
		}
	}

	return attempts, nil
}

// gitlabWaits notes under _gp how long each of a pipeline's attempts
// waited before it queued for a runner, given the pipeline's manual jobs,
// or nil when GitLab didn't say which they are: queuedAt, when it queued,
// its start less its time queued, as GitLab reckons that; waitSeconds,
// from its created_at, with its pipeline or on a retry, until then;
// manual, whether it's a manual job; and, splitting
// that wait, playWaitSeconds, from when the jobs it waited for had
// finished to when it was played, nothing for a job that isn't manual,
// and stageWaitSeconds, the rest, for earlier stages or the jobs it needs,
// and for a delay, a resource group or the like; and playedBy, who played
// a manual job, the user GitLab names on it once played. An attempt yet to
// start has none of the times.
func gitlabWaits(attempts []map[string]any, manual *gitlabManualPipeline) {
	stage := map[string]int{}
	if manual != nil {
		for i, name := range manual.stages {
			stage[name] = i + 1
		}
	}
	for _, a := range attempts {
		gp, _ := a[parentKey].(map[string]any)
		if gp == nil {
			continue
		}
		created, queued := timeOf(a, "created_at"), time.Time{}
		started := timeOf(a, "started_at")
		if queuedFor, ok := a["queued_duration"].(float64); ok && !started.IsZero() && !created.IsZero() {
			queued = started.Add(-time.Duration(queuedFor * float64(time.Second)))
		}
		maps.Copy(gp, map[string]any{
			"queuedAt": nil, "waitSeconds": nil, "manual": nil,
			"playWaitSeconds": nil, "stageWaitSeconds": nil, "playedBy": nil,
		})
		if !queued.IsZero() {
			gp["queuedAt"], gp["waitSeconds"] = queued.UTC().Format("2006-01-02T15:04:05.000Z"), gitlabSeconds(created, queued)
		}
		if manual == nil {
			continue
		}
		job, isManual := manual.jobs[idOf(a["id"])]
		gp["manual"] = isManual
		if queued.IsZero() {
			continue
		}
		ready := queued
		if isManual {
			ready = gitlabReady(attempts, a, job, stage, created, queued)
			gp["playedBy"] = gitlabUser(a["user"])
		}
		gp["stageWaitSeconds"], gp["playWaitSeconds"] = gitlabSeconds(created, ready), gitlabSeconds(ready, queued)
	}
}

// gitlabReady is when a manual attempt could first be played: when the
// last of the attempts it waited for had finished, of those that finished
// before it was played, so a retry of one after the play doesn't count,
// or when it was created, if later.
func gitlabReady(attempts []map[string]any, attempt map[string]any, job gitlabManualJob, stage map[string]int, created, queued time.Time) time.Time {
	ready := created
	own, _ := attempt["stage"].(string)
	for _, a := range attempts {
		name, _ := a["name"].(string)
		earlier, _ := a["stage"].(string)
		waited := slices.Contains(job.needs, name)
		if !job.dag {
			waited = stage[earlier] > 0 && stage[earlier] < stage[own]
		}
		if finished := timeOf(a, "finished_at"); waited && finished.After(ready) && !finished.After(queued) {
			ready = finished
		}
	}

	return ready
}

// gitlabSeconds is the time from one moment to a later one, in seconds,
// to the millisecond GitLab gives times to.
func gitlabSeconds(from, to time.Time) float64 {
	return max(math.Round(to.Sub(from).Seconds()*1000)/1000, 0)
}

// gitlabOldestFirst orders attempts by ID, which GitLab gives in the order
// it made them, so of attempts that write one entity, such as their
// runner, the newest is written last.
func gitlabOldestFirst(attempts []map[string]any) {
	slices.SortStableFunc(attempts, func(a, b map[string]any) int {
		x, _ := a["id"].(float64)
		y, _ := b["id"].(float64)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	})
}

// gitlabLatest are the attempts no later one replaced: each job as it
// stands.
func gitlabLatest(attempts []map[string]any) []map[string]any {
	var latest []map[string]any
	for _, a := range attempts {
		if gp, _ := a[parentKey].(map[string]any); gp["retried"] != true {
			latest = append(latest, a)
		}
	}

	return latest
}

// gitlabPipelineAttempts reads a pipeline's attempts, with what they
// waited for, once for the resources a webhook event gives of them, each
// read in turn: the later get what the first read, as answered when it
// was.
func gitlabPipelineAttempts(base, pipeline, fullPath string) func(context.Context, *gitlabClient) ([]map[string]any, error) {
	var (
		mu       sync.Mutex
		read     bool
		attempts []map[string]any
		at       time.Time
		err      error
	)
	return func(ctx context.Context, client *gitlabClient) ([]map[string]any, error) {
		mu.Lock()
		defer mu.Unlock()
		if read {
			answeredAt(ctx, at)
			return attempts, err
		}
		readCtx, clock := withSourceClock(ctx)
		attempts, err = gitlabAttempts(readCtx, client, base, pipeline, fullPath)
		var manual gitlabManual
		if err == nil {
			manual, err = gitlabManualJobs(readCtx, client, fullPath, []string{pipeline})
			gitlabWaits(attempts, manual[pipeline])
		}
		at, read = clock.take(), true
		return attempts, err
	}
}
