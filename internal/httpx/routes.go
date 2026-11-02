package httpx

import "gofr.dev/pkg/gofr"

// WebAPI is where the web UI's API is served. Routes are registered
// relative to it, and MCP tools call them the same way.
const WebAPI = "/api/web/"

// Routes is the registration surface shared by HTTP and in-process API callers.
type Routes interface {
	GET(string, gofr.Handler)
	POST(string, gofr.Handler)
	PUT(string, gofr.Handler)
	DELETE(string, gofr.Handler)
	AddCronJob(string, string, gofr.CronFunc)
}
