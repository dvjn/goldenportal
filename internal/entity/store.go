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

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/httpx"
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
	ID         string `json:"-"`
	Blueprint  string `json:"blueprint"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	// Properties include relations, computed properties, resolved lookups
	// and scorecard results.
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
	// Blueprints are the published ones, for filters that follow relations.
	Blueprints map[string]blueprint.Spec
	// Ladders are the published scorecards' tiers, which tiers sort by.
	Ladders map[string]blueprint.Ladder
	// Search matches the identifier or title, ignoring case.
	Search string
	Filter *Filter
	// Sort is a path, or -path for descending.
	Sort     string
	Page     int
	PageSize int
}

type Page = httpx.Page[*Entity]

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// SearchResult is an entity a search found.
type SearchResult struct {
	Blueprint  string `json:"blueprint"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

// Search finds up to limit entities of blueprints whose title or
// identifier contains text, ignoring case: exact matches first, then those
// starting with it, then by title.
func (s Store) Search(ctx context.Context, blueprints []string, text string, limit int) ([]SearchResult, error) {
	escaped := likeEscaper.Replace(text)
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.identifier, e.identifier, e.title
		FROM entities e
		JOIN config_documents b ON b.id = e.blueprint_id
		WHERE b.kind = 'blueprint' AND b.identifier = ANY($1)
			AND (e.title ILIKE $2 OR e.identifier ILIKE $2)
		ORDER BY lower(e.title) = lower($3) OR lower(e.identifier) = lower($3) DESC,
			e.title ILIKE $4 OR e.identifier ILIKE $4 DESC,
			e.title, b.identifier, e.identifier
		LIMIT $5`,
		pq.Array(blueprints), "%"+escaped+"%", text, escaped+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	found := []SearchResult{}
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Blueprint, &r.Identifier, &r.Title); err != nil {
			return nil, err
		}
		found = append(found, r)
	}

	return found, rows.Err()
}

// List returns a page of entities. Filter and sort problems come back as a
// FilterError, since they're the caller's to fix.
func (s Store) List(ctx context.Context, q Query) (Page, error) {
	c := &compiler{spec: q.Blueprint, specs: q.Blueprints, args: []any{q.Blueprint.Identifier}}
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
		where += " AND " + sql
	}
	order, err := c.orderBy(q.Sort, q.Ladders)
	if err != nil {
		return Page{}, FilterError{err}
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

	page := Page{Items: []*Entity{}}
	for rows.Next() {
		e, err := scanEntity(rows.Scan, &page.Total)
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, e)
	}

	return page, rows.Err()
}

// Identifiers lists the entities matching a filter, by identifier, in the
// catalog's order. At most limit come back. specs are the blueprints the
// filter's relations lead to.
func (s Store) Identifiers(ctx context.Context, specs map[string]blueprint.Spec, spec blueprint.Spec, filter *Filter, limit int) ([]string, error) {
	c := &compiler{spec: spec, specs: specs, args: []any{spec.Identifier}}
	where := "b.kind = 'blueprint' AND b.identifier = $1"
	if filter != nil {
		sql, err := c.compile(*filter, 1)
		if err != nil {
			return nil, FilterError{err}
		}
		where += " AND " + sql
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

// Document is an entity as jq reads it, everywhere: flat, as paths see it,
// with its properties beside the built-in fields, whose names they can't
// take. Times are whole seconds, which jq's fromdate reads.
func Document(e *Entity) map[string]any {
	encoded, _ := json.Marshal(e.Properties)
	doc := map[string]any{}
	_ = json.Unmarshal(encoded, &doc)
	doc["identifier"], doc["title"], doc["blueprint"], doc["integration"] = e.Identifier, e.Title, e.Blueprint, e.Integration
	doc["createdAt"] = e.CreatedAt.UTC().Format(time.RFC3339)
	doc["updatedAt"] = e.UpdatedAt.UTC().Format(time.RFC3339)

	return doc
}
