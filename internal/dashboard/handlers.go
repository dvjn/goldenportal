package dashboard

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/scorecard"
	"github.com/dvjn/goldenportal/internal/widget"
)

func Register(app *gofr.App) {
	configdoc.Register(app, Kind)
	app.GET("/api/v1/home", home)
	app.GET("/api/v1/dashboards/{identifier}/view", dashboardView)
	app.GET("/api/v1/dashboards/{identifier}/widgets/{widget}", dashboardWidget)
	app.GET("/api/v1/blueprints/{identifier}/widgets/{page}/{tab}/{widget}", blueprintWidget)
}

// home lists the published dashboards, in the order Home shows them.
func home(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return nil, err
	}
	sources, err := configdoc.NewStore(ctx.SQL, Name).PublishedSources(ctx)
	if err != nil {
		return nil, err
	}
	specs := []Spec{}
	for _, source := range sources {
		if spec, issues := Parse(source); !configdoc.HasErrors(issues) && spec.Visible(p) {
			specs = append(specs, spec)
		}
	}
	slices.SortFunc(specs, func(a, b Spec) int {
		if a.Order != b.Order {
			return a.Order - b.Order
		}
		return strings.Compare(a.Title+"\x00"+a.Identifier, b.Title+"\x00"+b.Identifier)
	})

	views := make([]homeDashboard, 0, len(specs))
	preferred := false
	for _, spec := range specs {
		v, problems := view(ctx, find(ctx), spec, p)
		if !preferred && spec.DefaultFor != nil && spec.DefaultFor.Matches(p) {
			v.Preferred = true
			preferred = true
		}
		logProblems(ctx, spec, problems)
		views = append(views, v)
	}

	return views, nil
}

// dashboardView is one dashboard as Home would show it to whoever is
// looking, from the draft for builders who ask, so they can preview it.
func dashboardView(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return nil, err
	}
	src, err := source(ctx, p, Name, ctx.PathParam("identifier"))
	if err != nil {
		return nil, err
	}
	spec, _ := Parse(src)
	v, problems := view(ctx, find(ctx), spec, p)
	logProblems(ctx, spec, problems)

	return v, nil
}

// homeDashboard is a dashboard as Home shows it to whoever is looking,
// with its text filled in for them.
type homeDashboard struct {
	Spec
	Preferred bool `json:"preferred,omitempty"`
	// Defaults are what the filters start at for them, by identifier, with
	// lookup defaults found. A filter without a default is left out; a
	// lookup that found nothing is an empty list, which selects nothing.
	Defaults map[string][]string `json:"defaults"`
	// Problems say why a lookup default couldn't be found, by filter. The
	// filter starts at nothing then, as if the lookup found none.
	Problems map[string]string `json:"problems,omitempty"`
}

// view works out how whoever is looking sees a dashboard, and what went
// wrong finding its defaults, for the log.
func view(ctx context.Context, find finder, spec Spec, p auth.Principal) (homeDashboard, map[string]error) {
	me := widget.MeValues(p)
	widget.SetToday(me, time.Now())
	values, problems := defaults(ctx, find, spec, me, nil)
	v := homeDashboard{Spec: spec.render(me), Defaults: map[string][]string{}, Problems: map[string]string{}}
	for _, f := range spec.Filters {
		if value := values[widget.ValueFilter+f.ID]; value != nil {
			v.Defaults[f.ID] = value
		}
		if err := problems[f.ID]; err != nil {
			v.Problems[f.ID] = explain(err)
		}
	}

	return v, problems
}

// explain says why a lookup failed, to whoever is looking. A filter the
// blueprint can't answer names the property, which a builder can fix;
// anything else is the server's problem, which the log has.
func explain(err error) string {
	var filterErr entity.FilterError
	if errors.As(err, &filterErr) {
		return "the default can't be found: " + filterErr.Error()
	}

	return "the default couldn't be looked up"
}

func logProblems(ctx *gofr.Context, spec Spec, problems map[string]error) {
	for id, err := range problems {
		ctx.Errorf("find the default of filter %s on dashboard %s: %v", id, spec.Identifier, err)
	}
}

// lookupLimit caps what a lookup default finds, at what the bar offers.
const lookupLimit = 100

// finder finds the entities of a blueprint matching where, by identifier.
// It finds nothing when the blueprint isn't published.
type finder func(ctx context.Context, from string, where *filter.Filter) ([]string, error)

func find(ctx *gofr.Context) finder {
	return func(c context.Context, from string, where *filter.Filter) ([]string, error) {
		blueprints, err := blueprint.Published(c, ctx.SQL)
		if err != nil {
			return nil, err
		}
		bp, ok := blueprints[from]
		if !ok {
			return nil, nil
		}

		return entity.NewStore(ctx.SQL).Identifiers(c, bp, where, lookupLimit)
	}
}

// defaults is what a dashboard's filters are set to for whoever is
// looking, by context value: what they picked, or else the filter's
// default, with a lookup default found in the catalog with their $me
// values. A lookup that finds no entity sets the filter to Nothing, so
// widgets show nothing, unlike a filter they cleared, which is unset and
// shows everything. A lookup that fails does the same, and says why by
// filter, so one broken dashboard neither takes Home down nor shows more
// than its author meant.
func defaults(ctx context.Context, find finder, spec Spec, me, picked widget.Values) (widget.Values, map[string]error) {
	values := widget.Values{}
	problems := map[string]error{}
	for _, f := range spec.Filters {
		key := widget.ValueFilter + f.ID
		if value, ok := picked[key]; ok {
			values[key] = value
			continue
		}
		if f.Lookup == nil {
			values[key] = f.Default
			continue
		}
		found, err := find(ctx, f.Lookup.From, widget.Bind(f.Lookup.Where, me))
		if err != nil {
			problems[f.ID] = err
		}
		if len(found) == 0 {
			found = widget.Nothing
		}
		values[key] = found
	}

	return values, problems
}

// source returns the source of a document: its draft when the request asks
// for it with draft=true and has one, which builders can load to see
// widgets before publishing, or else the published version.
func source(ctx *gofr.Context, p auth.Principal, kind, name string) (string, error) {
	draft := ctx.Param("draft") == "true"
	if draft && !p.Can(auth.RoleBuilder) {
		return "", httpx.Error{Code: http.StatusForbidden, Message: "drafts need the builder role"}
	}
	record, err := configdoc.NewStore(ctx.SQL, kind).Get(ctx, name)
	if errors.Is(err, configdoc.ErrNotFound) {
		return "", httpx.Error{Code: http.StatusNotFound, Message: "no " + kind + " " + name}
	}
	if err != nil {
		return "", err
	}
	if kind == Name && !p.Can(auth.RoleBuilder) {
		spec, issues := Parse(record.PublishedSource)
		if !record.Published() || configdoc.HasErrors(issues) || !spec.Visible(p) {
			return "", httpx.Error{Code: http.StatusNotFound, Message: "no published dashboard " + name}
		}
	}
	if draft && record.HasDraft() {
		return record.DraftSource, nil
	}
	if !record.Published() {
		return "", httpx.Error{Code: http.StatusNotFound, Message: "no published " + kind + " " + name}
	}

	return record.PublishedSource, nil
}

func dashboardWidget(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return nil, err
	}
	src, err := source(ctx, p, Name, ctx.PathParam("identifier"))
	if err != nil {
		return nil, err
	}
	spec, _ := Parse(src)
	w, ok := widget.Find(spec.Widgets, ctx.PathParam("widget"))
	if !ok {
		return nil, noWidget(ctx)
	}
	picked, err := picked(spec, httpx.Query(ctx))
	if err != nil {
		return nil, err
	}
	me := widget.MeValues(p)
	widget.SetToday(me, time.Now())
	values, problems := defaults(ctx, find(ctx), spec, me, picked)
	logProblems(ctx, spec, problems)
	maps.Copy(values, me)

	return respond(load(ctx, w, values, nil, nil))
}

// picked reads what Home picked for a dashboard's filters from the query,
// as filter.<id>=value, repeated for a multiple filter. An empty value
// clears the filter, leaving it unset, and a filter the query doesn't
// mention is left out, to take its default.
func picked(spec Spec, query url.Values) (widget.Values, error) {
	values := widget.Values{}
	for key, raw := range query {
		id, ok := strings.CutPrefix(key, "filter.")
		if !ok {
			continue
		}
		i := slices.IndexFunc(spec.Filters, func(f Filter) bool { return f.ID == id })
		if i < 0 {
			return nil, badRequest("the dashboard has no filter " + id)
		}
		f := spec.Filters[i]
		picked := slices.DeleteFunc(slices.Clone(raw), func(value string) bool { return value == "" })
		if len(picked) > 1 && !f.Multiple {
			return nil, badRequest(f.ID + " takes one value")
		}
		for _, value := range picked {
			if f.Type == FilterEnum && !slices.Contains(f.Values, value) {
				return nil, badRequest(f.ID + " must be one of " + strings.Join(f.Values, ", "))
			}
		}
		if len(picked) == 0 {
			picked = nil
		}
		values[widget.ValueFilter+id] = picked
	}

	return values, nil
}

// blueprintWidget loads a widget on one of a blueprint's pages. Entity
// pages need the entity, for $entity.
func blueprintWidget(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return nil, err
	}
	src, err := source(ctx, p, blueprint.Name, ctx.PathParam("identifier"))
	if err != nil {
		return nil, err
	}
	spec, _ := blueprint.Parse(src)

	values := widget.MeValues(p)
	widget.SetToday(values, time.Now())
	var page *blueprint.Page
	switch ctx.PathParam("page") {
	case "catalog":
		page = spec.Catalog
	case "entity":
		page = spec.Entity
		if ctx.Param("entity") == "" {
			return nil, badRequest("choose the entity whose page it is")
		}
		values[widget.ValueEntity] = []string{ctx.Param("entity")}
	default:
		return nil, badRequest("page must be catalog or entity")
	}
	if page == nil {
		return nil, noWidget(ctx)
	}
	tab, ok := widget.FindTab(page.Tabs, ctx.PathParam("tab"))
	if !ok {
		return nil, noWidget(ctx)
	}
	w, ok := widget.Find(tab.Widgets, ctx.PathParam("widget"))
	if !ok {
		return nil, noWidget(ctx)
	}

	var data map[string][]any
	if ctx.PathParam("page") == "entity" {
		store := entity.NewStore(ctx.SQL)
		current, err := store.Get(ctx, spec.Identifier, ctx.Param("entity"))
		if errors.Is(err, entity.ErrNotFound) {
			return nil, httpx.Error{Code: http.StatusNotFound, Message: "no entity " + ctx.Param("entity")}
		}
		if err != nil {
			return nil, err
		}
		blueprints, err := blueprint.Published(ctx, ctx.SQL)
		if err != nil {
			return nil, err
		}
		blueprints[spec.Identifier] = spec
		if err := store.Resolve(ctx, blueprints, spec, []*entity.Entity{current}); err != nil {
			return nil, err
		}
		names := make([]string, 0, len(spec.Properties))
		for _, property := range spec.Properties {
			names = append(names, property.ID)
		}
		data = widget.EntityValues(current.Identifier, current.Title, current.Blueprint, current.Properties, names)
		maps.Copy(values, widget.TextValues(data))
	}
	return respond(load(ctx, w, values, &spec, data))
}

func noWidget(ctx *gofr.Context) error {
	return httpx.Error{Code: http.StatusNotFound, Message: "no widget " + ctx.PathParam("widget")}
}

// respond turns filters the data can't answer into bad requests.
func respond(data any, err error) (any, error) {
	var filterErr entity.FilterError
	if errors.As(err, &filterErr) {
		return nil, badRequest(filterErr.Error())
	}
	if err != nil {
		return nil, err
	}

	return data, nil
}

type (
	statData struct {
		Value *float64 `json:"value"`
	}
	groupData struct {
		Groups []entity.Facet `json:"groups"`
		None   int            `json:"none"`
	}
	tierData struct {
		Tiers []scorecard.TierCount `json:"tiers"`
	}
	textData struct {
		Text string `json:"text"`
	}
)

// load works out what a widget shows for whoever is looking, with own in
// place of its published version, so a draft's widgets can show the
// draft's derived properties.
func load(ctx *gofr.Context, w widget.Widget, values widget.Values, own *blueprint.Spec, data map[string][]any) (any, error) {
	if w.Type == widget.TypeMarkdown {
		return textData{Text: widget.Render(w.Text, values)}, nil
	}
	if w.Type == widget.TypeProperties {
		return nil, badRequest(w.Type + " widgets have no data to load")
	}
	f := widget.BindContext(w.Filter, values, data)
	blueprints, err := blueprint.Published(ctx, ctx.SQL)
	if err != nil {
		return nil, err
	}
	if own != nil {
		blueprints[own.Identifier] = *own
	}

	if w.Type == widget.TypeScorecard {
		scorecards, err := scorecard.Published(ctx, ctx.SQL)
		if err != nil {
			return nil, err
		}
		sc, ok := scorecards[w.Scorecard]
		if !ok {
			return nil, httpx.Error{Code: http.StatusNotFound, Message: "no published scorecard " + w.Scorecard}
		}
		tiers, err := scorecard.Tiers(ctx, ctx.SQL, sc, blueprints[sc.Blueprint], f)
		if err != nil {
			return nil, err
		}
		return tierData{Tiers: tiers}, nil
	}

	bp, ok := blueprints[w.Blueprint]
	if !ok {
		return nil, httpx.Error{Code: http.StatusNotFound, Message: "no published blueprint " + w.Blueprint}
	}
	store := entity.NewStore(ctx.SQL)
	switch w.Type {
	case widget.TypeStat:
		value, err := store.Aggregate(ctx, bp, f, w.Aggregate, w.Property)
		if err != nil {
			return nil, err
		}
		return statData{Value: value}, nil
	case widget.TypeBar, widget.TypeDonut:
		groups, none, err := store.Group(ctx, bp, f, w.GroupBy, w.Limit)
		if err != nil {
			return nil, err
		}
		return groupData{Groups: groups, None: none}, nil
	}

	page, err := store.List(ctx, entity.Query{Blueprint: bp, Filter: f, Sort: w.Sort, Page: 1, PageSize: w.Limit})
	if err != nil {
		return nil, err
	}
	if err := store.Resolve(ctx, blueprints, bp, page.Entities); err != nil {
		return nil, err
	}

	if err := store.ResolveScores(ctx, page.Entities); err != nil {
		return nil, err
	}
	return page, nil
}

func badRequest(message string) error {
	return httpx.Error{Code: http.StatusBadRequest, Message: message}
}
