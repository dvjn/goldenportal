package integration

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

type GitLabConfig struct {
	// URL is the instance's address, whose REST API is under /api/v4.
	URL   string
	Token HeaderValue
	// Groups are read whole, with their subgroups.
	Groups      []string
	Projects    []string
	HistoryDays int
	// ExcludeArchived leaves archived projects out, with all that's in them.
	ExcludeArchived bool
}

const (
	gitlabGroups        = "groups"
	gitlabProjects      = "projects"
	gitlabTags          = "tags"
	gitlabMergeRequests = "merge_requests"
	gitlabIssues        = "issues"
	gitlabReleases      = "releases"
	gitlabPipelines     = "pipelines"
	gitlabJobs          = "jobs"
	gitlabDeployments   = "deployments"
	gitlabEnvironments  = "environments"
	gitlabMembers       = "members"
)

// gitlabResource is one of the connector's resources, and how it's read.
type gitlabResource struct {
	name string
	// fromGroups is read from groups rather than projects.
	fromGroups bool
	// features are the project features it needs, which a project can turn
	// off. GitLab refuses lists of a feature that's off, but such a project
	// has none to read.
	features []string
	// probe is the request, under a group or project, that shows whether
	// the token can read it.
	probe string
	// events are the webhook settings that send events about it. Without
	// any, only syncs keep it current: groups and members have their events
	// only on group webhooks, which are for GitLab's paid tiers.
	events []string
}

// gitlabTable is every resource. A project comes with pushes and pipelines
// on its default branch, whose head, CI config and latest pipeline it
// carries, and an environment with deployments to it.
var gitlabTable = []gitlabResource{
	{name: gitlabGroups, fromGroups: true, probe: "descendant_groups"},
	{name: gitlabProjects, events: []string{"pipeline_events", "push_events"}},
	{name: gitlabTags, features: []string{"repository"}, probe: "repository/tags", events: []string{"tag_push_events"}},
	{
		name: gitlabMergeRequests, features: []string{"repository", "merge_requests"}, probe: "merge_requests",
		events: []string{"merge_requests_events"},
	},
	{name: gitlabIssues, features: []string{"issues"}, probe: "issues", events: []string{"issues_events", "confidential_issues_events"}},
	{name: gitlabReleases, features: []string{"releases"}, probe: "releases", events: []string{"releases_events"}},
	{name: gitlabPipelines, features: []string{"builds"}, probe: "pipelines", events: []string{"pipeline_events"}},
	{name: gitlabJobs, features: []string{"builds"}, probe: "jobs", events: []string{"job_events"}},
	// GitLab forbids a project's deployments and environments while its
	// CI/CD is off, whatever its environments setting says.
	{name: gitlabDeployments, features: []string{"builds", "environments"}, probe: "deployments", events: []string{"deployment_events"}},
	{name: gitlabEnvironments, features: []string{"builds", "environments"}, probe: "environments", events: []string{"deployment_events"}},
	{name: gitlabMembers, fromGroups: true, probe: "members"},
}

// gitlabNames are the resources keep is true for, in the table's order.
func gitlabNames(keep func(gitlabResource) bool) []string {
	var names []string
	for _, r := range gitlabTable {
		if keep(r) {
			names = append(names, r.name)
		}
	}

	return names
}

func gitlabResourceNamed(name string) gitlabResource {
	i := slices.IndexFunc(gitlabTable, func(r gitlabResource) bool { return r.name == name })
	if i < 0 {
		return gitlabResource{}
	}

	return gitlabTable[i]
}

var (
	gitlabResources        = gitlabNames(func(gitlabResource) bool { return true })
	gitlabGroupResources   = gitlabNames(func(r gitlabResource) bool { return r.fromGroups })
	gitlabProjectResources = gitlabNames(func(r gitlabResource) bool { return !r.fromGroups })
)

const gitlabDefaultURL = "https://gitlab.com"

// GitLabConfigFields are the keys the gitlab connector's config takes,
// exported so the documents' JSON Schema can be checked against them.
var GitLabConfigFields = []string{"url", "token", "groups", "projects", "historyDays", "excludeArchived"}

func parseGitLabConfig(r *configdoc.Reader, node *yaml.Node, path string) Config {
	if node == nil {
		return nil
	}
	config := &GitLabConfig{URL: gitlabDefaultURL}
	fields, ok := r.Fields(node, path, GitLabConfigFields...)
	if !ok {
		return config
	}
	if n := fields["url"]; n != nil {
		config.URL = strings.TrimRight(r.Text(n, path+".url"), "/")
		u, err := url.Parse(config.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			r.Errorf(n, path+".url", "url must be the instance's http or https address without credentials, a query or a fragment, such as https://gitlab.example.com")
		}
	}
	if n := fields["token"]; n != nil {
		config.Token, _ = readSecret(r, n, path+".token")
	} else {
		r.Errorf(node, path+".token", "add a token, as {env: %s...} holding a personal, group or project access token", SecretEnvPrefix)
	}

	listed := map[string]bool{}
	config.Groups = readNames(r, fields, path, "groups", listed, gitlabPath(1), "a group's full path, such as acme or acme/platform")
	config.Projects = readNames(r, fields, path, "projects", listed, gitlabPath(2), "a project's full path, such as acme/api")
	for _, name := range []string{"groups", "projects"} {
		n := fields[name]
		if n == nil || n.Kind != yaml.SequenceNode {
			continue
		}
		for i, item := range n.Content {
			group := config.groupOf(item.Value)
			itemPath := fmt.Sprintf("%s.%s[%d]", path, name, i)
			switch {
			case group == "":
			case name == "groups":
				r.Errorf(item, itemPath, "%s is already read with the %s group, which includes its subgroups", item.Value, group)
			default:
				r.Report(configdoc.SeverityWarning, item, itemPath, "%s is already read with the %s group", item.Value, group)
			}
		}
	}
	// A list that's there, or a misspelt key, already has an issue of its own.
	if fields["groups"] == nil && fields["projects"] == nil && !configdoc.HasUnknownField(node, GitLabConfigFields...) {
		r.Errorf(node, path+".groups", "add the groups or projects to read")
	}
	config.HistoryDays = readHistoryDays(r, fields, path)
	config.ExcludeArchived = r.Bool(fields["excludeArchived"], path+".excludeArchived")

	return config
}

// gitlabPath says whether a name is a full path of at least min parts.
// GitLab's own answer that it has no such group or project covers the rest
// of its rules.
func gitlabPath(min int) func(string) bool {
	return func(p string) bool {
		parts := strings.Split(p, "/")
		return len(parts) >= min && !slices.Contains(parts, "") && !strings.ContainsFunc(p, unicode.IsSpace)
	}
}

func (c *GitLabConfig) apiURL() string { return c.URL + "/api/v4" }

func (c *GitLabConfig) Resources() []string { return gitlabResources }

func (c *GitLabConfig) Pulls() []string { return gitlabResources }

// Receives lists the resources a project's webhook sends events about.
func (c *GitLabConfig) Receives() []string {
	return gitlabNames(func(r gitlabResource) bool { return len(r.events) > 0 })
}

// ReceivesEvents marks webhook events as what it receives.
func (c *GitLabConfig) ReceivesEvents() {}

// cutoff is when what piles up must be newer than for a sync to read it.
func (c *GitLabConfig) cutoff() time.Time { return time.Now().AddDate(0, 0, -c.HistoryDays) }

// gitlabCurrent reports whether an object is one a sync reads, being open,
// or ended or made since cutoff, so a webhook event about one a sync would
// leave out writes nothing either. Jobs go with their pipeline, which
// their events check for themselves.
func gitlabCurrent(resource string, object map[string]any, cutoff time.Time) bool {
	switch resource {
	case gitlabMergeRequests:
		return current(object, cutoff, []string{"opened", "locked"}, "merged_at", "closed_at")
	case gitlabIssues:
		return current(object, cutoff, []string{"opened"}, "closed_at")
	case gitlabPipelines, gitlabDeployments:
		return current(object, cutoff, nil, "created_at")
	}

	return true
}

// groupOf is the listed group that p, a project's or another group's full
// path, is in, or "".
func (c *GitLabConfig) groupOf(p string) string {
	for _, g := range c.Groups {
		if strings.HasPrefix(strings.ToLower(p), strings.ToLower(g)+"/") {
			return g
		}
	}

	return ""
}

// selects reports whether a webhook event about the project at p is for
// one the connector reads.
func (c *GitLabConfig) selects(p string) bool {
	return c.groupOf(p) != "" || slices.ContainsFunc(c.Projects, func(listed string) bool { return strings.EqualFold(listed, p) })
}

// resourceProblem says why a mapping can't read resource with this config:
// groups and members come only from groups.
func (c *GitLabConfig) resourceProblem(resource string) string {
	if !slices.Contains(gitlabGroupResources, resource) || len(c.Groups) > 0 {
		return ""
	}

	return resource + " are read from groups, so list one in groups"
}
