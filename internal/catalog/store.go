// Package catalog stores the definitions, their revisions and the entities
// they describe, and serves the API that reads and changes them.
package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/definition"
	"gofr.dev/pkg/gofr/container"
)

var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
	ErrStale    = errors.New("the definitions changed since the plan")
)

type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type Store struct {
	db container.DB
}

func NewStore(db container.DB) *Store {
	return &Store{db: db}
}

// Author is who applied a revision. UserID is empty for a service token.
type Author struct {
	UserID string
	Label  string
}

// Revision is one applied change to the definitions. Snapshot holds every
// definition as it was after the change.
type Revision struct {
	Number     int
	Author     Author
	Message    string
	RollbackOf int
	CreatedAt  time.Time
	Snapshot   definition.Set
	Changes    []definition.Change
	Impact     Impact
}

type Page struct {
	Offset int
	Limit  int
}

const (
	queryRevisionNumber = `SELECT COALESCE(MAX(number), 0) FROM definition_revisions`
	queryDefinitions    = `SELECT document FROM definitions`
	queryLockRevisions  = `LOCK TABLE definition_revisions IN EXCLUSIVE MODE`
	queryClearDefs      = `DELETE FROM definitions`
	queryInsertDef      = `INSERT INTO definitions (kind, identifier, document) VALUES ($1, $2, $3)`
	queryInsertRevision = `INSERT INTO definition_revisions
			(number, author_user_id, author_label, message, rollback_of, snapshot, changes, impact)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, NULLIF($5, 0), $6, $7, $8)
		RETURNING created_at`
	queryCountRevisions = `SELECT COUNT(*) FROM definition_revisions`
	queryRevisions      = `SELECT number, COALESCE(author_user_id::text, ''), author_label, message,
			COALESCE(rollback_of, 0), created_at, changes, impact
		FROM definition_revisions ORDER BY number DESC OFFSET $1 LIMIT $2`
	queryRevision = `SELECT number, COALESCE(author_user_id::text, ''), author_label, message,
			COALESCE(rollback_of, 0), created_at, changes, impact
		FROM definition_revisions WHERE number = $1`
	querySnapshot = `SELECT snapshot FROM definition_revisions WHERE number = $1`
)

// Definitions returns the current definitions and the number of the
// revision that made them, 0 before the first apply.
func (s *Store) Definitions(ctx context.Context) (definition.Set, int, error) {
	var number int
	if err := s.db.QueryRowContext(ctx, queryRevisionNumber).Scan(&number); err != nil {
		return definition.Set{}, 0, err
	}
	rows, err := s.db.QueryContext(ctx, queryDefinitions)
	if err != nil {
		return definition.Set{}, 0, err
	}
	defer rows.Close()

	var documents []definition.Document
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return definition.Set{}, 0, err
		}
		document, err := definition.DecodeDocument(data)
		if err != nil {
			return definition.Set{}, 0, err
		}
		documents = append(documents, document)
	}

	return definition.NewSet(documents...), number, rows.Err()
}

// Impact measures what the effects would do to the entities now.
func (s *Store) Impact(ctx context.Context, effects definition.Effects) (Impact, error) {
	measured, err := measure(s.db, effects)
	return measured.impact, err
}

// Apply replaces the definitions with the plan's, applies its effects to the
// entities and records the revision, all in one transaction. It refuses a
// plan made on top of another revision than the current one.
func (s *Store) Apply(ctx context.Context, plan Plan, author audit.Actor, message string) (Revision, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Revision{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, queryLockRevisions); err != nil {
		return Revision{}, err
	}
	var current int
	if err := tx.QueryRowContext(ctx, queryRevisionNumber).Scan(&current); err != nil {
		return Revision{}, err
	}
	if current != plan.Base {
		return Revision{}, ErrStale
	}
	if err := replaceDefinitions(ctx, tx, plan.Desired); err != nil {
		return Revision{}, err
	}
	measured, err := measure(tx, plan.Effects)
	if err != nil {
		return Revision{}, err
	}
	if err := applyEffects(ctx, tx, plan.Effects, measured); err != nil {
		return Revision{}, err
	}

	revision := Revision{
		Number:     current + 1,
		Author:     Author{UserID: author.UserID(), Label: author.Label()},
		Message:    message,
		RollbackOf: plan.RollbackOf,
		Snapshot:   plan.Desired,
		Changes:    plan.Changes,
		Impact:     measured.impact,
	}
	snapshot, changes, impact, err := revisionJSON(revision)
	if err != nil {
		return Revision{}, err
	}
	if err := tx.QueryRowContext(ctx, queryInsertRevision, revision.Number, revision.Author.UserID, revision.Author.Label, message,
		plan.RollbackOf, snapshot, changes, impact).Scan(&revision.CreatedAt); err != nil {
		return Revision{}, err
	}

	return revision, tx.Commit()
}

func replaceDefinitions(ctx context.Context, q querier, set definition.Set) error {
	if _, err := q.ExecContext(ctx, queryClearDefs); err != nil {
		return err
	}
	for _, document := range set.Documents {
		data, err := json.Marshal(document)
		if err != nil {
			return err
		}
		ref := document.Ref()
		if _, err := q.ExecContext(ctx, queryInsertDef, ref.Kind, ref.Identifier, string(data)); err != nil {
			return err
		}
	}

	return nil
}

func revisionJSON(revision Revision) (snapshot, changes, impact string, err error) {
	encoded := make([][]byte, 3)
	for index, value := range []any{revision.Snapshot, nonNil(revision.Changes), revision.Impact} {
		if encoded[index], err = json.Marshal(value); err != nil {
			return "", "", "", err
		}
	}

	return string(encoded[0]), string(encoded[1]), string(encoded[2]), nil
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}

	return items
}

// Revisions returns a page of revisions, newest first, without snapshots.
func (s *Store) Revisions(ctx context.Context, page Page) ([]Revision, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, queryCountRevisions).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, queryRevisions, page.Offset, page.Limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	revisions := []Revision{}
	for rows.Next() {
		revision, err := scanRevision(rows)
		if err != nil {
			return nil, 0, err
		}
		revisions = append(revisions, revision)
	}

	return revisions, total, rows.Err()
}

func (s *Store) Revision(ctx context.Context, number int) (Revision, error) {
	revision, err := scanRevision(s.db.QueryRowContext(ctx, queryRevision, number))
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, ErrNotFound
	}
	if err != nil {
		return Revision{}, err
	}
	var snapshot []byte
	if err := s.db.QueryRowContext(ctx, querySnapshot, number).Scan(&snapshot); err != nil {
		return Revision{}, err
	}

	return revision, json.Unmarshal(snapshot, &revision.Snapshot)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRevision(row scanner) (Revision, error) {
	var (
		revision Revision
		changes  []byte
		impact   []byte
	)
	if err := row.Scan(&revision.Number, &revision.Author.UserID, &revision.Author.Label, &revision.Message,
		&revision.RollbackOf, &revision.CreatedAt, &changes, &impact); err != nil {
		return Revision{}, err
	}
	if err := json.Unmarshal(changes, &revision.Changes); err != nil {
		return Revision{}, err
	}

	return revision, json.Unmarshal(impact, &revision.Impact)
}
