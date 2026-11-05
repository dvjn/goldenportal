package httpx

import "gofr.dev/pkg/gofr"

// WebAPI is where the web UI's API is served. Routes are registered
// relative to it.
const WebAPI = "/api/web/"

// Routes is where packages register their web API handlers, relative to WebAPI.
type Routes interface {
	GET(string, gofr.Handler)
	POST(string, gofr.Handler)
	PUT(string, gofr.Handler)
	DELETE(string, gofr.Handler)
	AddCronJob(string, string, gofr.CronFunc)
	OnStart(func(*gofr.Context) error)
}
