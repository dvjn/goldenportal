package definition

import (
	"bytes"
	"encoding/json"
	"fmt"
	"iter"
	"slices"

	"github.com/invopop/jsonschema"
)

// Map is a JSON object that keeps its keys in the order they were written,
// so a blueprint's properties keep their order in forms, tables and exports.
type Map[T any] struct {
	keys   []string
	values map[string]T
}

func (m Map[T]) Len() int { return len(m.keys) }

func (m Map[T]) IsZero() bool { return len(m.keys) == 0 }

func (m Map[T]) Keys() []string { return slices.Clone(m.keys) }

func (m Map[T]) Get(key string) (T, bool) {
	value, ok := m.values[key]
	return value, ok
}

func (m Map[T]) All() iter.Seq2[string, T] {
	return func(yield func(string, T) bool) {
		for _, key := range m.keys {
			if !yield(key, m.values[key]) {
				return
			}
		}
	}
}

// Set adds the key at the end or replaces its value in place.
func (m *Map[T]) Set(key string, value T) {
	if m.values == nil {
		m.values = map[string]T{}
	}
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

func (m Map[T]) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	for index, key := range m.keys {
		if index > 0 {
			out.WriteByte(',')
		}
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(m.values[key])
		if err != nil {
			return nil, err
		}
		out.Write(name)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')

	return out.Bytes(), nil
}

func (m *Map[T]) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if open, err := decoder.Token(); err != nil || open != json.Delim('{') {
		return fmt.Errorf("want a JSON object, got %s", data)
	}
	*m = Map[T]{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		var value T
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		m.Set(token.(string), value)
	}

	return nil
}

// JSONSchema describes the map as an object whose keys are identifiers and
// whose values follow T's schema, inlined.
func (Map[T]) JSONSchema() *jsonschema.Schema {
	reflector := newReflector()
	reflector.DoNotReference = true
	value := reflector.Reflect(new(T))
	value.Version = ""
	value.ID = ""

	return &jsonschema.Schema{
		Type:                 "object",
		PropertyNames:        &jsonschema.Schema{Pattern: keyPattern.String()},
		AdditionalProperties: value,
	}
}
