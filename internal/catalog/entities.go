package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const uniqueViolation = "23505"

// Entity is one thing in the catalog, of one blueprint. Links are the
// entity's relations to other entities.
type Entity struct {
	Blueprint   string
	Identifier  string
	Title       string
	Properties  map[string]any
	Links       []Link
	Integration string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (e Entity) Name() EntityName {
	return EntityName{Blueprint: e.Blueprint, Identifier: e.Identifier, Title: e.Title}
}

// Link is one relation between two entities.
type Link struct {
	Relation string
	Source   EntityName
	Target   EntityName
}

// EntityFilter selects entities of a blueprint. Search matches the title or
// the identifier; an empty field matches every entity.
type EntityFilter struct {
	Blueprint   string
	Search      string
	Integration string
}

type IntegrationCount struct {
	Integration string
	Count       int
}

const (
	entityColumns     = `blueprint, identifier, title, properties, integration, created_at, updated_at`
	entityFilter      = `WHERE blueprint = $1 AND (title ILIKE $2 OR identifier ILIKE $2) AND ($3 = '' OR integration = $3)`
	queryCountMatches = `SELECT COUNT(*) FROM entities ` + entityFilter
	queryEntities     = `SELECT ` + entityColumns + ` FROM entities ` + entityFilter + `
		ORDER BY title, identifier OFFSET $4 LIMIT $5`
	queryEntity            = `SELECT ` + entityColumns + ` FROM entities WHERE blueprint = $1 AND identifier = $2`
	queryBlueprintCounts   = `SELECT blueprint, COUNT(*) FROM entities GROUP BY blueprint`
	queryIntegrationCounts = `SELECT integration, COUNT(*) FROM entities WHERE blueprint = $1
		GROUP BY integration ORDER BY integration`
	linkColumns = `SELECT r.relation, s.blueprint, s.identifier, s.title, t.blueprint, t.identifier, t.title
		FROM entity_relations r
		JOIN entities s ON s.blueprint = r.source_blueprint AND s.identifier = r.source_identifier
		JOIN entities t ON t.blueprint = r.target_blueprint AND t.identifier = r.target_identifier`
	queryOutgoing = linkColumns + `
		WHERE r.source_blueprint = $1 AND r.source_identifier = $2 ORDER BY t.title, t.identifier`
	queryIncoming = linkColumns + `
		WHERE r.target_blueprint = $1 AND r.target_identifier = $2 ORDER BY s.title, s.identifier`
	queryExisting = `SELECT identifier FROM entities
		WHERE blueprint = $1 AND identifier IN (SELECT jsonb_array_elements_text($2::jsonb))`
	queryReaches = `WITH RECURSIVE chain (identifier) AS (
			SELECT $3::text
			UNION
			SELECT r.target_identifier FROM entity_relations r JOIN chain c ON r.source_identifier = c.identifier
			WHERE r.source_blueprint = $1 AND r.target_blueprint = $1 AND r.relation = $2
		)
		SELECT EXISTS (SELECT 1 FROM chain WHERE identifier = $4)`
	queryInsertEntity = `INSERT INTO entities (blueprint, identifier, title, properties, integration)
		VALUES ($1, $2, $3, $4, $5) RETURNING created_at, updated_at`
	queryUpdateEntity = `UPDATE entities SET title = $3, properties = $4, updated_at = NOW()
		WHERE blueprint = $1 AND identifier = $2 RETURNING created_at, updated_at`
	queryDeleteEntity = `DELETE FROM entities WHERE blueprint = $1 AND identifier = $2`
	queryClearLinks   = `DELETE FROM entity_relations WHERE source_blueprint = $1 AND source_identifier = $2`
	queryInsertLink   = `INSERT INTO entity_relations
		(source_blueprint, source_identifier, relation, target_blueprint, target_identifier)
		VALUES ($1, $2, $3, $4, $5)`
)

// Entities returns a page of the entities that match the filter, by title,
// and how many match.
func (s *Store) Entities(ctx context.Context, filter EntityFilter, page Page) ([]Entity, int, error) {
	search := "%" + escapeLike(filter.Search) + "%"
	var total int
	if err := s.db.QueryRowContext(ctx, queryCountMatches, filter.Blueprint, search, filter.Integration).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, queryEntities, filter.Blueprint, search, filter.Integration, page.Offset, page.Limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entities := []Entity{}
	for rows.Next() {
		entity, err := scanEntity(rows)
		if err != nil {
			return nil, 0, err
		}
		entities = append(entities, entity)
	}

	return entities, total, rows.Err()
}

func escapeLike(text string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text)
}

// Entity returns the entity with its links.
func (s *Store) Entity(ctx context.Context, blueprint, identifier string) (Entity, error) {
	entity, err := scanEntity(s.db.QueryRowContext(ctx, queryEntity, blueprint, identifier))
	if errors.Is(err, sql.ErrNoRows) {
		return Entity{}, ErrNotFound
	}
	if err != nil {
		return Entity{}, err
	}
	entity.Links, err = s.links(ctx, queryOutgoing, entity.Name())

	return entity, err
}

// Incoming returns the links to the entity from other entities.
func (s *Store) Incoming(ctx context.Context, entity EntityName) ([]Link, error) {
	return s.links(ctx, queryIncoming, entity)
}

func (s *Store) links(ctx context.Context, query string, entity EntityName) ([]Link, error) {
	rows, err := s.db.QueryContext(ctx, query, entity.Blueprint, entity.Identifier)
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

func (s *Store) BlueprintCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, queryBlueprintCounts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var (
			blueprint string
			count     int
		)
		if err := rows.Scan(&blueprint, &count); err != nil {
			return nil, err
		}
		counts[blueprint] = count
	}

	return counts, rows.Err()
}

// IntegrationCounts returns how many entities of the blueprint each
// integration owns.
func (s *Store) IntegrationCounts(ctx context.Context, blueprint string) ([]IntegrationCount, error) {
	rows, err := s.db.QueryContext(ctx, queryIntegrationCounts, blueprint)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := []IntegrationCount{}
	for rows.Next() {
		var count IntegrationCount
		if err := rows.Scan(&count.Integration, &count.Count); err != nil {
			return nil, err
		}
		counts = append(counts, count)
	}

	return counts, rows.Err()
}

// Missing returns the identifiers that no entity of the blueprint has.
func (s *Store) Missing(ctx context.Context, blueprint string, identifiers []string) ([]string, error) {
	encoded, err := json.Marshal(nonNil(identifiers))
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, queryExisting, blueprint, string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var identifier string
		if err := rows.Scan(&identifier); err != nil {
			return nil, err
		}
		existing[identifier] = true
	}
	var missing []string
	for _, identifier := range identifiers {
		if !existing[identifier] {
			missing = append(missing, identifier)
		}
	}

	return missing, rows.Err()
}

// Reaches reports whether following the relation from the start entity,
// within one blueprint, leads to the goal entity.
func (s *Store) Reaches(ctx context.Context, blueprint, relation, start, goal string) (bool, error) {
	var reaches bool
	err := s.db.QueryRowContext(ctx, queryReaches, blueprint, relation, start, goal).Scan(&reaches)

	return reaches, err
}

func (s *Store) CreateEntity(ctx context.Context, entity Entity) (Entity, error) {
	return s.writeEntity(ctx, entity, func(tx querier, properties string) *sql.Row {
		return tx.QueryRowContext(ctx, queryInsertEntity, entity.Blueprint, entity.Identifier, entity.Title, properties, entity.Integration)
	})
}

// UpdateEntity replaces the entity's title, properties and relations.
func (s *Store) UpdateEntity(ctx context.Context, entity Entity) (Entity, error) {
	return s.writeEntity(ctx, entity, func(tx querier, properties string) *sql.Row {
		return tx.QueryRowContext(ctx, queryUpdateEntity, entity.Blueprint, entity.Identifier, entity.Title, properties)
	})
}

func (s *Store) writeEntity(ctx context.Context, entity Entity, write func(tx querier, properties string) *sql.Row) (Entity, error) {
	properties, err := json.Marshal(entity.Properties)
	if err != nil {
		return Entity{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Entity{}, err
	}
	defer func() { _ = tx.Rollback() }()

	err = write(tx, string(properties)).Scan(&entity.CreatedAt, &entity.UpdatedAt)
	var state interface{ SQLState() string }
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Entity{}, ErrNotFound
	case errors.As(err, &state) && state.SQLState() == uniqueViolation:
		return Entity{}, ErrExists
	case err != nil:
		return Entity{}, err
	}
	if _, err := tx.ExecContext(ctx, queryClearLinks, entity.Blueprint, entity.Identifier); err != nil {
		return Entity{}, err
	}
	for _, link := range entity.Links {
		if _, err := tx.ExecContext(ctx, queryInsertLink, entity.Blueprint, entity.Identifier, link.Relation,
			link.Target.Blueprint, link.Target.Identifier); err != nil {
			return Entity{}, err
		}
	}

	return entity, tx.Commit()
}

func (s *Store) DeleteEntity(ctx context.Context, blueprint, identifier string) error {
	result, err := s.db.ExecContext(ctx, queryDeleteEntity, blueprint, identifier)
	if err != nil {
		return err
	}
	if deleted, err := result.RowsAffected(); err == nil && deleted == 0 {
		return ErrNotFound
	}

	return err
}

func scanEntity(row scanner) (Entity, error) {
	var (
		entity     Entity
		properties []byte
	)
	if err := row.Scan(&entity.Blueprint, &entity.Identifier, &entity.Title, &properties, &entity.Integration,
		&entity.CreatedAt, &entity.UpdatedAt); err != nil {
		return Entity{}, err
	}

	return entity, json.Unmarshal(properties, &entity.Properties)
}
