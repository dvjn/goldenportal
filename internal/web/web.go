package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/dvjn/goldenportal/internal/httpx"
)

//go:embed all:dist
var embedded embed.FS

const (
	indexName    = "index.html"
	assetsPrefix = "assets/"
)

var passthroughPrefixes = []string{"/api/", "/scim/", "/.well-known/"}

// passthroughPaths are served by the server alone, like the prefixes: the
// MCP endpoint answers GET itself.
var passthroughPaths = []string{"/mcp"}

func Dist() fs.FS {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}

	return dist
}

func SPA(files fs.FS) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name, ok := resolve(files, r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			setCacheControl(w, name)
			// No other site may frame the portal, so pages like the OAuth
			// consent can't be clicked through from under another page.
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
			http.ServeFileFS(w, r, files, name)
		})
	}
}

func resolve(files fs.FS, r *http.Request) (string, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return "", false
	}
	if isPassthrough(r.URL.Path) {
		return "", false
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = indexName
	}
	if isFile(files, name) {
		return name, true
	}
	if strings.HasPrefix(name, assetsPrefix) || !isFile(files, indexName) {
		return "", false
	}

	return indexName, true
}

func isPassthrough(urlPath string) bool {
	if urlPath == "/api" || slices.Contains(passthroughPaths, urlPath) {
		return true
	}
	for _, prefix := range passthroughPrefixes {
		if strings.HasPrefix(urlPath, prefix) {
			return true
		}
	}

	return false
}

func isFile(files fs.FS, name string) bool {
	info, err := fs.Stat(files, name)
	return err == nil && !info.IsDir()
}

func setCacheControl(w http.ResponseWriter, name string) {
	switch {
	case strings.HasPrefix(name, assetsPrefix):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case name == indexName:
		w.Header().Set("Cache-Control", "no-cache")
	}
}

var buildPattern = regexp.MustCompile(`<meta name="gp-build" content="([a-z0-9]+)"`)

// Build is the ID of the UI build files hold, from its index.html, or
// nothing when it has none.
func Build(files fs.FS) string {
	index, err := fs.ReadFile(files, indexName)
	if err != nil {
		return ""
	}
	if m := buildPattern.FindSubmatch(index); m != nil {
		return string(m[1])
	}

	return ""
}

// BuildHeader tells the web API's callers which UI build is served, so a
// tab running an older one knows to reload: the web API changes with it.
func BuildHeader(build string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if build != "" && strings.HasPrefix(r.URL.Path, httpx.WebAPI) {
				w.Header().Set("X-GP-Build", build)
			}
			next.ServeHTTP(w, r)
		})
	}
}
