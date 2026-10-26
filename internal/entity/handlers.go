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

func Register(app *gofr.App) {
	app.GET("/api/v1/catalog", catalog)
	app.GET("/api/v1/entities", list)
	app.GET("/api/v1/entities/facets", facets)
	// Identifiers come from other systems and may contain slashes.
	app.GET("/api/v1/entities/{blueprint}/{identifier:.+}", get)
}

// CatalogEntry is a published blueprint and how many entities it has.
type CatalogEntry struct {
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Entities    int    `json:"entities"`
}

// catalog lists the published blueprints, which are what the catalog shows.
func catalog(ctx *gofr.Context) (any, error) {
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
			Description: spec.Description, Icon: spec.Icon, Entities: counts[spec.Identifier],
		})
	}
	slices.SortFunc(entries, func(a, b CatalogEntry) int {
		return strings.Compare(a.Title+"\x00"+a.Identifier, b.Title+"\x00"+b.Identifier)
	})

	return entries, nil
}

var pageSizes = []int{10, 20, 50, 100}

// list returns a page of one blueprint's entities: searched by q, filtered by
// filter (see Filter) and sorted by sort.
func list(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	specs, spec, err := publishedBlueprint(ctx, ctx.Param("blueprint"))
	if err != nil {
		return nil, err
	}

	q := Query{Blueprint: spec, Search: ctx.Param("q"), Sort: ctx.Param("sort"), Page: 1, PageSize: pageSizes[0]}
	if value := ctx.Param("page"); value != "" {
		page, err := strconv.Atoi(value)
		if err != nil || page < 1 {
			return nil, badRequest("page must be a whole number from 1")
		}
		q.Page = page
	}
	if value := ctx.Param("size"); value != "" {
		size, err := strconv.Atoi(value)
		if err != nil || !slices.Contains(pageSizes, size) {
			return nil, badRequest("size must be 10, 20, 50 or 100")
		}
		q.PageSize = size
	}
	if q.Filter, err = ParseFilter(ctx.Param("filter")); err != nil {
		return nil, badRequest(err.Error())
	}

	store := NewStore(ctx.SQL)
	page, err := store.List(ctx, q)
	var filterErr FilterError
	if errors.As(err, &filterErr) {
		return nil, badRequest(filterErr.Error())
	}
	if err != nil {
		return nil, err
	}
	if err := store.Resolve(ctx, specs, spec, page.Entities); err != nil {
		return nil, err
	}

	if err := store.ResolveScores(ctx, page.Entities); err != nil {
		return nil, err
	}
	return page, nil
}

// detail is an entity with the entities that point at it.
type detail struct {
	*Entity
	Incoming []Incoming `json:"incoming"`
}

func get(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	specs, spec, err := publishedBlueprint(ctx, ctx.PathParam("blueprint"))
	if err != nil {
		return nil, err
	}

	store := NewStore(ctx.SQL)
	e, err := store.Get(ctx, spec.Identifier, ctx.PathParam("identifier"))
	if errors.Is(err, ErrNotFound) {
		return nil, httpx.Error{Code: http.StatusNotFound, Message: "no such entity"}
	}
	if err != nil {
		return nil, err
	}
	if err := store.Resolve(ctx, specs, spec, []*Entity{e}); err != nil {
		return nil, err
	}
	incoming, err := store.Incoming(ctx, specs, spec.Identifier, e.Identifier)
	if err != nil {
		return nil, err
	}

	return detail{Entity: e, Incoming: incoming}, nil
}

// facets counts a property's values, for filter options.
func facets(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	_, spec, err := publishedBlueprint(ctx, ctx.Param("blueprint"))
	if err != nil {
		return nil, err
	}

	values, err := NewStore(ctx.SQL).Facets(ctx, spec, ctx.Param("property"))
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
