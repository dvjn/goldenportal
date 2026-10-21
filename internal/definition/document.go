// Package definition is the catalog's model: the documents that define
// blueprints and integrations, their JSON Schema, and the checks that a set
// of documents must pass before the portal applies it.
package definition

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
)

type Kind string

const (
	KindBlueprint   Kind = "Blueprint"
	KindIntegration Kind = "Integration"
)

type kindSpec struct {
	kind Kind
	new  func() Document
}

// kinds is the registry of document kinds, in the order a set lists them. A
// new kind is a Go type that implements Document plus an entry here.
var kinds = []kindSpec{
	{KindBlueprint, func() Document { return new(Blueprint) }},
	{KindIntegration, func() Document { return new(Integration) }},
}

func Kinds() []Kind {
	names := make([]Kind, 0, len(kinds))
	for _, spec := range kinds {
		names = append(names, spec.kind)
	}

	return names
}

func newDocument(kind Kind) (Document, bool) {
	index := slices.IndexFunc(kinds, func(spec kindSpec) bool { return spec.kind == kind })
	if index < 0 {
		return nil, false
	}

	return kinds[index].new(), true
}

func kindOrder(kind Kind) int {
	return slices.Index(Kinds(), kind)
}

var (
	identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	keyPattern        = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)
)

// Ref names one document.
type Ref struct {
	Kind       Kind   `json:"kind"`
	Identifier string `json:"identifier"`
}

func (r Ref) String() string { return fmt.Sprintf("%s %s", r.Kind, r.Identifier) }

// Document is one definition. Every kind embeds Header.
type Document interface {
	Ref() Ref
	Heading() Header
	check(c *checker)
}

// Header holds the fields that every kind of document has.
type Header struct {
	Kind        Kind   `json:"kind"`
	Identifier  string `json:"identifier" jsonschema_description:"Lower-case letters, digits and hyphens, starting with a letter. Never changes."`
	Title       string `json:"title" jsonschema:"minLength=1" jsonschema_description:"The name people see."`
	Description string `json:"description,omitempty"`
}

func (h Header) Ref() Ref { return Ref{Kind: h.Kind, Identifier: h.Identifier} }

func (h Header) Heading() Header { return h }

// DecodeDocument reads one document from JSON, choosing the type by its kind.
func DecodeDocument(data []byte) (Document, error) {
	var head struct {
		Kind Kind `json:"kind"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	document, ok := newDocument(head.Kind)
	if !ok {
		return nil, fmt.Errorf("unknown kind %q", head.Kind)
	}
	if err := json.Unmarshal(data, document); err != nil {
		return nil, err
	}

	return document, nil
}
