package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// fetch loads entities of a blueprint by identifier.
func (s Store) fetch(ctx context.Context, blueprintIdentifier string, identifiers []string) (map[string]*Entity, error) {
	found := map[string]*Entity{}
	if len(identifiers) == 0 {
		return found, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+entityColumns+entityFrom+`
		WHERE b.kind = 'blueprint' AND b.identifier = $1 AND e.identifier = ANY($2)`,
		blueprintIdentifier, pq.Array(identifiers))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanEntity(rows.Scan)
		if err != nil {
			return nil, err
		}
		found[e.Identifier] = e
	}

	return found, rows.Err()
}

// targets reads a relation's value as identifiers.
func targets(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}

	return nil
}

// Resolve fills in what reading entities of spec needs beyond their rows: the
// titles of related entities, and the values lookups follow relations to.
// specs are the published blueprints, for the hops.
func (s Store) Resolve(ctx context.Context, specs map[string]blueprint.Spec, spec blueprint.Spec, entities []*Entity) error {
	if len(entities) == 0 {
		return nil
	}

	for _, prop := range spec.Properties {
		if !prop.IsRelation() {
			continue
		}
		var ids []string
		for _, e := range entities {
			ids = append(ids, targets(e.relations[prop.ID])...)
		}
		found, err := s.fetch(ctx, prop.Relation, ids)
		if err != nil {
			return err
		}
		for _, e := range entities {
			links := []Link{}
			for _, id := range targets(e.relations[prop.ID]) {
				link := Link{Identifier: id}
				if target, ok := found[id]; ok {
					link.Title = target.Title
				}
				links = append(links, link)
			}
			if len(links) > 0 {
				e.Related[prop.ID] = links
			}
		}
	}

	for _, prop := range spec.Properties {
		if prop.Lookup == "" {
			continue
		}
		if err := s.lookup(ctx, specs, spec, prop, entities); err != nil {
			return err
		}
	}

	return nil
}

// lookup follows a lookup's relations hop by hop, loading each hop's
// entities in one query, and sets the value it ends on.
func (s Store) lookup(ctx context.Context, specs map[string]blueprint.Spec, spec blueprint.Spec, prop blueprint.Property, entities []*Entity) error {
	segments := strings.Split(prop.Lookup, ".")
	current := make(map[*Entity]*Entity, len(entities))
	for _, e := range entities {
		current[e] = e
	}

	at := spec
	for _, segment := range segments[:len(segments)-1] {
		rel, ok := at.Property(segment)
		if !ok || !rel.IsRelation() {
			return nil
		}
		var ids []string
		for _, node := range current {
			if id, ok := node.relations[segment].(string); ok {
				ids = append(ids, id)
			}
		}
		found, err := s.fetch(ctx, rel.Relation, ids)
		if err != nil {
			return err
		}
		next := map[*Entity]*Entity{}
		for e, node := range current {
			id, _ := node.relations[segment].(string)
			if target, ok := found[id]; ok {
				next[e] = target
			}
		}
		current = next
		if at, ok = specs[rel.Relation]; !ok {
			return nil
		}
	}

	last := segments[len(segments)-1]
	for e, node := range current {
		var value any
		switch last {
		case "$identifier":
			value = node.Identifier
		case "$title":
			value = node.Title
		default:
			value = node.Properties[last]
		}
		if value != nil {
			e.Properties[prop.ID] = value
		}
	}

	return nil
}

// Incoming is the entities of another blueprint whose relation points at one entity.
type Incoming struct {
	Blueprint      string `json:"blueprint"`
	BlueprintTitle string `json:"blueprintTitle"`
	Property       string `json:"property"`
	PropertyTitle  string `json:"propertyTitle"`
	Entities       []Link `json:"entities"`
	Total          int    `json:"total"`
}

// MaxIncoming caps how many pointing entities are listed per relation.
const MaxIncoming = 50

// Incoming lists, per relation of a published blueprint that points at
// blueprintIdentifier, the entities whose relation names identifier.
func (s Store) Incoming(ctx context.Context, specs map[string]blueprint.Spec, blueprintIdentifier, identifier string) ([]Incoming, error) {
	out := []Incoming{}
	for _, ref := range blueprint.References(specs, blueprintIdentifier) {
		target, _ := json.Marshal(identifier)
		rows, err := s.db.QueryContext(ctx, `
			SELECT e.identifier, e.title, count(*) OVER ()
			FROM entities e JOIN config_documents b ON b.id = e.blueprint_id
			WHERE b.kind = 'blueprint' AND b.identifier = $1
				AND (e.relations -> $2 = $3::jsonb OR e.relations -> $2 @> jsonb_build_array($3::jsonb))
			ORDER BY e.title, e.identifier
			LIMIT $4`, ref.Blueprint, ref.Property, string(target), MaxIncoming)
		if err != nil {
			return nil, err
		}
		incoming := Incoming{
			Blueprint: ref.Blueprint, BlueprintTitle: ref.Title, Property: ref.Property,
			PropertyTitle: ref.PropertyTitle, Entities: []Link{},
		}
		for rows.Next() {
			var link Link
			if err := rows.Scan(&link.Identifier, &link.Title, &incoming.Total); err != nil {
				rows.Close()
				return nil, err
			}
			incoming.Entities = append(incoming.Entities, link)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if incoming.Total > 0 {
			out = append(out, incoming)
		}
	}

	return out, nil
}

// Facet is one value of a property and how many entities have it.
type Facet struct {
	Value any    `json:"value"`
	Title string `json:"title,omitempty"`
	Count int    `json:"count"`
}

// MaxFacets caps the values offered per property.
const MaxFacets = 50

// Facets counts the values a property has across a blueprint's entities,
// most common first, for filter options. Lists count each item.
func (s Store) Facets(ctx context.Context, spec blueprint.Spec, property string) ([]Facet, error) {
	facets, _, err := s.Group(ctx, spec, nil, property, MaxFacets)
	return facets, err
}

// Group counts the values a property has across the entities matching
// filter, most common first, up to limit values. Lists count each item.
// None counts the entities without a value.
func (s Store) Group(ctx context.Context, spec blueprint.Spec, filter *Filter, property string, limit int) (facets []Facet, none int, err error) {
	fld, err := fieldFor(spec, property)
	if err != nil {
		return nil, 0, FilterError{err}
	}
	if fld.column {
		return nil, 0, FilterError{fmt.Errorf("%s can't be counted by value", property)}
	}
	c := &compiler{spec: spec, args: []any{spec.Identifier}}
	where := "b.kind = 'blueprint' AND b.identifier = $1"
	if filter != nil {
		sql, err := c.compile(*filter, 1)
		if err != nil {
			return nil, 0, FilterError{err}
		}
		where += " AND coalesce(" + sql + ", false)"
	}
	empty := "(" + fld.value + " IS NULL OR " + fld.value + " = 'null'::jsonb"
	if fld.many {
		empty += " OR " + fld.value + " = '[]'::jsonb"
	}
	empty += ")"
	if err := s.db.QueryRowContext(ctx, `SELECT count(*)`+entityFrom+` WHERE `+where+` AND `+empty, c.args...).Scan(&none); err != nil {
		return nil, 0, err
	}

	value := fld.value
	from := entityFrom
	if fld.many {
		from += `, jsonb_array_elements(CASE WHEN jsonb_typeof(` + fld.value + `) = 'array' THEN ` + fld.value + ` ELSE '[]' END) item`
		value = "item"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+value+`, count(*)`+from+`
		WHERE `+where+` AND `+value+` IS NOT NULL AND `+value+` <> 'null'::jsonb
		GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT `+c.arg(limit), c.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	facets = []Facet{}
	for rows.Next() {
		var (
			raw []byte
			f   Facet
		)
		if err := rows.Scan(&raw, &f.Count); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(raw, &f.Value); err != nil {
			return nil, 0, err
		}
		facets = append(facets, f)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	if prop, _ := spec.Property(property); prop.IsRelation() {
		var ids []string
		for _, f := range facets {
			if id, ok := f.Value.(string); ok {
				ids = append(ids, id)
			}
		}
		found, err := s.fetch(ctx, prop.Relation, ids)
		if err != nil {
			return nil, 0, err
		}
		for i, f := range facets {
			if id, ok := f.Value.(string); ok {
				if target, ok := found[id]; ok {
					facets[i].Title = target.Title
				}
			}
		}
	}

	return facets, none, nil
}
