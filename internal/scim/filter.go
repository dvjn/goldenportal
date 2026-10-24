package scim

import (
	"regexp"
	"strings"
)

// filter is the one kind of SCIM filter the portal answers: attribute eq "value".
// Identity providers use it to look up a user or group before creating it.
type filter struct {
	attribute string
	value     string
}

var filterPattern = regexp.MustCompile(`(?i)^\s*([a-z][a-z0-9.:]*)\s+eq\s+"((?:[^"\\]|\\.)*)"\s*$`)

// parseFilter reads a filter. An empty filter matches everything; anything
// other than an eq comparison on a known attribute is rejected.
func parseFilter(text string, attributes map[string]string) (*filter, bool) {
	if strings.TrimSpace(text) == "" {
		return nil, true
	}

	match := filterPattern.FindStringSubmatch(text)
	if match == nil {
		return nil, false
	}

	column, ok := attributes[strings.ToLower(match[1])]
	if !ok {
		return nil, false
	}

	return &filter{attribute: column, value: strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(match[2])}, true
}

// userFilterAttributes maps the user attributes a filter may use to the
// expressions the store compares.
var userFilterAttributes = map[string]string{
	"username":     "username",
	"externalid":   "external_id",
	"emails.value": "email",
	"emails":       "email",
	"id":           "id",
}

var groupFilterAttributes = map[string]string{
	"displayname": "name",
	"externalid":  "external_id",
	"id":          "id",
}
