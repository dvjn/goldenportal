package connection

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/telemetry"
)

// gitlabEnabled reports whether a project has what resource needs turned
// on. Instances too old to say have it on.
func gitlabEnabled(project map[string]any, resource string) bool {
	return gitlabFeaturesOn(project, gitlabResourceNamed(resource).features...)
}

func gitlabFeaturesOn(project map[string]any, features ...string) bool {
	for _, feature := range features {
		if project[feature+"_access_level"] == "disabled" {
			return false
		}
	}

	return true
}

// Every object a sync and an event both give must look alike, or a source
// would see two things. Groups and projects are read on their own, as listed
// ones and an event's project are, as well as in lists, and what GitLab
// says of one on its own grows as it adds fields, with secrets among them,
// such as runners' tokens, so both are cut to the fields of their lists,
// which are what's below. So are pipelines, whose lists are what a sync
// reads, since reading each on its own would cost a request a pipeline,
// with their times, which GraphQL gives a page at a time.
// Merge requests and issues are read again from
// their lists, by iid, and a deployment read on its own has only its
// approvals more, which are dropped.
var (
	gitlabGroupFields = []string{
		"id", "web_url", "name", "path", "full_name", "full_path", "description", "visibility", "parent_id", "created_at",
		"avatar_url", "archived", "marked_for_deletion_on", "default_branch", "request_access_enabled", "lfs_enabled",
		"project_creation_level", "subgroup_creation_level", "require_two_factor_authentication", "organization_id",
	}
	gitlabPipelineFields = []string{
		"id", "iid", "project_id", "sha", "ref", "status", "source", "name", "created_at", "updated_at", "web_url",
	}
	gitlabProjectFields = []string{
		"id", "description", "name", "name_with_namespace", "path", "path_with_namespace", "created_at", "updated_at",
		"default_branch", "tag_list", "topics", "ssh_url_to_repo", "http_url_to_repo", "web_url", "readme_url", "avatar_url",
		"forks_count", "star_count", "open_issues_count", "last_activity_at", "visibility", "namespace", "owner", "creator_id",
		"forked_from_project", "archived", "empty_repo", "marked_for_deletion_at", "marked_for_deletion_on", "ci_config_path",
		"issues_access_level", "repository_access_level", "merge_requests_access_level", "builds_access_level",
		"releases_access_level", "environments_access_level", "wiki_access_level", "_links",
		"merge_method", "squash_option", "only_allow_merge_if_pipeline_succeeds", "only_allow_merge_if_all_discussions_are_resolved",
		"allow_merge_on_skipped_pipeline", "remove_source_branch_after_merge",
	}
)

func gitlabShape(object map[string]any, fields []string) map[string]any {
	out := map[string]any{}
	for _, field := range fields {
		if value, ok := object[field]; ok {
			out[field] = value
		}
	}

	return out
}

func gitlabProjectShape(project map[string]any) map[string]any {
	return gitlabShape(project, gitlabProjectFields)
}

// gitlabPull is one sync's reading of GitLab.
type gitlabPull struct {
	*pullRun
	config  *GitLabConfig
	request PullRequest
	client  *gitlabClient
	warn    func(string)
	viewer  *gitlabViewer
	// projects are those read, whose webhooks the sync sets up.
	projects []map[string]any
	// listedAt is when GitLab listed each project, by ID.
	listedAt map[string]time.Time
	groups   *gitlabMergeGroups
	runners  *gitlabRunnerSightings
}

// Pull reads each resource asked for. Groups come first, then the
// projects, a few at a time. Merge requests, issues, pipelines with their
// jobs and job attempts, and deployments are read from where the request
// says in each project, as what changed since; the rest is read whole, as
// GitLab can't say what of it changed. Something that fails, such as one
// project's pipelines, fails its resource, so a complete sync deletes none
// of it, without stopping the rest. Then it points each project's webhook
// at the connection, where it can, warning of what it couldn't.
func (c *GitLabConfig) Pull(ctx context.Context, _ configdoc.DB, p PullRequest,
	emit func(resource string, items []Item) error,
) (Report, error) {
	pull := &gitlabPull{
		pullRun: newPullRun(p.Resources, emit), config: c, request: p, warn: p.Warn, groups: newGitLabMergeGroups(),
		listedAt: map[string]time.Time{}, runners: newGitLabRunnerSightings(),
	}
	client, err := newGitLabClient(c)
	if err != nil {
		return pull.failAll(err), nil
	}
	pull.client = client

	err = pull.run(ctx)
	hookCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hookTimeout)
	defer cancel()
	ensureGitLabWebhooks(hookCtx, client, pull.projects, p.IngestURL, p.WebhookSecret, p.Resources, pull.warn)
	scopes := make([]string, len(pull.projects))
	for i, project := range pull.projects {
		scopes[i] = idOf(project["id"])
	}

	return pull.result(scopes, err)
}

func (p *gitlabPull) run(ctx context.Context) error {
	groups, err := p.pullGroups(ctx)
	if err != nil || !p.wantsAny(gitlabProjectResources) {
		return err
	}
	if err := p.readProjects(ctx, groups); err != nil {
		return err
	}

	if err := forEach(p.projects, apiConcurrency, func(project map[string]any) error { return p.pullProject(ctx, project) }); err != nil {
		return err
	}

	return p.pullRunners(ctx)
}

// pullRunners reads the runners the attempts read in every project ran on,
// each once, as the newest attempt on it across them found it. They're in
// no one project, as several may share a runner. A runner that can't be
// read fails them as a whole, so no project's cursor moves past the
// attempts that would read it again.
func (p *gitlabPull) pullRunners(ctx context.Context) (err error) {
	if !p.want[gitlabRunners] {
		return nil
	}
	ctx, span := telemetry.Start(ctx, "read "+gitlabRunners, telemetry.Resource.String(gitlabRunners))
	defer func() { telemetry.End(span, err) }()
	runners, at, readErr := p.runners.read(ctx, p.client)
	if err := p.emitObjects(gitlabRunners, runners, nil, source{at: at}); err != nil {
		return err
	}

	return p.fail(ctx, []string{gitlabRunners}, "", "", readErr)
}

// gitlabMissing is a listed group or project GitLab has no answer about,
// which a token that lost access to it gets as well as one whose group is
// gone, so it fails what it holds rather than deleting it.
type gitlabMissing struct{ kind, path string }

func (e gitlabMissing) Error() string {
	return fmt.Sprintf("GitLab has no %s %s that the token can see", e.kind, e.path)
}

// gitlabFind reads a listed group or project by its full path. GitLab
// follows its old path once it's moved, so moved is where it is now, when
// that's elsewhere.
func gitlabFind(ctx context.Context, client *gitlabClient, kind, path string) (object map[string]any, moved string, err error) {
	query, field := "", "path_with_namespace"
	if kind == "group" {
		query, field = "?with_projects=false", "full_path"
	}
	_, err = client.get(ctx, kind+"s/"+url.PathEscape(path)+query, &object)
	switch {
	case apiGone(err):
		return nil, "", gitlabMissing{kind, path}
	case err != nil:
		return nil, "", err
	}
	if now, _ := object[field].(string); !strings.EqualFold(now, path) {
		moved = now
	}

	return object, moved, nil
}

// find reads a listed group or project, warning of one GitLab has no more
// or has moved, and checks the token sees all of it.
func (p *gitlabPull) find(ctx context.Context, kind, path string) (map[string]any, error) {
	object, moved, err := gitlabFind(ctx, p.client, kind, path)
	var missing gitlabMissing
	switch {
	case errors.As(err, &missing):
		p.warn(sentence(missing.Error()) + " Nothing read from it is deleted, and no project a sync no longer finds counts as gone, " +
			"until it's taken out of " + kind + "s, if it's gone, or the token gets access to it again.")
		return nil, err
	case err != nil:
		return nil, err
	case moved != "":
		p.warn("GitLab has moved the " + kind + " " + path + " to " + moved + ", where it's read; list it under its new path, since events about it are taken only once it is.")
	}
	if p.viewer == nil {
		viewer, err := gitlabSignIn(ctx, p.client)
		if err != nil {
			return nil, err
		}
		p.viewer = &viewer
	}

	return object, gitlabWholeView(ctx, p.client, p.viewer, kind, object)
}

// pullGroups reads each listed group, and its subgroups and members if they
// were asked for, giving the groups whose projects can be read whole. One
// that can't be read whole fails every resource it holds, lest a complete
// sync delete the rest.
func (p *gitlabPull) pullGroups(ctx context.Context) ([]map[string]any, error) {
	var groups []map[string]any
	for _, path := range p.config.Groups {
		groupCtx, clock := withSourceClock(ctx)
		group, err := p.find(groupCtx, "group", path)
		if err != nil {
			// The group's projects aren't listed either, which holds back
			// deleting what's in any project gone since.
			if err := p.fail(ctx, gitlabGroupResources, path, "", err); err != nil {
				return nil, err
			}
			if err := p.unlist(ctx, "the group "+path, err); err != nil {
				return nil, err
			}
			continue
		}
		groups = append(groups, group)
		if !p.wantsAny(gitlabGroupResources) {
			continue
		}
		all := []map[string]any{gitlabShape(group, gitlabGroupFields)}
		err = p.read(groupCtx, listing{path: "groups/" + idOf(group["id"]) + "/descendant_groups"}, func(objects []map[string]any, _ time.Time) error {
			for _, g := range objects {
				all = append(all, gitlabShape(g, gitlabGroupFields))
			}
			return nil
		})
		if err := skipGone(err); err != nil {
			if err := p.fail(ctx, gitlabGroupResources, path, "", err); err != nil {
				return nil, err
			}
			continue
		}
		if err := p.fail(ctx, []string{gitlabGroups}, path, "", p.emitObjects(gitlabGroups, all, nil, source{at: clock.take()})); err != nil {
			return nil, err
		}
		if !p.want[gitlabMembers] {
			continue
		}
		for _, g := range all {
			fullPath, _ := g["full_path"].(string)
			err := p.read(ctx, listing{path: "groups/" + idOf(g["id"]) + "/members"}, func(members []map[string]any, at time.Time) error {
				return p.emitObjects(gitlabMembers, members, map[string]any{"group": fullPath}, source{at: at})
			})
			if err := p.fail(ctx, []string{gitlabMembers}, fullPath, "", skipGone(err)); err != nil {
				return nil, err
			}
		}
	}

	return groups, nil
}

// gitlabViewer is the token's user, and whether it sees everything.
type gitlabViewer struct {
	ID    float64 `json:"id"`
	Admin bool    `json:"is_admin"`
	// scopes are the token's, when GitLab says.
	scopes []string
	// everything is whether it's an administrator in effect, once asked.
	everything *bool
}

// gitlabSignIn reads the token's user, and the token's scopes. Instances
// too old to say leave the scopes unknown.
func gitlabSignIn(ctx context.Context, client *gitlabClient) (gitlabViewer, error) {
	var viewer gitlabViewer
	if _, err := client.get(ctx, "user", &viewer); err != nil {
		return viewer, err
	}
	var self struct {
		Scopes []string `json:"scopes"`
	}
	if _, err := client.get(ctx, "personal_access_tokens/self", &self); err == nil {
		viewer.scopes = self.Scopes
	}

	return viewer, nil
}

// seesEverything reports whether the token reads all there is: as an
// administrator, unless Admin Mode is on and the token lacks its scope, when
// it reads no more than a member. Only an administrator in effect may read
// the instance's settings, which says which.
func (v *gitlabViewer) seesEverything(ctx context.Context, client *gitlabClient) (bool, error) {
	if !v.Admin {
		return false, nil
	}
	if v.everything == nil {
		var settings map[string]any
		_, err := client.get(ctx, "application/settings", &settings)
		switch status := apiStatus(err); {
		case status == http.StatusForbidden || status == http.StatusUnauthorized:
		case err != nil:
			return false, err
		}
		everything := err == nil
		v.everything = &everything
	}

	return *v.everything, nil
}

// gitlabWholeView says why the token sees only part of a group or project,
// kind says which, if it does. It sees all of it as a member, directly or
// through a group above it, with at least the Reporter role, or as an
// administrator in effect. Below Reporter, private repositories, pipelines
// and confidential issues are hidden, even in a public project, so a
// complete sync would delete them.
func gitlabWholeView(ctx context.Context, client *gitlabClient, viewer *gitlabViewer, kind string, object map[string]any) error {
	if everything, err := viewer.seesEverything(ctx, client); everything || err != nil {
		return err
	}
	var member struct {
		AccessLevel int `json:"access_level"`
	}
	_, err := client.get(ctx, kind+"s/"+idOf(object["id"])+"/members/all/"+idOf(viewer.ID), &member)
	if err != nil && !apiGone(err) {
		return err
	}
	if err == nil && member.AccessLevel >= 20 {
		return nil
	}
	path, _ := object["full_path"].(string)
	if kind == "project" {
		path, _ = object["path_with_namespace"].(string)
	}
	problem := "the token's user isn't a member of the " + kind + " " + path + " with at least the Reporter role, so it sees only some of what's there; " +
		"make it one, or use a group or project access token there with that role"
	if viewer.Admin {
		problem += "; or, as the token's user is an administrator, give the token the admin_mode scope, which Admin Mode needs"
	}

	return errors.New(problem)
}

// readProjects reads the projects to read from. A listed one GitLab has
// moved is read where it is now, saying so.
func (p *gitlabPull) readProjects(ctx context.Context, groups []map[string]any) error {
	seen := map[string]bool{}
	add := func(project map[string]any, at time.Time) {
		if id := idOf(project["id"]); id != "" && !seen[id] {
			seen[id] = true
			p.projects = append(p.projects, gitlabProjectShape(project))
			p.listedAt[id] = at
		}
	}

	for _, group := range groups {
		fullPath, _ := group["full_path"].(string)
		query := url.Values{"include_subgroups": {"true"}, "with_shared": {"false"}, "order_by": {"id"}, "sort": {"asc"}}
		if p.config.ExcludeArchived {
			query.Set("archived", "false")
		}
		err := p.read(ctx, listing{path: "groups/" + idOf(group["id"]) + "/projects?" + query.Encode()}, func(objects []map[string]any, at time.Time) error {
			for _, project := range objects {
				add(project, at)
			}
			return nil
		})
		if err := p.unlist(ctx, "the projects of "+fullPath, skipGone(err)); err != nil {
			return err
		}
	}
	for _, path := range p.config.Projects {
		projectCtx, clock := withSourceClock(ctx)
		project, err := p.find(projectCtx, "project", path)
		switch {
		case err != nil:
			if err := p.unlist(ctx, "the project "+path, err); err != nil {
				return err
			}
		case p.config.ExcludeArchived && project["archived"] == true:
			p.warn("The project " + path + " is archived, so it's left out, as excludeArchived says; take it out of projects, or let archived projects in.")
		default:
			add(project, clock.take())
		}
	}

	return nil
}

// gitlabWithDefaultBranch puts what a project's default branch has under
// _gp, each null when the project has none: the branch itself, its CI
// config, and its latest pipeline, as the project's CI status. They're the
// project's, rather than resources of their own, since a catalog of
// repositories wants its default branch, not every branch.
func gitlabWithDefaultBranch(ctx context.Context, client *gitlabClient, groups *gitlabMergeGroups, project map[string]any) error {
	base := "projects/" + idOf(project["id"]) + "/"
	branch, _ := project["default_branch"].(string)
	repository := project["empty_repo"] != true && branch != "" && gitlabFeaturesOn(project, "repository")
	var head, config, latest, files any
	if repository {
		var b map[string]any
		_, err := client.get(ctx, base+"repository/branches/"+url.PathEscape(branch), &b)
		if err := skipGone(err); err != nil {
			return err
		}
		if b != nil {
			head = b
		}
		f, err := gitlabFiles(ctx, client, base, branch)
		if err != nil {
			return err
		}
		files = f
	}
	if repository && gitlabFeaturesOn(project, "builds") {
		c, err := gitlabCIConfig(ctx, client, base)
		if err := skipGone(err); err != nil {
			return err
		}
		if c != nil {
			config = c
		}
	}
	// The list, filtered to the branch, gives its latest pipeline as a sync
	// reads pipelines, and leaves out child pipelines, which a parent's
	// jobs make. A token that reads pipelines may still be refused the
	// latest one on its own.
	if project["empty_repo"] != true && branch != "" && gitlabFeaturesOn(project, "builds") {
		var pipelines []map[string]any
		query := url.Values{"ref": {branch}, "order_by": {"id"}, "sort": {"desc"}, "per_page": {"1"}}
		_, err := client.get(ctx, base+"pipelines?"+query.Encode(), &pipelines)
		if err := skipGone(err); err != nil {
			return err
		}
		if len(pipelines) > 0 {
			latest = gitlabShape(pipelines[0], gitlabPipelineFields)
		}
	}
	checks, err := groups.checks(ctx, client, project)
	if err != nil {
		return err
	}
	vulnerabilities, err := gitlabVulnerabilities(ctx, client, project)
	if err != nil {
		return err
	}
	var dependencies map[string]any
	if repository {
		if dependencies, err = gitlabDependencies(ctx, client, base); err != nil {
			return err
		}
	}
	project[parentKey] = map[string]any{
		"defaultBranch": head, "ciConfig": config, "latestPipeline": latest, "files": files, "mergeChecks": checks,
		"vulnerabilities": vulnerabilities, "dependencies": dependencies,
	}

	return nil
}

// gitlabOSPackagers are the package managers of an image's operating
// system, whose packages container scanning lists beside the application's
// dependencies. They ship with the base image rather than link into the
// application, so their licenses aren't the application's.
var gitlabOSPackagers = []string{"apk", "apt", "deb", "dpkg", "rpm", "yum", "dnf"}

// gitlabDependencies summarises a project's dependency list, as its
// scanners last found it: how many dependencies the application has, how
// many under each license, by SPDX identifier or "unknown", how many OS
// packages its images carry, and how many of either GitLab flags as
// malware. A dependency offered under several licenses counts once, under
// them all joined with " OR ", such as "EPL-2.0 OR GPL-2.0-only", since
// its user picks one. It's nil when the token can't read the list, which takes a role
// that can read vulnerabilities, or the project has none.
func gitlabDependencies(ctx context.Context, client *gitlabClient, base string) (map[string]any, error) {
	total, osPackages, malware := 0, 0, 0
	licenses := map[string]any{}
	err := client.list(ctx, base+"dependencies", func(page []map[string]any) (bool, error) {
		for _, d := range page {
			if d["malware"] == true {
				malware++
			}
			if packager, _ := d["package_manager"].(string); slices.Contains(gitlabOSPackagers, packager) {
				osPackages++
				continue
			}
			total++
			var ids []string
			list, _ := d["licenses"].([]any)
			for _, l := range list {
				license, _ := l.(map[string]any)
				if id, _ := license["spdx_identifier"].(string); id != "" {
					ids = append(ids, id)
				}
			}
			key := "unknown"
			if len(ids) > 0 {
				slices.Sort(ids)
				key = strings.Join(slices.Compact(ids), " OR ")
			}
			n, _ := licenses[key].(float64)
			licenses[key] = n + 1
		}
		return true, nil
	})
	if status := apiStatus(err); status == http.StatusForbidden || status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil || total+osPackages == 0 {
		return nil, err
	}

	return map[string]any{"count": float64(total), "osPackages": float64(osPackages), "malware": float64(malware), "licenses": licenses}, nil
}

// gitlabVulnerabilityQuery asks for a project's open vulnerabilities by
// severity, and when its oldest open critical and high ones were found,
// rather than every finding: a group can have tens of thousands, and the
// counts and ages are what a catalog of repositories scores and sorts on.
const gitlabVulnerabilityQuery = `query($path: ID!) {
  project(fullPath: $path) {
    vulnerabilitySeveritiesCount { critical high medium low }
    sast: vulnerabilitySeveritiesCount(reportType: [SAST]) { critical high medium low }
    secretDetection: vulnerabilitySeveritiesCount(reportType: [SECRET_DETECTION]) { critical high medium low }
    dependencyScanning: vulnerabilitySeveritiesCount(reportType: [DEPENDENCY_SCANNING]) { critical high medium low }
    containerScanning: vulnerabilitySeveritiesCount(reportType: [CONTAINER_SCANNING]) { critical high medium low }
    dast: vulnerabilitySeveritiesCount(reportType: [DAST]) { critical high medium low }
    oldestCritical: vulnerabilities(severity: [CRITICAL], state: [DETECTED, CONFIRMED], sort: detected_asc, first: 1) { nodes { detectedAt } }
    oldestHigh: vulnerabilities(severity: [HIGH], state: [DETECTED, CONFIRMED], sort: detected_asc, first: 1) { nodes { detectedAt } }
  }
}`

// gitlabVulnerabilities summarises a project's open vulnerabilities, or
// gives nil when the token can't read them, which takes a role that can,
// such as Security Manager, or when GitLab has no GraphQL API.
func gitlabVulnerabilities(ctx context.Context, client *gitlabClient, project map[string]any) (map[string]any, error) {
	path, _ := project["path_with_namespace"].(string)
	type oldest struct {
		Nodes []struct {
			DetectedAt string `json:"detectedAt"`
		} `json:"nodes"`
	}
	var data struct {
		Project *struct {
			Counts             map[string]int `json:"vulnerabilitySeveritiesCount"`
			SAST               map[string]int `json:"sast"`
			SecretDetection    map[string]int `json:"secretDetection"`
			DependencyScanning map[string]int `json:"dependencyScanning"`
			ContainerScanning  map[string]int `json:"containerScanning"`
			DAST               map[string]int `json:"dast"`
			OldestCritical     *oldest        `json:"oldestCritical"`
			OldestHigh         *oldest        `json:"oldestHigh"`
		} `json:"project"`
	}
	err := client.graphql(ctx, gitlabVulnerabilityQuery, map[string]any{"path": path}, &data)
	if err := skipGone(err); err != nil {
		return nil, err
	}
	if data.Project == nil || data.Project.Counts == nil {
		return nil, nil
	}
	// Sources' jq takes JSON's types only, so counts go in as they would
	// have come from JSON.
	counts := func(by map[string]int) any {
		if by == nil {
			return nil
		}
		out := map[string]any{}
		for severity, n := range by {
			out[severity] = float64(n)
		}
		return out
	}
	pr := data.Project
	summary := map[string]any{"oldestCriticalAt": nil, "oldestHighAt": nil, "byScanner": map[string]any{
		"sast": counts(pr.SAST), "secretDetection": counts(pr.SecretDetection), "dependencyScanning": counts(pr.DependencyScanning),
		"containerScanning": counts(pr.ContainerScanning), "dast": counts(pr.DAST),
	}}
	for severity, n := range pr.Counts {
		summary[severity] = float64(n)
	}
	for key, o := range map[string]*oldest{"oldestCriticalAt": data.Project.OldestCritical, "oldestHighAt": data.Project.OldestHigh} {
		if o != nil && len(o.Nodes) > 0 {
			summary[key] = o.Nodes[0].DetectedAt
		}
	}

	return summary, nil
}

// gitlabMergeChecks are the merge checks GitLab enforces on a project's
// merge requests, keyed as _gp.mergeChecks has them.
var gitlabMergeChecks = map[string]string{
	"pipelineMustSucceed":       "only_allow_merge_if_pipeline_succeeds",
	"discussionsMustBeResolved": "only_allow_merge_if_all_discussions_are_resolved",
}

// gitlabMergeGroups reads each group's merge checks once a sync. A group
// that turns a check on enforces it on every project beneath it, though
// the project's own setting, all the project shows, stays off.
type gitlabMergeGroups struct {
	mu   sync.Mutex
	read map[string]map[string]bool
}

func newGitLabMergeGroups() *gitlabMergeGroups {
	return &gitlabMergeGroups{read: map[string]map[string]bool{}}
}

// checks gives which merge checks apply to the project: those it turns on,
// and those any group above it does. A group the token can't see turns
// none on.
func (g *gitlabMergeGroups) checks(ctx context.Context, client *gitlabClient, project map[string]any) (map[string]any, error) {
	on := map[string]any{}
	for check, field := range gitlabMergeChecks {
		on[check] = project[field] == true
	}
	ns, _ := project["namespace"].(map[string]any)
	namespace, _ := ns["full_path"].(string)
	parts := strings.Split(namespace, "/")
	for i := range parts {
		if namespace == "" {
			break
		}
		group, err := g.group(ctx, client, strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		for check := range gitlabMergeChecks {
			on[check] = on[check] == true || group[check]
		}
	}

	return on, nil
}

func (g *gitlabMergeGroups) group(ctx context.Context, client *gitlabClient, path string) (map[string]bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if checks, ok := g.read[path]; ok {
		return checks, nil
	}
	var group map[string]any
	_, err := client.get(ctx, "groups/"+url.PathEscape(path)+"?with_projects=false", &group)
	if err := skipGone(err); err != nil && apiStatus(err) != http.StatusForbidden {
		return nil, err
	}
	checks := map[string]bool{}
	for check, field := range gitlabMergeChecks {
		checks[check] = group[field] == true
	}
	g.read[path] = checks

	return checks, nil
}

// gitlabFiles lists the paths of the files at the root of a branch and in
// its .gitlab directory, where repositories keep their tooling's config,
// such as CODEOWNERS or renovate.json, so a source can tell what's set up
// without reading each file.
func gitlabFiles(ctx context.Context, client *gitlabClient, base, branch string) ([]any, error) {
	files := []any{}
	gitlabDir := false
	for _, dir := range []string{"", ".gitlab"} {
		if dir != "" && !gitlabDir {
			break
		}
		query := url.Values{"ref": {branch}, "per_page": {"100"}}
		if dir != "" {
			query.Set("path", dir)
		}
		err := client.list(ctx, base+"repository/tree?"+query.Encode(), func(entries []map[string]any) (bool, error) {
			for _, entry := range entries {
				path, _ := entry["path"].(string)
				switch entry["type"] {
				case "blob":
					files = append(files, path)
				case "tree":
					gitlabDir = gitlabDir || path == ".gitlab"
				}
			}
			return true, nil
		})
		if err := skipGone(err); err != nil {
			return nil, err
		}
	}

	return files, nil
}

// gitlabWithLastDeployment puts an environment's last successful
// deployment, or null, and its project, under _gp, as what's deployed
// there.
func gitlabWithLastDeployment(ctx context.Context, client *gitlabClient, base, fullPath string, environment map[string]any) error {
	var deployments []map[string]any
	name, _ := environment["name"].(string)
	query := url.Values{"environment": {name}, "status": {"success"}, "order_by": {"id"}, "sort": {"desc"}, "per_page": {"1"}}
	if _, err := client.get(ctx, base+"deployments?"+query.Encode(), &deployments); err != nil {
		return err
	}
	var last any
	if len(deployments) > 0 {
		last = deployments[0]
	}
	environment[parentKey] = map[string]any{"project": fullPath, "lastDeployment": last}

	return nil
}

// gitlabApprovals reads the approvals of a project's deployments, which
// GitLab gives only of a deployment on its own. An environment's rules say
// whether its deployments need approval, so once one deployment to an
// environment says none do, the rest of the sync reads no others of it,
// and once one says nothing of approvals, as on GitLab's free tier, it
// reads none.
type gitlabApprovals struct {
	client         *gitlabClient
	base, fullPath string
	none           map[string]bool
	unknown        bool
}

// add puts what GitLab says of its approvals on a deployment it listed.
func (a *gitlabApprovals) add(ctx context.Context, deployment map[string]any) error {
	environment, _ := objectOf(deployment, "environment")["name"].(string)
	var one map[string]any
	switch {
	case a.unknown:
	case a.none[environment]:
		one = map[string]any{"approvals": []any{}, "pending_approval_count": float64(0)}
	default:
		if _, err := a.client.get(ctx, a.base+"deployments/"+idOf(deployment["id"]), &one); err != nil && !apiGone(err) {
			return err
		}
	}
	gitlabWithApprovals(deployment, one, a.fullPath)
	switch required := objectOf(deployment, parentKey)["approvalRequired"]; {
	case one != nil && required == nil:
		a.unknown = true
	case required == false:
		a.none[environment] = true
	}

	return nil
}

// gitlabWithApprovals puts under a deployment's _gp its project and what
// one, the deployment as GitLab gives it on its own, says of its
// approvals: approvalRequired, whether its environment's rules ask for
// any; approvals, each with its user, status, created_at and comment;
// approvedAt, when the last approval it needed was given, unless it still
// waits for one or one was a rejection; and approvalSeconds, from when the
// deployment was made, as its job became ready to run, until then. Each
// is nil where GitLab says nothing of approvals. waitSeconds is from when
// the deployment was made until its job started: approval, a manual play
// and the wait for a runner.
func gitlabWithApprovals(deployment, one map[string]any, fullPath string) {
	created := timeOf(deployment, "created_at")
	gp := map[string]any{"project": fullPath, "approvalRequired": nil, "approvals": nil, "approvedAt": nil, "approvalSeconds": nil, "waitSeconds": nil}
	deployment[parentKey] = gp
	if started := timeOf(objectOf(deployment, "deployable"), "started_at"); !started.IsZero() && !created.IsZero() {
		gp["waitSeconds"] = gitlabSeconds(created, started)
	}
	given, ok := one["approvals"].([]any)
	if !ok {
		return
	}
	required := false
	rules, _ := objectOf(one, "approval_summary")["rules"].([]any)
	for _, rule := range rules {
		r, _ := rule.(map[string]any)
		if n, _ := r["required_approvals"].(float64); n > 0 {
			required = true
		}
	}
	pending, _ := one["pending_approval_count"].(float64)
	approvals := make([]any, 0, len(given))
	var (
		approved   time.Time
		approvedAt any
		rejected   bool
	)
	for _, value := range given {
		approval, _ := value.(map[string]any)
		approvals = append(approvals, map[string]any{
			"user": gitlabUser(approval["user"]), "status": approval["status"], "created_at": approval["created_at"], "comment": approval["comment"],
		})
		switch at := timeOf(approval, "created_at"); approval["status"] {
		case "rejected":
			rejected = true
		case "approved":
			if at.After(approved) {
				approved, approvedAt = at, approval["created_at"]
			}
		}
	}
	gp["approvalRequired"], gp["approvals"] = required || pending > 0 || len(given) > 0, approvals
	if approvedAt != nil && pending == 0 && !rejected {
		gp["approvedAt"], gp["approvalSeconds"] = approvedAt, gitlabSeconds(created, approved)
	}
}

// pullProject reads one project.
func (p *gitlabPull) pullProject(ctx context.Context, project map[string]any) (err error) {
	fullPath, _ := project["path_with_namespace"].(string)
	ctx, span := telemetry.Start(ctx, "pull project", telemetry.Scope.String(fullPath))
	defer func() { telemetry.End(span, err) }()
	scope := idOf(project["id"])
	base := "projects/" + scope + "/"
	parent := map[string]any{"project": fullPath}
	page := func(resource string, parent map[string]any) func([]map[string]any, time.Time) error {
		return func(objects []map[string]any, at time.Time) error {
			return p.emitObjects(resource, objects, parent, source{scope: scope, at: at})
		}
	}
	simple := func(resource, path string) func(context.Context) error {
		return func(ctx context.Context) error {
			return p.read(ctx, listing{path: base + path}, page(resource, parent))
		}
	}
	steps := []struct {
		resources []string
		run       func(ctx context.Context) error
	}{
		{[]string{gitlabProjects}, func(ctx context.Context) error {
			ctx, clock := withSourceClock(ctx)
			if err := gitlabWithDefaultBranch(ctx, p.client, p.groups, project); err != nil {
				return err
			}
			from := source{scope: scope, at: earliest(p.listedAt[scope], clock.take())}
			return p.emitObjects(gitlabProjects, []map[string]any{project}, nil, from)
		}},
		{[]string{gitlabTags}, simple(gitlabTags, "repository/tags")},
		{[]string{gitlabMergeRequests}, func(ctx context.Context) error {
			return p.readChanged(ctx, base+"merge_requests", gitlabMergeRequests, scope, page(gitlabMergeRequests, parent))
		}},
		{[]string{gitlabIssues}, func(ctx context.Context) error {
			return p.readChanged(ctx, base+"issues", gitlabIssues, scope, page(gitlabIssues, parent))
		}},
		{[]string{gitlabReleases}, simple(gitlabReleases, "releases")},
		{gitlabRunResources, func(ctx context.Context) error { return p.pullPipelines(ctx, base, fullPath, scope) }},
		{[]string{gitlabDeployments}, func(ctx context.Context) error {
			since, _ := p.request.From(gitlabDeployments, scope)
			approvals := &gitlabApprovals{client: p.client, base: base, fullPath: fullPath, none: map[string]bool{}}
			return p.read(ctx, listing{path: base + "deployments?" + changedSince(since).Encode()}, func(deployments []map[string]any, at time.Time) error {
				for _, deployment := range deployments {
					if err := approvals.add(ctx, deployment); err != nil {
						return err
					}
				}
				return page(gitlabDeployments, nil)(deployments, at)
			})
		}},
		{[]string{gitlabEnvironments}, func(ctx context.Context) error {
			return p.read(ctx, listing{path: base + "environments"}, func(environments []map[string]any, at time.Time) error {
				for _, environment := range environments {
					if err := gitlabWithLastDeployment(ctx, p.client, base, fullPath, environment); err != nil {
						return err
					}
				}
				return page(gitlabEnvironments, nil)(environments, at)
			})
		}},
	}
	for _, s := range steps {
		if !p.wantsAny(s.resources) || !gitlabEnabled(project, s.resources[0]) {
			continue
		}
		if err := p.step(ctx, s.resources, fullPath, scope, base, s.run); err != nil {
			return err
		}
	}

	return nil
}

// step reads resources of a project, as a span of its own.
func (p *gitlabPull) step(ctx context.Context, resources []string, fullPath, scope, base string, run func(context.Context) error) error {
	ctx, span := telemetry.Start(ctx, "read "+resources[0], telemetry.Resource.String(resources[0]))
	err := run(ctx)
	if apiGone(err) {
		err = p.unlessGone(ctx, base, err)
	}
	telemetry.End(span, err)

	return p.fail(ctx, resources, fullPath, scope, err)
}

// unlessGone is err, a 404 on part of a project, unless the project is gone
// since it was listed, which has nothing more to read.
func (p *gitlabPull) unlessGone(ctx context.Context, base string, err error) error {
	var project map[string]any
	if _, again := p.client.get(ctx, strings.TrimSuffix(base, "/"), &project); apiGone(again) {
		return nil
	}

	return err
}

// changedSince asks a list for what changed since a time, the latest
// change first, or for everything, newest first, for the zero time. Lists
// of deployments take updated_after only ordered by updated_at.
func changedSince(since time.Time) url.Values {
	if since.IsZero() {
		return url.Values{"order_by": {"id"}, "sort": {"desc"}}
	}

	return url.Values{"updated_after": {since.UTC().Format(time.RFC3339)}, "order_by": {"updated_at"}, "sort": {"desc"}}
}

// readChanged reads the merge requests or issues of a project that changed
// since the last sync read them there, or all of them when the sync reads
// them in full. Closing or merging one changes it. A first sync that reads
// back only as far as the blueprints keep rows reads the open ones too,
// however long since they changed, since retention keeps what's open.
func (p *gitlabPull) readChanged(ctx context.Context, path, resource, scope string, page func([]map[string]any, time.Time) error) error {
	since, first := p.request.From(resource, scope)
	if first && !since.IsZero() {
		if err := p.read(ctx, listing{path: path + "?state=opened"}, page); err != nil {
			return err
		}
	}
	changed := listing{path: path + "?" + changedSince(since).Encode()}
	if first && !since.IsZero() {
		changed.keep = func(object map[string]any) bool { return object["state"] != "opened" }
	}

	return p.read(ctx, changed, page)
}

// gitlabRunResources are read together, as pipelines say which jobs to
// read, and jobs which runners.
var gitlabRunResources = []string{gitlabPipelines, gitlabJobs, gitlabJobAttempts, gitlabRunners}

// gitlabAttemptResources are read from pipelines' jobs.
var gitlabAttemptResources = []string{gitlabJobs, gitlabJobAttempts, gitlabRunners}

// pullPipelines reads the pipelines that changed since the last sync read
// them in the project, child pipelines among them, with their times, and
// every attempt of their jobs, at a request a pipeline, as jobs and as
// job attempts, noting the runners they ran on. Retrying a job changes its pipeline, so its pipeline's
// attempts are read again, and numbered again, with it. A project's
// attempts are emitted together, oldest first. Times or jobs that fail
// fail only their resources, and a pipeline deleted since it was listed
// has none.
func (p *gitlabPull) pullPipelines(ctx context.Context, base, fullPath, scope string) error {
	var (
		since, jobsSince  time.Time
		anyFrom, jobsFrom bool
		wantJobs          = p.wantsAny(gitlabAttemptResources)
	)
	for _, resource := range gitlabRunResources {
		if !p.want[resource] {
			continue
		}
		from, _ := p.request.From(resource, scope)
		if !anyFrom || from.Before(since) {
			since, anyFrom = from, true
		}
		if resource != gitlabPipelines && (!jobsFrom || from.Before(jobsSince)) {
			jobsSince, jobsFrom = from, true
		}
	}

	// GitLab lists child pipelines, which a parent's trigger jobs make, only
	// when asked for them alone, but sends their events like any other's.
	var ids []string
	manual := gitlabManual{}
	for _, children := range []bool{false, true} {
		query := changedSince(since)
		if children {
			query.Set("source", "parent_pipeline")
		}
		var (
			pipelines []map[string]any
			listedAt  time.Time
		)
		err := p.read(ctx, listing{path: base + "pipelines?" + query.Encode()}, func(objects []map[string]any, at time.Time) error {
			for _, pipeline := range objects {
				if id := idOf(pipeline["id"]); wantJobs && id != "" && !timeOf(pipeline, "updated_at").Before(jobsSince) {
					ids = append(ids, id)
				}
				pipelines = append(pipelines, gitlabShape(pipeline, gitlabPipelineFields))
			}
			listedAt = earliest(listedAt, at)
			return nil
		})
		if err != nil {
			return err
		}
		if !p.want[gitlabPipelines] || len(pipelines) == 0 {
			continue
		}
		timesCtx, clock := withSourceClock(ctx)
		var read gitlabManual
		pipelines, read, err = gitlabWithTimes(timesCtx, p.client, base, fullPath, since, pipelines, children)
		maps.Copy(manual, read)
		if err == nil {
			err = p.emitObjects(gitlabPipelines, pipelines, map[string]any{"project": fullPath}, source{scope: scope, at: earliest(listedAt, clock.take())})
		}
		if err := p.fail(ctx, []string{gitlabPipelines}, fullPath, scope, err); err != nil {
			return err
		}
	}
	if !wantJobs {
		return nil
	}

	return p.fail(ctx, gitlabAttemptResources, fullPath, scope, p.pullAttempts(ctx, base, fullPath, scope, ids, manual))
}

// pullAttempts reads the attempts of the project's pipelines with ids, a
// few pipelines at a time, with what they waited for, given the manual
// jobs of those whose times said them and read for the rest, and emits
// them once all are read, as both resources, so that across the project
// the newest is written last, and notes their runners, which are read once
// every project's are.
func (p *gitlabPull) pullAttempts(ctx context.Context, base, fullPath, scope string, ids []string, manual gitlabManual) error {
	var (
		mu         sync.Mutex
		byPipeline = map[string][]map[string]any{}
		readAt     time.Time
	)
	err := forEach(ids, apiConcurrency, func(id string) error {
		readCtx, clock := withSourceClock(ctx)
		read, err := gitlabAttempts(readCtx, p.client, base, id, fullPath)
		mu.Lock()
		defer mu.Unlock()
		byPipeline[id] = read
		readAt = earliest(readAt, clock.take())
		return skipGone(err)
	})
	if err != nil {
		return err
	}
	unknown := slices.DeleteFunc(slices.Sorted(maps.Keys(byPipeline)), func(id string) bool { return manual[id] != nil || len(byPipeline[id]) == 0 })
	if len(unknown) > 0 {
		readCtx, clock := withSourceClock(ctx)
		read, err := gitlabManualJobs(readCtx, p.client, fullPath, unknown)
		if err != nil {
			return err
		}
		readAt = earliest(readAt, clock.take())
		maps.Copy(manual, read)
	}
	var attempts []map[string]any
	for id, read := range byPipeline {
		gitlabWaits(read, manual[id])
		attempts = append(attempts, read...)
	}
	gitlabOldestFirst(attempts)
	p.runners.add(attempts, readAt)
	from := source{scope: scope, at: readAt}
	if err := p.emitObjects(gitlabJobAttempts, attempts, nil, from); err != nil {
		return err
	}

	return p.emitObjects(gitlabJobs, gitlabLatest(attempts), nil, from)
}

// gitlabScriptFields are what a job runs, left out of CI configs, since
// they're long and may hold what shouldn't leave GitLab.
var gitlabScriptFields = []string{"before_script", "script", "after_script"}

// gitlabNoCIConfig is what GitLab's lint says of a project without a CI
// configuration file, which has no CI config rather than an invalid one.
const gitlabNoCIConfig = "Please provide content of .gitlab-ci.yml"

// gitlabCIConfig lints the project's CI configuration on its default
// branch, giving whether it's valid, its problems, the files it includes
// and its jobs without their scripts, or nil for a project without one.
func gitlabCIConfig(ctx context.Context, client *gitlabClient, base string) (map[string]any, error) {
	var lint map[string]any
	if _, err := client.get(ctx, base+"ci/lint?include_jobs=true", &lint); err != nil {
		return nil, err
	}
	if errs, _ := lint["errors"].([]any); len(errs) == 1 && errs[0] == gitlabNoCIConfig {
		return nil, nil
	}
	jobs, _ := lint["jobs"].([]any)
	for _, job := range jobs {
		if fields, ok := job.(map[string]any); ok {
			for _, field := range gitlabScriptFields {
				delete(fields, field)
			}
		}
	}
	if jobs == nil {
		jobs = []any{}
	}

	return map[string]any{
		"valid": lint["valid"], "errors": lint["errors"], "warnings": lint["warnings"], "includes": lint["includes"], "jobs": jobs,
	}, nil
}

// read reads a listing, handing page each page's objects with when GitLab
// answered with them.
func (p *gitlabPull) read(ctx context.Context, l listing, page func([]map[string]any, time.Time) error) error {
	ctx, clock := withSourceClock(ctx)
	return p.client.list(ctx, l.path, l.pages(func(objects []map[string]any) error {
		// Reads made while handing a page on aren't the page's.
		defer clock.take()
		return page(objects, clock.take())
	}))
}
