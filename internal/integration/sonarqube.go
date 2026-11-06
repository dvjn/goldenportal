package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// SonarQubeConfig reads a SonarQube instance's projects, each with its
// quality gate, its measures and the repository it's bound to.
type SonarQubeConfig struct {
	// URL is the instance's address, whose web API is under /api.
	URL   string
	Token HeaderValue
	// Projects are patterns of the project keys to read, * matching any
	// run of characters; none reads every project the token can browse.
	Projects []string
}

const sonarProjects = "projects"

// SonarQubeConfigFields are the keys the sonarqube connector's config takes,
// exported so the documents' JSON Schema can be checked against them.
var SonarQubeConfigFields = []string{"url", "token", "projects"}

// sonarMetrics are the measures read for each project: its gate, the size
// and health of its code overall and of its new code, and its hotspots.
var sonarMetrics = []string{
	"alert_status", "ncloc", "coverage", "new_coverage", "duplicated_lines_density", "new_duplicated_lines_density",
	"bugs", "vulnerabilities", "code_smells", "new_violations", "security_hotspots", "security_hotspots_reviewed",
	"reliability_rating", "security_rating", "sqale_rating",
}

// sonarMeasuresBatch is how many projects one measures request asks about,
// SonarQube's limit.
const sonarMeasuresBatch = 100

func parseSonarQubeConfig(r *configdoc.Reader, node *yaml.Node, path string) Config {
	if node == nil {
		return nil
	}
	config := &SonarQubeConfig{}
	fields, ok := r.Fields(node, path, SonarQubeConfigFields...)
	if !ok {
		return config
	}
	if n := fields["url"]; n != nil {
		config.URL = strings.TrimRight(r.Text(n, path+".url"), "/")
		u, err := url.Parse(config.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			r.Errorf(n, path+".url", "url must be the instance's http or https address without credentials, a query or a fragment, such as https://sonarqube.example.com")
		}
	} else {
		r.Errorf(node, path+".url", "add the instance's url, such as https://sonarqube.example.com")
	}
	if n := fields["token"]; n != nil {
		config.Token, _ = readSecret(r, n, path+".token")
	} else {
		r.Errorf(node, path+".token", "add a token, as {env: %s...} holding a user token whose user can browse the projects", SecretEnvPrefix)
	}
	config.Projects = readNames(r, fields, path, "projects", map[string]bool{}, sonarPattern,
		"a project key, or a pattern of them with *, such as acme_*")

	return config
}

// sonarPattern reports whether p is a project key or a pattern of them.
func sonarPattern(p string) bool {
	_, err := path.Match(p, "")
	return p != "" && err == nil && !strings.ContainsFunc(p, unicode.IsSpace)
}

func (c *SonarQubeConfig) Resources() []string { return []string{sonarProjects} }

func (c *SonarQubeConfig) Pulls() []string { return []string{sonarProjects} }

func newSonarQubeClient(config *SonarQubeConfig) (*apiClient, error) {
	api, err := newAPIClient("SonarQube", config.URL+"/api")
	if err != nil {
		return nil, err
	}
	token := config.Token.Resolve()
	if token == "" {
		return nil, fmt.Errorf("%s isn't set on the server, so the connector can't sign in to SonarQube", config.Token.Env)
	}
	cred := credential{header: "Authorization", value: "Bearer " + token, bucket: config.URL}
	api.headers = map[string]string{"Accept": "application/json", "User-Agent": "golden-portal"}
	api.authorize = func(context.Context, string) (credential, error) { return cred, nil }
	api.message = sonarMessage
	api.limited = sonarLimited

	return api, nil
}

// sonarLimited reads SonarQube's 429s, which hold back the token's every
// request, since the limit is the user's.
func sonarLimited(status int, header http.Header, _ []byte, attempt int) *rateLimit {
	if status != http.StatusTooManyRequests {
		return nil
	}
	wait := httpx.RetryAfter(header.Get("Retry-After"))
	if wait == 0 {
		wait = retryBackoff << attempt
	}

	return &rateLimit{wait: wait, shared: true}
}

// sonarMessage is the first of the errors SonarQube answers a refusal with.
func sonarMessage(body []byte) string {
	var answer struct {
		Errors []struct {
			Msg string `json:"msg"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &answer) != nil || len(answer.Errors) == 0 {
		return ""
	}

	return answer.Errors[0].Msg
}

// Check checks the token is valid and its user can browse a project.
func (c *SonarQubeConfig) Check(ctx context.Context, _ configdoc.DB, req CheckRequest) error {
	client, err := newSonarQubeClient(c)
	if err != nil {
		return err
	}
	var valid struct {
		Valid bool `json:"valid"`
	}
	if _, err := client.get(ctx, "", "authentication/validate", &valid); err != nil {
		return err
	}
	if !valid.Valid {
		return errors.New("SonarQube doesn't accept the token; make a user token for a user that can browse the projects")
	}
	projects, err := c.projects(ctx, client)
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		req.Warn("The token's user can browse no projects that projects selects, so a sync reads none; give the user Browse on them.")
	}

	return nil
}

// Pull reads the projects, each with its quality gate, its measures and
// the repository it's bound to, under _gp.
func (c *SonarQubeConfig) Pull(ctx context.Context, _ configdoc.DB, p PullRequest,
	emit func(resource string, items []Item) error,
) (map[string]error, error) {
	failed := func(err error) (map[string]error, error) { return map[string]error{sonarProjects: err}, nil }
	client, err := newSonarQubeClient(c)
	if err != nil {
		return failed(err)
	}
	projects, err := c.projects(ctx, client)
	if err != nil {
		return failed(err)
	}
	measures, err := sonarMeasures(ctx, client, projects)
	if err != nil {
		return failed(err)
	}
	items := make([]Item, 0, len(projects))
	for _, project := range projects {
		key, _ := project["key"].(string)
		gate, err := sonarGate(ctx, client, key)
		if err != nil {
			return failed(err)
		}
		repository, err := sonarRepository(ctx, client, key)
		if err != nil {
			return failed(err)
		}
		project[parentKey] = map[string]any{"measures": measures[key], "qualityGate": gate, "repository": repository}
		items = append(items, Item{Resource: sonarProjects, Object: project})
	}

	return nil, emit(sonarProjects, items)
}

// projects lists the projects the token can browse that Projects selects.
func (c *SonarQubeConfig) projects(ctx context.Context, client *apiClient) ([]map[string]any, error) {
	var out []map[string]any
	for page := 1; ; page++ {
		if page > DefaultMaxPages {
			return nil, fmt.Errorf("SonarQube lists more than %d pages of projects", DefaultMaxPages)
		}
		var answer struct {
			Paging struct {
				Total int `json:"total"`
			} `json:"paging"`
			Components []map[string]any `json:"components"`
		}
		query := url.Values{"ps": {"500"}, "p": {strconv.Itoa(page)}, "f": {"analysisDate"}}
		if _, err := client.get(ctx, "", "components/search_projects?"+query.Encode(), &answer); err != nil {
			return nil, err
		}
		for _, project := range answer.Components {
			if key, _ := project["key"].(string); c.selects(key) {
				out = append(out, project)
			}
		}
		if len(answer.Components) == 0 || page*500 >= answer.Paging.Total {
			return out, nil
		}
	}
}

func (c *SonarQubeConfig) selects(key string) bool {
	if len(c.Projects) == 0 {
		return true
	}
	for _, p := range c.Projects {
		if ok, _ := path.Match(p, key); ok {
			return true
		}
	}

	return false
}

// sonarMeasures reads the projects' measures, a batch of projects per
// request, as numbers but for the gate's status. A new-code measure is
// its value over the new code period.
func sonarMeasures(ctx context.Context, client *apiClient, projects []map[string]any) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	for start := 0; start < len(projects); start += sonarMeasuresBatch {
		var keys []string
		for _, project := range projects[start:min(start+sonarMeasuresBatch, len(projects))] {
			key, _ := project["key"].(string)
			keys = append(keys, key)
			out[key] = map[string]any{}
		}
		var answer struct {
			Measures []struct {
				Metric    string `json:"metric"`
				Component string `json:"component"`
				Value     string `json:"value"`
				Period    *struct {
					Value string `json:"value"`
				} `json:"period"`
			} `json:"measures"`
		}
		query := url.Values{"projectKeys": {strings.Join(keys, ",")}, "metricKeys": {strings.Join(sonarMetrics, ",")}}
		if _, err := client.get(ctx, "", "measures/search?"+query.Encode(), &answer); err != nil {
			return nil, err
		}
		for _, m := range answer.Measures {
			value := m.Value
			if value == "" && m.Period != nil {
				value = m.Period.Value
			}
			if n, err := strconv.ParseFloat(value, 64); err == nil {
				out[m.Component][m.Metric] = n
			} else {
				out[m.Component][m.Metric] = value
			}
		}
	}

	return out, nil
}

// sonarGate is a project's quality gate status and the conditions behind
// it, or nil for a project never analysed.
func sonarGate(ctx context.Context, client *apiClient, key string) (map[string]any, error) {
	var answer struct {
		ProjectStatus map[string]any `json:"projectStatus"`
	}
	_, err := client.get(ctx, "", "qualitygates/project_status?"+url.Values{"projectKey": {key}}.Encode(), &answer)
	if err := skipGone(err); err != nil {
		return nil, err
	}
	if answer.ProjectStatus == nil || answer.ProjectStatus["status"] == "NONE" {
		return nil, nil
	}

	return map[string]any{"status": answer.ProjectStatus["status"], "conditions": answer.ProjectStatus["conditions"]}, nil
}

// sonarRepository is the repository a project is bound to, as its URL, or
// nil when it's bound to none, or the token's user may not see bindings.
func sonarRepository(ctx context.Context, client *apiClient, key string) (map[string]any, error) {
	var binding map[string]any
	_, err := client.get(ctx, "", "alm_settings/get_binding?"+url.Values{"project": {key}}.Encode(), &binding)
	if status := apiStatus(err); status == http.StatusNotFound || status == http.StatusForbidden {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	address, _ := binding["repositoryUrl"].(string)
	if address == "" {
		return nil, nil
	}

	return map[string]any{"alm": binding["alm"], "url": address}, nil
}
