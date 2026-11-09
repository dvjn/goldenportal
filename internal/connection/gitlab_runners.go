package connection

import (
	"cmp"
	"context"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// What GitLab says of the runners CI jobs ran on, read on their own, as a
// job says little of its runner. Only runners jobs ran on are read, since
// listing a group's or the instance's runners takes more than read access.

// gitlabRunnerFields are what a runner read on its own gives that's kept,
// which leaves out its IP address and anything of its token.
var gitlabRunnerFields = []string{
	"id", "description", "name", "runner_type", "is_shared", "active", "paused", "online", "status", "job_execution_status",
	"tag_list", "run_untagged", "locked", "access_level", "maximum_timeout", "maintenance_note", "created_at", "contacted_at",
	"version", "revision", "platform", "architecture",
}

// gitlabRunnerManagerFields are what the manager that picked up a job says
// that a runner read on its own says of its newest manager.
var gitlabRunnerManagerFields = []string{"version", "revision", "platform", "architecture"}

// gitlabRunnerTypes name each type of runner for its title.
var gitlabRunnerTypes = map[string]string{"instance_type": "Instance", "group_type": "Group", "project_type": "Project"}

// gitlabRunnerSeen is a runner as the newest attempt on it found it.
type gitlabRunnerSeen struct {
	attempt         float64
	runner, manager map[string]any
	lastJobAt       any
	// at is when GitLab answered with the attempts.
	at time.Time
}

// gitlabRunnerSightings are the runners attempts ran on, by ID.
type gitlabRunnerSightings struct {
	mu   sync.Mutex
	seen map[string]*gitlabRunnerSeen
}

func newGitLabRunnerSightings() *gitlabRunnerSightings {
	return &gitlabRunnerSightings{seen: map[string]*gitlabRunnerSeen{}}
}

// add notes the runners of attempts GitLab answered with at, each as the
// newest attempt on it found it, of all those added.
func (s *gitlabRunnerSightings) add(attempts []map[string]any, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range attempts {
		runner := objectOf(a, "runner")
		id := idOf(runner["id"])
		if id == "" {
			continue
		}
		n, _ := a["id"].(float64)
		seen := s.seen[id]
		if seen == nil {
			seen = &gitlabRunnerSeen{attempt: -1}
			s.seen[id] = seen
		}
		seen.at = earliest(seen.at, at)
		if n < seen.attempt {
			continue
		}
		seen.attempt, seen.runner, seen.manager = n, runner, objectOf(a, "runner_manager")
		seen.lastJobAt = a["started_at"]
		if seen.lastJobAt == nil {
			seen.lastJobAt = a["created_at"]
		}
	}
}

// read reads each runner seen on its own, a few at a time, giving them by
// ID with when GitLab answered, the earliest of that and the attempts. One
// GitLab refuses or doesn't have, as a project's runner may be to a token
// without access to its project, is as its newest attempt found it. Any
// other failure leaves its runner out, and is returned once the rest are
// read.
func (s *gitlabRunnerSightings) read(ctx context.Context, client *gitlabClient) ([]map[string]any, time.Time, error) {
	s.mu.Lock()
	ids := slices.Sorted(maps.Keys(s.seen))
	seen := make([]*gitlabRunnerSeen, len(ids))
	for i, id := range ids {
		seen[i] = s.seen[id]
	}
	s.mu.Unlock()
	var (
		mu    sync.Mutex
		read  = map[*gitlabRunnerSeen]map[string]any{}
		at    time.Time
		first error
	)
	_ = forEach(seen, apiConcurrency, func(one *gitlabRunnerSeen) error {
		readCtx, clock := withSourceClock(ctx)
		runner, err := gitlabRunner(readCtx, client, one)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			first = cmp.Or(first, err)
			return nil
		}
		read[one] = runner
		at = earliest(at, earliest(one.at, clock.take()))
		return nil
	})
	var runners []map[string]any
	for _, one := range seen {
		if runner := read[one]; runner != nil {
			runners = append(runners, runner)
		}
	}

	return runners, at, first
}

// gitlabRunner reads a runner seen on its own, giving it with what it
// belongs to under _gp: its title, its groups' and projects' full paths,
// its page in GitLab, and when its newest attempt started.
func gitlabRunner(ctx context.Context, client *gitlabClient, seen *gitlabRunnerSeen) (map[string]any, error) {
	var detail map[string]any
	_, err := client.get(ctx, "runners/"+idOf(seen.runner["id"]), &detail)
	if status := apiStatus(err); status == http.StatusForbidden || status == http.StatusNotFound {
		detail, err = nil, nil
	}
	if err != nil {
		return nil, err
	}

	return gitlabRunnerObject(detail, seen), nil
}

// gitlabRunnerObject is a runner as read on its own, or, without that, as
// its newest attempt found it, with the manager that picked it up standing
// in for its newest manager.
func gitlabRunnerObject(detail map[string]any, seen *gitlabRunnerSeen) map[string]any {
	var runner map[string]any
	if detail != nil {
		runner = gitlabShape(detail, gitlabRunnerFields)
		if by := objectOf(detail, "created_by"); by != nil {
			runner["created_by"] = gitlabShape(by, []string{"id", "username", "name"})
		}
	} else {
		runner = gitlabShape(seen.runner, gitlabRunnerFields)
		maps.Copy(runner, gitlabShape(seen.manager, gitlabRunnerManagerFields))
	}
	// A group's runner belongs to that group alone, and a project's to the
	// project it was registered in first, though others may use it too.
	// An instance's runners have their pages only in its Admin area.
	groups, groupURLs := gitlabRunnerOwners(detail, "groups", func(group map[string]any) string {
		web, _ := group["web_url"].(string)
		_, path, _ := strings.Cut(web, "/groups/")
		return path
	})
	projects, projectURLs := gitlabRunnerOwners(detail, "projects", func(project map[string]any) string {
		path, _ := project["path_with_namespace"].(string)
		return path
	})
	var page any
	if owners := map[any][]string{"group_type": groupURLs, "project_type": projectURLs}[runner["runner_type"]]; len(owners) > 0 {
		page = owners[0] + "/-/runners/" + idOf(runner["id"])
	}
	runner[parentKey] = map[string]any{
		"title": gitlabRunnerTitle(runner), "groups": groups, "projects": projects, "url": page, "lastJobAt": seen.lastJobAt,
	}

	return runner
}

// gitlabRunnerOwners are the full paths, by path, of the groups or projects
// a runner read on its own lists under key, and their web addresses.
func gitlabRunnerOwners(detail map[string]any, key string, path func(map[string]any) string) (paths []any, urls []string) {
	paths = []any{}
	list, _ := detail[key].([]any)
	for _, o := range list {
		owner, _ := o.(map[string]any)
		if p := path(owner); p != "" {
			paths = append(paths, p)
		}
		if web, _ := owner["web_url"].(string); web != "" {
			urls = append(urls, strings.TrimRight(web, "/"))
		}
	}

	return paths, urls
}

// gitlabRunnerTitle names a runner by its description, or else its name,
// either of which runners registered in a group or project often lack, or
// else by its type and ID.
func gitlabRunnerTitle(runner map[string]any) string {
	for _, field := range []string{"description", "name"} {
		if text, _ := runner[field].(string); strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	kind, _ := runner["runner_type"].(string)
	if name, ok := gitlabRunnerTypes[kind]; ok {
		return name + " runner " + idOf(runner["id"])
	}

	return "Runner " + idOf(runner["id"])
}
