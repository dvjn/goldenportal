package integration

import (
	"context"
	"fmt"
	"strconv"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/jq"
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
	filter, identifier, title *jq.Code
	properties                map[string]*jq.Code
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
func (m mapper) Map(ctx context.Context, items []Item) []Entry {
	var entries []Entry
	for _, item := range items {
		for _, c := range m.byResource[item.Resource] {
			if entry, ok := c.apply(ctx, item); ok {
				entries = append(entries, entry)
			}
		}
	}

	return entries
}

func (m Mapping) compile() (compiled, error) {
	var (
		c   = compiled{blueprint: m.Blueprint, properties: map[string]*jq.Code{}}
		err error
	)
	for _, field := range []struct {
		target     **jq.Code
		expression string
	}{{&c.filter, m.Filter}, {&c.identifier, m.Entity.Identifier}, {&c.title, m.Entity.Title}} {
		if field.expression == "" {
			continue
		}
		if *field.target, err = jq.Compile(field.expression); err != nil {
			return c, err
		}
	}
	for _, f := range m.Entity.Properties {
		if c.properties[f.ID], err = jq.Compile(f.Expression); err != nil {
			return c, err
		}
	}

	return c, nil
}

// apply maps one item, or reports false when the filter drops it. A deleted
// item only needs its identifier.
func (c compiled) apply(ctx context.Context, item Item) (Entry, bool) {
	input := item.Object
	if c.filter != nil && !item.Deleted {
		keep, err := first(ctx, c.filter, input)
		if err != nil {
			return Entry{Blueprint: c.blueprint, Problems: []string{"filter: " + err.Error()}}, true
		}
		if keep != true {
			return Entry{}, false
		}
	}

	entry := Entry{Blueprint: c.blueprint, Deleted: item.Deleted, Entity: blueprint.Entity{Properties: map[string]any{}}}
	identifier, err := first(ctx, c.identifier, input)
	if err != nil {
		entry.Problems = append(entry.Problems, "identifier: "+err.Error())
	}
	entry.Entity.Identifier = identifierText(identifier)
	if item.Deleted {
		return entry, true
	}
	if c.title != nil {
		title, err := first(ctx, c.title, input)
		if err != nil {
			entry.Problems = append(entry.Problems, "title: "+err.Error())
		}
		entry.Entity.Title, _ = title.(string)
	}
	for id, code := range c.properties {
		value, err := first(ctx, code, input)
		if err != nil {
			entry.Problems = append(entry.Problems, "properties."+id+": "+err.Error())
			continue
		}
		entry.Entity.Properties[id] = value
	}

	return entry, true
}

// first runs code and returns its first result, or nil for none.
func first(ctx context.Context, code *jq.Code, input any, variables ...any) (any, error) {
	value, _, err := code.First(ctx, jq.DefaultTimeout, input, variables...)

	return value, err
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
