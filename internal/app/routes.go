package app

import (
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/httpx"
)

// webRoutes registers the web API's handlers under httpx.WebAPI, so
// packages name routes relative to it.
type webRoutes struct{ *gofr.App }

func (w webRoutes) GET(path string, h gofr.Handler)    { w.App.GET(httpx.WebAPI+path, h) }
func (w webRoutes) POST(path string, h gofr.Handler)   { w.App.POST(httpx.WebAPI+path, h) }
func (w webRoutes) PUT(path string, h gofr.Handler)    { w.App.PUT(httpx.WebAPI+path, h) }
func (w webRoutes) DELETE(path string, h gofr.Handler) { w.App.DELETE(httpx.WebAPI+path, h) }
