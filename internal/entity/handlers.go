package entity

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/httpx"
)

func Register(app *gofr.App) {
	app.GET("/api/v1/entities", list)
	// Identifiers come from other systems and may contain slashes.
	app.GET("/api/v1/entities/{blueprint}/{identifier:.+}", get)
}

var pageSizes = []int{10, 20, 50, 100}

// list returns a page of entities, filtered by blueprint, integration and
// free text (q).
func list(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}

	q := Query{Blueprint: ctx.Param("blueprint"), Integration: ctx.Param("integration"), Search: ctx.Param("q"),
		Page: 1, PageSize: pageSizes[0]}
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

	page, err := NewStore(ctx.SQL).List(ctx, q)
	if err != nil {
		return nil, err
	}

	return page, nil
}

func get(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}

	e, err := NewStore(ctx.SQL).Get(ctx, ctx.PathParam("blueprint"), ctx.PathParam("identifier"))
	if errors.Is(err, ErrNotFound) {
		return nil, httpx.Error{Code: http.StatusNotFound, Message: "no such entity"}
	}
	if err != nil {
		return nil, err
	}

	return e, nil
}

func badRequest(message string) error {
	return httpx.Error{Code: http.StatusBadRequest, Message: message}
}
