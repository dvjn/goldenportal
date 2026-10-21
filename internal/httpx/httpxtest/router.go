// Package httpxtest serves gofr handlers through gofr's router in tests,
// without the port probe that a full gofr.App runs.
package httpxtest

import (
	"net/http"
	"sync"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	gofrHTTP "gofr.dev/pkg/gofr/http"
	"gofr.dev/pkg/gofr/logging"
)

type Router struct {
	router    *gofrHTTP.Router
	container *container.Container
	catchAll  sync.Once
}

func NewRouter(c *container.Container) *Router {
	return &Router{router: gofrHTTP.NewRouter(), container: c}
}

func (r *Router) Use(middlewares ...gofrHTTP.Middleware) {
	r.router.UseMiddleware(middlewares...)
}

func (r *Router) GET(pattern string, handler gofr.Handler) {
	r.add(http.MethodGet, pattern, handler)
}

func (r *Router) POST(pattern string, handler gofr.Handler) {
	r.add(http.MethodPost, pattern, handler)
}

func (r *Router) PUT(pattern string, handler gofr.Handler) {
	r.add(http.MethodPut, pattern, handler)
}

func (r *Router) PATCH(pattern string, handler gofr.Handler) {
	r.add(http.MethodPatch, pattern, handler)
}

func (r *Router) DELETE(pattern string, handler gofr.Handler) {
	r.add(http.MethodDelete, pattern, handler)
}

// ServeHTTP adds gofr's catch-all route on the first request, after every
// other route, as gofr does when its server starts.
func (r *Router) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	r.catchAll.Do(func() {
		r.router.PathPrefix("/").Handler(http.NotFoundHandler())
	})
	r.router.ServeHTTP(w, request)
}

func (r *Router) add(method, pattern string, handler gofr.Handler) {
	r.router.Add(method, pattern, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		ctx := &gofr.Context{
			Context:       request.Context(),
			Request:       gofrHTTP.NewRequest(request),
			Container:     r.container,
			ContextLogger: logging.ContextLoggerFor(request.Context(), r.container.Logger),
		}
		result, err := handler(ctx)
		gofrHTTP.NewResponder(w, request.Method).Respond(result, err)
	}))
}
