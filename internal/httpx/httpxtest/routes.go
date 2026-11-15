// Package httpxtest gives tests somewhere to register a package's routes.
package httpxtest

import "gofr.dev/pkg/gofr"

// Routes keeps what a package registers: handlers by method and path, such
// as "GET schemas", cron jobs' schedules by name, and start-up hooks.
type Routes struct {
	Handlers  map[string]gofr.Handler
	Schedules map[string]string
	OnStarts  []func(*gofr.Context) error
}

func New() *Routes {
	return &Routes{Handlers: map[string]gofr.Handler{}, Schedules: map[string]string{}}
}

func (r *Routes) GET(path string, h gofr.Handler)    { r.Handlers["GET "+path] = h }
func (r *Routes) POST(path string, h gofr.Handler)   { r.Handlers["POST "+path] = h }
func (r *Routes) PUT(path string, h gofr.Handler)    { r.Handlers["PUT "+path] = h }
func (r *Routes) DELETE(path string, h gofr.Handler) { r.Handlers["DELETE "+path] = h }

func (r *Routes) AddCronJob(schedule, name string, _ gofr.CronFunc) { r.Schedules[name] = schedule }

func (r *Routes) OnStart(hook func(*gofr.Context) error) { r.OnStarts = append(r.OnStarts, hook) }
