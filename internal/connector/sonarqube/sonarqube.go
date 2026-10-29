// Package sonarqube reads SonarQube's web API: projects, each with its
// measures, quality gate and DevOps platform binding, and the quality gates
// with their conditions.
package sonarqube

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/connector"
	"github.com/dvjn/goldport/internal/definition"
)

const (
	KindProject     = "project"
	KindQualityGate = "qualityGate"

	pageSize          = 500
	measuresBatchSize = 100
	fanOutWorkers     = 8
)

// uuidSuffix is the suffix SonarQube adds to the key of a project it imports
// from a DevOps platform.
var uuidSuffix = regexp.MustCompile(`_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func readConfig(raw []byte) (definition.SonarQubeConfig, error) {
	var read definition.SonarQubeConfig
	if err := definition.DecodeConfig(raw, &read); err != nil {
		return definition.SonarQubeConfig{}, err
	}
	parsed, err := url.Parse(read.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return definition.SonarQubeConfig{}, errors.New("give the config a url, the address of the SonarQube server such as https://sonarqube.example.com")
	}
	read.URL = strings.TrimRight(read.URL, "/")
	if read.Metrics == nil {
		read.Metrics = definition.DefaultSonarQubeMetrics
	}

	return read, nil
}

type Connector struct {
	http *http.Client
}

func New(client *http.Client) *Connector {
	return &Connector{http: client}
}

func (*Connector) Kinds() []string { return []string{KindProject, KindQualityGate} }

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
		config: read,
	}

	records := connector.Records{}
	if request.Wants(KindQualityGate) {
		if records[KindQualityGate], err = source.qualityGates(ctx); err != nil {
			return nil, err
		}
	}
	if request.Wants(KindProject) {
		if records[KindProject], err = source.projects(ctx); err != nil {
			return nil, err
		}
	}

	return records, nil
}

type sonarqube struct {
	client connector.Client
	base   string
	config definition.SonarQubeConfig
}

func (s sonarqube) get(ctx context.Context, path string, query url.Values, target any) error {
	_, err := s.client.Get(ctx, s.base+"/api/"+path+"?"+query.Encode(), target)
	return err
}

type paging struct {
	PageIndex int `json:"pageIndex"`
	PageSize  int `json:"pageSize"`
	Total     int `json:"total"`
}

// projectList reads every page of projects the config names, each with
// keyWithoutSuffix and url.
func (s sonarqube) projectList(ctx context.Context) ([]map[string]any, error) {
	query := url.Values{"f": {"analysisDate,leakPeriodDate"}, "ps": {strconv.Itoa(pageSize)}}
	if s.config.ProjectFilter != "" {
		query.Set("filter", s.config.ProjectFilter)
	}
	var projects []map[string]any
	for page := 1; ; page++ {
		query.Set("p", strconv.Itoa(page))
		var read struct {
			Paging     paging           `json:"paging"`
			Components []map[string]any `json:"components"`
		}
		if err := s.get(ctx, "components/search_projects", query, &read); err != nil {
			return nil, err
		}
		for _, project := range read.Components {
			key, _ := project["key"].(string)
			if len(s.config.Projects) > 0 && !slices.Contains(s.config.Projects, key) {
				continue
			}
			for _, field := range []string{"analysisDate", "leakPeriodDate"} {
				if text, ok := project[field].(string); ok {
					project[field] = rfc3339(text)
				}
			}
			project["keyWithoutSuffix"] = uuidSuffix.ReplaceAllString(key, "")
			project["url"] = s.base + "/dashboard?" + url.Values{"id": {key}}.Encode()
			projects = append(projects, project)
		}
		if len(read.Components) == 0 || page*pageSize >= read.Paging.Total {
			return projects, nil
		}
	}
}

// projects reads each project with its measures by metric key, its quality
// gate with the project's status under status, and its binding to a DevOps
// platform when it has one.
func (s sonarqube) projects(ctx context.Context) ([]any, error) {
	projects, err := s.projectList(ctx)
	if err != nil {
		return nil, err
	}
	measures, err := s.measures(ctx, projects)
	if err != nil {
		return nil, err
	}

	return connector.EachConcurrently(ctx, projects, fanOutWorkers, func(ctx context.Context, project map[string]any) ([]any, error) {
		key, _ := project["key"].(string)
		project["measures"] = measures[key]
		gate, err := s.projectGate(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", key, err)
		}
		if gate != nil {
			project["qualityGate"] = gate
		}
		binding, err := s.binding(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", key, err)
		}
		if binding != nil {
			project["binding"] = binding
		}

		return []any{project}, nil
	})
}

type measure struct {
	Metric    string `json:"metric"`
	Component string `json:"component"`
	Value     string `json:"value"`
	Period    struct {
		Value string `json:"value"`
	} `json:"period"`
}

// value is the measure as a number when it is one, such as coverage, and as
// text otherwise, such as alert_status. A measure of new code has its value
// in the period.
func (m measure) value() any {
	text := m.Value
	if text == "" {
		text = m.Period.Value
	}
	if number, err := strconv.ParseFloat(text, 64); err == nil {
		return number
	}

	return text
}

// measures reads the configured metrics of every project, by project key.
func (s sonarqube) measures(ctx context.Context, projects []map[string]any) (map[string]map[string]any, error) {
	byProject := map[string]map[string]any{}
	keys := make([]string, 0, len(projects))
	for _, project := range projects {
		key, _ := project["key"].(string)
		keys = append(keys, key)
		byProject[key] = map[string]any{}
	}
	if len(s.config.Metrics) == 0 || len(keys) == 0 {
		return byProject, nil
	}
	if err := s.checkMetrics(ctx); err != nil {
		return nil, err
	}
	for batch := range slices.Chunk(keys, measuresBatchSize) {
		query := url.Values{"projectKeys": {strings.Join(batch, ",")}, "metricKeys": {strings.Join(s.config.Metrics, ",")}}
		var read struct {
			Measures []measure `json:"measures"`
		}
		if err := s.get(ctx, "measures/search", query, &read); err != nil {
			return nil, err
		}
		for _, item := range read.Measures {
			if values, known := byProject[item.Component]; known {
				values[item.Metric] = item.value()
			}
		}
	}

	return byProject, nil
}

// checkMetrics refuses metrics the server does not know, which SonarQube
// would otherwise answer with an error that does not name them.
func (s sonarqube) checkMetrics(ctx context.Context) error {
	known := map[string]bool{}
	for page := 1; ; page++ {
		var read struct {
			Total   int `json:"total"`
			Metrics []struct {
				Key string `json:"key"`
			} `json:"metrics"`
		}
		if err := s.get(ctx, "metrics/search", url.Values{"p": {strconv.Itoa(page)}, "ps": {strconv.Itoa(pageSize)}}, &read); err != nil {
			return err
		}
		for _, metric := range read.Metrics {
			known[metric.Key] = true
		}
		if len(read.Metrics) == 0 || page*pageSize >= read.Total {
			break
		}
	}
	unknown := slices.DeleteFunc(slices.Clone(s.config.Metrics), func(metric string) bool { return known[metric] })
	if len(unknown) > 0 {
		return fmt.Errorf("SonarQube does not know the configured metrics %s", strings.Join(unknown, ", "))
	}

	return nil
}

// projectGate reads the quality gate the project uses, with the project's
// status against it.
func (s sonarqube) projectGate(ctx context.Context, key string) (map[string]any, error) {
	var gate struct {
		QualityGate map[string]any `json:"qualityGate"`
	}
	err := s.get(ctx, "qualitygates/get_by_project", url.Values{"project": {key}}, &gate)
	if err != nil && !isNotFound(err) {
		return nil, err
	}
	if gate.QualityGate == nil {
		return nil, nil
	}
	var status struct {
		ProjectStatus struct {
			Status string `json:"status"`
		} `json:"projectStatus"`
	}
	if err := s.get(ctx, "qualitygates/project_status", url.Values{"projectKey": {key}}, &status); err != nil && !isNotFound(err) {
		return nil, err
	}
	gate.QualityGate["status"] = status.ProjectStatus.Status

	return gate.QualityGate, nil
}

// binding reads the project's binding to a DevOps platform, such as the
// GitLab project it analyses, or nil when it has none.
func (s sonarqube) binding(ctx context.Context, key string) (map[string]any, error) {
	var binding map[string]any
	err := s.get(ctx, "alm_settings/get_binding", url.Values{"project": {key}}, &binding)
	if isNotFound(err) {
		return nil, nil
	}

	return binding, err
}

// qualityGates reads every quality gate with its conditions.
func (s sonarqube) qualityGates(ctx context.Context) ([]any, error) {
	var list struct {
		QualityGates []map[string]any `json:"qualitygates"`
	}
	if err := s.get(ctx, "qualitygates/list", url.Values{}, &list); err != nil {
		return nil, err
	}
	gates := make([]any, 0, len(list.QualityGates))
	for _, gate := range list.QualityGates {
		name, _ := gate["name"].(string)
		details := struct {
			Conditions []any `json:"conditions"`
		}{Conditions: []any{}}
		if err := s.get(ctx, "qualitygates/show", url.Values{"name": {name}}, &details); err != nil {
			return nil, fmt.Errorf("quality gate %s: %w", name, err)
		}
		gate["conditions"] = details.Conditions
		gates = append(gates, gate)
	}

	return gates, nil
}

// sonarTime is how SonarQube writes times, with a zone offset that has no
// colon, which RFC 3339 requires.
const sonarTime = "2006-01-02T15:04:05-0700"

// rfc3339 rewrites a SonarQube time in RFC 3339, and keeps any other text.
func rfc3339(text string) string {
	at, err := time.Parse(sonarTime, text)
	if err != nil {
		return text
	}

	return at.Format(time.RFC3339)
}

func isNotFound(err error) bool {
	var status connector.StatusError
	return errors.As(err, &status) && status.Code == http.StatusNotFound
}
