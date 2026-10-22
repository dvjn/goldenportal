// Package sonarqube reads projects from SonarQube's web API, each with its
// quality gate status and a few measures.
package sonarqube

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/dvjn/goldport/internal/connector"
)

const (
	KindProject = "project"

	pageSize = 100
)

// Metrics are the measures each project record carries, by SonarQube
// metric key.
var Metrics = []string{"coverage", "bugs", "vulnerabilities", "code_smells"}

// config names the projects to read in Projects; without it the connector
// reads every project the token can browse.
type config struct {
	URL      string   `json:"url"`
	Projects []string `json:"projects"`
}

func readConfig(raw []byte) (config, error) {
	var read config
	if err := connector.DecodeConfig(raw, &read); err != nil {
		return config{}, err
	}
	parsed, err := url.Parse(read.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return config{}, errors.New("give the config a url, the address of the SonarQube server such as https://sonarqube.example.com")
	}
	read.URL = strings.TrimRight(read.URL, "/")

	return read, nil
}

type Connector struct {
	http *http.Client
}

func New(client *http.Client) *Connector {
	return &Connector{http: client}
}

func (*Connector) Kinds() []string { return []string{KindProject} }

func (c *Connector) Fetch(ctx context.Context, request connector.Request) (connector.Records, error) {
	read, err := readConfig(request.Config)
	if err != nil {
		return nil, err
	}
	token, err := request.Secret("token")
	if err != nil {
		return nil, err
	}
	source := sonarqube{
		client: connector.Client{Source: "SonarQube", HTTP: c.http, Header: http.Header{"Authorization": {"Bearer " + token}}},
		base:   read.URL,
	}
	if !request.Wants(KindProject) {
		return connector.Records{}, nil
	}

	components, err := source.projects(ctx)
	if err != nil {
		return nil, err
	}
	projects := []any{}
	for _, component := range components {
		if len(read.Projects) > 0 && !slices.Contains(read.Projects, component.Key) {
			continue
		}
		project, err := source.project(ctx, component)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}

	return connector.Records{KindProject: projects}, nil
}

type sonarqube struct {
	client connector.Client
	base   string
}

type component struct {
	Key              string `json:"key"`
	Name             string `json:"name"`
	LastAnalysisDate string `json:"lastAnalysisDate"`
}

type componentPage struct {
	Paging struct {
		PageIndex int `json:"pageIndex"`
		PageSize  int `json:"pageSize"`
		Total     int `json:"total"`
	} `json:"paging"`
	Components []component `json:"components"`
}

// projects reads every page of projects.
func (s sonarqube) projects(ctx context.Context) ([]component, error) {
	var all []component
	for page := 1; ; page++ {
		query := url.Values{"qualifiers": {"TRK"}, "p": {strconv.Itoa(page)}, "ps": {strconv.Itoa(pageSize)}}
		var read componentPage
		if _, err := s.client.Get(ctx, s.base+"/api/components/search?"+query.Encode(), &read); err != nil {
			return nil, err
		}
		all = append(all, read.Components...)
		if len(read.Components) == 0 || page*pageSize >= read.Paging.Total {
			return all, nil
		}
	}
}

// project reads the project's quality gate status and measures. A measure
// the project has no value for is left out.
func (s sonarqube) project(ctx context.Context, project component) (map[string]any, error) {
	var gate struct {
		ProjectStatus struct {
			Status string `json:"status"`
		} `json:"projectStatus"`
	}
	if _, err := s.client.Get(ctx, s.base+"/api/qualitygates/project_status?"+url.Values{"projectKey": {project.Key}}.Encode(), &gate); err != nil {
		return nil, err
	}
	var measured struct {
		Component struct {
			Measures []struct {
				Metric string `json:"metric"`
				Value  string `json:"value"`
			} `json:"measures"`
		} `json:"component"`
	}
	query := url.Values{"component": {project.Key}, "metricKeys": {strings.Join(Metrics, ",")}}
	if _, err := s.client.Get(ctx, s.base+"/api/measures/component?"+query.Encode(), &measured); err != nil {
		return nil, err
	}
	measures := map[string]any{}
	for _, measure := range measured.Component.Measures {
		if value, err := strconv.ParseFloat(measure.Value, 64); err == nil {
			measures[measure.Metric] = value
		}
	}

	return map[string]any{
		"key":              project.Key,
		"name":             project.Name,
		"lastAnalysisDate": project.LastAnalysisDate,
		"url":              s.base + "/dashboard?" + url.Values{"id": {project.Key}}.Encode(),
		"qualityGate":      gate.ProjectStatus.Status,
		"measures":         measures,
	}, nil
}
