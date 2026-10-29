package sonarqube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dvjn/goldport/internal/connector"
)

const token = "squ_test_token"

// fakeSonarQube serves 501 projects over two pages; ledger has a failing
// quality gate, measures and a GitLab binding, the others pass without
// measures or binding.
func fakeSonarQube(t *testing.T) *httptest.Server {
	t.Helper()
	keys := []string{"ledger_0a1b2c3d-0000-4000-8000-000000000000"}
	for index := range 500 {
		keys = append(keys, fmt.Sprintf("service-%03d", index))
	}
	ledger := keys[0]
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/components/search_projects", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter") != "qualifier = TRK" || r.URL.Query().Get("ps") != "500" {
			http.Error(w, "want projects, 500 a page", http.StatusBadRequest)
			return
		}
		page := map[string]int{"1": 0, "2": 1}[r.URL.Query().Get("p")]
		var components []map[string]string
		for _, key := range keys[page*500 : min(len(keys), page*500+500)] {
			components = append(components, map[string]string{"key": key, "name": strings.ToUpper(key), "analysisDate": "2026-10-24T08:00:00+0000"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"paging":     map[string]int{"pageIndex": page + 1, "pageSize": 500, "total": len(keys)},
			"components": components,
		})
	})
	mux.HandleFunc("GET /api/metrics/search", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"total": 3, "metrics": [{"key": "coverage"}, {"key": "bugs"}, {"key": "alert_status"}]}`))
	})
	mux.HandleFunc("GET /api/measures/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("metricKeys") != "coverage,bugs,alert_status" {
			http.Error(w, "want the configured metrics", http.StatusBadRequest)
			return
		}
		if !slices.Contains(strings.Split(r.URL.Query().Get("projectKeys"), ","), ledger) {
			_, _ = w.Write([]byte(`{"measures": []}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"measures": [
			{"component": %[1]q, "metric": "coverage", "value": "81.5"},
			{"component": %[1]q, "metric": "bugs", "value": "3"},
			{"component": %[1]q, "metric": "alert_status", "value": "ERROR"}
		]}`, ledger)
	})
	mux.HandleFunc("GET /api/qualitygates/get_by_project", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"qualityGate": {"name": "Sonar way", "default": true}}`))
	})
	mux.HandleFunc("GET /api/qualitygates/project_status", func(w http.ResponseWriter, r *http.Request) {
		status := map[bool]string{true: "ERROR", false: "OK"}[r.URL.Query().Get("projectKey") == ledger]
		_, _ = fmt.Fprintf(w, `{"projectStatus": {"status": %q}}`, status)
	})
	mux.HandleFunc("GET /api/alm_settings/get_binding", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("project") != ledger {
			http.Error(w, `{"errors": [{"msg": "not bound"}]}`, http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"alm": "gitlab", "repository": "11"}`))
	})
	mux.HandleFunc("GET /api/qualitygates/list", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"qualitygates": [{"name": "Sonar way", "isDefault": true, "isBuiltIn": true}]}`))
	})
	mux.HandleFunc("GET /api/qualitygates/show", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"conditions": [{"metric": "new_coverage", "op": "LT", "error": "80"}]}`))
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, `{"errors": [{"msg": "Unauthorized"}]}`, http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	return server
}

func request(server *httptest.Server, config string) connector.Request {
	return connector.Request{
		Config:  json.RawMessage(fmt.Sprintf(`{"url": %q, "projectFilter": "qualifier = TRK", "metrics": ["coverage", "bugs", "alert_status"]%s}`, server.URL, config)),
		Secrets: map[string]string{"token": token},
		Kinds:   []string{KindProject, KindQualityGate},
	}
}

func TestFetchReadsEveryPageOfProjectsWithGateAndMeasures(t *testing.T) {
	server := fakeSonarQube(t)

	records, err := New(server.Client()).Fetch(context.Background(), request(server, ""))
	if err != nil {
		t.Fatal(err)
	}

	projects := records[KindProject]
	if len(projects) != 501 {
		t.Fatalf("read %d projects, want all 501 over both pages", len(projects))
	}
	ledger := "ledger_0a1b2c3d-0000-4000-8000-000000000000"
	want := map[string]any{
		"key":              ledger,
		"keyWithoutSuffix": "ledger",
		"name":             strings.ToUpper(ledger),
		"analysisDate":     "2026-10-24T08:00:00Z",
		"url":              server.URL + "/dashboard?id=" + ledger,
		"qualityGate":      map[string]any{"name": "Sonar way", "default": true, "status": "ERROR"},
		"binding":          map[string]any{"alm": "gitlab", "repository": "11"},
		"measures":         map[string]any{"coverage": 81.5, "bugs": 3.0, "alert_status": "ERROR"},
	}
	if !reflect.DeepEqual(projects[0], want) {
		t.Fatalf("ledger = %v, want %v", projects[0], want)
	}
	last := projects[500].(map[string]any)
	if last["qualityGate"].(map[string]any)["status"] != "OK" || len(last["measures"].(map[string]any)) != 0 || last["binding"] != nil {
		t.Fatalf("last project = %v, want a passing gate without measures or binding", last)
	}
	gates := records[KindQualityGate]
	if len(gates) != 1 || len(gates[0].(map[string]any)["conditions"].([]any)) != 1 {
		t.Fatalf("quality gates = %v, want Sonar way with its condition", gates)
	}
}

func TestFetchReadsOnlyTheConfiguredProjects(t *testing.T) {
	server := fakeSonarQube(t)

	records, err := New(server.Client()).Fetch(context.Background(), request(server, `, "projects": ["service-099", "service-001"]`))
	if err != nil {
		t.Fatal(err)
	}

	var keys []string
	for _, project := range records[KindProject] {
		keys = append(keys, project.(map[string]any)["key"].(string))
	}
	if strings.Join(keys, ",") != "service-001,service-099" {
		t.Fatalf("projects = %v, want service-001 and service-099", keys)
	}
}

func TestFetchExplainsWhatWentWrong(t *testing.T) {
	server := fakeSonarQube(t)
	cases := []struct {
		name    string
		request connector.Request
		message string
	}{
		{
			name:    "rejected token",
			request: connector.Request{Config: request(server, "").Config, Secrets: map[string]string{"token": "wrong"}, Kinds: []string{KindProject}},
			message: "SonarQube answered 401 Unauthorized for /api/components/search_projects",
		},
		{
			name:    "no url",
			request: connector.Request{Config: json.RawMessage(`{"projects": ["ledger"]}`), Secrets: map[string]string{"token": token}},
			message: "give the config a url",
		},
		{
			name:    "no token",
			request: connector.Request{Config: request(server, "").Config, Kinds: []string{KindProject}},
			message: "give the integration a token secret under secrets",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(server.Client()).Fetch(context.Background(), tc.request)

			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("Fetch() error = %v, want %q", err, tc.message)
			}
		})
	}
}
