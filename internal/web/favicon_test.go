package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func serveFavicon(t *testing.T, source, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	favicon, err := Favicon(source)
	if err != nil {
		t.Fatalf("Favicon(%q) error = %v", source, err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	rec := httptest.NewRecorder()
	favicon(next).ServeHTTP(rec, httptest.NewRequest(method, path, nil))

	return rec
}

func TestFaviconServesADataURI(t *testing.T) {
	rec := serveFavicon(t, "data:image/svg+xml,<svg fill='%23d97706'/>", http.MethodGet, "/favicon.ico")

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("got %d %q, want 200 image/svg+xml", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := rec.Body.String(); got != "<svg fill='#d97706'/>" {
		t.Fatalf("body = %q", got)
	}
}

func TestFaviconServesABase64DataURI(t *testing.T) {
	rec := serveFavicon(t, "data:image/png;base64,iVBORw==", http.MethodGet, "/favicon.ico")

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || rec.Body.Len() != 4 {
		t.Fatalf("got %d %q %d bytes", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
}

func TestFaviconRedirectsToAURL(t *testing.T) {
	rec := serveFavicon(t, "https://cdn.example.com/icon.png", http.MethodGet, "/favicon.ico")

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://cdn.example.com/icon.png" {
		t.Fatalf("got %d to %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestFaviconIsNotFoundWithoutASource(t *testing.T) {
	if rec := serveFavicon(t, "", http.MethodGet, "/favicon.ico"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestFaviconLeavesOtherPathsAlone(t *testing.T) {
	if rec := serveFavicon(t, "", http.MethodGet, "/catalog"); rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want the next handler", rec.Code)
	}
}

func TestFaviconRejectsAMalformedDataURI(t *testing.T) {
	if _, err := Favicon("data:image/png;base64"); err == nil {
		t.Fatal("Favicon() accepted a data URI without data")
	}
}
