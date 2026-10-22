// Package integration feeds the catalog from integrations: it maps the
// records they push or a connector pulls to entities, writes them, keeps a
// history of runs, and schedules pull syncs.
package integration

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dvjn/goldport/internal/catalog"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/itchyny/gojq"
)

// Problem is why one record does not make an entity. Index is the record's
// 0-based position among the records of its kind.
type Problem struct {
	Kind       string `json:"kind"`
	Index      int    `json:"index"`
	Identifier string `json:"identifier,omitempty"`
	Field      string `json:"field"`
	Message    string `json:"message"`
}

func (p Problem) String() string {
	record := fmt.Sprintf("%s record %d", p.Kind, p.Index+1)
	if p.Identifier != "" {
		record += fmt.Sprintf(" (%s)", p.Identifier)
	}

	return fmt.Sprintf("%s: %s: %s", record, p.Field, p.Message)
}

// draft is an entity as a record describes it, before its values are
// checked against the blueprint. Relations holds the relations the record
// names, so the write replaces only those.
type draft struct {
	identifier any
	title      any
	properties map[string]any
	relations  map[string]any
}

// extractor reads a draft from a record.
type extractor func(ctx context.Context, record any) (draft, []fieldProblem)

type fieldProblem struct {
	field   string
	message string
}

// Mapper turns the records of one kind into entities of its mapping's
// blueprint.
type Mapper struct {
	kind      string
	blueprint *definition.Blueprint
	set       definition.Set
	extract   extractor
}

// NewMapper compiles the mapping. A canonical mapping reads records that
// are already entities.
func NewMapper(set definition.Set, mapping definition.Mapping) (*Mapper, error) {
	blueprint, ok := set.Blueprint(mapping.Blueprint)
	if !ok {
		return nil, fmt.Errorf("the %s mapping writes blueprint %s, which does not exist", mapping.Kind, mapping.Blueprint)
	}
	mapper := &Mapper{kind: mapping.Kind, blueprint: blueprint, set: set, extract: readCanonical}
	if mapping.Canonical() {
		return mapper, nil
	}
	extract, err := compileMapping(mapping)
	if err != nil {
		return nil, err
	}
	mapper.extract = extract

	return mapper, nil
}

func (m *Mapper) Blueprint() string { return m.blueprint.Identifier }

// Map returns the entities the records describe and the problems of the
// records that describe none.
func (m *Mapper) Map(ctx context.Context, records []any) ([]catalog.OwnedEntity, []Problem) {
	var (
		entities []catalog.OwnedEntity
		problems []Problem
		seen     = map[string]bool{}
	)
	for index, record := range records {
		draft, extracted := m.extract(ctx, record)
		entity, checked := m.check(draft)
		if len(extracted) == 0 && len(checked) == 0 && seen[entity.Identifier] {
			checked = append(checked, fieldProblem{"identifier", "another record already has this identifier"})
		}
		recordProblems := slices.Concat(extracted, checked)
		for _, problem := range recordProblems {
			problems = append(problems, Problem{
				Kind: m.kind, Index: index, Identifier: entity.Identifier, Field: problem.field, Message: problem.message,
			})
		}
		if len(recordProblems) == 0 {
			seen[entity.Identifier] = true
			entities = append(entities, entity)
		}
	}

	return entities, problems
}

// check returns the entity the draft describes, with what the blueprint
// does not accept.
func (m *Mapper) check(draft draft) (catalog.OwnedEntity, []fieldProblem) {
	var problems []fieldProblem
	identifier, _ := draft.identifier.(string)
	if !catalog.ValidIdentifier(identifier) {
		problems = append(problems, fieldProblem{
			"identifier", fmt.Sprintf("give a string of letters, digits, dots, hyphens and underscores, at most 128, not %s", describe(draft.identifier)),
		})
	}
	title, isString := draft.title.(string)
	title = strings.TrimSpace(title)
	switch {
	case draft.title == nil || isString && title == "":
		title = identifier
	case !isString || !catalog.ValidTitle(title):
		problems = append(problems, fieldProblem{"title", fmt.Sprintf("give a string of at most 200 characters, not %s", describe(draft.title))})
	}
	properties := maps.Clone(draft.properties)
	maps.DeleteFunc(properties, func(_ string, value any) bool { return value == nil })
	invalid := m.blueprint.CheckValues(properties)
	for _, key := range slices.Sorted(maps.Keys(invalid)) {
		problems = append(problems, fieldProblem{"properties." + key, invalid[key]})
	}

	entity := catalog.OwnedEntity{Entity: catalog.Entity{
		Blueprint:  m.blueprint.Identifier,
		Identifier: identifier,
		Title:      title,
		Properties: orEmpty(properties),
	}}
	for _, key := range slices.Sorted(maps.Keys(draft.relations)) {
		targets, problem := m.targets(key, draft.relations[key])
		if problem != "" {
			problems = append(problems, fieldProblem{"relations." + key, problem})
			continue
		}
		entity.Relations = append(entity.Relations, key)
		entity.Links = append(entity.Links, targets...)
	}
	for index := range entity.Links {
		entity.Links[index].Source = entity.Name()
	}

	return entity, problems
}

// targets reads the identifiers a relation links to: one, a list, or none.
func (m *Mapper) targets(key string, value any) ([]catalog.Link, string) {
	relation, ok := m.blueprint.Relations.Get(key)
	if !ok {
		return nil, fmt.Sprintf("%s is not a relation of %s", key, m.blueprint.Title)
	}
	values, isList := value.([]any)
	if !isList {
		values = []any{value}
	}
	var identifiers []string
	for _, item := range values {
		switch identifier := item.(type) {
		case nil:
		case string:
			identifiers = append(identifiers, identifier)
		default:
			return nil, fmt.Sprintf("give target identifiers as strings, not %s", describe(item))
		}
	}
	identifiers = slices.Compact(slices.Sorted(slices.Values(identifiers)))
	if len(identifiers) > 1 && !relation.Many {
		target, _ := m.set.Blueprint(relation.Target)
		return nil, fmt.Sprintf("%s links to one %s, not %d", relation.Title, target.Title, len(identifiers))
	}
	links := make([]catalog.Link, 0, len(identifiers))
	for _, identifier := range identifiers {
		links = append(links, catalog.Link{Relation: key, Target: catalog.EntityName{Blueprint: relation.Target, Identifier: identifier}})
	}

	return links, ""
}

// canonicalRecord is a record that is already an entity.
type canonicalRecord struct {
	Identifier any            `json:"identifier"`
	Title      any            `json:"title"`
	Properties map[string]any `json:"properties"`
	Relations  map[string]any `json:"relations"`
}

func readCanonical(_ context.Context, record any) (draft, []fieldProblem) {
	var canonical canonicalRecord
	if err := remarshal(record, &canonical); err != nil {
		return draft{}, []fieldProblem{{"record", "give an object with identifier, title, properties and relations"}}
	}

	return draft{
		identifier: canonical.Identifier,
		title:      canonical.Title,
		properties: canonical.Properties,
		relations:  canonical.Relations,
	}, nil
}

// expression is one jq expression of a mapping and where its value goes in
// the draft.
type expression struct {
	field  string
	source string
	code   *gojq.Code
	assign func(draft *draft, value any)
}

// compileMapping compiles every jq expression of the mapping into an
// extractor that runs them over a record.
func compileMapping(mapping definition.Mapping) (extractor, error) {
	expressions := []expression{
		{field: "identifier", source: mapping.Identifier, assign: func(d *draft, value any) { d.identifier = value }},
		{field: "title", source: cmp.Or(mapping.Title, "null"), assign: func(d *draft, value any) { d.title = value }},
	}
	for key, source := range mapping.Properties.All() {
		expressions = append(expressions, expression{
			field: "properties." + key, source: source, assign: func(d *draft, value any) { d.properties[key] = value },
		})
	}
	for key, source := range mapping.Relations.All() {
		expressions = append(expressions, expression{
			field: "relations." + key, source: source, assign: func(d *draft, value any) { d.relations[key] = value },
		})
	}
	for index := range expressions {
		query, err := gojq.Parse(expressions[index].source)
		if err == nil {
			expressions[index].code, err = gojq.Compile(query)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", expressions[index].field, err)
		}
	}

	return func(ctx context.Context, record any) (draft, []fieldProblem) {
		result := draft{properties: map[string]any{}, relations: map[string]any{}}
		var problems []fieldProblem
		for _, expression := range expressions {
			value, err := run(ctx, expression.code, record)
			if err != nil {
				problems = append(problems, fieldProblem{expression.field, err.Error()})
				continue
			}
			expression.assign(&result, value)
		}

		return result, problems
	}, nil
}

// run returns the first value the expression gives, as JSON would decode
// it, or nil when it gives none.
func run(ctx context.Context, code *gojq.Code, record any) (any, error) {
	value, ok := code.RunWithContext(ctx, record).Next()
	if !ok {
		return nil, nil
	}
	if err, isError := value.(error); isError {
		return nil, fmt.Errorf("the jq expression failed: %w", err)
	}
	var decoded any
	err := remarshal(value, &decoded)

	return decoded, err
}

func remarshal(value, target any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return json.Unmarshal(encoded, target)
}

func describe(value any) string {
	if value == nil {
		return "nothing"
	}
	encoded, _ := json.Marshal(value)
	text := string(encoded)
	if len(text) > 40 {
		text = text[:37] + "..."
	}

	return text
}

func orEmpty(properties map[string]any) map[string]any {
	if properties == nil {
		return map[string]any{}
	}

	return properties
}
