package integration

import (
	"fmt"
	"strconv"

	"github.com/itchyny/gojq"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// Item is one entity made from incoming data, or the reason it couldn't be.
type Item struct {
	Blueprint string
	Entity    blueprint.Entity
	Problems  []string
}

// Map turns incoming data into entities through the mappings.
func (s Spec) Map(data any) ([]Item, error) {
	var items []Item
	for _, m := range s.Mappings {
		mapped, err := m.apply(data)
		if err != nil {
			return nil, err
		}
		items = append(items, mapped...)
	}

	return items, nil
}

type compiled struct {
	items, filter, identifier, title *gojq.Code
	properties                       map[string]*gojq.Code
}

func compile(expression string) (*gojq.Code, error) {
	query, err := gojq.Parse(expression)
	if err != nil {
		return nil, err
	}

	return gojq.Compile(query)
}

func (m Mapping) compile() (compiled, error) {
	var (
		c   = compiled{properties: map[string]*gojq.Code{}}
		err error
	)
	for _, field := range []struct {
		target     **gojq.Code
		expression string
	}{{&c.items, m.Items}, {&c.filter, m.Filter}, {&c.identifier, m.Identifier}, {&c.title, m.Title}} {
		if field.expression == "" {
			continue
		}
		if *field.target, err = compile(field.expression); err != nil {
			return c, err
		}
	}
	for _, f := range m.Properties {
		if c.properties[f.ID], err = compile(f.Expression); err != nil {
			return c, err
		}
	}

	return c, nil
}

func (m Mapping) apply(data any) ([]Item, error) {
	c, err := m.compile()
	if err != nil {
		return nil, err
	}

	var inputs []any
	iter := c.items.Run(data)
	for {
		value, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := value.(error); isErr {
			return nil, fmt.Errorf("the items of the %s mapping: %w", m.Blueprint, err)
		}
		inputs = append(inputs, value)
	}

	items := make([]Item, 0, len(inputs))
	for _, input := range inputs {
		if c.filter != nil {
			keep, err := first(c.filter, input)
			if err != nil {
				items = append(items, Item{Blueprint: m.Blueprint, Problems: []string{"filter: " + err.Error()}})
				continue
			}
			if keep != true {
				continue
			}
		}

		item := Item{Blueprint: m.Blueprint, Entity: blueprint.Entity{Properties: map[string]any{}}}
		identifier, err := first(c.identifier, input)
		if err != nil {
			item.Problems = append(item.Problems, "identifier: "+err.Error())
		}
		item.Entity.Identifier = identifierText(identifier)
		if c.title != nil {
			title, err := first(c.title, input)
			if err != nil {
				item.Problems = append(item.Problems, "title: "+err.Error())
			}
			item.Entity.Title, _ = title.(string)
		}
		for id, code := range c.properties {
			value, err := first(code, input)
			if err != nil {
				item.Problems = append(item.Problems, "properties."+id+": "+err.Error())
				continue
			}
			item.Entity.Properties[id] = value
		}
		items = append(items, item)
	}

	return items, nil
}

// first runs code and returns its first result, with numbers as float64.
func first(code *gojq.Code, input any) (any, error) {
	iter := code.Run(input)
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
