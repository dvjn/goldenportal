package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/http/response"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/httpx"
)

func TestInProcessRoutesBindRequestAndPreserveResponses(t *testing.T) {
	routes := newAPIRoutes(nil)
	c := &container.Container{Logger: logging.NewLogger(logging.ERROR)}
	routes.add("PUT", "/api/v1/documents/{name}", func(ctx *gofr.Context) (any, error) {
		var body map[string]any
		if err := ctx.Bind(&body); err != nil {
			return nil, err
		}
		if ctx.SQL != c.SQL || ctx.PathParam("name") != "example" || ctx.Param("dryRun") != "true" || body["source"] != "yaml" {
			t.Fatalf("incorrect container, path, query or body: %+v %+v", ctx.Request, body)
		}
		return response.Response{Headers: map[string]string{"X-Example": "yes"}, Data: body}, nil
	})
	routes.add("GET", "/api/v1/export", func(*gofr.Context) (any, error) {
		return response.File{Content: []byte("kind: Blueprint\n"), ContentType: "application/yaml"}, nil
	})
	routes.add("GET", "/api/v1/denied", func(*gofr.Context) (any, error) {
		return nil, httpx.Error{Code: 403, Message: "denied"}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/v1/documents/example?dryRun=true", strings.NewReader(`{"source":"yaml"}`))
	r.Header.Set("Content-Type", "application/json")
	routes.handler(c).ServeHTTP(w, r)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || w.Header().Get("X-Example") != "yes" || out["data"].(map[string]any)["source"] != "yaml" {
		t.Fatalf("response = %d %v %s", w.Code, w.Header(), w.Body)
	}
	w = httptest.NewRecorder()
	routes.handler(c).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/export", nil))
	if w.Header().Get("Content-Type") != "application/yaml" || w.Body.String() != "kind: Blueprint\n" {
		t.Fatalf("file response = %v %s", w.Header(), w.Body)
	}
	w = httptest.NewRecorder()
	routes.handler(c).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/denied", nil))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "denied") {
		t.Fatalf("error response = %d %s", w.Code, w.Body)
	}
}
