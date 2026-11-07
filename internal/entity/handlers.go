package entity

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/widget"
)

// Register serves the catalog. The handlers read the request and call the
// services below, which MCP calls too.
func Register(app httpx.Routes) {
	app.GET("catalog", catalog)
	app.GET("catalog/{blueprint}", list)
	app.GET("catalog/{blueprint}/facets", facets)
	// Identifiers come from other systems and may contain slashes.
	app.GET("catalog/{blueprint}/entities/{identifier:.+}", get)
}

// CatalogEntry is a published blueprint and how many entities it has.
type CatalogEntry struct {
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
	Entities    int    `json:"entities"`
}

func catalog(ctx *gofr.Context) (any, error) { return Catalog(ctx) }

// Catalog lists the published blueprints, which are what the catalog shows.
func Catalog(ctx *gofr.Context) ([]CatalogEntry, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	specs, err := blueprint.Published(ctx, ctx.SQL)
	if err != nil {
		return nil, err
	}

	counts := map[string]int{}
	rows, err := ctx.SQL.QueryContext(ctx, `
		SELECT b.identifier, count(*) FROM entities e
		JOIN config_documents b ON b.id = e.blueprint_id
		GROUP BY b.identifier`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			identifier string
			count      int
		)
		if err := rows.Scan(&identifier, &count); err != nil {
			return nil, err
		}
		counts[identifier] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	entries := make([]CatalogEntry, 0, len(specs))
	for _, spec := range specs {
		entries = append(entries, CatalogEntry{
			Identifier: spec.Identifier, Title: spec.Title,
			Description: spec.Description, Icon: spec.Icon, Color: spec.Color, Entities: counts[spec.Identifier],
		})
	}
	slices.SortFunc(entries, func(a, b CatalogEntry) int {
		return strings.Compare(a.Title+"\x00"+a.Identifier, b.Title+"\x00"+b.Identifier)
	})

	return entries, nil
}

// ListRequest asks for a page of a blueprint's entities: searched by
// Search, filtered by Filter (see Filter) and sorted by Sort, a path. A
// zero Page or Size means the first page or the smallest size.
type ListRequest struct {
	Search, Filter, Sort string
	Page, Size           int
}

func list(ctx *gofr.Context) (any, error) {
	req := ListRequest{Search: ctx.Param("q"), Filter: ctx.Param("filter"), Sort: ctx.Param("sort")}
	var err error
	if req.Page, req.Size, err = httpx.Paging(ctx.Param); err != nil {
		return nil, err
	}

	return httpx.Result(List(ctx, ctx.PathParam("blueprint"), req))
}

// List returns a page of one blueprint's entities.
func List(ctx *gofr.Context, blueprintID string, req ListRequest) (Page, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return Page{}, err
	}
	specs, spec, err := publishedBlueprint(ctx, blueprintID)
	if err != nil {
		return Page{}, err
	}

	q := Query{Blueprint: spec, Blueprints: specs, Search: req.Search, Sort: req.Sort}
	if q.Page, q.PageSize, err = httpx.CheckPaging(req.Page, req.Size); err != nil {
		return Page{}, err
	}
	if q.Filter, err = ParseFilter(req.Filter); err != nil {
		return Page{}, httpx.BadRequest(err.Error())
	}
	if q.Ladders, err = blueprint.Ladders(ctx, ctx.SQL); err != nil {
		return Page{}, err
	}

	store := NewStore(ctx.SQL)
	page, err := store.List(ctx, q)
	var filterErr FilterError
	if errors.As(err, &filterErr) {
		return Page{}, httpx.BadRequest(filterErr.Error())
	}
	if err != nil {
		return Page{}, err
	}
	if err := store.Resolve(ctx, specs, spec, page.Items); err != nil {
		return Page{}, err
	}

	return page, nil
}

// Detail is an entity with the entities that point at it.
type Detail struct {
	*Entity
	Incoming []Incoming `json:"incoming"`
	// HiddenTabs are the page's tabs whose when the entity doesn't meet.
	HiddenTabs []string `json:"hiddenTabs,omitempty"`
}

func get(ctx *gofr.Context) (any, error) {
	return httpx.Result(Get(ctx, ctx.PathParam("blueprint"), ctx.PathParam("identifier")))
}

// Get reads an entity with its resolved properties and incoming relations.
func Get(ctx *gofr.Context, blueprintID, identifier string) (Detail, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return Detail{}, err
	}
	specs, spec, err := publishedBlueprint(ctx, blueprintID)
	if err != nil {
		return Detail{}, err
	}

	store := NewStore(ctx.SQL)
	e, err := store.Get(ctx, spec.Identifier, identifier)
	if errors.Is(err, ErrNotFound) {
		return Detail{}, httpx.NotFound("no such entity")
	}
	if err != nil {
		return Detail{}, err
	}
	if err := store.Resolve(ctx, specs, spec, []*Entity{e}); err != nil {
		return Detail{}, err
	}
	incoming, err := store.Incoming(ctx, specs, spec.Identifier, e.Identifier)
	if err != nil {
		return Detail{}, err
	}
	values := widget.MeValues(p)
	widget.SetToday(values, time.Now())
	hidden, err := store.HiddenTabs(ctx, Query{Blueprint: spec, Blueprints: specs}, e, values)
	if err != nil {
		return Detail{}, err
	}

	return Detail{Entity: e, Incoming: incoming, HiddenTabs: hidden}, nil
}

// HiddenTabs lists the tabs of e's page whose when e doesn't meet, in one
// statement: each when is a share of e alone, 100 or 0. Values are what
// they compare with besides e's own fields, such as $me.
func (s Store) HiddenTabs(ctx context.Context, q Query, e *Entity, values widget.Values) ([]string, error) {
	if q.Blueprint.Entity == nil {
		return nil, nil
	}
	names := make([]string, 0, len(q.Blueprint.Properties))
	for _, property := range q.Blueprint.Properties {
		names = append(names, property.ID)
	}
	data := widget.EntityValues(e.Identifier, e.Title, q.Blueprint.Identifier, e.Properties, names)
	var tabs []string
	var shares []Aggregate
	for _, tab := range q.Blueprint.Entity.Tabs {
		// A when that compares only with values that aren't set has
		// nothing left to ask, so the tab shows.
		if when := widget.BindContext(tab.When, values, data); when != nil {
			tabs = append(tabs, tab.ID)
			shares = append(shares, Aggregate{Op: "share", Share: when})
		}
	}
	if len(shares) == 0 {
		return nil, nil
	}
	q.Filter = &Filter{Property: "identifier", Op: filter.OpEq, Value: e.Identifier}
	m, err := NewMatch(q)
	if err != nil {
		return nil, err
	}
	met, err := s.Aggregates(ctx, m, shares)
	if err != nil {
		return nil, err
	}
	var hidden []string
	for i, share := range met {
		if share == nil || *share == 0 {
			hidden = append(hidden, tabs[i])
		}
	}

	return hidden, nil
}

func facets(ctx *gofr.Context) (any, error) {
	return Facets(ctx, ctx.PathParam("blueprint"), ctx.Param("property"))
}

// Facets counts a property's values, for filter options.
func Facets(ctx *gofr.Context, blueprintID, property string) ([]Facet, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	specs, spec, err := publishedBlueprint(ctx, blueprintID)
	if err != nil {
		return nil, err
	}

	ladders, err := blueprint.Ladders(ctx, ctx.SQL)
	if err != nil {
		return nil, err
	}
	values, err := NewStore(ctx.SQL).Facets(ctx, Query{Blueprint: spec, Blueprints: specs, Ladders: ladders}, property)
	var filterErr FilterError
	if errors.As(err, &filterErr) {
		return nil, httpx.BadRequest(filterErr.Error())
	}
	if err != nil {
		return nil, err
	}

	return values, nil
}

// publishedBlueprint returns the published blueprints and the one named.
func publishedBlueprint(ctx *gofr.Context, identifier string) (map[string]blueprint.Spec, blueprint.Spec, error) {
	if identifier == "" {
		return nil, blueprint.Spec{}, httpx.BadRequest("choose a blueprint")
	}
	specs, err := blueprint.Published(ctx, ctx.SQL)
	if err != nil {
		return nil, blueprint.Spec{}, err
	}
	spec, ok := specs[identifier]
	if !ok {
		return nil, blueprint.Spec{}, httpx.NotFound("no such blueprint")
	}

	return specs, spec, nil
}
