// Package scmfixture serves canned GitHub and GitLab API responses, so the
// connector tests and the dev stack exercise private-source authentication
// and pagination without real provider accounts.
package scmfixture

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

var (
	//go:embed github.json
	githubJSON []byte
	//go:embed gitlab.json
	gitlabJSON []byte
)

// Handler serves GitHub under /github and GitLab under /gitlab/api/v4, so
// those are the apiURL values to configure. Requests without the provider's
// token get the provider's 401. A key ending in ?page=N is that page of the
// collection, linked the way each provider paginates.
func Handler(githubToken, gitlabToken string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/github/", serve(githubJSON, "/github/", func(r *http.Request) bool {
		return githubToken != "" && r.Header.Get("Authorization") == "Bearer "+githubToken
	}))
	mux.Handle("/gitlab/api/v4/", serve(gitlabJSON, "/gitlab/api/v4/", func(r *http.Request) bool {
		return gitlabToken != "" && r.Header.Get("PRIVATE-TOKEN") == gitlabToken
	}))
	return mux
}

func serve(source []byte, prefix string, authorized func(*http.Request) bool) http.Handler {
	responses := map[string]json.RawMessage{}
	if err := json.Unmarshal(source, &responses); err != nil {
		panic(err)
	}
	gitlab := strings.HasPrefix(prefix, "/gitlab")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			if gitlab {
				_, _ = w.Write([]byte(`{"message":"401 Unauthorized"}`))
			} else {
				_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
			}
			return
		}
		key := strings.TrimPrefix(r.URL.EscapedPath(), prefix)
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		at := func(n int) string {
			if n == 1 {
				return key
			}
			return key + "?page=" + strconv.Itoa(n)
		}
		body, ok := responses[at(page)]
		if r.Method != http.MethodGet || !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		if _, more := responses[at(page+1)]; more {
			if gitlab {
				w.Header().Set("X-Next-Page", strconv.Itoa(page+1))
			} else {
				next := *r.URL
				q := next.Query()
				q.Set("page", strconv.Itoa(page+1))
				next.RawQuery = q.Encode()
				w.Header().Set("Link", `<http://`+r.Host+next.RequestURI()+`>; rel="next"`)
			}
		}
		_, _ = w.Write(body)
	})
}
