package sonarqube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dvjn/goldport/internal/connector"
)

const token = "squ_test_token"

// fakeSonarQube serves 101 projects over two pages; ledger has a failing
// quality gate and measures, the others pass without measures.
func fakeSonarQube(t *testing.T) *httptest.Server {
	t.Helper()
	keys := []string{"ledger"}
	for index := range 100 {
		keys = append(keys, fmt.Sprintf("service-%03d", index))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/components/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("qualifiers") != "TRK" || r.URL.Query().Get("ps") != "100" {
			http.Error(w, "want projects, 100 a page", http.StatusBadRequest)
			return
		}
		page := map[string]int{"1": 0, "2": 1}[r.URL.Query().Get("p")]
		var components []map[string]string
		for _, key := range keys[page*100 : min(len(keys), page*100+100)] {
			components = append(components, map[string]string{"key": key, "name": strings.ToUpper(key), "lastAnalysisDate": "2026-10-24T08:00:00+0000"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"paging":     map[string]int{"pageIndex": page + 1, "pageSize": 100, "total": len(keys)},
			"components": components,
		})
	})
	mux.HandleFunc("GET /api/qualitygates/project_status", func(w http.ResponseWriter, r *http.Request) {
		status := map[bool]string{true: "ERROR", false: "OK"}[r.URL.Query().Get("projectKey") == "ledger"]
		_, _ = fmt.Fprintf(w, `{"projectStatus": {"status": %q}}`, status)
	})
	mux.HandleFunc("GET /api/measures/component", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("metricKeys") != "coverage,bugs,vulnerabilities,code_smells" {
			http.Error(w, "want the four metrics", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("component") != "ledger" {
			_, _ = w.Write([]byte(`{"component": {"measures": []}}`))
			return
		}
		_, _ = w.Write([]byte(`{"component": {"measures": [
			{"metric": "coverage", "value": "81.5"}, {"metric": "bugs", "value": "3"},
			{"metric": "vulnerabilities", "value": "0"}, {"metric": "code_smells", "value": "42"}
		]}}`))
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
		Config:  json.RawMessage(fmt.Sprintf(`{"url": %q%s}`, server.URL, config)),
		Secrets: map[string]string{"token": token},
		Kinds:   []string{KindProject},
	}
}

func TestFetchReadsEveryPageOfProjectsWithGateAndMeasures(t *testing.T) {
	server := fakeSonarQube(t)

	records, err := New(server.Client()).Fetch(context.Background(), request(server, ""))
	if err != nil {
		t.Fatal(err)
	}

	projects := records[KindProject]
	if len(projects) != 101 {
		t.Fatalf("read %d projects, want all 101 over both pages", len(projects))
	}
	want := map[string]any{
		"key":              "ledger",
		"name":             "LEDGER",
		"lastAnalysisDate": "2026-10-24T08:00:00+0000",
		"url":              server.URL + "/dashboard?id=ledger",
		"qualityGate":      "ERROR",
		"measures":         map[string]any{"coverage": 81.5, "bugs": 3.0, "vulnerabilities": 0.0, "code_smells": 42.0},
	}
	if !reflect.DeepEqual(projects[0], want) {
		t.Fatalf("ledger = %v, want %v", projects[0], want)
	}
	if last := projects[100].(map[string]any); last["qualityGate"] != "OK" || len(last["measures"].(map[string]any)) != 0 {
		t.Fatalf("last project = %v, want a passing gate without measures", last)
	}
}

func TestFetchReadsOnlyTheConfiguredProjects(t *testing.T) {
	server := fakeSonarQube(t)

	records, err := New(server.Client()).Fetch(context.Background(), request(server, `, "projects": ["service-099", "ledger"]`))
	if err != nil {
		t.Fatal(err)
	}

	var keys []string
	for _, project := range records[KindProject] {
		keys = append(keys, project.(map[string]any)["key"].(string))
	}
	if strings.Join(keys, ",") != "ledger,service-099" {
		t.Fatalf("projects = %v, want ledger and service-099", keys)
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
			message: "SonarQube answered 401 Unauthorized for /api/components/search",
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
