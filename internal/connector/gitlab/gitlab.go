// Package gitlab reads a GitLab group through GitLab's REST API v4: the
// group with its subgroups, their projects, and for each project the latest
// pipeline and the CI configuration.
package gitlab

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/dvjn/goldport/internal/connector"
	"github.com/dvjn/goldport/internal/definition"
)

const (
	KindGroup    = "group"
	KindProject  = "project"
	KindPipeline = "pipeline"
	KindCIConfig = "ciConfig"

	pageSize      = "100"
	fanOutWorkers = 8
)

func readConfig(raw []byte) (definition.GitLabConfig, error) {
	var read definition.GitLabConfig
	if err := definition.DecodeConfig(raw, &read); err != nil {
		return definition.GitLabConfig{}, err
	}
	parsed, err := url.Parse(read.URL)
	switch {
	case err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "":
		return definition.GitLabConfig{}, errors.New("give the config a url, the address of the GitLab instance such as https://gitlab.example.com")
	case strings.Trim(read.Group, "/") == "":
		return definition.GitLabConfig{}, errors.New("give the config a group, the full path of the GitLab group to read such as acme/platform")
	}
	read.URL = strings.TrimRight(read.URL, "/")
	read.Group = strings.Trim(read.Group, "/")

	return read, nil
}

type Connector struct {
	http *http.Client
}

func New(client *http.Client) *Connector {
	return &Connector{http: client}
}

func (*Connector) Kinds() []string {
	return []string{KindGroup, KindProject, KindPipeline, KindCIConfig}
}

func (c *Connector) Fetch(ctx context.Context, request connector.Request) (connector.Records, error) {
	read, err := readConfig(request.Config)
	if err != nil {
		return nil, err
	}
	token, err := request.Secret("token")
	if err != nil {
		return nil, err
	}
	source := gitlab{
		client: connector.Client{Source: "GitLab", HTTP: c.http, Header: http.Header{"Private-Token": {token}}},
		api:    read.URL + "/api/v4",
		group:  url.PathEscape(read.Group),
		config: read,
	}

	records := connector.Records{}
	if request.Wants(KindGroup) {
		if records[KindGroup], err = source.groups(ctx); err != nil {
			return nil, err
		}
	}
	if !slices.ContainsFunc([]string{KindProject, KindPipeline, KindCIConfig}, request.Wants) {
		return records, nil
	}
	projects, err := source.projects(ctx)
	if err != nil {
		return nil, err
	}
	if request.Wants(KindProject) {
		records[KindProject] = projects
	}
	perProject := map[string]projectRead{KindPipeline: source.latestPipeline, KindCIConfig: source.ciConfig}
	for kind, read := range perProject {
		if request.Wants(kind) {
			if records[kind], err = source.eachProject(ctx, projects, read); err != nil {
				return nil, err
			}
		}
	}

	return records, nil
}

type gitlab struct {
	client connector.Client
	api    string
	group  string
	config definition.GitLabConfig
}

// groups returns the group and every group under it.
func (g gitlab) groups(ctx context.Context) ([]any, error) {
	var group any
	if _, err := g.client.Get(ctx, g.api+"/groups/"+g.group+"?with_projects=false", &group); err != nil {
		return nil, err
	}
	descendants, err := g.pages(ctx, "/groups/"+g.group+"/descendant_groups", url.Values{})
	if err != nil {
		return nil, err
	}

	return append([]any{group}, descendants...), nil
}

// projects returns the projects of the group and its subgroups that the
// config does not leave out.
func (g gitlab) projects(ctx context.Context) ([]any, error) {
	query := url.Values{"include_subgroups": {"true"}, "order_by": {"id"}, "sort": {"asc"}}
	if g.config.Projects.ExcludeArchived {
		query.Set("archived", "false")
	}
	projects, err := g.pages(ctx, "/groups/"+g.group+"/projects", query)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(projects, func(item any) bool {
		project, _ := item.(map[string]any)
		return !g.includes(project)
	}), nil
}

func (g gitlab) includes(project map[string]any) bool {
	filter := g.config.Projects
	path, _ := project["path_with_namespace"].(string)
	markedForDeletion := project["marked_for_deletion_on"] != nil || project["marked_for_deletion_at"] != nil

	return !(filter.ExcludeArchived && project["archived"] == true) &&
		!(filter.ExcludeMarkedForDeletion && markedForDeletion) &&
		!(filter.ExcludeEmpty && project["empty_repo"] == true) &&
		(len(filter.PathPrefixes) == 0 || slices.ContainsFunc(filter.PathPrefixes, func(prefix string) bool {
			return strings.HasPrefix(path, prefix)
		}))
}

// pages reads every page of a list, following the next links GitLab gives
// within the instance.
func (g gitlab) pages(ctx context.Context, path string, query url.Values) ([]any, error) {
	query.Set("per_page", pageSize)
	address := g.api + path + "?" + query.Encode()
	var all []any
	for address != "" {
		var page []any
		header, err := g.client.Get(ctx, address, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		address = nextLink(header)
		if address != "" && !strings.HasPrefix(address, g.api+"/") {
			return nil, fmt.Errorf("GitLab gave a next page outside %s", g.api)
		}
	}

	return all, nil
}

// nextLink reads the rel="next" address of a Link header, RFC 8288.
func nextLink(header http.Header) string {
	for _, link := range strings.Split(header.Get("Link"), ",") {
		target, params, found := strings.Cut(link, ";")
		if found && strings.Contains(params, `rel="next"`) {
			return strings.Trim(strings.TrimSpace(target), "<>")
		}
	}

	return ""
}

// projectRead reads the records of one kind that belong to a project.
type projectRead func(ctx context.Context, project map[string]any) ([]map[string]any, error)

// eachProject reads every project that has a repository, and marks each
// record with the project it belongs to: projectId, projectPath and
// projectTopics, and the project's id, name, path and address under project.
func (g gitlab) eachProject(ctx context.Context, projects []any, read projectRead) ([]any, error) {
	withRepository := slices.DeleteFunc(slices.Clone(projects), func(item any) bool {
		project, _ := item.(map[string]any)
		return project["empty_repo"] == true
	})

	return connector.EachConcurrently(ctx, withRepository, fanOutWorkers, func(ctx context.Context, item any) ([]any, error) {
		project, _ := item.(map[string]any)
		records, err := read(ctx, project)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", project["path_with_namespace"], err)
		}
		marked := make([]any, 0, len(records))
		for _, record := range records {
			record["projectId"] = project["id"]
			record["projectPath"] = project["path_with_namespace"]
			record["projectTopics"] = project["topics"]
			record["project"] = map[string]any{
				"id":                  project["id"],
				"name":                project["name"],
				"path_with_namespace": project["path_with_namespace"],
				"web_url":             project["web_url"],
			}
			marked = append(marked, record)
		}

		return marked, nil
	})
}

func (g gitlab) projectPath(project map[string]any) string {
	id, _ := project["id"].(float64)
	return g.api + "/projects/" + strconv.FormatFloat(id, 'f', -1, 64)
}

// latestPipeline reads the latest pipeline of the configured ref or the
// project's default branch, which carries finished_at.
func (g gitlab) latestPipeline(ctx context.Context, project map[string]any) ([]map[string]any, error) {
	branch, _ := project["default_branch"].(string)
	ref := cmp.Or(g.config.PipelineRef, branch)
	if ref == "" {
		return nil, nil
	}
	var pipeline map[string]any
	_, err := g.client.Get(ctx, g.projectPath(project)+"/pipelines/latest?"+url.Values{"ref": {ref}}.Encode(), &pipeline)

	return ignoreUnavailable([]map[string]any{pipeline}, err)
}

var jobScriptFields = []string{"before_script", "script", "after_script"}

// ciConfig lints the project's CI configuration with its jobs, leaving out
// the jobs' scripts. A project with CI/CD turned off has none.
func (g gitlab) ciConfig(ctx context.Context, project map[string]any) ([]map[string]any, error) {
	if project["jobs_enabled"] == false || project["builds_access_level"] == "disabled" {
		return nil, nil
	}
	var lint map[string]any
	if _, err := g.client.Get(ctx, g.projectPath(project)+"/ci/lint?include_jobs=true", &lint); err != nil {
		return ignoreUnavailable(nil, err)
	}
	jobs, _ := lint["jobs"].([]any)
	for _, job := range jobs {
		if fields, ok := job.(map[string]any); ok {
			for _, field := range jobScriptFields {
				delete(fields, field)
			}
		}
	}

	return []map[string]any{{
		"valid":    lint["valid"],
		"errors":   lint["errors"],
		"warnings": lint["warnings"],
		"includes": lint["includes"],
		"jobs":     jobs,
	}}, nil
}

// ignoreUnavailable reads a project's missing or forbidden resource as no
// records.
func ignoreUnavailable(records []map[string]any, err error) ([]map[string]any, error) {
	var status connector.StatusError
	if errors.As(err, &status) && (status.Code == http.StatusNotFound || status.Code == http.StatusForbidden) {
		return nil, nil
	}

	return records, err
}
