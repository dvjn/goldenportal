// Package entity stores catalog entities: the data integrations ingest,
// shaped by published blueprints.
package entity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

var ErrNotFound = errors.New("entity not found")

// OwnedError means another integration wrote the entity, and only it may change it.
type OwnedError struct{ Integration string }

func (e OwnedError) Error() string {
	return "the entity belongs to the " + e.Integration + " integration"
}

type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type Store struct{ db DB }

func NewStore(db DB) Store { return Store{db: db} }

// Outcomes of an upsert.
const (
	Created   = "created"
	Updated   = "updated"
	Unchanged = "unchanged"
)

// Upsert writes an entity on behalf of an integration.
func (s Store) Upsert(ctx context.Context, blueprintID, integrationID string, e blueprint.Entity, computed map[string]any) (string, error) {
	properties, _ := json.Marshal(e.Properties)
	relations, _ := json.Marshal(e.Relations)
	computedJSON, _ := json.Marshal(computed)

	var created bool
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO entities (blueprint_id, identifier, title, properties, relations, computed, integration_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (blueprint_id, identifier) DO UPDATE
			SET title = excluded.title, properties = excluded.properties, relations = excluded.relations,
				computed = excluded.computed, updated_at = now()
			WHERE entities.integration_id = excluded.integration_id
				AND (entities.title, entities.properties, entities.relations, entities.computed)
					IS DISTINCT FROM (excluded.title, excluded.properties, excluded.relations, excluded.computed)
		RETURNING xmax = 0`,
		blueprintID, e.Identifier, e.Title, properties, relations, computedJSON, integrationID).Scan(&created)
	switch {
	case err == nil && created:
		return Created, nil
	case err == nil:
		return Updated, nil
	case !errors.Is(err, sql.ErrNoRows):
		return "", err
	}

	// Nothing was written: the entity is either unchanged or someone else's.
	var ownerID, owner string
	err = s.db.QueryRowContext(ctx, `
		SELECT e.integration_id, i.identifier FROM entities e
		JOIN config_documents i ON i.id = e.integration_id
		WHERE e.blueprint_id = $1 AND e.identifier = $2`, blueprintID, e.Identifier).Scan(&ownerID, &owner)
	if err != nil {
		return "", err
	}
	if ownerID != integrationID {
		return "", OwnedError{Integration: owner}
	}

	return Unchanged, nil
}

// Delete removes an entity an integration wrote.
func (s Store) Delete(ctx context.Context, blueprintID, integrationID, identifier string) error {
	var ownerID, owner string
	err := s.db.QueryRowContext(ctx, `
		SELECT e.integration_id, i.identifier FROM entities e
		JOIN config_documents i ON i.id = e.integration_id
		WHERE e.blueprint_id = $1 AND e.identifier = $2`, blueprintID, identifier).Scan(&ownerID, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if ownerID != integrationID {
		return OwnedError{Integration: owner}
	}

	_, err = s.db.ExecContext(ctx, `
		DELETE FROM entities WHERE blueprint_id = $1 AND identifier = $2 AND integration_id = $3`,
		blueprintID, identifier, integrationID)

	return err
}

// Entity is a stored entity as the API shows it.
type Entity struct {
	// ID is the row's ID, which other tables reference.
	ID         string           `json:"-"`
	Blueprint  string           `json:"blueprint"`
	Identifier string           `json:"identifier"`
	Title      string           `json:"title"`
	Scorecards map[string]Score `json:"scorecards,omitempty"`
	// Properties include relations, computed properties and resolved lookups.
	Properties map[string]any `json:"properties"`
	// Related names the entities each relation points at, with their titles.
	Related     map[string][]Link `json:"related"`
	Integration string            `json:"integration"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`

	stored, relations, computed map[string]any
}

// Link is a related entity. Title is empty when it hasn't been ingested yet.
type Link struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title,omitempty"`
}

const (
	entityColumns = `
	e.id, b.identifier, e.identifier, e.title, e.properties, e.relations, e.computed, i.identifier, e.created_at, e.updated_at`
	entityFrom = `
	FROM entities e
	JOIN config_documents b ON b.id = e.blueprint_id
	JOIN config_documents i ON i.id = e.integration_id`
)

func scanEntity(scan func(...any) error, extra ...any) (*Entity, error) {
	var (
		e                               Entity
		properties, relations, computed []byte
	)
	err := scan(append([]any{
		&e.ID, &e.Blueprint, &e.Identifier, &e.Title, &properties, &relations, &computed,
		&e.Integration, &e.CreatedAt, &e.UpdatedAt,
	}, extra...)...)
	if err != nil {
		return nil, err
	}
	for _, field := range []struct {
		raw    []byte
		target *map[string]any
	}{{properties, &e.stored}, {relations, &e.relations}, {computed, &e.computed}} {
		if err := json.Unmarshal(field.raw, field.target); err != nil {
			return nil, err
		}
	}
	e.Properties = map[string]any{}
	for _, values := range []map[string]any{e.stored, e.relations, e.computed} {
		for key, value := range values {
			e.Properties[key] = value
		}
	}
	e.Related = map[string][]Link{}

	return &e, nil
}

func (s Store) Get(ctx context.Context, blueprintIdentifier, identifier string) (*Entity, error) {
	e, err := scanEntity(s.db.QueryRowContext(ctx, `SELECT `+entityColumns+entityFrom+`
		WHERE b.identifier = $1 AND b.kind = 'blueprint' AND e.identifier = $2`, blueprintIdentifier, identifier).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}

	return e, err
}

// Query picks a page of one blueprint's entities.
type Query struct {
	Blueprint blueprint.Spec
	// Search matches the identifier or title, ignoring case.
	Search string
	Filter *Filter
	// Sort is a property, or -property for descending.
	Sort     string
	Page     int
	PageSize int
}

type Page struct {
	Entities []*Entity `json:"entities"`
	Total    int       `json:"total"`
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// List returns a page of entities. Filter and sort problems come back as a
// FilterError, since they're the caller's to fix.
func (s Store) List(ctx context.Context, q Query) (Page, error) {
	c := &compiler{spec: q.Blueprint, args: []any{q.Blueprint.Identifier}}
	where := "b.kind = 'blueprint' AND b.identifier = $1"
	if q.Search != "" {
		search := c.arg("%" + likeEscaper.Replace(q.Search) + "%")
		where += " AND (e.identifier ILIKE " + search + " OR e.title ILIKE " + search + ")"
	}
	if q.Filter != nil {
		sql, err := c.compile(*q.Filter, 1)
		if err != nil {
			return Page{}, FilterError{err}
		}
		where += " AND coalesce(" + sql + ", false)"
	}
	order, err := s.orderBy(ctx, q.Blueprint, q.Sort, c)
	if err != nil {
		return Page{}, err
	}

	limit, offset := c.arg(q.PageSize), c.arg((q.Page-1)*q.PageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT `+entityColumns+`, count(*) OVER ()`+entityFrom+`
		WHERE `+where+`
		ORDER BY `+order+`
		LIMIT `+limit+` OFFSET `+offset, c.args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()

	page := Page{Entities: []*Entity{}}
	for rows.Next() {
		e, err := scanEntity(rows.Scan, &page.Total)
		if err != nil {
			return Page{}, err
		}
		page.Entities = append(page.Entities, e)
	}

	return page, rows.Err()
}

// Identifiers lists the entities matching a filter, by identifier, in the
// catalog's order. At most limit come back.
func (s Store) Identifiers(ctx context.Context, spec blueprint.Spec, filter *Filter, limit int) ([]string, error) {
	c := &compiler{spec: spec, args: []any{spec.Identifier}}
	where := "b.kind = 'blueprint' AND b.identifier = $1"
	if filter != nil {
		sql, err := c.compile(*filter, 1)
		if err != nil {
			return nil, FilterError{err}
		}
		where += " AND coalesce(" + sql + ", false)"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.identifier`+entityFrom+`
		WHERE `+where+`
		ORDER BY e.title, e.identifier
		LIMIT `+c.arg(limit), c.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	identifiers := []string{}
	for rows.Next() {
		var identifier string
		if err := rows.Scan(&identifier); err != nil {
			return nil, err
		}
		identifiers = append(identifiers, identifier)
	}

	return identifiers, rows.Err()
}

// FilterError is a filter or sort the blueprint can't answer.
type FilterError struct{ err error }

func (e FilterError) Error() string { return e.err.Error() }
