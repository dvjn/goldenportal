package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dvjn/goldport/internal/httpx/httpxtest"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/logging"
)

func TestRoutesHideUnexpectedErrors(t *testing.T) {
	router := httpxtest.NewRouter(&container.Container{Logger: logging.NewMockLogger(logging.FATAL)})
	routes := NewRoutes(router)
	failures := map[string]error{
		"/api/v1/database": errors.New(`pq: relation "entities" does not exist`),
		"/api/v1/missing":  Error{Code: http.StatusNotFound, Message: "no entity has that identifier"},
		"/api/v1/server":   Error{Code: http.StatusInternalServerError, Message: "pq: connection refused"},
	}
	for path, failure := range failures {
		routes.GET(path, Public(), func(*gofr.Context) (any, error) { return nil, failure })
	}

	cases := []struct {
		path    string
		status  int
		message string
	}{
		{"/api/v1/database", http.StatusInternalServerError, "Something went wrong. Try again."},
		{"/api/v1/server", http.StatusInternalServerError, "Something went wrong. Try again."},
		{"/api/v1/missing", http.StatusNotFound, "no entity has that identifier"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))

		if recorder.Code != tc.status || !strings.Contains(recorder.Body.String(), `"message":"`+tc.message+`"`) {
			t.Errorf("GET %s = %d %s, want %d with %q", tc.path, recorder.Code, recorder.Body.String(), tc.status, tc.message)
		}
	}
}

func TestErrorCarriesTheStatusAndMessage(t *testing.T) {
	err := Error{Code: http.StatusNotFound, Message: "entity not found"}

	if got := err.StatusCode(); got != http.StatusNotFound {
		t.Fatalf("StatusCode() = %d, want %d", got, http.StatusNotFound)
	}
	if got := err.Error(); got != "entity not found" {
		t.Fatalf("Error() = %q, want %q", got, "entity not found")
	}
}

func TestWriteErrorWritesTheEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteError(recorder, http.StatusUnauthorized, "token is invalid")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", got, "application/json")
	}
	want := `{"error":{"message":"token is invalid"}}` + "\n"
	if got := recorder.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
