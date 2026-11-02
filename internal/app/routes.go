package app

import (
	"context"
	"net/http"

	"github.com/dvjn/goldenportal/internal/httpx"

	"github.com/gorilla/mux"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	gofrHTTP "gofr.dev/pkg/gofr/http"
	"gofr.dev/pkg/gofr/http/response"
	"gofr.dev/pkg/gofr/logging"
)

// apiRoutes registers each API handler once for both GoFr and MCP dispatch.
// GoFr middleware wraps an already matched handler, not its router; changing a
// request's URL there cannot select another endpoint.
type apiRoutes struct {
	*gofr.App
	router *mux.Router
}

func newAPIRoutes(app *gofr.App) *apiRoutes {
	return &apiRoutes{App: app, router: newRouter()}
}

type containerKey struct{}

func (a *apiRoutes) handler(c *container.Container) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.router.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), containerKey{}, c)))
	})
}

// add serves h in-process. Unlike GoFr's own serving it applies no
// REQUEST_TIMEOUT: MCP calls such as a sync legitimately outlast it, and the
// caller's request context still cancels them.
func (a *apiRoutes) add(method, path string, h gofr.Handler) {
	a.router.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		c := r.Context().Value(containerKey{}).(*container.Container)
		ctx := &gofr.Context{
			Context: r.Context(), Request: gofrHTTP.NewRequest(r), Container: c,
			ContextLogger: logging.ContextLoggerFor(r.Context(), c.Logger),
		}
		responder := gofrHTTP.NewResponder(w, r.Method)
		defer func() {
			if recovered := recover(); recovered != nil {
				c.Logger.Errorf("in-process API handler panicked: %v", recovered)
				responder.Respond(nil, gofrHTTP.ErrorPanicRecovery{})
			}
		}()
		result, err := h(ctx)
		switch value := result.(type) {
		case response.Response:
			value.SetCustomHeaders(w)
		case *response.Response:
			if value != nil {
				value.SetCustomHeaders(w)
			}
		}
		responder.Respond(result, err)
	}).Methods(method)
}

func (a *apiRoutes) GET(path string, h gofr.Handler) {
	a.App.GET(path, h)
	a.add("GET", path, h)
}

func (a *apiRoutes) POST(path string, h gofr.Handler) {
	a.App.POST(path, h)
	a.add("POST", path, h)
}

func (a *apiRoutes) PUT(path string, h gofr.Handler) {
	a.App.PUT(path, h)
	a.add("PUT", path, h)
}

func (a *apiRoutes) DELETE(path string, h gofr.Handler) {
	a.App.DELETE(path, h)
	a.add("DELETE", path, h)
}

// newRouter answers unmatched in-process routes with the API error envelope.
func newRouter() *mux.Router {
	r := mux.NewRouter().SkipClean(true)
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, "route not found")
	})
	r.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
	})
	return r
}
