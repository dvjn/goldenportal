// Package filter is the catalog's filter language: conditions on entities,
// which the entity package compiles to SQL.
package filter

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Filter is a condition on entities, written as JSON. It's a tree: and, or
// and not combine conditions, and a leaf compares one property:
//
//	{"and": [{"property": "tier", "op": "in", "values": ["critical"]},
//	         {"not": {"property": "owner", "op": "exists"}}]}
//
// Built-in fields are $identifier, $title, $integration, $createdAt and $updatedAt.
type Filter struct {
	And []Filter `json:"and,omitempty"`
	Or  []Filter `json:"or,omitempty"`
	Not *Filter  `json:"not,omitempty"`

	Property string `json:"property,omitempty"`
	Op       string `json:"op,omitempty"`
	Value    any    `json:"value,omitempty"`
	Values   []any  `json:"values,omitempty"`
}

// Comparison operators. contains matches a list item, a related entity or
// text within a string.
const (
	OpEq       = "eq"
	OpNe       = "ne"
	OpIn       = "in"
	OpContains = "contains"
	OpExists   = "exists"
	OpGt       = "gt"
	OpGte      = "gte"
	OpLt       = "lt"
	OpLte      = "lte"
)

// Parse reads a filter from JSON. An empty string is no filter.
func Parse(raw string) (*Filter, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var f Filter
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return nil, fmt.Errorf("the filter is not valid JSON: %w", err)
	}

	return &f, nil
}
