package httpx

import (
	"net/http"

	"gofr.dev/pkg/gofr"
)

// Permission names an action as resource.action. The access package declares
// every permission.
type Permission string

// Access is the rule a route declares for who may call it. A route that does
// not declare one is refused.
type Access struct {
	session bool
}

func Public() Access { return Access{} }

func Authenticated() Access { return Access{session: true} }

func (a Access) RequiresSession() bool { return a.session }

type registrar interface {
	GET(pattern string, handler gofr.Handler)
	POST(pattern string, handler gofr.Handler)
	PUT(pattern string, handler gofr.Handler)
	PATCH(pattern string, handler gofr.Handler)
	DELETE(pattern string, handler gofr.Handler)
}

// Routes is the only place that registers handlers on the gofr app, so every
// route carries an Access rule that middleware can look up.
type Routes struct {
	app      registrar
	declared map[routeKey]Access
}

type routeKey struct {
	method  string
	pattern string
}

func NewRoutes(app registrar) *Routes {
	return &Routes{app: app, declared: map[routeKey]Access{}}
}

func (r *Routes) GET(pattern string, access Access, handler gofr.Handler) {
	r.declare(http.MethodGet, pattern, access)
	r.app.GET(pattern, handler)
}

func (r *Routes) POST(pattern string, access Access, handler gofr.Handler) {
	r.declare(http.MethodPost, pattern, access)
	r.app.POST(pattern, handler)
}

func (r *Routes) PUT(pattern string, access Access, handler gofr.Handler) {
	r.declare(http.MethodPut, pattern, access)
	r.app.PUT(pattern, handler)
}

func (r *Routes) PATCH(pattern string, access Access, handler gofr.Handler) {
	r.declare(http.MethodPatch, pattern, access)
	r.app.PATCH(pattern, handler)
}

func (r *Routes) DELETE(pattern string, access Access, handler gofr.Handler) {
	r.declare(http.MethodDelete, pattern, access)
	r.app.DELETE(pattern, handler)
}

// Lookup returns the Access declared for a method and a matched route
// template, as gofr's RouteTemplate reports it.
func (r *Routes) Lookup(method, template string) (Access, bool) {
	access, ok := r.declared[routeKey{method: method, pattern: template}]
	return access, ok
}

func (r *Routes) declare(method, pattern string, access Access) {
	r.declared[routeKey{method: method, pattern: pattern}] = access
}
