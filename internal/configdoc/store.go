package configdoc

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("document not found")
	ErrNoDraft      = errors.New("document has no draft")
	ErrDraftChanged = errors.New("the draft changed")
)

type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Store reads and writes the documents of one kind.
type Store struct {
	db   DB
	kind string
}

func NewStore(db DB, kind string) Store { return Store{db: db, kind: kind} }

// Record is a document as stored: its latest published version, if any,
// and its draft, if any.
type Record struct {
	Identifier       string
	PublishedVersion int
	PublishedSource  string
	PublishedAt      time.Time
	PublishedBy      string
	DraftSource      string
	DraftUpdatedAt   time.Time
	DraftUpdatedBy   string
	CreatedAt        time.Time
}

func (r Record) Published() bool { return r.PublishedVersion > 0 }
func (r Record) HasDraft() bool  { return r.DraftSource != "" }

const recordColumns = `
	b.identifier, coalesce(b.published_version, 0), coalesce(v.source, ''), coalesce(v.published_at, b.created_at),
	coalesce(pu.email, ''), coalesce(b.draft_source, ''), coalesce(b.draft_updated_at, b.created_at),
	coalesce(du.email, ''), b.created_at
	FROM config_documents b
	LEFT JOIN config_versions v ON v.document_id = b.id AND v.version = b.published_version
	LEFT JOIN users pu ON pu.id = v.published_by
	LEFT JOIN users du ON du.id = b.draft_updated_by`

func scanRecord(scan func(...any) error) (Record, error) {
	var r Record
	err := scan(&r.Identifier, &r.PublishedVersion, &r.PublishedSource, &r.PublishedAt, &r.PublishedBy,
		&r.DraftSource, &r.DraftUpdatedAt, &r.DraftUpdatedBy, &r.CreatedAt)

	return r, err
}

// List returns every document, by identifier.
func (s Store) List(ctx context.Context) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+recordColumns+` WHERE b.kind = $1 ORDER BY b.identifier`, s.kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []Record{}
	for rows.Next() {
		record, err := scanRecord(rows.Scan)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

func (s Store) Get(ctx context.Context, identifier string) (Record, error) {
	record, err := scanRecord(s.db.QueryRowContext(ctx, `SELECT `+recordColumns+` WHERE b.kind = $1 AND b.identifier = $2`, s.kind, identifier).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}

	return record, err
}

// PublishedSources returns the published source of every document, by identifier.
func (s Store) PublishedSources(ctx context.Context) (map[string]string, error) {
	records, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	sources := map[string]string{}
	for _, record := range records {
		if record.Published() {
			sources[record.Identifier] = record.PublishedSource
		}
	}

	return sources, nil
}

// SaveDraft stores source as the draft, creating the document if it's new.
func (s Store) SaveDraft(ctx context.Context, identifier, source, userID string) (created bool, err error) {
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO config_documents (kind, identifier, draft_source, draft_updated_at, draft_updated_by, created_by)
		VALUES ($1, $2, $3, now(), $4, $4)
		ON CONFLICT (kind, identifier) DO UPDATE
			SET draft_source = excluded.draft_source, draft_updated_at = now(), draft_updated_by = excluded.draft_updated_by
		RETURNING xmax = 0`,
		s.kind, identifier, source, nullable(userID)).Scan(&created)

	return created, err
}

// DiscardDraft drops the draft. A document that was never published goes with it.
func (s Store) DiscardDraft(ctx context.Context, identifier string) (deleted bool, err error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM config_documents WHERE kind = $1 AND identifier = $2 AND published_version IS NULL`, s.kind, identifier)
	if err != nil {
		return false, err
	}
	if n, _ := result.RowsAffected(); n > 0 {
		return true, nil
	}

	result, err = s.db.ExecContext(ctx, `
		UPDATE config_documents SET draft_source = NULL, draft_updated_at = NULL, draft_updated_by = NULL
		WHERE kind = $1 AND identifier = $2 AND draft_source IS NOT NULL`, s.kind, identifier)
	if err != nil {
		return false, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return false, ErrNoDraft
	}

	return false, nil
}

// Publish makes source, which must still be the draft, the next version.
func (s Store) Publish(ctx context.Context, identifier, source, userID string) (int, error) {
	var version int
	err := s.db.QueryRowContext(ctx, `
		WITH draft AS (
			SELECT id, coalesce(published_version, 0) + 1 AS version
			FROM config_documents WHERE kind = $1 AND identifier = $2 AND draft_source = $3
			FOR UPDATE
		), inserted AS (
			INSERT INTO config_versions (document_id, version, source, published_by)
			SELECT id, version, $3, $4 FROM draft
			RETURNING document_id, version
		)
		UPDATE config_documents b
		SET published_version = inserted.version, draft_source = NULL, draft_updated_at = NULL, draft_updated_by = NULL
		FROM inserted WHERE b.id = inserted.document_id
		RETURNING inserted.version`,
		s.kind, identifier, source, nullable(userID)).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrDraftChanged
	}

	return version, err
}

// Version is one published version of a document.
type Version struct {
	Version     int       `json:"version"`
	PublishedAt time.Time `json:"publishedAt"`
	PublishedBy string    `json:"publishedBy"`
	Source      string    `json:"source,omitempty"`
}

// Versions lists every published version, newest first, without sources.
func (s Store) Versions(ctx context.Context, identifier string) ([]Version, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.version, v.published_at, coalesce(u.email, '')
		FROM config_versions v
		JOIN config_documents b ON b.id = v.document_id
		LEFT JOIN users u ON u.id = v.published_by
		WHERE b.kind = $1 AND b.identifier = $2
		ORDER BY v.version DESC`, s.kind, identifier)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := []Version{}
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.Version, &v.PublishedAt, &v.PublishedBy); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}

	return versions, rows.Err()
}

func (s Store) Version(ctx context.Context, identifier string, version int) (Version, error) {
	v := Version{Version: version}
	err := s.db.QueryRowContext(ctx, `
		SELECT v.published_at, coalesce(u.email, ''), v.source
		FROM config_versions v
		JOIN config_documents b ON b.id = v.document_id
		LEFT JOIN users u ON u.id = v.published_by
		WHERE b.kind = $1 AND b.identifier = $2 AND v.version = $3`, s.kind, identifier, version).Scan(&v.PublishedAt, &v.PublishedBy, &v.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, ErrNotFound
	}

	return v, err
}

func (s Store) Delete(ctx context.Context, identifier string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM config_documents WHERE kind = $1 AND identifier = $2`, s.kind, identifier)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}

	return nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}

	return s
}

// PublishedID returns the row ID of a published document, which other
// tables reference.
func (s Store) PublishedID(ctx context.Context, identifier string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM config_documents
		WHERE kind = $1 AND identifier = $2 AND published_version IS NOT NULL`, s.kind, identifier).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}

	return id, err
}
