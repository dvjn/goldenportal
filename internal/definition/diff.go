package definition

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"

	"github.com/goccy/go-yaml"
)

type Action string

const (
	Added   Action = "add"
	Changed Action = "change"
	Removed Action = "remove"
)

// Change is what happens to one document between two sets. Fields lists the
// fields that differ when the document changes.
type Change struct {
	Kind       Kind          `json:"kind"`
	Identifier string        `json:"identifier"`
	Title      string        `json:"title"`
	Action     Action        `json:"action"`
	Fields     []FieldChange `json:"fields,omitempty"`
}

// FieldChange is one field that differs. Before is absent for an added
// field and After for a removed one.
type FieldChange struct {
	Path   string          `json:"path"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

// Diff lists the documents added, changed and removed on the way from one
// set to the other, in the order of the sets.
func Diff(from, to Set) ([]Change, error) {
	var changes []Change
	for _, document := range to.Documents {
		previous, existed := from.Find(document.Ref())
		if !existed {
			changes = append(changes, change(document, Added, nil))
			continue
		}
		fields, err := fieldChanges(previous, document)
		if err != nil {
			return nil, err
		}
		if len(fields) > 0 {
			changes = append(changes, change(document, Changed, fields))
		}
	}
	for _, document := range from.Documents {
		if _, kept := to.Find(document.Ref()); !kept {
			changes = append(changes, change(document, Removed, nil))
		}
	}

	return changes, nil
}

func change(document Document, action Action, fields []FieldChange) Change {
	header := document.Heading()
	return Change{Kind: header.Kind, Identifier: header.Identifier, Title: header.Title, Action: action, Fields: fields}
}

func fieldChanges(from, to Document) ([]FieldChange, error) {
	before, err := ordered(from)
	if err != nil {
		return nil, err
	}
	after, err := ordered(to)
	if err != nil {
		return nil, err
	}
	var fields []FieldChange
	err = compare(nil, before, after, &fields)

	return fields, err
}

func ordered(document Document) (any, error) {
	data, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	var value any
	err = yaml.UnmarshalWithOptions(data, &value, yaml.UseOrderedMap())

	return value, err
}

// compare walks two values. Objects and lists of objects are compared field
// by field; any other value, such as a list of enum values, as a whole.
func compare(at []string, before, after any, fields *[]FieldChange) error {
	beforeMap, beforeIsMap := before.(yaml.MapSlice)
	afterMap, afterIsMap := after.(yaml.MapSlice)
	if beforeIsMap && afterIsMap {
		return compareMaps(at, beforeMap, afterMap, fields)
	}
	beforeList, beforeIsList := before.([]any)
	afterList, afterIsList := after.([]any)
	if beforeIsList && afterIsList && holdsObjects(beforeList) && holdsObjects(afterList) {
		for index := range max(len(beforeList), len(afterList)) {
			if err := compare(append(slices.Clone(at), strconv.Itoa(index)), item(beforeList, index), item(afterList, index), fields); err != nil {
				return err
			}
		}
		return nil
	}

	beforeJSON, err := rawJSON(before)
	if err != nil {
		return err
	}
	afterJSON, err := rawJSON(after)
	if err != nil {
		return err
	}
	if !bytes.Equal(beforeJSON, afterJSON) {
		*fields = append(*fields, FieldChange{Path: dotted(at), Before: beforeJSON, After: afterJSON})
	}

	return nil
}

func compareMaps(at []string, before, after yaml.MapSlice, fields *[]FieldChange) error {
	beforeValues := before.ToMap()
	afterValues := after.ToMap()
	for _, pair := range after {
		if err := compare(append(slices.Clone(at), keyOf(pair)), beforeValues[pair.Key], pair.Value, fields); err != nil {
			return err
		}
	}
	for _, pair := range before {
		if _, kept := afterValues[pair.Key]; !kept {
			if err := compare(append(slices.Clone(at), keyOf(pair)), pair.Value, nil, fields); err != nil {
				return err
			}
		}
	}

	return nil
}

func keyOf(pair yaml.MapItem) string {
	key, _ := pair.Key.(string)
	return key
}

func holdsObjects(list []any) bool {
	return slices.ContainsFunc(list, func(value any) bool {
		_, ok := value.(yaml.MapSlice)
		return ok
	})
}

func item(list []any, index int) any {
	if index < len(list) {
		return list[index]
	}

	return nil
}

func rawJSON(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}

	return orderedJSON(value)
}
