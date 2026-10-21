package scim

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/dvjn/goldport/internal/directory"
)

const maxResults = 200

// The only filters supported are the equality lookups that Authentik sends
// after a 409: userName eq "x" for users and displayName eq "x" for groups.
// externalId eq "x" works for both.
var equalityFilter = regexp.MustCompile(`(?i)^\s*([a-z]+)\s+eq\s+("(?:[^"\\]|\\.)+")\s*$`)

var (
	userFilters = map[string]func(string) directory.UserFilter{
		"username":   func(value string) directory.UserFilter { return directory.UserFilter{UserName: value} },
		"externalid": func(value string) directory.UserFilter { return directory.UserFilter{ExternalID: value} },
	}
	groupFilters = map[string]func(string) directory.GroupFilter{
		"displayname": func(value string) directory.GroupFilter { return directory.GroupFilter{DisplayName: value} },
		"externalid":  func(value string) directory.GroupFilter { return directory.GroupFilter{ExternalID: value} },
	}
)

// parseFilter turns a filter into the store filter it names. An empty filter
// matches everything.
func parseFilter[F any](raw string, filters map[string]func(string) F) (F, error) {
	var none F
	if strings.TrimSpace(raw) == "" {
		return none, nil
	}

	match := equalityFilter.FindStringSubmatch(raw)
	if match == nil {
		return none, badRequest("invalidFilter", `only filters of the form attribute eq "value" are supported`)
	}
	build, supported := filters[strings.ToLower(match[1])]
	if !supported {
		return none, badRequest("invalidFilter", "filtering on "+match[1]+" is not supported")
	}
	value, err := strconv.Unquote(match[2])
	if err != nil {
		return none, badRequest("invalidFilter", "the filter value is not a valid string")
	}

	return build(value), nil
}

// parsePage reads startIndex, which counts from 1, and count from the query.
func parsePage(r *http.Request) directory.Page {
	startIndex := queryInt(r, "startIndex", 1)
	count := queryInt(r, "count", maxResults)

	return directory.Page{Offset: max(startIndex, 1) - 1, Limit: min(max(count, 0), maxResults)}
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}

	return value
}
