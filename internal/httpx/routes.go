package httpx

import "gofr.dev/pkg/gofr"

// Routes is the registration surface shared by HTTP and in-process API callers.
type Routes interface {
	GET(string, gofr.Handler)
	POST(string, gofr.Handler)
	PUT(string, gofr.Handler)
	DELETE(string, gofr.Handler)
	AddCronJob(string, string, gofr.CronFunc)
}
