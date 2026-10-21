// Package directory stores the users and groups that SCIM provisions.
package directory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"gofr.dev/pkg/gofr/container"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

// An empty ExternalID or OIDCSubject is stored as NULL.
type User struct {
	ID          string
	ExternalID  string
	OIDCSubject string
	UserName    string
	Email       string
	Name        string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Group struct {
	ID          string
	ExternalID  string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Member struct {
	UserID   string
	UserName string
}

// An empty field matches every row.
type UserFilter struct {
	UserName   string
	ExternalID string
}

type GroupFilter struct {
	DisplayName string
	ExternalID  string
}

type Page struct {
	Offset int
	Limit  int
}

type Store struct {
	db container.DB
}

func NewStore(db container.DB) *Store {
	return &Store{db: db}
}

const userColumns = `id, COALESCE(external_id, ''), COALESCE(oidc_subject, ''), user_name, email, name, active, created_at, updated_at`

const groupColumns = `id, COALESCE(external_id, ''), display_name, created_at, updated_at`

const (
	queryUserByID          = `SELECT ` + userColumns + ` FROM users WHERE id = $1`
	queryUserByOIDCSubject = `SELECT ` + userColumns + ` FROM users WHERE oidc_subject = $1`
	queryUserByExternalID  = `SELECT ` + userColumns + ` FROM users WHERE external_id = $1`
	queryActiveUserByEmail = `SELECT ` + userColumns + ` FROM users WHERE active AND email <> '' AND lower(email) = lower($1)`
	queryRecordSignIn      = `UPDATE users
		SET oidc_subject = $2, last_sign_in_at = NOW(),
			updated_at = CASE WHEN oidc_subject IS DISTINCT FROM $2 THEN NOW() ELSE updated_at END
		WHERE id = $1`
	queryLastSignIn              = `SELECT last_sign_in_at FROM users WHERE id = $1`
	queryCreateUnprovisionedUser = `INSERT INTO users (user_name, email, name)
		SELECT $1, $1, $2 WHERE NOT EXISTS (SELECT 1 FROM users WHERE lower(email) = lower($1))
		RETURNING ` + userColumns
	queryCountUsers = `SELECT COUNT(*) FROM users
		WHERE ($1 = '' OR user_name = $1) AND ($2 = '' OR external_id = $2)`
	queryListUsers = `SELECT ` + userColumns + ` FROM users
		WHERE ($1 = '' OR user_name = $1) AND ($2 = '' OR external_id = $2)
		ORDER BY created_at, id OFFSET $3 LIMIT $4`
	queryCreateUser = `WITH adopted AS (
			UPDATE users
			SET external_id = NULLIF($1, ''), user_name = $2, email = $3, name = $4, active = $5, updated_at = NOW()
			WHERE id = (
				SELECT id FROM users
				WHERE external_id IS NULL AND lower(email) = lower($3)
				ORDER BY active DESC, created_at
				LIMIT 1
			)
			RETURNING ` + userColumns + `
		), created AS (
			INSERT INTO users (external_id, user_name, email, name, active)
			SELECT NULLIF($1, ''), $2, $3, $4, $5
			WHERE NOT EXISTS (SELECT 1 FROM adopted)
			RETURNING ` + userColumns + `
		)
		SELECT * FROM adopted UNION ALL SELECT * FROM created`
	queryReplaceUser = `UPDATE users
		SET external_id = NULLIF($2, ''), user_name = $3, email = $4, name = $5, active = $6, updated_at = NOW()
		WHERE id = $1
		RETURNING ` + userColumns
	queryDeleteUser = `DELETE FROM users WHERE id = $1`

	queryGroupByID   = `SELECT ` + groupColumns + ` FROM groups WHERE id = $1`
	queryCountGroups = `SELECT COUNT(*) FROM groups
		WHERE ($1 = '' OR display_name = $1) AND ($2 = '' OR external_id = $2)`
	queryListGroups = `SELECT ` + groupColumns + ` FROM groups
		WHERE ($1 = '' OR display_name = $1) AND ($2 = '' OR external_id = $2)
		ORDER BY created_at, id OFFSET $3 LIMIT $4`
	queryCreateGroup = `INSERT INTO groups (external_id, display_name) VALUES (NULLIF($1, ''), $2)
		RETURNING ` + groupColumns
	queryReplaceGroup = `UPDATE groups SET external_id = NULLIF($2, ''), display_name = $3, updated_at = NOW()
		WHERE id = $1
		RETURNING ` + groupColumns
	queryDeleteGroup = `DELETE FROM groups WHERE id = $1`

	queryMembers = `SELECT u.id, u.user_name FROM group_members m JOIN users u ON u.id = m.user_id
		WHERE m.group_id = $1 ORDER BY u.user_name`
	querySetMembers = `WITH wanted AS (
			SELECT id FROM users WHERE id::text IN (SELECT jsonb_array_elements_text($2::jsonb))
		), removed AS (
			DELETE FROM group_members WHERE group_id = $1 AND user_id NOT IN (SELECT id FROM wanted)
		)
		INSERT INTO group_members (group_id, user_id) SELECT $1, id FROM wanted ON CONFLICT DO NOTHING`
	queryAddMembers = `INSERT INTO group_members (group_id, user_id)
		SELECT $1, id FROM users WHERE id::text IN (SELECT jsonb_array_elements_text($2::jsonb))
		ON CONFLICT DO NOTHING`
	queryRemoveMembers = `DELETE FROM group_members
		WHERE group_id = $1 AND user_id::text IN (SELECT jsonb_array_elements_text($2::jsonb))`
	queryGroupsOfUser = `SELECT g.id, COALESCE(g.external_id, ''), g.display_name, g.created_at, g.updated_at
		FROM group_members m JOIN groups g ON g.id = m.group_id
		WHERE m.user_id = $1 ORDER BY g.display_name`
)

func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	return s.user(ctx, queryUserByID, id)
}

func (s *Store) UserByOIDCSubject(ctx context.Context, subject string) (User, error) {
	return s.user(ctx, queryUserByOIDCSubject, subject)
}

func (s *Store) UserByExternalID(ctx context.Context, externalID string) (User, error) {
	return s.user(ctx, queryUserByExternalID, externalID)
}

func (s *Store) ActiveUserByEmail(ctx context.Context, email string) (User, error) {
	return s.user(ctx, queryActiveUserByEmail, email)
}

// RecordSignIn binds the user to the OIDC subject and records the time.
func (s *Store) RecordSignIn(ctx context.Context, userID, subject string) error {
	return s.exec(ctx, queryRecordSignIn, userID, subject)
}

// LastSignIn returns when the user last signed in, or the zero time.
func (s *Store) LastSignIn(ctx context.Context, userID string) (time.Time, error) {
	var at sql.NullTime
	err := s.db.QueryRowContext(ctx, queryLastSignIn, userID).Scan(&at)

	return at.Time, classify(err)
}

// CreateUnprovisionedUser creates an active user without an external ID, for
// SCIM to adopt by email later. It returns ErrNotFound when any user, active
// or not, already has the email.
func (s *Store) CreateUnprovisionedUser(ctx context.Context, email, name string) (User, error) {
	return s.user(ctx, queryCreateUnprovisionedUser, email, name)
}

func (s *Store) ListUsers(ctx context.Context, filter UserFilter, page Page) ([]User, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, queryCountUsers, filter.UserName, filter.ExternalID).Scan(&total); err != nil {
		return nil, 0, classify(err)
	}

	rows, err := s.db.QueryContext(ctx, queryListUsers, filter.UserName, filter.ExternalID, page.Offset, page.Limit)
	if err != nil {
		return nil, 0, classify(err)
	}
	users, err := collect(rows, scanUser)

	return users, total, err
}

// CreateUser adopts a row with no external ID and the same email, such as a
// bootstrap admin, before it inserts a new one.
func (s *Store) CreateUser(ctx context.Context, user User) (User, error) {
	return s.user(ctx, queryCreateUser, user.ExternalID, user.UserName, user.Email, user.Name, user.Active)
}

func (s *Store) ReplaceUser(ctx context.Context, id string, user User) (User, error) {
	return s.user(ctx, queryReplaceUser, id, user.ExternalID, user.UserName, user.Email, user.Name, user.Active)
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	return s.execOne(ctx, queryDeleteUser, id)
}

func (s *Store) GroupByID(ctx context.Context, id string) (Group, error) {
	return s.group(ctx, queryGroupByID, id)
}

func (s *Store) ListGroups(ctx context.Context, filter GroupFilter, page Page) ([]Group, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, queryCountGroups, filter.DisplayName, filter.ExternalID).Scan(&total); err != nil {
		return nil, 0, classify(err)
	}

	rows, err := s.db.QueryContext(ctx, queryListGroups, filter.DisplayName, filter.ExternalID, page.Offset, page.Limit)
	if err != nil {
		return nil, 0, classify(err)
	}
	groups, err := collect(rows, scanGroup)

	return groups, total, err
}

func (s *Store) CreateGroup(ctx context.Context, group Group) (Group, error) {
	return s.group(ctx, queryCreateGroup, group.ExternalID, group.DisplayName)
}

func (s *Store) ReplaceGroup(ctx context.Context, id string, group Group) (Group, error) {
	return s.group(ctx, queryReplaceGroup, id, group.ExternalID, group.DisplayName)
}

func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	return s.execOne(ctx, queryDeleteGroup, id)
}

func (s *Store) Members(ctx context.Context, groupID string) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, queryMembers, groupID)
	if err != nil {
		return nil, classify(err)
	}

	return collect(rows, func(row scanner) (Member, error) {
		var member Member
		err := row.Scan(&member.UserID, &member.UserName)
		return member, err
	})
}

// SetMembers, AddMembers and RemoveMembers ignore user IDs that match no user.
func (s *Store) SetMembers(ctx context.Context, groupID string, userIDs []string) error {
	return s.execWithIDs(ctx, querySetMembers, groupID, userIDs)
}

func (s *Store) AddMembers(ctx context.Context, groupID string, userIDs []string) error {
	return s.execWithIDs(ctx, queryAddMembers, groupID, userIDs)
}

func (s *Store) RemoveMembers(ctx context.Context, groupID string, userIDs []string) error {
	return s.execWithIDs(ctx, queryRemoveMembers, groupID, userIDs)
}

func (s *Store) GroupsOfUser(ctx context.Context, userID string) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, queryGroupsOfUser, userID)
	if err != nil {
		return nil, classify(err)
	}

	return collect(rows, scanGroup)
}

func (s *Store) user(ctx context.Context, query string, args ...any) (User, error) {
	user, err := scanUser(s.db.QueryRowContext(ctx, query, args...))
	return user, classify(err)
}

func (s *Store) group(ctx context.Context, query string, args ...any) (Group, error) {
	group, err := scanGroup(s.db.QueryRowContext(ctx, query, args...))
	return group, classify(err)
}

func (s *Store) exec(ctx context.Context, query string, args ...any) error {
	_, err := s.db.ExecContext(ctx, query, args...)
	return classify(err)
}

func (s *Store) execOne(ctx context.Context, query string, args ...any) error {
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return classify(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}

	return nil
}

func (s *Store) execWithIDs(ctx context.Context, query, groupID string, userIDs []string) error {
	ids, err := json.Marshal(append([]string{}, userIDs...))
	if err != nil {
		return err
	}

	return s.exec(ctx, query, groupID, string(ids))
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.ExternalID, &user.OIDCSubject, &user.UserName, &user.Email, &user.Name,
		&user.Active, &user.CreatedAt, &user.UpdatedAt)

	return user, err
}

func scanGroup(row scanner) (Group, error) {
	var group Group
	err := row.Scan(&group.ID, &group.ExternalID, &group.DisplayName, &group.CreatedAt, &group.UpdatedAt)

	return group, err
}

func collect[T any](rows *sql.Rows, scan func(scanner) (T, error)) ([]T, error) {
	defer rows.Close()

	items := []T{}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

// https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	uniqueViolation           = "23505"
	invalidTextRepresentation = "22P02"
)

func classify(err error) error {
	var state interface{ SQLState() string }
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	case errors.As(err, &state) && state.SQLState() == uniqueViolation:
		return ErrConflict
	case errors.As(err, &state) && state.SQLState() == invalidTextRepresentation:
		return ErrNotFound
	}

	return err
}
