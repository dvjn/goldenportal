package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

const (
	indexName    = "index.html"
	assetsPrefix = "assets/"
)

var passthroughPrefixes = []string{"/api/", "/scim/", "/.well-known/"}

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
	if urlPath == "/api" {
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
