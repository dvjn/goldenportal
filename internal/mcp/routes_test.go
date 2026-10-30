package mcp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gorilla/mux"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/dashboard"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/integration"
	"github.com/dvjn/goldenportal/internal/scorecard"
)

// stubRoutes answers each route the API registers with the route it matched,
// so tool calls are checked against the real route table, not a fake one.
type stubRoutes struct{ router *mux.Router }

func (s stubRoutes) add(method, path string) {
	s.router.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"route": method + " " + path}})
	}).Methods(method)
}

func (s stubRoutes) GET(path string, _ gofr.Handler)        { s.add("GET", path) }
func (s stubRoutes) POST(path string, _ gofr.Handler)       { s.add("POST", path) }
func (s stubRoutes) PUT(path string, _ gofr.Handler)        { s.add("PUT", path) }
func (s stubRoutes) DELETE(path string, _ gofr.Handler)     { s.add("DELETE", path) }
func (stubRoutes) AddCronJob(string, string, gofr.CronFunc) {}

func TestToolsReachRegisteredRoutes(t *testing.T) {
	routes := stubRoutes{mux.NewRouter().SkipClean(true)}
	routes.router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, "route not found")
	})
	routes.router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
	})
	// The same registrations app.New makes for the API that MCP dispatches to.
	blueprint.Register(routes, dashboard.CheckPages)
	entity.Register(routes)
	integration.Register(routes)
	scorecard.Register(routes)
	dashboard.Register(routes)
	configdoc.RegisterBulk(routes)

	session := connect(t, testServer(t, routes.router), "builder")
	doc := map[string]any{"kind": "integrations", "name": "source"}
	args := map[string]any{
		"list_documents":        map[string]any{"kind": "dashboards"},
		"get_document":          doc,
		"list_versions":         doc,
		"get_version":           map[string]any{"kind": "scorecards", "name": "ready", "version": 2},
		"export_document":       doc,
		"export_documents":      map[string]any{},
		"list_connectors":       map[string]any{},
		"list_catalog":          map[string]any{},
		"query_entities":        map[string]any{"blueprint": "service", "search": "pay", "page": 2},
		"get_entity":            map[string]any{"blueprint": "github-branch", "identifier": "acme/repo/feature/a"},
		"get_home":              map[string]any{},
		"get_entity_scorecards": map[string]any{"blueprint": "service", "identifier": "payments"},
		"preview_impact":        doc,
		"list_sync_runs":        map[string]any{"name": "source"},
		"validate_document":     map[string]any{"kind": "blueprints", "source": "kind: Blueprint"},
		"save_draft":            map[string]any{"kind": "blueprints", "name": "service", "source": "kind: Blueprint"},
		"discard_draft":         doc,
		"publish_document":      doc,
		"delete_document":       map[string]any{"kind": "dashboards", "name": "home", "confirm": "home"},
		"apply_documents":       map[string]any{"source": "kind: Blueprint"},
		"publish_documents":     map[string]any{"documents": []map[string]any{{"kind": "Blueprint", "name": "service"}}},
		"sync_integration":      map[string]any{"name": "source"},
		"evaluate_scorecard":    map[string]any{"name": "ready"},
	}
	tools, err := session.ListTools(t.Context(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != len(args) {
		t.Errorf("tools = %d, cases = %d; give every tool a case", len(tools.Tools), len(args))
	}
	for _, tool := range tools.Tools {
		a, ok := args[tool.Name]
		if !ok {
			t.Errorf("no case for %s", tool.Name)
			continue
		}
		out := output(t, call(t, session, tool.Name, a))
		if out.Status != http.StatusOK {
			t.Errorf("%s reached no route: %+v", tool.Name, out)
		}
	}
}
