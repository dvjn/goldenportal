package integration

import (
	"fmt"
	"strconv"

	"github.com/itchyny/gojq"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// Entry is one entity made from an item, the identifier of one to delete,
// or the reason it couldn't be either.
type Entry struct {
	Blueprint string
	Entity    blueprint.Entity
	Deleted   bool
	Problems  []string
}

// mapper runs an integration's mappings, compiled once for a run.
type mapper struct {
	byResource map[string][]compiled
}

type compiled struct {
	blueprint                 string
	filter, identifier, title *gojq.Code
	properties                map[string]*gojq.Code
}

// mapper compiles the mappings.
func (s Spec) mapper() (mapper, error) {
	m := mapper{byResource: map[string][]compiled{}}
	for _, mapping := range s.Mappings {
		c, err := mapping.compile()
		if err != nil {
			return m, fmt.Errorf("the %s mapping: %w", mapping.Blueprint, err)
		}
		m.byResource[mapping.Resource] = append(m.byResource[mapping.Resource], c)
	}

	return m, nil
}

// Map turns items into entries through the mappings of each item's resource.
func (m mapper) Map(items []Item) []Entry {
	var entries []Entry
	for _, item := range items {
		for _, c := range m.byResource[item.Resource] {
			if entry, ok := c.apply(item); ok {
				entries = append(entries, entry)
			}
		}
	}

	return entries
}

// compile parses jq that may use the variables named.
func compile(expression string, variables ...string) (*gojq.Code, error) {
	query, err := gojq.Parse(expression)
	if err != nil {
		return nil, err
	}

	return gojq.Compile(query, gojq.WithVariables(variables))
}

func (m Mapping) compile() (compiled, error) {
	var (
		c   = compiled{blueprint: m.Blueprint, properties: map[string]*gojq.Code{}}
		err error
	)
	for _, field := range []struct {
		target     **gojq.Code
		expression string
	}{{&c.filter, m.Filter}, {&c.identifier, m.Entity.Identifier}, {&c.title, m.Entity.Title}} {
		if field.expression == "" {
			continue
		}
		if *field.target, err = compile(field.expression); err != nil {
			return c, err
		}
	}
	for _, f := range m.Entity.Properties {
		if c.properties[f.ID], err = compile(f.Expression); err != nil {
			return c, err
		}
	}

	return c, nil
}

// apply maps one item, or reports false when the filter drops it. A deleted
// item only needs its identifier.
func (c compiled) apply(item Item) (Entry, bool) {
	input := item.Object
	if c.filter != nil && !item.Deleted {
		keep, err := first(c.filter, input)
		if err != nil {
			return Entry{Blueprint: c.blueprint, Problems: []string{"filter: " + err.Error()}}, true
		}
		if keep != true {
			return Entry{}, false
		}
	}

	entry := Entry{Blueprint: c.blueprint, Deleted: item.Deleted, Entity: blueprint.Entity{Properties: map[string]any{}}}
	identifier, err := first(c.identifier, input)
	if err != nil {
		entry.Problems = append(entry.Problems, "identifier: "+err.Error())
	}
	entry.Entity.Identifier = identifierText(identifier)
	if item.Deleted {
		return entry, true
	}
	if c.title != nil {
		title, err := first(c.title, input)
		if err != nil {
			entry.Problems = append(entry.Problems, "title: "+err.Error())
		}
		entry.Entity.Title, _ = title.(string)
	}
	for id, code := range c.properties {
		value, err := first(code, input)
		if err != nil {
			entry.Problems = append(entry.Problems, "properties."+id+": "+err.Error())
			continue
		}
		entry.Entity.Properties[id] = value
	}

	return entry, true
}

// first runs code and returns its first result, with numbers as float64.
func first(code *gojq.Code, input any) (any, error) { return firstWith(code, input) }

// firstWith is first with values for the code's variables.
func firstWith(code *gojq.Code, input any, variables ...any) (any, error) {
	iter := code.Run(input, variables...)
	value, ok := iter.Next()
	if !ok {
		return nil, nil
	}
	if err, isErr := value.(error); isErr {
		return nil, err
	}

	return normalise(value), nil
}

func normalise(value any) any {
	switch v := value.(type) {
	case int:
		return float64(v)
	case []any:
		for i := range v {
			v[i] = normalise(v[i])
		}
	case map[string]any:
		for key := range v {
			v[key] = normalise(v[key])
		}
	}

	return value
}

// identifierText accepts numeric identifiers, which many systems use.
func identifierText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	}

	return ""
}
