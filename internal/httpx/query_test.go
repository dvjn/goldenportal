package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithQueryKeepsEveryKey(t *testing.T) {
	var keys int
	handler := WithQuery(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		query := Query(r.Context())
		keys = len(query)
		if got := query["filter.tier"]; len(got) != 2 || got[0] != "a,b" || got[1] != "" {
			t.Errorf("filter.tier = %q", got)
		}
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?filter.tier=a,b&filter.tier=&d=x", nil))

	if keys != 2 {
		t.Fatalf("keys = %d, want 2", keys)
	}
	if Query(t.Context()) != nil {
		t.Fatal("Query() found a query outside a request")
	}
}
