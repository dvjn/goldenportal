package catalog

import (
	"context"
	"encoding/json"

	"github.com/dvjn/goldport/internal/definition"
)

const impactExamples = 10

// Impact is what a plan does to entities, with up to ten example entities
// for each effect.
type Impact struct {
	DeletedEntities []EntityGroup `json:"deleted_entities"`
	ClearedValues   []FieldImpact `json:"cleared_values"`
	DroppedLinks    []FieldImpact `json:"dropped_links"`
}

// MarshalJSON writes an effect without entities as an empty list.
func (i Impact) MarshalJSON() ([]byte, error) {
	type plain Impact
	return json.Marshal(plain{
		DeletedEntities: nonNil(i.DeletedEntities),
		ClearedValues:   nonNil(i.ClearedValues),
		DroppedLinks:    nonNil(i.DroppedLinks),
	})
}

// EntityGroup is the entities deleted because their blueprint or their
// integration goes away.
type EntityGroup struct {
	Blueprint   string       `json:"blueprint,omitempty"`
	Integration string       `json:"integration,omitempty"`
	Count       int          `json:"count"`
	Examples    []EntityName `json:"examples"`
}

// FieldImpact is the entities that lose a property's value or a relation's
// links.
type FieldImpact struct {
	definition.Field
	Count    int          `json:"count"`
	Examples []EntityName `json:"examples"`
}

type EntityName struct {
	Blueprint  string `json:"blueprint"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

const (
	queryBlueprintEntities = `SELECT blueprint, identifier, title, COUNT(*) OVER ()
		FROM entities WHERE blueprint = $1 ORDER BY title, identifier LIMIT $2`
	queryIntegrationEntities = `SELECT blueprint, identifier, title, COUNT(*) OVER ()
		FROM entities
		WHERE integration = $1 AND blueprint NOT IN (SELECT jsonb_array_elements_text($2::jsonb))
		ORDER BY title, identifier LIMIT $3`
	queryValueHolders = `SELECT blueprint, identifier, title, COUNT(*) OVER ()
		FROM entities WHERE blueprint = $1 AND properties -> $2 IS NOT NULL
		ORDER BY title, identifier LIMIT $3`
	queryValues = `SELECT identifier, title, properties -> $2
		FROM entities WHERE blueprint = $1 AND properties -> $2 IS NOT NULL
		ORDER BY title, identifier`
	queryLinkHolders = `SELECT e.blueprint, e.identifier, e.title, COUNT(*) OVER ()
		FROM entities e
		WHERE e.blueprint = $1 AND (SELECT COUNT(*) FROM entity_relations r
			WHERE r.source_blueprint = e.blueprint AND r.source_identifier = e.identifier AND r.relation = $2) > $3
		ORDER BY e.title, e.identifier LIMIT $4`

	queryDeleteBlueprintEntities   = `DELETE FROM entities WHERE blueprint = $1`
	queryDeleteIntegrationEntities = `DELETE FROM entities WHERE integration = $1`
	queryClearProperty             = `UPDATE entities SET properties = properties - $2::text, updated_at = NOW()
		WHERE blueprint = $1 AND properties -> $2 IS NOT NULL`
	queryClearValues = `UPDATE entities SET properties = properties - $2::text, updated_at = NOW()
		WHERE blueprint = $1 AND identifier IN (SELECT jsonb_array_elements_text($3::jsonb))`
	queryDropLinks      = `DELETE FROM entity_relations WHERE source_blueprint = $1 AND relation = $2`
	queryDropExtraLinks = `DELETE FROM entity_relations r
		WHERE r.source_blueprint = $1 AND r.relation = $2 AND (SELECT COUNT(*) FROM entity_relations o
			WHERE o.source_blueprint = r.source_blueprint AND o.source_identifier = r.source_identifier
				AND o.relation = r.relation) > 1`
)

// measured is the impact, plus the entities whose values a changed property
// no longer accepts, which applying the effects clears.
type measured struct {
	impact  Impact
	invalid map[definition.Field][]string
}

func measure(q querier, effects definition.Effects) (measured, error) {
	result := measured{invalid: map[definition.Field][]string{}}
	for _, blueprint := range effects.RemovedBlueprints {
		count, examples, err := names(q, queryBlueprintEntities, blueprint, impactExamples)
		if err != nil {
			return measured{}, err
		}
		if count > 0 {
			result.impact.DeletedEntities = append(result.impact.DeletedEntities, EntityGroup{Blueprint: blueprint, Count: count, Examples: examples})
		}
	}
	removed, err := json.Marshal(nonNil(effects.RemovedBlueprints))
	if err != nil {
		return measured{}, err
	}
	for _, integration := range effects.RemovedIntegrations {
		count, examples, err := names(q, queryIntegrationEntities, integration, string(removed), impactExamples)
		if err != nil {
			return measured{}, err
		}
		if count > 0 {
			result.impact.DeletedEntities = append(result.impact.DeletedEntities, EntityGroup{Integration: integration, Count: count, Examples: examples})
		}
	}
	if err := result.values(q, effects); err != nil {
		return measured{}, err
	}

	return result, result.links(q, effects)
}

func (m *measured) values(q querier, effects definition.Effects) error {
	for _, field := range effects.RemovedProperties {
		count, examples, err := names(q, queryValueHolders, field.Blueprint, field.Name, impactExamples)
		if err != nil {
			return err
		}
		m.addValues(field, count, examples)
	}
	for _, changed := range effects.ChangedProperties {
		invalid, err := invalidValues(q, changed)
		if err != nil {
			return err
		}
		for _, name := range invalid {
			m.invalid[changed.Field] = append(m.invalid[changed.Field], name.Identifier)
		}
		m.addValues(changed.Field, len(invalid), invalid[:min(len(invalid), impactExamples)])
	}

	return nil
}

func (m *measured) addValues(field definition.Field, count int, examples []EntityName) {
	if count > 0 {
		m.impact.ClearedValues = append(m.impact.ClearedValues, FieldImpact{Field: field, Count: count, Examples: examples})
	}
}

// links counts the entities that lose links: every entity with a link of a
// removed relation, and every entity with more than one of a narrowed one.
func (m *measured) links(q querier, effects definition.Effects) error {
	groups := []struct {
		fields []definition.Field
		above  int
	}{
		{effects.RemovedRelations, 0},
		{effects.NarrowedRelations, 1},
	}
	for _, group := range groups {
		for _, field := range group.fields {
			count, examples, err := names(q, queryLinkHolders, field.Blueprint, field.Name, group.above, impactExamples)
			if err != nil {
				return err
			}
			if count > 0 {
				m.impact.DroppedLinks = append(m.impact.DroppedLinks, FieldImpact{Field: field, Count: count, Examples: examples})
			}
		}
	}

	return nil
}

func invalidValues(q querier, changed definition.ChangedProperty) ([]EntityName, error) {
	rows, err := q.Query(queryValues, changed.Blueprint, changed.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invalid []EntityName
	for rows.Next() {
		var (
			name  = EntityName{Blueprint: changed.Blueprint}
			value []byte
		)
		if err := rows.Scan(&name.Identifier, &name.Title, &value); err != nil {
			return nil, err
		}
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			return nil, err
		}
		if changed.Property.Check(decoded) != nil {
			invalid = append(invalid, name)
		}
	}

	return invalid, rows.Err()
}

// names runs a query for entity names with their total count.
func names(q querier, query string, args ...any) (int, []EntityName, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	var (
		count    int
		examples = []EntityName{}
	)
	for rows.Next() {
		var name EntityName
		if err := rows.Scan(&name.Blueprint, &name.Identifier, &name.Title, &count); err != nil {
			return 0, nil, err
		}
		examples = append(examples, name)
	}

	return count, examples, rows.Err()
}

func applyEffects(ctx context.Context, q querier, effects definition.Effects, measured measured) error {
	var statements []statement
	for _, blueprint := range effects.RemovedBlueprints {
		statements = append(statements, statement{queryDeleteBlueprintEntities, []any{blueprint}})
	}
	for _, integration := range effects.RemovedIntegrations {
		statements = append(statements, statement{queryDeleteIntegrationEntities, []any{integration}})
	}
	for _, field := range effects.RemovedProperties {
		statements = append(statements, statement{queryClearProperty, []any{field.Blueprint, field.Name}})
	}
	for _, changed := range effects.ChangedProperties {
		identifiers, err := json.Marshal(nonNil(measured.invalid[changed.Field]))
		if err != nil {
			return err
		}
		statements = append(statements, statement{queryClearValues, []any{changed.Blueprint, changed.Name, string(identifiers)}})
	}
	for _, field := range effects.RemovedRelations {
		statements = append(statements, statement{queryDropLinks, []any{field.Blueprint, field.Name}})
	}
	for _, field := range effects.NarrowedRelations {
		statements = append(statements, statement{queryDropExtraLinks, []any{field.Blueprint, field.Name}})
	}
	for _, statement := range statements {
		if _, err := q.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return err
		}
	}

	return nil
}

type statement struct {
	query string
	args  []any
}
