package definition

import (
	"bytes"
	"cmp"
	"encoding/json"
	"slices"

	"github.com/goccy/go-yaml"
)

// Set is a whole collection of definitions, as the portal applies it.
type Set struct {
	Documents []Document
}

func NewSet(documents ...Document) Set {
	set := Set{Documents: slices.Clone(documents)}
	slices.SortStableFunc(set.Documents, func(a, b Document) int {
		return cmp.Or(
			cmp.Compare(kindOrder(a.Ref().Kind), kindOrder(b.Ref().Kind)),
			cmp.Compare(a.Ref().Identifier, b.Ref().Identifier),
		)
	})

	return set
}

func (s Set) Find(ref Ref) (Document, bool) {
	index := slices.IndexFunc(s.Documents, func(document Document) bool { return document.Ref() == ref })
	if index < 0 {
		return nil, false
	}

	return s.Documents[index], true
}

func (s Set) Blueprint(identifier string) (*Blueprint, bool) {
	document, ok := s.Find(Ref{Kind: KindBlueprint, Identifier: identifier})
	if !ok {
		return nil, false
	}

	return document.(*Blueprint), true
}

func (s Set) Integration(identifier string) (*Integration, bool) {
	document, ok := s.Find(Ref{Kind: KindIntegration, Identifier: identifier})
	if !ok {
		return nil, false
	}

	return document.(*Integration), true
}

func (s Set) Blueprints() []*Blueprint { return documentsOf[*Blueprint](s) }

func (s Set) Integrations() []*Integration { return documentsOf[*Integration](s) }

func documentsOf[T Document](s Set) []T {
	var documents []T
	for _, document := range s.Documents {
		if typed, ok := document.(T); ok {
			documents = append(documents, typed)
		}
	}

	return documents
}

// Merge returns the set with the documents added or replacing the ones with
// the same reference, and without the removed ones.
func (s Set) Merge(documents []Document, remove []Ref) Set {
	replaced := map[Ref]bool{}
	for _, document := range documents {
		replaced[document.Ref()] = true
	}
	for _, ref := range remove {
		replaced[ref] = true
	}
	kept := slices.DeleteFunc(slices.Clone(s.Documents), func(document Document) bool { return replaced[document.Ref()] })

	return NewSet(append(kept, documents...)...)
}

func (s Set) Refs() []Ref {
	refs := make([]Ref, 0, len(s.Documents))
	for _, document := range s.Documents {
		refs = append(refs, document.Ref())
	}

	return refs
}

func (s Set) MarshalJSON() ([]byte, error) {
	if s.Documents == nil {
		return []byte("[]"), nil
	}

	return json.Marshal(s.Documents)
}

func (s *Set) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	documents := make([]Document, 0, len(raw))
	for _, item := range raw {
		document, err := DecodeDocument(item)
		if err != nil {
			return err
		}
		documents = append(documents, document)
	}
	*s = NewSet(documents...)

	return nil
}

// YAML writes the set as one YAML document per definition.
func (s Set) YAML() ([]byte, error) {
	var out bytes.Buffer
	for index, document := range s.Documents {
		text, err := DocumentYAML(document)
		if err != nil {
			return nil, err
		}
		if index > 0 {
			out.WriteString("---\n")
		}
		out.Write(text)
	}

	return out.Bytes(), nil
}

// DocumentYAML writes a document in block style, keys in the order the
// document holds them.
func DocumentYAML(document Document) ([]byte, error) {
	data, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	var ordered yaml.MapSlice
	if err := yaml.UnmarshalWithOptions(data, &ordered, yaml.UseOrderedMap()); err != nil {
		return nil, err
	}

	return yaml.MarshalWithOptions(ordered, yaml.IndentSequence(true))
}
