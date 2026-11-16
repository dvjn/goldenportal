package mcp

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/action"
	"github.com/dvjn/goldenportal/internal/scorecard"
)

// page reads a page tool's result as the page it is.
func page[T any](t *testing.T, out Output) T {
	t.Helper()
	if out.Status != http.StatusOK {
		t.Fatalf("status %d: %v", out.Status, out.Error)
	}
	raw, _ := json.Marshal(out.Data)
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDashboardsReadWhole(t *testing.T) {
	session := connect(t, testServer(t, newFake()), userPath, "member")

	list := page[[]dashboardSummary](t, output(t, call(t, session, "list_dashboards", map[string]any{})))
	if len(list) != 2 || list[0].Identifier != "teams" || list[0].OpensFirst || !list[1].OpensFirst {
		t.Errorf("list_dashboards = %+v", list)
	}

	// Without an identifier, it's the one Home opens on.
	raw := output(t, call(t, session, "get_dashboard", map[string]any{"inputs": map[string]any{"tier": []string{"gold"}}}))
	d := page[dashboardPage](t, raw)
	if d.Identifier != "services" || len(d.Widgets) != 1 || len(d.Inputs) != 2 {
		t.Fatalf("get_dashboard = %+v", d)
	}
	w := d.Widgets[0]
	if w.Title != "Services" || w.Value == nil || w.Value.Query.Blueprint != "service" ||
		w.Data.(map[string]any)["value"] != float64(3) {
		t.Errorf("widget = %+v", w)
	}
	team, tier := d.Inputs[0], d.Inputs[1]
	if !slices.Equal(team.Value, []string{"payments"}) || len(team.Entities) != 1 || team.Entities[0].Title != "Payments" ||
		!slices.Equal(tier.Value, []string{"gold"}) {
		t.Errorf("inputs = %+v", d.Inputs)
	}
	// Layout is the page's, not the agent's.
	widgets := raw.Data.(map[string]any)["widgets"].([]any)[0].(map[string]any)
	for _, layout := range []string{"width", "icon", "color", "link"} {
		if _, ok := widgets[layout]; ok {
			t.Errorf("widget has %s: %v", layout, widgets)
		}
	}
	if _, ok := raw.Data.(map[string]any)["icon"]; ok {
		t.Errorf("dashboard has its icon: %v", raw.Data)
	}
}

func TestCatalogOpensATab(t *testing.T) {
	fake := newFake()
	session := connect(t, testServer(t, fake), userPath, "member")

	// It opens on the blueprint's own first tab, as the page does.
	c := page[catalogPage](t, output(t, call(t, session, "get_catalog", map[string]any{"blueprint": "service"})))
	var tabs []string
	for _, tab := range c.Tabs {
		tabs = append(tabs, tab.Identifier)
	}
	if c.Blueprint.Title != "Service" || !slices.Equal(tabs, []string{"health", "entities", "scorecard-ready"}) ||
		c.Tab != "health" || len(c.Widgets) != 2 || c.Widgets[0].Data.(map[string]any)["value"] != float64(7) {
		t.Errorf("get_catalog = %+v", c)
	}
	// Away from an entity, an actions widget offers what Self-Service does.
	if run := c.Widgets[1]; !slices.Equal(run.Actions, []named{{"restart", "Restart"}}) ||
		!slices.Equal(run.Workflows, []named{{"register", "Register a service"}}) {
		t.Errorf("actions widget = %+v", run)
	}

	// Searching opens the entities, as the service lists them.
	c = page[catalogPage](t, output(t, call(t, session, "get_catalog", map[string]any{"blueprint": "service", "search": "pay", "size": 20})))
	if c.Tab != "entities" || c.Entities == nil || c.Entities.Items[0].Identifier != "payments/pay" ||
		!slices.Equal(c.Entities.Columns, []string{"tier", "updatedAt"}) {
		t.Errorf("entities = %+v", c)
	}
	// Each row offers what the table's row menu does, asked for the page's rows.
	if rows := c.Entities.RowActions; rows == nil || !slices.Equal(rows.Entities["billing"], []string{"restart"}) {
		t.Errorf("row actions = %+v", rows)
	}
	if fake.last.name != "RowActions" || !slices.Equal(fake.last.args[1].([]string), []string{"payments/pay", "billing"}) {
		t.Errorf("row actions asked %+v", *fake.last)
	}

	// Paging on opens the entities too, with the cursor the service read
	// the page by.
	c = page[catalogPage](t, output(t, call(t, session, "get_catalog", map[string]any{"blueprint": "service", "cursor": "abc"})))
	if c.Tab != "entities" || c.Entities == nil || c.Entities.Previous != "before-abc" {
		t.Errorf("paged entities = %+v", c)
	}
	// A cursor from a scorecard's results opens its tab.
	results := base64.RawURLEncoding.EncodeToString([]byte(`{"d":"last","l":"` + scorecard.ResultsList("ready") + `","h":"x"}`))
	c = page[catalogPage](t, output(t, call(t, session, "get_catalog", map[string]any{"blueprint": "service", "cursor": results})))
	if c.Tab != "scorecard-ready" || c.Scorecard == nil {
		t.Errorf("paged results = %+v", c)
	}
	if req := fake.last.args[1].(scorecard.ResultsRequest); req.Cursor != results {
		t.Errorf("results listed %+v", req)
	}

	c = page[catalogPage](t, output(t, call(t, session, "get_catalog", map[string]any{"blueprint": "service", "tab": "scorecard-ready", "sort": "-tier"})))
	if c.Scorecard == nil || c.Scorecard.Results.Items[0].Title != "Payments" {
		t.Errorf("scorecard tab = %+v", c)
	}
	if req := fake.last.args[1].(scorecard.ResultsRequest); fake.last.args[0] != "ready" || req.Sort != "-tier" {
		t.Errorf("results listed %+v", *fake.last)
	}

	if missing := call(t, session, "get_catalog", map[string]any{"blueprint": "service", "tab": "nope"}); !missing.IsError {
		t.Error("an unknown tab opened")
	}
}

func TestEntityPageReadsWhole(t *testing.T) {
	fake := newFake()
	session := connect(t, testServer(t, fake), userPath, "member")
	e := page[entityPage](t, output(t, call(t, session, "get_entity", map[string]any{"blueprint": "service", "identifier": "payments"})))
	if e.Entity.Title != "Payments" || len(e.Scorecards) != 1 || e.Scorecards[0].Tier != "gold" ||
		len(e.Actions) != 1 || e.Actions[0].Title != "Restart" || e.Runs.Total != 1 {
		t.Errorf("get_entity = %+v", e)
	}
	// The tab whose when the entity doesn't meet isn't on its page, which
	// opens on its overview.
	if !slices.Equal(e.Tabs, []named{{"deploys", "Deploys"}, {"overview", "Overview"}}) || e.Tab == nil || e.Tab.Identifier != "overview" ||
		e.Tab.Widgets[0].Data.(map[string]any)["value"] != float64(5) {
		t.Fatalf("tabs = %+v, open %+v", e.Tabs, e.Tab)
	}
	// On an entity's page, an actions widget offers what can run on it.
	if run := e.Tab.Widgets[1]; !slices.Equal(run.Actions, []named{{"restart", "Restart"}}) ||
		!slices.Equal(run.Workflows, []named{{"register", "Register a service"}}) {
		t.Errorf("actions widget = %+v", run)
	}
	if got := fake.last; got.name != "EntityTab" || !slices.Equal(got.args, []any{"service", "payments", "overview"}) {
		t.Errorf("tab loaded %+v", *got)
	}

	// A tab that fails says so on the tab, leaving the page standing.
	e = page[entityPage](t, output(t, call(t, session, "get_entity", map[string]any{"blueprint": "service", "identifier": "payments", "tab": "deploys"})))
	if e.Tab == nil || e.Tab.Error == "" || strings.Contains(e.Tab.Error, "10.0.0.5") || e.Entity.Title != "Payments" {
		t.Errorf("failed tab = %+v", e.Tab)
	}
	for _, tab := range []string{"oncall", "nope"} {
		if hidden := call(t, session, "get_entity", map[string]any{"blueprint": "service", "identifier": "payments", "tab": tab}); !hidden.IsError {
			t.Errorf("tab %s opened", tab)
		}
	}
	if missing := call(t, session, "get_entity", map[string]any{"blueprint": "team", "identifier": "x"}); !missing.IsError {
		t.Error("an entity of an unknown blueprint was read")
	}
}

// The Runs page lists the caller's own runs unless they ask for everyone's.
func TestRunsAreTheCallersOwnUnlessAsked(t *testing.T) {
	fake := newFake()
	session := connect(t, testServer(t, fake), userPath, "builder")
	call(t, session, "list_runs", map[string]any{})
	if req := fake.last.args[0].(action.RunsRequest); !req.Mine {
		t.Errorf("list_runs asked %+v", req)
	}
	call(t, session, "list_runs", map[string]any{"everyone": true})
	if req := fake.last.args[0].(action.RunsRequest); req.Mine {
		t.Errorf("list_runs everyone asked %+v", req)
	}
}
