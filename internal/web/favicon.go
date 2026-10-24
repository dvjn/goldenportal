package web

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const faviconPath = "/favicon.ico"

// Favicon serves /favicon.ico from source: a data URI
// is decoded and served, and any other URL is redirected to. An empty source
// answers not found, so browsers fall back to no icon.
func Favicon(source string) (func(http.Handler) http.Handler, error) {
	var serve http.HandlerFunc

	switch {
	case source == "":
		serve = http.NotFound
	case strings.HasPrefix(source, "data:"):
		contentType, content, err := decodeDataURI(source)
		if err != nil {
			return nil, err
		}
		serve = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", "public, max-age=3600")
			if r.Method == http.MethodGet {
				_, _ = w.Write(content)
			}
		}
	default:
		serve = func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, source, http.StatusFound)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == faviconPath && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
				serve(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

// decodeDataURI reads data:[<media type>][;base64],<data>.
func decodeDataURI(uri string) (string, []byte, error) {
	header, data, ok := strings.Cut(strings.TrimPrefix(uri, "data:"), ",")
	if !ok {
		return "", nil, errors.New("the data URI has no comma before its data")
	}

	contentType, isBase64 := strings.CutSuffix(header, ";base64")
	if contentType == "" {
		contentType = "text/plain;charset=US-ASCII"
	}

	if isBase64 {
		content, err := base64.StdEncoding.DecodeString(data)
		return contentType, content, err
	}

	content, err := url.PathUnescape(data)

	return contentType, []byte(content), err
}
