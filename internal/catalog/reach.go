package catalog

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/dvjn/goldport/internal/definition"
)

const maxReached = 5000

var errReachTooWide = fmt.Errorf("the path reaches more than %d entities", maxReached)

// Reached is an entity a path reaches, with the value of the property the
// path ends in, if it ends in one.
type Reached struct {
	Entity EntityName `json:"entity"`
	Value  any        `json:"value,omitempty"`
}

const (
	queryLinksFrom = linkColumns + `
		WHERE r.source_blueprint = $1 AND r.source_identifier IN (SELECT jsonb_array_elements_text($2::jsonb))
		ORDER BY r.relation, t.title, t.identifier`
	queryEdges = `SELECT source_identifier, target_identifier FROM entity_relations
		WHERE source_blueprint = $1 AND relation = $2 AND source_identifier IN (SELECT jsonb_array_elements_text($3::jsonb))`
	queryInverseEdges = `SELECT target_identifier, source_identifier FROM entity_relations
		WHERE target_blueprint = $1 AND relation = $2 AND source_blueprint = $3
		AND target_identifier IN (SELECT jsonb_array_elements_text($4::jsonb))`
	queryReachedValues = `SELECT identifier, title, properties -> $3::text FROM entities
		WHERE blueprint = $1 AND identifier IN (SELECT jsonb_array_elements_text($2::jsonb))`
	queryChanges = `SELECT ` + entityColumns + ` FROM entities ORDER BY updated_at DESC, blueprint, identifier LIMIT $1`
)

// LinksFrom returns the outgoing links of the entities of a blueprint.
func (s *Store) LinksFrom(ctx context.Context, blueprint string, identifiers []string) ([]Link, error) {
	encoded, err := json.Marshal(nonNil(identifiers))
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, queryLinksFrom, blueprint, string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	links := []Link{}
	for rows.Next() {
		var link Link
		if err := rows.Scan(&link.Relation, &link.Source.Blueprint, &link.Source.Identifier, &link.Source.Title,
			&link.Target.Blueprint, &link.Target.Identifier, &link.Target.Title); err != nil {
			return nil, err
		}
		links = append(links, link)
	}

	return links, rows.Err()
}

// Reach follows the path from each of the entities of the blueprint and
// returns what it reaches, by the identifier it started from. When the path
// ends in a property, entities without a value are left out.
func (s *Store) Reach(ctx context.Context, blueprint string, identifiers []string, path definition.Path) (map[string][]Reached, error) {
	reached := make(map[string][]string, len(identifiers))
	for _, identifier := range identifiers {
		reached[identifier] = []string{identifier}
	}
	for _, hop := range path.Hops {
		frontier := map[string]bool{}
		for _, found := range reached {
			for _, identifier := range found {
				frontier[identifier] = true
			}
		}
		if len(frontier) > maxReached {
			return nil, errReachTooWide
		}
		edges, err := s.edges(ctx, hop, slices.Sorted(maps.Keys(frontier)))
		if err != nil {
			return nil, err
		}
		for origin, found := range reached {
			next := map[string]bool{}
			for _, identifier := range found {
				for _, target := range edges[identifier] {
					next[target] = true
				}
			}
			reached[origin] = slices.Sorted(maps.Keys(next))
		}
	}

	values, err := s.reachedValues(ctx, path.Target(blueprint), reached, path.Property)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]Reached, len(reached))
	for origin, found := range reached {
		items := []Reached{}
		for _, identifier := range found {
			if item, ok := values[identifier]; ok {
				items = append(items, item)
			}
		}
		slices.SortFunc(items, func(a, b Reached) int {
			return cmp.Or(cmp.Compare(a.Entity.Title, b.Entity.Title), cmp.Compare(a.Entity.Identifier, b.Entity.Identifier))
		})
		result[origin] = items
	}

	return result, nil
}

func (s *Store) edges(ctx context.Context, hop definition.Hop, from []string) (map[string][]string, error) {
	encoded, err := json.Marshal(from)
	if err != nil {
		return nil, err
	}
	query, args := queryEdges, []any{hop.From, hop.Relation, string(encoded)}
	if hop.Inverse {
		query, args = queryInverseEdges, []any{hop.From, hop.Relation, hop.To, string(encoded)}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	edges := map[string][]string{}
	for rows.Next() {
		var source, target string
		if err := rows.Scan(&source, &target); err != nil {
			return nil, err
		}
		edges[source] = append(edges[source], target)
	}

	return edges, rows.Err()
}

func (s *Store) reachedValues(ctx context.Context, blueprint string, reached map[string][]string, property string) (map[string]Reached, error) {
	all := map[string]bool{}
	for _, found := range reached {
		for _, identifier := range found {
			all[identifier] = true
		}
	}
	values := map[string]Reached{}
	if len(all) == 0 {
		return values, nil
	}
	encoded, err := json.Marshal(slices.Sorted(maps.Keys(all)))
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, queryReachedValues, blueprint, string(encoded), property)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		item := Reached{Entity: EntityName{Blueprint: blueprint}}
		var raw []byte
		if err := rows.Scan(&item.Entity.Identifier, &item.Entity.Title, &raw); err != nil {
			return nil, err
		}
		if property != "" {
			if raw == nil {
				continue
			}
			if err := json.Unmarshal(raw, &item.Value); err != nil {
				return nil, err
			}
			if item.Value == nil {
				continue
			}
		}
		values[item.Entity.Identifier] = item
	}

	return values, rows.Err()
}

// Changes returns the most recently updated entities of every blueprint.
func (s *Store) Changes(ctx context.Context, limit int) ([]Entity, error) {
	rows, err := s.db.QueryContext(ctx, queryChanges, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entities := []Entity{}
	for rows.Next() {
		entity, err := scanEntity(rows)
		if err != nil {
			return nil, err
		}
		entities = append(entities, entity)
	}

	return entities, rows.Err()
}
