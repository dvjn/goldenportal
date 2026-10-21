package httpx

import (
	"context"
	"net/http"
)

type exchangeKey struct{}

type exchange struct {
	writer  http.ResponseWriter
	request *http.Request
}

// ExposeRaw keeps the response writer and the request in the request context,
// so gofr handlers can read and set cookies, which the gofr Request hides.
func ExposeRaw(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := &exchange{writer: w}
		current.request = r.WithContext(context.WithValue(r.Context(), exchangeKey{}, current))
		next.ServeHTTP(w, current.request)
	})
}

func RawRequest(ctx context.Context) *http.Request {
	return exchangeFrom(ctx).request
}

func RawWriter(ctx context.Context) http.ResponseWriter {
	return exchangeFrom(ctx).writer
}

func exchangeFrom(ctx context.Context) *exchange {
	current, _ := ctx.Value(exchangeKey{}).(*exchange)
	if current == nil {
		return &exchange{}
	}

	return current
}
