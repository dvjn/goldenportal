package mcp

import (
	"slices"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/action"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/dashboard"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/scorecard"
	"github.com/dvjn/goldenportal/internal/widget"
)

// Pages read what one of the portal's pages shows, from the services the
// page calls, so an agent reads a page in one call. They leave out how the
// page lays it out: widths, icons, colors and where clicking goes.

// widgetView is a widget as an agent reads it: what it shows and how it
// measures it, with the numbers it shows, or why they couldn't load.
type widgetView struct {
	Identifier  string `json:"identifier"`
	Type        string `json:"type"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`

	Query      *widget.Query    `json:"query,omitempty"`
	Value      *widget.Value    `json:"value,omitempty"`
	GroupBy    string           `json:"groupBy,omitempty"`
	Measure    *widget.Measure  `json:"measure,omitempty"`
	Measures   []widget.Measure `json:"measures,omitempty"`
	Date       string           `json:"date,omitempty"`
	Range      string           `json:"range,omitempty"`
	Columns    []widget.Column  `json:"columns,omitempty"`
	Sort       string           `json:"sort,omitempty"`
	Limit      int              `json:"limit,omitempty"`
	Scorecard  string           `json:"scorecard,omitempty"`
	Properties []string         `json:"properties,omitempty"`
	Actions    []named          `json:"actions,omitempty"`
	Workflows  []named          `json:"workflows,omitempty"`

	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

type named struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

func widgetViews(widgets []widget.Widget, data dashboard.PageData, can offers) []widgetView {
	views := make([]widgetView, 0, len(widgets))
	for _, w := range widgets {
		v := widgetView{
			Identifier: w.ID, Type: w.Type, Title: w.Title, Description: w.Description,
			Query: w.Query, Value: w.Value, GroupBy: w.GroupBy, Measure: w.Measure, Measures: w.Measures,
			Date: w.Date, Range: w.Range, Columns: w.Columns, Sort: w.Sort, Limit: w.Limit, Scorecard: w.Scorecard,
			Properties: w.Properties, Actions: pick(can.actions, w.Actions), Workflows: pick(can.workflows, w.Workflows),
		}
		if r, ok := data.Widgets[w.ID]; ok {
			v.Data, v.Error = r.Data, r.Error
		}
		views = append(views, v)
	}
	return views
}

// offers are what actions widgets offer the caller, as the widget does: on
// an entity's page, the actions they can run on it, and elsewhere those
// in their Self-Service catalog, with its workflows after them.
type offers struct{ actions, workflows []named }

// offersFor finds what widgets' actions widgets can offer, reading the
// Self-Service catalog only when one needs it. onEntity, when set, are the
// actions the caller can run on the page's entity.
func offersFor(b bridge, ctx *gofr.Context, widgets []widget.Widget, onEntity *[]action.Available) (offers, error) {
	var can offers
	needsCatalog := false
	for _, w := range widgets {
		if w.Type == widget.TypeActions {
			needsCatalog = needsCatalog || len(w.Workflows) > 0 || (onEntity == nil && len(w.Actions) > 0)
		}
	}
	if onEntity != nil {
		for _, a := range *onEntity {
			can.actions = append(can.actions, named{a.Identifier, a.Title})
		}
	}
	if !needsCatalog {
		return can, nil
	}
	catalog, err := b.services.SelfServiceCatalog(ctx)
	if err != nil {
		return offers{}, err
	}
	for _, g := range catalog.Groups {
		for _, o := range g.Items {
			switch {
			case o.Kind == action.WorkflowName:
				can.workflows = append(can.workflows, named{o.Identifier, o.Title})
			case onEntity == nil:
				can.actions = append(can.actions, named{o.Identifier, o.Title})
			}
		}
	}
	return can, nil
}

// pick is what of ids, in their order, the caller can run.
func pick(can []named, ids []string) []named {
	var picked []named
	for _, id := range ids {
		if i := slices.IndexFunc(can, func(n named) bool { return n.Identifier == id }); i >= 0 {
			picked = append(picked, can[i])
		}
	}
	return picked
}

// dashboardSummary is a dashboard as Home's sidebar lists it.
type dashboardSummary struct {
	Identifier  string      `json:"identifier"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Group       string      `json:"group,omitempty"`
	OpensFirst  bool        `json:"opensFirst,omitempty"`
	Inputs      []inputView `json:"inputs,omitempty"`
}

// inputView is a dashboard input: what it can be set to and what it is.
// Entities name the entities a relation input is set to.
type inputView struct {
	Identifier string        `json:"identifier"`
	Title      string        `json:"title"`
	Type       string        `json:"type"`
	Values     []string      `json:"values,omitempty"`
	Relation   string        `json:"relation,omitempty"`
	Multiple   bool          `json:"multiple,omitempty"`
	Value      []string      `json:"value"`
	Entities   []entity.Link `json:"entities,omitempty"`
	Problem    string        `json:"problem,omitempty"`
}

type dashboardPage struct {
	Identifier  string       `json:"identifier"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Group       string       `json:"group,omitempty"`
	Inputs      []inputView  `json:"inputs,omitempty"`
	Widgets     []widgetView `json:"widgets"`
}

func inputViews(d dashboard.HomeDashboard, picked map[string][]string, data dashboard.PageData) []inputView {
	var views []inputView
	for _, in := range d.Inputs {
		value, ok := picked[in.ID]
		if !ok {
			value = d.Defaults[in.ID]
		}
		views = append(views, inputView{
			Identifier: in.ID, Title: in.Title, Type: in.Type, Values: in.Values, Relation: in.Relation, Multiple: in.Multiple,
			Value: nonNil(value), Entities: data.Inputs[in.ID], Problem: d.Problems[in.ID],
		})
	}
	return views
}

func groupTitle(d dashboard.HomeDashboard) string {
	if d.Group == nil {
		return ""
	}
	return d.Group.Title
}

// listDashboards is Home's dashboards, in its order, marking the one it
// opens on.
func listDashboards(b bridge, ctx *gofr.Context) ([]dashboardSummary, error) {
	home, err := b.services.Home(ctx)
	if err != nil {
		return nil, err
	}
	opens := opening(home)
	list := make([]dashboardSummary, 0, len(home))
	for _, d := range home {
		list = append(list, dashboardSummary{
			Identifier: d.Identifier, Title: d.Title, Description: d.Description, Group: groupTitle(d),
			OpensFirst: d.Identifier == opens, Inputs: inputViews(d, nil, dashboard.PageData{}),
		})
	}
	return list, nil
}

// opening is the dashboard Home opens on: the viewer's preferred one, else
// the first.
func opening(home []dashboard.HomeDashboard) string {
	if len(home) == 0 {
		return ""
	}
	if i := slices.IndexFunc(home, func(d dashboard.HomeDashboard) bool { return d.Preferred }); i >= 0 {
		return home[i].Identifier
	}
	return home[0].Identifier
}

// readDashboard is a dashboard whole, as Home shows it with its inputs set
// as picked, from its draft when draft is set.
func readDashboard(b bridge, ctx *gofr.Context, identifier string, inputs map[string][]string, draft bool) (dashboardPage, error) {
	if identifier == "" {
		home, err := b.services.Home(ctx)
		if err != nil {
			return dashboardPage{}, err
		}
		if identifier = opening(home); identifier == "" {
			return dashboardPage{}, httpx.NotFound("Home has no dashboards")
		}
	}
	d, err := b.services.ViewDashboard(ctx, identifier, draft)
	if err != nil {
		return dashboardPage{}, err
	}
	data, err := b.services.DashboardData(ctx, identifier, inputs, draft)
	if err != nil {
		return dashboardPage{}, err
	}
	can, err := offersFor(b, ctx, d.Widgets, nil)
	if err != nil {
		return dashboardPage{}, err
	}
	return dashboardPage{
		Identifier: d.Identifier, Title: d.Title, Description: d.Description, Group: groupTitle(d),
		Inputs: inputViews(d, inputs, data), Widgets: widgetViews(d.Widgets, data, can),
	}, nil
}

// blueprintView is a blueprint as its pages head it, with what each of its
// entities' properties means.
type blueprintView struct {
	Identifier  string         `json:"identifier"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Properties  []propertyView `json:"properties"`
}

type propertyView struct {
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type"`
	Format      string `json:"format,omitempty"`
	Enum        []any  `json:"enum,omitempty"`
	Relation    string `json:"relation,omitempty"`
	Many        bool   `json:"many,omitempty"`
	Scorecard   string `json:"scorecard,omitempty"`
}

func viewBlueprint(spec blueprint.Spec) blueprintView {
	v := blueprintView{Identifier: spec.Identifier, Title: spec.Title, Description: spec.Description, Properties: []propertyView{}}
	for _, p := range spec.Properties {
		v.Properties = append(v.Properties, propertyView{
			Identifier: p.ID, Title: p.Title, Description: p.Description, Type: p.Type, Format: p.Format, Enum: p.Enum,
			Relation: p.Relation, Many: p.Many, Scorecard: p.Scorecard,
		})
	}
	return v
}

// Tab kinds on a blueprint's catalog page.
const (
	tabWidgets   = "widgets"
	tabEntities  = "entities"
	tabScorecard = "scorecard"
	// scorecardTab prefixes a scorecard's tab, as the page does, keeping it
	// apart from the blueprint's own.
	scorecardTab = "scorecard-"
)

type tabView struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Scorecard  string `json:"scorecard,omitempty"`
}

type entitiesView struct {
	Columns []string `json:"columns"`
	entity.Page
	// RowActions are the actions the caller can run on each row, as the
	// table's row menus offer them.
	RowActions *action.RowActions `json:"rowActions,omitempty"`
}

// catalogPage is a blueprint's catalog page with one of its tabs open.
type catalogPage struct {
	Blueprint blueprintView  `json:"blueprint"`
	Tabs      []tabView      `json:"tabs"`
	Tab       string         `json:"tab"`
	Widgets   []widgetView   `json:"widgets,omitempty"`
	Entities  *entitiesView  `json:"entities,omitempty"`
	Scorecard *scorecardPage `json:"scorecard,omitempty"`
}

// tableArgs are what a catalog table is searched, filtered, sorted and
// paged by.
type tableArgs struct {
	Search string `json:"search,omitempty" jsonschema:"Text in an entity's title or identifier, ignoring case: anywhere from three characters, at the start before that"`
	Filter string `json:"filter,omitempty" jsonschema:"Shared filter tree encoded as JSON, on the blueprint's properties. A property of type scorecard reaches its result: the property alone is the tier (empty for none), <property>.checks.<id> a check's status (passed, failed or skipped)"`
	Sort   string `json:"sort,omitempty" jsonschema:"A property, title, createdAt or updatedAt, with a leading - for descending, empty values first ascending and last descending; on a scorecard's results: tier, score, passed, failed or skipped, worst tier first when omitted"`
	Page   int    `json:"page,omitempty"`
	Size   int    `json:"size,omitempty" jsonschema:"Page size: 10, 20, 50 or 100; omit for the default"`
}

func (a tableArgs) set() bool { return a.Search != "" || a.Filter != "" || a.Sort != "" || a.Page > 0 }

func readCatalog(b bridge, ctx *gofr.Context, identifier, tab string, table tableArgs) (catalogPage, error) {
	spec, err := b.services.Blueprint(ctx, identifier)
	if err != nil {
		return catalogPage{}, err
	}
	grading, err := b.services.GradingScorecards(ctx, identifier)
	if err != nil {
		return catalogPage{}, err
	}
	page := catalogPage{Blueprint: viewBlueprint(spec), Tabs: []tabView{}}
	var own []widget.Tab
	if spec.Catalog != nil {
		own = spec.Catalog.Tabs
	}
	widgetsOf := map[string][]widget.Widget{}
	for _, t := range own {
		page.Tabs = append(page.Tabs, tabView{Identifier: t.ID, Title: t.Title, Kind: tabWidgets})
		widgetsOf[t.ID] = t.Widgets
	}
	page.Tabs = append(page.Tabs, tabView{Identifier: tabEntities, Title: "Entities", Kind: tabEntities})
	for _, s := range grading {
		page.Tabs = append(page.Tabs, tabView{Identifier: scorecardTab + s.Identifier, Title: s.Title, Kind: tabScorecard, Scorecard: s.Identifier})
	}

	switch {
	case tab != "":
	case len(own) > 0 && !table.set():
		tab = own[0].ID
	default:
		tab = tabEntities
	}
	i := slices.IndexFunc(page.Tabs, func(t tabView) bool { return t.Identifier == tab })
	if i < 0 {
		return catalogPage{}, httpx.NotFound("the " + spec.Title + " catalog has no tab " + tab)
	}
	page.Tab = tab
	switch open := page.Tabs[i]; open.Kind {
	case tabWidgets:
		data, err := b.services.PageTab(ctx, dashboard.PageRequest{Blueprint: identifier, Page: "catalog", Tab: tab})
		if err != nil {
			return catalogPage{}, err
		}
		can, err := offersFor(b, ctx, widgetsOf[tab], nil)
		if err != nil {
			return catalogPage{}, err
		}
		page.Widgets = widgetViews(widgetsOf[tab], data, can)
	case tabEntities:
		entities, err := b.services.Entities(ctx, identifier, entity.ListRequest{
			Search: table.Search, Filter: table.Filter, Sort: table.Sort, Page: table.Page, Size: table.Size,
		})
		if err != nil {
			return catalogPage{}, err
		}
		page.Entities = &entitiesView{Columns: columns(spec), Page: entities}
		if len(entities.Items) > 0 {
			ids := make([]string, 0, len(entities.Items))
			for _, e := range entities.Items {
				ids = append(ids, e.Identifier)
			}
			rows, err := b.services.RowActions(ctx, identifier, ids)
			if err != nil {
				return catalogPage{}, err
			}
			page.Entities.RowActions = &rows
		}
	case tabScorecard:
		s, err := readScorecard(b, ctx, open.Scorecard, table)
		if err != nil {
			return catalogPage{}, err
		}
		page.Scorecard = &s
	}
	return page, nil
}

// columns are the catalog table's: the blueprint's, or else the first few
// properties that fit in a cell and when each entity was last updated.
func columns(spec blueprint.Spec) []string {
	if spec.Catalog != nil && len(spec.Catalog.Columns) > 0 {
		return spec.Catalog.Columns
	}
	var ids []string
	for _, p := range spec.Properties {
		if p.Type != "object" && len(ids) < 5 {
			ids = append(ids, p.ID)
		}
	}
	return append(ids, "updatedAt")
}

// scorecardPage is a scorecard's results, as its tab on its blueprint's
// catalog page shows them: its tiers and checks, how many entities reach
// each tier and pass each check, and a page of entities' results.
type scorecardPage struct {
	Summary scorecard.SummaryView `json:"summary"`
	Results scorecard.ResultPage  `json:"results"`
}

func readScorecard(b bridge, ctx *gofr.Context, identifier string, table tableArgs) (scorecardPage, error) {
	summary, err := b.services.ScorecardSummary(ctx, identifier, scorecard.SummaryRequest{Filter: table.Filter})
	if err != nil {
		return scorecardPage{}, err
	}
	results, err := b.services.ScorecardResults(ctx, identifier, scorecard.ResultsRequest{
		Search: table.Search, Filter: table.Filter, Sort: table.Sort, Page: table.Page, Size: table.Size,
	})
	if err != nil {
		return scorecardPage{}, err
	}
	return scorecardPage{Summary: summary, Results: results}, nil
}

// entityPage is what an entity's page shows: its properties and
// relations, its scorecard results, the actions the caller can run on it,
// its latest runs, the blueprint's tabs that apply to it and one of them
// open.
type entityPage struct {
	Blueprint  blueprintView               `json:"blueprint"`
	Entity     entity.Detail               `json:"entity"`
	Scorecards []scorecard.EntityScorecard `json:"scorecards"`
	Actions    []action.Available          `json:"actions"`
	Runs       action.RunPage              `json:"runs"`
	Tabs       []named                     `json:"tabs"`
	Tab        *entityTab                  `json:"tab,omitempty"`
}

// entityTab is an open tab's widgets, or why they couldn't load, which
// leaves the rest of the page standing.
type entityTab struct {
	Identifier string       `json:"identifier"`
	Title      string       `json:"title"`
	Widgets    []widgetView `json:"widgets,omitempty"`
	Error      string       `json:"error,omitempty"`
}

const latestRuns = 10

// readEntity reads an entity's page with tab open: by default its
// blueprint's overview tab, when it has one, since the built-in overview
// is the properties the page always reads.
func readEntity(b bridge, ctx *gofr.Context, blueprintID, identifier, tab string) (entityPage, error) {
	spec, err := b.services.Blueprint(ctx, blueprintID)
	if err != nil {
		return entityPage{}, err
	}
	detail, err := b.services.Entity(ctx, blueprintID, identifier)
	if err != nil {
		return entityPage{}, err
	}
	page := entityPage{Blueprint: viewBlueprint(spec), Entity: detail, Tabs: []named{}}
	if page.Scorecards, err = b.services.EntityScorecards(ctx, blueprintID, identifier); err != nil {
		return entityPage{}, err
	}
	if page.Actions, err = b.services.EntityActions(ctx, blueprintID, identifier); err != nil {
		return entityPage{}, err
	}
	if page.Runs, err = b.services.Runs(ctx, action.RunsRequest{Blueprint: blueprintID, Entity: identifier, Size: latestRuns}); err != nil {
		return entityPage{}, err
	}

	var open *widget.Tab
	if spec.Entity != nil {
		for i, t := range spec.Entity.Tabs {
			if slices.Contains(detail.HiddenTabs, t.ID) {
				continue
			}
			page.Tabs = append(page.Tabs, named{t.ID, t.Title})
			if t.ID == tab || (tab == "" && t.ID == widget.OverviewTab) {
				open = &spec.Entity.Tabs[i]
			}
		}
	}
	switch {
	case open != nil:
	case tab != "":
		return entityPage{}, httpx.NotFound(detail.Title + "'s page has no tab " + tab)
	default:
		return page, nil
	}
	page.Tab = &entityTab{Identifier: open.ID, Title: open.Title}
	data, err := b.services.EntityTab(ctx, spec, detail.Entity, open.ID)
	if err == nil {
		var can offers
		if can, err = offersFor(b, ctx, open.Widgets, &page.Actions); err == nil {
			page.Tab.Widgets = widgetViews(open.Widgets, data, can)
		}
	}
	if err != nil {
		_, page.Tab.Error = failure(ctx, err)
	}
	return page, nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
