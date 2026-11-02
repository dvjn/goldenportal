package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPA(t *testing.T) {
	files := fstest.MapFS{
		"index.html":        {Data: []byte("<html>shell</html>")},
		"favicon.svg":       {Data: []byte("<svg/>")},
		"assets/app-abc.js": {Data: []byte("console.log(1)")},
	}
	handler := SPA(files)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	tests := []struct {
		name          string
		method        string
		path          string
		wantStatus    int
		wantBody      string
		wantCache     string
		wantEmptyBody bool
	}{
		{
			name:       "root serves index",
			method:     http.MethodGet,
			path:       "/",
			wantStatus: http.StatusOK,
			wantBody:   "shell",
			wantCache:  "no-cache",
		},
		{
			name:       "asset is immutable",
			method:     http.MethodGet,
			path:       "/assets/app-abc.js",
			wantStatus: http.StatusOK,
			wantBody:   "console.log(1)",
			wantCache:  "public, max-age=31536000, immutable",
		},
		{
			name:       "other file has no cache policy",
			method:     http.MethodGet,
			path:       "/favicon.svg",
			wantStatus: http.StatusOK,
			wantBody:   "<svg/>",
		},
		{
			name:       "deep link falls back to index",
			method:     http.MethodGet,
			path:       "/settings/profile",
			wantStatus: http.StatusOK,
			wantBody:   "shell",
			wantCache:  "no-cache",
		},
		{
			name:       "missing asset is not found",
			method:     http.MethodGet,
			path:       "/assets/gone.js",
			wantStatus: http.StatusTeapot,
		},
		{
			name:       "api path passes through",
			method:     http.MethodGet,
			path:       "/api/x",
			wantStatus: http.StatusTeapot,
		},
		{
			name:       "bare api path passes through",
			method:     http.MethodGet,
			path:       "/api",
			wantStatus: http.StatusTeapot,
		},
		{
			name:       "scim path passes through",
			method:     http.MethodGet,
			path:       "/scim/v2/Users",
			wantStatus: http.StatusTeapot,
		},
		{
			name:       "well-known path passes through",
			method:     http.MethodGet,
			path:       "/.well-known/health",
			wantStatus: http.StatusTeapot,
		},
		{
			name:          "head serves index without body",
			method:        http.MethodHead,
			path:          "/settings",
			wantStatus:    http.StatusOK,
			wantCache:     "no-cache",
			wantEmptyBody: true,
		},
		{
			name:       "post on spa path passes through",
			method:     http.MethodPost,
			path:       "/settings",
			wantStatus: http.StatusTeapot,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if got := recorder.Header().Get("Cache-Control"); got != test.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, test.wantCache)
			}
			if test.wantBody != "" && !strings.Contains(recorder.Body.String(), test.wantBody) {
				t.Errorf("body = %q, want it to contain %q", recorder.Body.String(), test.wantBody)
			}
			if test.wantEmptyBody && recorder.Body.Len() != 0 {
				t.Errorf("body = %q, want empty", recorder.Body.String())
			}
		})
	}
}

func TestSPAWithoutIndexPassesThrough(t *testing.T) {
	handler := SPA(fstest.MapFS{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTeapot)
	}
}

func TestSPAForbidsFraming(t *testing.T) {
	files := fstest.MapFS{"index.html": {Data: []byte("<html>shell</html>")}}
	handler := SPA(files)(http.NotFoundHandler())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=x", nil))
	if w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("Content-Security-Policy") != "frame-ancestors 'none'" {
		t.Errorf("pages can be framed: %v", w.Header())
	}
}

func TestSPALeavesMCPToTheServer(t *testing.T) {
	files := fstest.MapFS{"index.html": {Data: []byte("<html>shell</html>")}}
	handler := SPA(files)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	if w.Code != http.StatusTeapot {
		t.Errorf("GET /mcp got the app shell: %d", w.Code)
	}
}

func TestBuildHeaderNamesTheServedBuild(t *testing.T) {
	files := fstest.MapFS{"index.html": {Data: []byte(`<head><meta name="gp-build" content="m4k2x9"></head>`)}}
	build := Build(files)
	if build != "m4k2x9" || Build(fstest.MapFS{"index.html": {Data: []byte("<html>dev</html>")}}) != "" {
		t.Fatalf("build = %q", build)
	}

	handler := BuildHeader(build)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for path, want := range map[string]string{"/api/web/me": build, "/api/ingest/ci": "", "/catalog": ""} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if got := w.Header().Get("X-GP-Build"); got != want {
			t.Errorf("%s: X-GP-Build = %q, want %q", path, got, want)
		}
	}
}
