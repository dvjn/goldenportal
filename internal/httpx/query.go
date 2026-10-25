package httpx

import (
	"context"
	"net/http"
	"net/url"
)

type queryKey struct{}

// WithQuery keeps each request's query in its context, for handlers that
// need every key it has, which gofr's Request can't list.
func WithQuery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), queryKey{}, r.URL.Query())))
	})
}

// Query returns the request's query, as WithQuery kept it.
func Query(ctx context.Context) url.Values {
	query, _ := ctx.Value(queryKey{}).(url.Values)
	return query
}
