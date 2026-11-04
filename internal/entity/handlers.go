package entity

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/httpx"
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

var pageSizes = []int{10, 20, 50, 100}

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
	if req.Page, err = intParam(ctx, "page"); err != nil {
		return nil, badRequest("page must be a whole number from 1")
	}
	if req.Size, err = intParam(ctx, "size"); err != nil {
		return nil, badRequest("size must be 10, 20, 50 or 100")
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

	q := Query{Blueprint: spec, Blueprints: specs, Search: req.Search, Sort: req.Sort, Page: 1, PageSize: pageSizes[0]}
	if req.Page != 0 {
		if req.Page < 1 {
			return Page{}, badRequest("page must be a whole number from 1")
		}
		q.Page = req.Page
	}
	if req.Size != 0 {
		if !slices.Contains(pageSizes, req.Size) {
			return Page{}, badRequest("size must be 10, 20, 50 or 100")
		}
		q.PageSize = req.Size
	}
	if q.Filter, err = ParseFilter(req.Filter); err != nil {
		return Page{}, badRequest(err.Error())
	}
	if q.Ladders, err = blueprint.Ladders(ctx, ctx.SQL); err != nil {
		return Page{}, err
	}

	store := NewStore(ctx.SQL)
	page, err := store.List(ctx, q)
	var filterErr FilterError
	if errors.As(err, &filterErr) {
		return Page{}, badRequest(filterErr.Error())
	}
	if err != nil {
		return Page{}, err
	}
	if err := store.Resolve(ctx, specs, spec, page.Entities); err != nil {
		return Page{}, err
	}

	return page, nil
}

// intParam reads a whole-number query parameter, zero when it's absent.
func intParam(ctx *gofr.Context, name string) (int, error) {
	value := ctx.Param(name)
	if value == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(value)
	if err == nil && n == 0 {
		err = strconv.ErrRange
	}

	return n, err
}

// Detail is an entity with the entities that point at it.
type Detail struct {
	*Entity
	Incoming []Incoming `json:"incoming"`
}

func get(ctx *gofr.Context) (any, error) {
	return httpx.Result(Get(ctx, ctx.PathParam("blueprint"), ctx.PathParam("identifier")))
}

// Get reads an entity with its resolved properties and incoming relations.
func Get(ctx *gofr.Context, blueprintID, identifier string) (Detail, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return Detail{}, err
	}
	specs, spec, err := publishedBlueprint(ctx, blueprintID)
	if err != nil {
		return Detail{}, err
	}

	store := NewStore(ctx.SQL)
	e, err := store.Get(ctx, spec.Identifier, identifier)
	if errors.Is(err, ErrNotFound) {
		return Detail{}, httpx.Error{Code: http.StatusNotFound, Message: "no such entity"}
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

	return Detail{Entity: e, Incoming: incoming}, nil
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
		return nil, badRequest(filterErr.Error())
	}
	if err != nil {
		return nil, err
	}

	return values, nil
}

// publishedBlueprint returns the published blueprints and the one named.
func publishedBlueprint(ctx *gofr.Context, identifier string) (map[string]blueprint.Spec, blueprint.Spec, error) {
	if identifier == "" {
		return nil, blueprint.Spec{}, badRequest("choose a blueprint")
	}
	specs, err := blueprint.Published(ctx, ctx.SQL)
	if err != nil {
		return nil, blueprint.Spec{}, err
	}
	spec, ok := specs[identifier]
	if !ok {
		return nil, blueprint.Spec{}, httpx.Error{Code: http.StatusNotFound, Message: "no such blueprint"}
	}

	return specs, spec, nil
}

func badRequest(message string) error {
	return httpx.Error{Code: http.StatusBadRequest, Message: message}
}
