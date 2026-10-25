package dashboard

import (
	"errors"
	"net/http"
	"slices"
	"strings"

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
	app.GET("/api/v1/dashboards/{identifier}/widgets/{widget}", dashboardWidget)
	app.GET("/api/v1/blueprints/{identifier}/widgets/{page}/{tab}/{widget}", blueprintWidget)
}

// home lists the published dashboards, in the order Home shows them.
func home(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	sources, err := configdoc.NewStore(ctx.SQL, Name).PublishedSources(ctx)
	if err != nil {
		return nil, err
	}
	specs := []Spec{}
	for _, source := range sources {
		if spec, issues := Parse(source); !configdoc.HasErrors(issues) {
			specs = append(specs, spec)
		}
	}
	slices.SortFunc(specs, func(a, b Spec) int {
		if a.Order != b.Order {
			return a.Order - b.Order
		}
		return strings.Compare(a.Title+"\x00"+a.Identifier, b.Title+"\x00"+b.Identifier)
	})

	return specs, nil
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

	return respond(load(ctx, w, widget.Bind(w.Filter, map[string]string{widget.ValueMe: p.Email}), nil))
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

	values := map[string]string{widget.ValueMe: p.Email}
	var page *blueprint.Page
	switch ctx.PathParam("page") {
	case "catalog":
		page = spec.Catalog
	case "entity":
		page = spec.Entity
		values[widget.ValueEntity] = ctx.Param("entity")
		if values[widget.ValueEntity] == "" {
			return nil, badRequest("choose the entity whose page it is")
		}
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

	return respond(load(ctx, w, widget.Bind(w.Filter, values), &spec))
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
)

// load works out what a widget shows, with own in place of its published
// version, so a draft's widgets can show the draft's derived properties.
func load(ctx *gofr.Context, w widget.Widget, f *filter.Filter, own *blueprint.Spec) (any, error) {
	if w.Type == widget.TypeMarkdown || w.Type == widget.TypeProperties {
		return nil, badRequest(w.Type + " widgets have no data to load")
	}
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

	return page, nil
}

func badRequest(message string) error {
	return httpx.Error{Code: http.StatusBadRequest, Message: message}
}
