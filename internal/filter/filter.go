// Package filter is the catalog's filter language: conditions on entities,
// which the entity package compiles to SQL.
package filter

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Filter is a condition on entities, written as JSON. It's a tree: and, or
// and not combine conditions, and a leaf compares one field, named by its
// path:
//
//	{"and": [{"property": "tier", "op": "in", "values": ["critical"]},
//	         {"not": {"property": "owner.title", "op": "exists"}}]}
//
// Built-in fields are identifier, title, blueprint, integration, createdAt
// and updatedAt. A $ starts a variable, never a field.
type Filter struct {
	And []Filter `json:"and,omitempty"`
	Or  []Filter `json:"or,omitempty"`
	Not *Filter  `json:"not,omitempty"`

	Property string `json:"property,omitempty"`
	Op       string `json:"op,omitempty"`
	Value    any    `json:"value,omitempty"`
	Values   []any  `json:"values,omitempty"`
	// ValuesFrom is the variable values names instead of a list, such as
	// $input.team, until it's bound.
	ValuesFrom string `json:"-"`
}

// Comparison operators. contains matches a list item, a related entity or
// text within a string; within matches dates in the last duration, such as 7d.
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
	OpWithin   = "within"
)

type plain Filter

// UnmarshalJSON reads values written as a variable, such as
// "values": "$input.team", into ValuesFrom.
func (f *Filter) UnmarshalJSON(data []byte) error {
	var raw struct {
		plain
		Values json.RawMessage `json:"values,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*f = Filter(raw.plain)
	if len(raw.Values) == 0 || string(raw.Values) == "null" {
		return nil
	}
	var variable string
	if json.Unmarshal(raw.Values, &variable) == nil {
		f.ValuesFrom = variable
		return nil
	}
	if err := json.Unmarshal(raw.Values, &f.Values); err != nil {
		return fmt.Errorf("values must be a list, or a variable such as $input.team")
	}

	return nil
}

func (f Filter) MarshalJSON() ([]byte, error) {
	if f.ValuesFrom == "" {
		return json.Marshal(plain(f))
	}

	return json.Marshal(struct {
		plain
		Values string `json:"values"`
	}{plain(f), f.ValuesFrom})
}

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

var durationPattern = regexp.MustCompile(`^([1-9][0-9]{0,5})([mhdw])$`)

var durationUnits = map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}

// ParseDuration reads a within duration, such as 30m, 12h, 7d or 2w.
func ParseDuration(text string) (time.Duration, bool) {
	match := durationPattern.FindStringSubmatch(text)
	if match == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(match[1])

	return time.Duration(n) * durationUnits[match[2]], true
}
