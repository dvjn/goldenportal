package entity

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// fetch loads entities of a blueprint by identifier.
func (s Store) fetch(ctx context.Context, spec blueprint.Spec, identifiers []string) (map[string]*Entity, error) {
	found := map[string]*Entity{}
	if len(identifiers) == 0 {
		return found, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+entityColumns(quoteLiteral(spec.Identifier))+` FROM entities e
		WHERE e.blueprint_id = `+BlueprintID(spec)+` AND e.identifier = ANY($1)`, pq.Array(identifiers))
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

// Links names entities of a blueprint by identifier, in the order given.
// One that doesn't exist keeps its identifier and no title.
func (s Store) Links(ctx context.Context, blueprintIdentifier string, identifiers []string) ([]Link, error) {
	found, err := s.fetch(ctx, blueprint.Spec{Identifier: blueprintIdentifier}, identifiers)
	if err != nil {
		return nil, err
	}
	links := make([]Link, len(identifiers))
	for i, id := range identifiers {
		links[i] = Link{Identifier: id}
		if e, ok := found[id]; ok {
			links[i].Title = e.Title
		}
	}

	return links, nil
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
		found, err := s.fetch(ctx, specNamed(specs, prop.Relation), ids)
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

	return s.resolveScorecards(ctx, spec, entities)
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
		found, err := s.fetch(ctx, specNamed(specs, rel.Relation), ids)
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
		case "identifier":
			value = node.Identifier
		case "title":
			value = node.Title
		case "blueprint":
			value = node.Blueprint
		case "connection":
			value = node.Connection
		case "createdAt":
			value = node.CreatedAt
		case "updatedAt":
			value = node.UpdatedAt
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
		incoming, err := s.incoming(ctx, specs[ref.Blueprint], ref, identifier)
		if err != nil {
			return nil, err
		}
		if incoming.Total > 0 {
			out = append(out, incoming)
		}
	}

	return out, nil
}

// incoming lists the entities of spec whose relation ref names identifier.
// Containment is what the relations index answers; a relation holds one
// identifier or a list of them.
func (s Store) incoming(ctx context.Context, spec blueprint.Spec, ref blueprint.Reference, identifier string) (Incoming, error) {
	one, _ := json.Marshal(map[string]any{ref.Property: identifier})
	many, _ := json.Marshal(map[string]any{ref.Property: []string{identifier}})
	where := "e.blueprint_id = " + BlueprintID(spec) + " AND (e.relations @> $1::jsonb OR e.relations @> $2::jsonb)"
	args := []any{string(one), string(many)}
	rows, err := s.db.QueryContext(ctx, `SELECT e.identifier, e.title FROM entities e
		WHERE `+where+`
		ORDER BY e.title, e.identifier
		LIMIT $3`, append(args, MaxIncoming)...)
	if err != nil {
		return Incoming{}, err
	}
	defer rows.Close()
	incoming := Incoming{
		Blueprint: ref.Blueprint, BlueprintTitle: ref.Title, Property: ref.Property,
		PropertyTitle: ref.PropertyTitle, Entities: []Link{},
	}
	for rows.Next() {
		var link Link
		if err := rows.Scan(&link.Identifier, &link.Title); err != nil {
			return Incoming{}, err
		}
		incoming.Entities = append(incoming.Entities, link)
	}
	if err := rows.Err(); err != nil {
		return Incoming{}, err
	}
	incoming.Total, err = s.total(ctx, where, args, 0, len(incoming.Entities), MaxIncoming)

	return incoming, err
}
