// Package entity stores catalog entities: the data connections ingest,
// shaped by published blueprints.
package entity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/httpx"
)

var ErrNotFound = errors.New("entity not found")

// OwnedError means another connection wrote the entity, and only it may change it.
type OwnedError struct{ Connection string }

func (e OwnedError) Error() string {
	return "the entity belongs to the " + e.Connection + " connection"
}

type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type Store struct{ db DB }

func NewStore(db DB) Store { return Store{db: db} }

// Delete removes an entity a connection wrote.
func (s Store) Delete(ctx context.Context, blueprintID, connectionID, identifier string) error {
	var ownerID, owner string
	err := s.db.QueryRowContext(ctx, `
		SELECT e.connection_id, i.identifier FROM entities e
		JOIN config_documents i ON i.id = e.connection_id
		WHERE e.blueprint_id = $1 AND e.identifier = $2`, blueprintID, identifier).Scan(&ownerID, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if ownerID != connectionID {
		return OwnedError{Connection: owner}
	}

	_, err = s.db.ExecContext(ctx, `
		DELETE FROM entities WHERE blueprint_id = $1 AND identifier = $2 AND connection_id = $3`,
		blueprintID, identifier, connectionID)

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
	Related    map[string][]Link `json:"related"`
	Connection string            `json:"connection"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`

	stored, relations, computed map[string]any
}

// Link is a related entity. Title is empty when it hasn't been ingested yet.
type Link struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title,omitempty"`
}

// entityColumns are what scanEntity reads, with blueprint SQL for the
// blueprint's identifier.
func entityColumns(blueprint string) string {
	return `e.id, ` + blueprint + `, e.identifier, e.title, e.properties, e.relations, e.computed, ` +
		connectionOf("e.connection_id") + `, e.created_at, e.updated_at`
}

func scanEntity(scan func(...any) error) (*Entity, error) {
	var (
		e                               Entity
		properties, relations, computed []byte
	)
	err := scan(&e.ID, &e.Blueprint, &e.Identifier, &e.Title, &properties, &relations, &computed,
		&e.Connection, &e.CreatedAt, &e.UpdatedAt)
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
	spec := blueprint.Spec{Identifier: blueprintIdentifier}
	e, err := scanEntity(s.db.QueryRowContext(ctx, `SELECT `+entityColumns(quoteLiteral(spec.Identifier))+` FROM entities e
		WHERE e.blueprint_id = `+BlueprintID(spec)+` AND e.identifier = $1`, identifier).Scan)
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

// minContains is how long search text must be to match anywhere in a
// title or identifier: trigram indexes find shorter text only at the start.
const minContains = 3

// TextPattern is a LIKE pattern for text in a title or identifier: anywhere
// in it, or at its start for text shorter than minContains. LIKE wildcards
// in text match themselves.
func TextPattern(text string) string {
	pattern := likeEscaper.Replace(text) + "%"
	if utf8.RuneCountInString(text) >= minContains {
		pattern = "%" + pattern
	}

	return pattern
}

// matchText is SQL for whether e's title or identifier holds text, as
// TextPattern says, which the trigram indexes on both answer.
func (c *compiler) matchText(text string) string {
	pattern := c.arg(TextPattern(text))
	return "(lower(e.title) LIKE lower(" + pattern + ") OR lower(e.identifier) LIKE lower(" + pattern + "))"
}

// SearchResult is an entity a search found.
type SearchResult struct {
	Blueprint  string `json:"blueprint"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

// Search finds up to limit entities of blueprints whose title or
// identifier holds text, ignoring case, as TextPattern says: exact matches
// first, then those starting with it, then by title.
func (s Store) Search(ctx context.Context, blueprints []blueprint.Spec, text string, limit int) ([]SearchResult, error) {
	ids, identifiers := make([]string, 0, len(blueprints)), map[string]string{}
	byName := func(a, b blueprint.Spec) int { return strings.Compare(a.Identifier, b.Identifier) }
	for _, spec := range slices.SortedFunc(slices.Values(blueprints), byName) {
		if spec.ID != "" {
			ids, identifiers[spec.ID] = append(ids, spec.ID), spec.Identifier
		}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.blueprint_id, e.identifier, e.title
		FROM entities e
		WHERE e.blueprint_id = ANY($1::uuid[])
			AND (lower(e.title) LIKE lower($2) OR lower(e.identifier) LIKE lower($2))
		ORDER BY lower(e.title) = lower($3) OR lower(e.identifier) = lower($3) DESC,
			lower(e.title) LIKE lower($4) OR lower(e.identifier) LIKE lower($4) DESC,
			e.title, array_position($1::uuid[], e.blueprint_id), e.identifier
		LIMIT $5`,
		pq.Array(ids), TextPattern(text), text, likeEscaper.Replace(text)+"%", limit)
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
		r.Blueprint = identifiers[r.Blueprint]
		found = append(found, r)
	}

	return found, rows.Err()
}

// schema is what q's paths read.
func (q Query) schema() Schema {
	return Schema{Blueprint: q.Blueprint, Blueprints: q.Blueprints, Ladders: q.Ladders}
}

// check checks q's filter and sort as they're written: strictly, so a
// scorecard, tier or check that isn't there is an error rather than
// matching nothing.
func (q Query) check() error {
	if q.Filter != nil {
		if err := CheckFilter(q.schema(), *q.Filter); err != nil {
			return err
		}
	}

	return CheckSort(q.schema(), q.Sort)
}

func (q Query) where(c *compiler) (string, error) {
	where := "e.blueprint_id = " + BlueprintID(q.Blueprint)
	if q.Search != "" {
		where += " AND " + c.matchText(q.Search)
	}
	if q.Filter != nil {
		sql, err := c.compile(*q.Filter, 1)
		if err != nil {
			return "", FilterError{err}
		}
		where += " AND " + sql
	}

	return where, nil
}

// List returns a page of entities. Filter and sort problems come back as a
// FilterError, since they're the caller's to fix.
func (s Store) List(ctx context.Context, q Query) (Page, error) {
	c := q.schema().compiler(false)
	where, err := q.where(c)
	if err != nil {
		return Page{}, err
	}
	matchArgs := slices.Clone(c.args)
	order, err := c.orderBy(q.Sort)
	if err != nil {
		return Page{}, FilterError{err}
	}

	offset := (q.Page - 1) * q.PageSize
	rows, err := s.db.QueryContext(ctx, `SELECT `+entityColumns(quoteLiteral(q.Blueprint.Identifier))+c.from()+`
		WHERE `+where+`
		ORDER BY `+order+`
		LIMIT `+c.arg(q.PageSize)+` OFFSET `+c.arg(offset), c.args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()

	page := Page{Items: []*Entity{}}
	for rows.Next() {
		e, err := scanEntity(rows.Scan)
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, e)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	page.Total, err = s.total(ctx, where, matchArgs, offset, len(page.Items), q.PageSize)

	return page, err
}

// total is how many entities match where, for a page of size from offset
// that holds got of them.
func (s Store) total(ctx context.Context, where string, args []any, offset, got, size int) (int, error) {
	if total, known := httpx.PageTotal(offset, got, size); known {
		return total, nil
	}

	return s.count(ctx, where, args)
}

func (s Store) count(ctx context.Context, where string, args []any) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM entities e WHERE `+where, args...).Scan(&n)

	return n, err
}

// Count is how many entities q's search and filter match.
func (s Store) Count(ctx context.Context, q Query) (int, error) {
	c := q.schema().compiler(false)
	where, err := q.where(c)
	if err != nil {
		return 0, err
	}

	return s.count(ctx, where, c.args)
}

// Exists reports whether any entity matches q's search and filter.
func (s Store) Exists(ctx context.Context, q Query) (bool, error) {
	c := q.schema().compiler(false)
	where, err := q.where(c)
	if err != nil {
		return false, err
	}
	var exists bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM entities e WHERE `+where+`)`, c.args...).Scan(&exists)

	return exists, err
}

// Identifiers lists the entities matching a filter, by identifier, in the
// catalog's order. At most limit come back. specs are the blueprints the
// filter's relations lead to.
func (s Store) Identifiers(ctx context.Context, specs map[string]blueprint.Spec, spec blueprint.Spec, filter *Filter, limit int) ([]string, error) {
	c := &compiler{spec: spec, specs: specs}
	where, err := Query{Blueprint: spec, Filter: filter}.where(c)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.identifier FROM entities e
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
	doc["identifier"], doc["title"], doc["blueprint"], doc["connection"] = e.Identifier, e.Title, e.Blueprint, e.Connection
	doc["createdAt"] = e.CreatedAt.UTC().Format(time.RFC3339)
	doc["updatedAt"] = e.UpdatedAt.UTC().Format(time.RFC3339)

	return doc
}
