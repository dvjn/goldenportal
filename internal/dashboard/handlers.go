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

func Register(app httpx.Routes) {
	configdoc.Register(app, Kind)
	app.GET("/api/v1/home", home)
	app.GET("/api/v1/dashboards/{identifier}/view", dashboardView)
	app.GET("/api/v1/dashboards/{identifier}/data", dashboardData)
	app.GET("/api/v1/blueprints/{identifier}/pages/{page}/{tab}/data", blueprintData)
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
	// Defaults are what the inputs start at for them, by identifier, with
	// default queries run. An input without a default is left out; a query
	// that found nothing is an empty list, which selects nothing.
	Defaults map[string][]string `json:"defaults"`
	// Problems say why a default query failed, by input. The input starts
	// at nothing then, as if the query found none.
	Problems map[string]string `json:"problems,omitempty"`
}

// view works out how whoever is looking sees a dashboard, and what went
// wrong finding its defaults, for the log.
func view(ctx context.Context, find finder, spec Spec, p auth.Principal) (homeDashboard, map[string]error) {
	me := widget.MeValues(p)
	widget.SetToday(me, time.Now())
	values, problems := defaults(ctx, find, spec, me, nil)
	v := homeDashboard{Spec: spec.render(me), Defaults: map[string][]string{}, Problems: map[string]string{}}
	for _, in := range spec.Inputs {
		if value := values[widget.ValueInput+in.ID]; value != nil {
			v.Defaults[in.ID] = value
		}
		if err := problems[in.ID]; err != nil {
			v.Problems[in.ID] = explain(err)
		}
	}

	return v, problems
}

// explain says why a default query failed, to whoever is looking. A filter
// the blueprint can't answer names the field, which a builder can fix;
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
		ctx.Errorf("find the default of input %s on dashboard %s: %v", id, spec.Identifier, err)
	}
}

// defaultLimit caps what a default query finds, at what the bar offers.
const defaultLimit = 100

// finder finds the entities of a blueprint matching a filter, by
// identifier. It finds nothing when the blueprint isn't published.
type finder func(ctx context.Context, blueprint string, where *filter.Filter) ([]string, error)

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

		return entity.NewStore(ctx.SQL).Identifiers(c, blueprints, bp, where, defaultLimit)
	}
}

// defaults is what a dashboard's inputs are set to for whoever is looking,
// by context value: what they picked, or else the input's default, with a
// default query run against the catalog with their $me values. A query
// that finds no entity sets the input to Nothing, so widgets show nothing,
// unlike an input they cleared, which is unset and shows everything,
// unless the default says to start at everything otherwise. A query that
// fails shows nothing either way, and says why by input, so one broken
// dashboard neither takes Home down nor shows more than its author meant.
func defaults(ctx context.Context, find finder, spec Spec, me, picked widget.Values) (widget.Values, map[string]error) {
	values := widget.Values{}
	problems := map[string]error{}
	for _, in := range spec.Inputs {
		key := widget.ValueInput + in.ID
		if value, ok := picked[key]; ok {
			values[key] = value
			continue
		}
		if in.DefaultQuery == nil {
			values[key] = in.Default
			continue
		}
		q := in.DefaultQuery
		found, err := find(ctx, q.Query.Blueprint, widget.Bind(q.Query.Filter, me))
		if err != nil {
			problems[in.ID] = err
		}
		switch {
		case len(found) > 0:
			values[key] = found
		case err == nil && q.Otherwise == OtherwiseEverything:
			values[key] = nil
		default:
			values[key] = widget.Nothing
		}
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

// dashboardData loads every widget on a dashboard, as Home shows it to
// whoever is looking, with its inputs as picked.
func dashboardData(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return nil, err
	}
	src, err := source(ctx, p, Name, ctx.PathParam("identifier"))
	if err != nil {
		return nil, err
	}
	spec, _ := Parse(src)
	picked, err := picked(spec, httpx.Query(ctx))
	if err != nil {
		return nil, err
	}
	me := widget.MeValues(p)
	widget.SetToday(me, time.Now())
	values, problems := defaults(ctx, find(ctx), spec, me, picked)
	logProblems(ctx, spec, problems)
	maps.Copy(values, me)
	l, err := newLoader(ctx, widget.Env{Values: values}, nil)
	if err != nil {
		return nil, err
	}

	data := l.load(spec.Widgets)
	data.Inputs, err = inputLinks(ctx, l.store, spec, values)

	return data, err
}

// inputLinks names the entities each relation input is set to, so Home can
// show what's picked without loading what there is to pick from.
func inputLinks(ctx context.Context, store entity.Store, spec Spec, values widget.Values) (map[string][]entity.Link, error) {
	links := map[string][]entity.Link{}
	for _, in := range spec.Inputs {
		picked := values[widget.ValueInput+in.ID]
		if in.Type != InputRelation || len(picked) == 0 {
			continue
		}
		found, err := store.Links(ctx, in.Relation, picked)
		if err != nil {
			return nil, err
		}
		links[in.ID] = found
	}

	return links, nil
}

// newLoader loads what a page's widgets read, with own in place of its
// published version, so a draft's widgets can show the draft's fields.
func newLoader(ctx *gofr.Context, env widget.Env, own *blueprint.Spec) (loader, error) {
	blueprints, err := blueprint.Published(ctx, ctx.SQL)
	if err != nil {
		return loader{}, err
	}
	if own != nil {
		blueprints[own.Identifier] = *own
	}
	ladders, err := blueprint.Ladders(ctx, ctx.SQL)
	if err != nil {
		return loader{}, err
	}
	scorecards, err := scorecard.Published(ctx, ctx.SQL)
	if err != nil {
		return loader{}, err
	}

	return loader{
		ctx: ctx, db: ctx.SQL, store: entity.NewStore(ctx.SQL), blueprints: blueprints, ladders: ladders,
		scorecards: scorecards, env: env, logf: ctx.Errorf,
	}, nil
}

// picked reads what Home picked for a dashboard's inputs from the query,
// as input.<id>=value, repeated for a multiple input. An empty value
// clears the input, leaving it unset, and an input the query doesn't
// mention is left out, to take its default.
func picked(spec Spec, query url.Values) (widget.Values, error) {
	values := widget.Values{}
	for key, raw := range query {
		id, ok := strings.CutPrefix(key, "input.")
		if !ok {
			continue
		}
		i := slices.IndexFunc(spec.Inputs, func(in Input) bool { return in.ID == id })
		if i < 0 {
			return nil, badRequest("the dashboard has no input " + id)
		}
		in := spec.Inputs[i]
		picked := slices.DeleteFunc(slices.Clone(raw), func(value string) bool { return value == "" })
		if len(picked) > 1 && !in.Multiple {
			return nil, badRequest(in.ID + " takes one value")
		}
		for _, value := range picked {
			if in.Type == InputEnum && !slices.Contains(in.Values, value) {
				return nil, badRequest(in.ID + " must be one of " + strings.Join(in.Values, ", "))
			}
		}
		if len(picked) == 0 {
			picked = nil
		}
		values[widget.ValueInput+id] = picked
	}

	return values, nil
}

// blueprintData loads every widget on one tab of a blueprint's pages.
// Entity pages need the entity, for $entity.
func blueprintData(ctx *gofr.Context) (any, error) {
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
		return nil, noTab(ctx)
	}
	tab, ok := widget.FindTab(page.Tabs, ctx.PathParam("tab"))
	if !ok {
		return nil, noTab(ctx)
	}

	env := widget.Env{Values: values}
	l, err := newLoader(ctx, env, &spec)
	if err != nil {
		return nil, err
	}
	if ctx.PathParam("page") == "entity" {
		current, err := l.store.Get(ctx, spec.Identifier, ctx.Param("entity"))
		if errors.Is(err, entity.ErrNotFound) {
			return nil, httpx.Error{Code: http.StatusNotFound, Message: "no entity " + ctx.Param("entity")}
		}
		if err != nil {
			return nil, err
		}
		if err := l.store.Resolve(ctx, l.blueprints, spec, []*entity.Entity{current}); err != nil {
			return nil, err
		}
		names := make([]string, 0, len(spec.Properties))
		for _, property := range spec.Properties {
			names = append(names, property.ID)
		}
		l.env.Data = widget.EntityValues(current.Identifier, current.Title, current.Blueprint, current.Properties, names)
		maps.Copy(values, widget.TextValues(l.env.Data))
		l.env.Entity = entityVariable(current)
	}

	return l.load(tab.Widgets), nil
}

func noTab(ctx *gofr.Context) error {
	return httpx.Error{Code: http.StatusNotFound, Message: "no tab " + ctx.PathParam("tab")}
}

func notFound(message string) error {
	return httpx.Error{Code: http.StatusNotFound, Message: message}
}

func badRequest(message string) error {
	return httpx.Error{Code: http.StatusBadRequest, Message: message}
}
