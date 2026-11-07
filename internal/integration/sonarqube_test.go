package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sonarYAML = `kind: Integration
metadata: {identifier: sonar, title: SonarQube}
spec:
  connector:
    type: sonarqube
    config:
      url: https://sonar.example.com
      token: {env: GP_SECRET_SONAR}
      projects: [acme_*]
  mappings:
    - resource: projects
      blueprint: sonar-project
      entity: {identifier: .key}
`

// sonarFixture is a SonarQube instance with three projects: acme_api,
// analysed and bound to its repository; acme_web, never analysed and bound
// to none; and other_tool, which the config's patterns leave out.
func sonarFixture(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sonar-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		q := r.URL.Query()
		var answer any
		switch r.URL.Path {
		case "/api/authentication/validate":
			answer = map[string]any{"valid": true}
		case "/api/components/search_projects":
			answer = map[string]any{"paging": map[string]any{"total": 3}, "components": []any{
				map[string]any{"key": "acme_api", "name": "api", "analysisDate": "2026-11-01T10:00:00+0000"},
				map[string]any{"key": "acme_web", "name": "web"},
				map[string]any{"key": "other_tool", "name": "tool"},
			}}
		case "/api/measures/search":
			if q.Get("projectKeys") != "acme_api,acme_web" {
				t.Errorf("measures asked for %s, want the selected projects", q.Get("projectKeys"))
			}
			answer = map[string]any{"measures": []any{
				map[string]any{"component": "acme_api", "metric": "alert_status", "value": "ERROR"},
				map[string]any{"component": "acme_api", "metric": "coverage", "value": "84.1"},
				map[string]any{"component": "acme_api", "metric": "new_coverage", "period": map[string]any{"value": "61.5"}},
			}}
		case "/api/qualitygates/project_status":
			status := "NONE"
			if q.Get("projectKey") == "acme_api" {
				status = "ERROR"
			}
			answer = map[string]any{"projectStatus": map[string]any{
				"status": status, "conditions": []any{map[string]any{"metricKey": "new_coverage", "status": "ERROR"}},
			}}
		case "/api/alm_settings/get_binding":
			if q.Get("project") != "acme_api" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			answer = map[string]any{"alm": "gitlab", "repositoryUrl": "https://gitlab.com/acme/api"}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(server.Close)
	t.Setenv("GP_SECRET_SONAR", "sonar-token")

	return server
}

func sonarConfig(t *testing.T, url string) *SonarQubeConfig {
	t.Helper()
	spec := mustParse(t, strings.Replace(sonarYAML, "https://sonar.example.com", url, 1))
	config, ok := spec.config.(*SonarQubeConfig)
	if !ok {
		t.Fatalf("config = %T, want the sonarqube connector's", spec.config)
	}

	return config
}

func TestSonarQubeConfigProblems(t *testing.T) {
	for _, c := range []struct{ config, want string }{
		{"      token: {env: GP_SECRET_SONAR}\n", "add the instance's url"},
		{"      url: https://sonar.example.com\n", "add a token"},
		{"      url: sonar.example.com\n      token: {env: GP_SECRET_SONAR}\n", "url must be"},
		{"      url: https://sonar.example.com\n      token: {env: GP_SECRET_SONAR}\n      projects: ['acme_[']\n", "isn't a project key"},
	} {
		source := strings.Replace(sonarYAML, "      url: https://sonar.example.com\n      token: {env: GP_SECRET_SONAR}\n      projects: [acme_*]\n", c.config, 1)
		_, issues := Parse(source)
		if len(issues) != 1 || !strings.Contains(issues[0].Message, c.want) {
			t.Errorf("%q: issues = %+v, want one that says %q", c.config, issues, c.want)
		}
	}
}

func TestSonarQubePull(t *testing.T) {
	config := sonarConfig(t, sonarFixture(t).URL)
	var items []Item
	failed, err := config.Pull(t.Context(), nil, PullRequest{Resources: []string{sonarProjects}, Warn: func(string) {}},
		func(_ string, page []Item) error { items = append(items, page...); return nil })
	if err != nil || len(failed) != 0 {
		t.Fatalf("failed = %v, err = %v", failed, err)
	}
	got, _ := json.Marshal(items)
	for _, want := range []string{
		// The selected projects only, with measures as numbers, a new-code
		// measure from its period, and the status alone left as text.
		`"key":"acme_api"`, `"key":"acme_web"`,
		`"measures":{"alert_status":"ERROR","coverage":84.1,"new_coverage":61.5}`,
		`"qualityGate":{"conditions":[{"metricKey":"new_coverage","status":"ERROR"}],"status":"ERROR"}`,
		`"repository":{"alm":"gitlab","url":"https://gitlab.com/acme/api"}`,
		// Never analysed, and bound to nothing.
		`"_gp":{"measures":{},"qualityGate":null,"repository":null}`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("items = %s\nwant %s", got, want)
		}
	}
	if strings.Contains(string(got), "other_tool") {
		t.Errorf("items = %s, want other_tool left out", got)
	}
}

func TestSonarQubeCheck(t *testing.T) {
	config := sonarConfig(t, sonarFixture(t).URL)
	var warned []string
	if err := config.Check(t.Context(), nil, CheckRequest{Warn: func(w string) { warned = append(warned, w) }}); err != nil || len(warned) != 0 {
		t.Errorf("check = %v, warned %v, want it to pass", err, warned)
	}
	config.Projects = []string{"nothing_*"}
	if err := config.Check(t.Context(), nil, CheckRequest{Warn: func(w string) { warned = append(warned, w) }}); err != nil || len(warned) != 1 {
		t.Errorf("check = %v, warned %v, want a warning that no project is selected", err, warned)
	}
	t.Setenv("GP_SECRET_SONAR", "wrong")
	if err := config.Check(t.Context(), nil, CheckRequest{Warn: func(string) {}}); err == nil {
		t.Error("check passed with a token SonarQube refuses")
	}
}
