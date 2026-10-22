// Package gitlab reads a GitLab group through GitLab's REST API v4: the
// group with its subgroups, their projects, and the latest pipeline of each
// project's default branch.
package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dvjn/goldport/internal/connector"
)

const (
	KindGroup    = "group"
	KindProject  = "project"
	KindPipeline = "pipeline"

	pageSize = "100"
)

type config struct {
	URL   string `json:"url"`
	Group string `json:"group"`
}

func readConfig(raw []byte) (config, error) {
	var read config
	if err := connector.DecodeConfig(raw, &read); err != nil {
		return config{}, err
	}
	parsed, err := url.Parse(read.URL)
	switch {
	case err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "":
		return config{}, errors.New("give the config a url, the address of the GitLab instance such as https://gitlab.example.com")
	case strings.Trim(read.Group, "/") == "":
		return config{}, errors.New("give the config a group, the full path of the GitLab group to read such as acme/platform")
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

func (*Connector) Kinds() []string { return []string{KindGroup, KindProject, KindPipeline} }

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
	}

	records := connector.Records{}
	if request.Wants(KindGroup) {
		if records[KindGroup], err = source.groups(ctx); err != nil {
			return nil, err
		}
	}
	if !request.Wants(KindProject) && !request.Wants(KindPipeline) {
		return records, nil
	}
	projects, err := source.pages(ctx, "/groups/"+source.group+"/projects", url.Values{
		"include_subgroups": {"true"},
		"order_by":          {"id"},
		"sort":              {"asc"},
	})
	if err != nil {
		return nil, err
	}
	if request.Wants(KindProject) {
		records[KindProject] = projects
	}
	if request.Wants(KindPipeline) {
		if records[KindPipeline], err = source.latestPipelines(ctx, projects); err != nil {
			return nil, err
		}
	}

	return records, nil
}

type gitlab struct {
	client connector.Client
	api    string
	group  string
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

// latestPipelines returns the newest pipeline of each project's default
// branch, with the project it belongs to under project.
func (g gitlab) latestPipelines(ctx context.Context, projects []any) ([]any, error) {
	pipelines := []any{}
	for _, item := range projects {
		project, _ := item.(map[string]any)
		id, hasID := project["id"].(float64)
		branch, _ := project["default_branch"].(string)
		if !hasID || branch == "" {
			continue
		}
		query := url.Values{"ref": {branch}, "per_page": {"1"}, "order_by": {"id"}, "sort": {"desc"}}
		var latest []map[string]any
		address := g.api + "/projects/" + strconv.FormatFloat(id, 'f', -1, 64) + "/pipelines?" + query.Encode()
		if _, err := g.client.Get(ctx, address, &latest); err != nil {
			return nil, err
		}
		for _, pipeline := range latest {
			pipeline["project"] = map[string]any{
				"id":                  project["id"],
				"name":                project["name"],
				"path_with_namespace": project["path_with_namespace"],
				"web_url":             project["web_url"],
			}
			pipelines = append(pipelines, pipeline)
		}
	}

	return pipelines, nil
}
