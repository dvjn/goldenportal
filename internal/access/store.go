// Package access lets admins grant roles to users and groups directly.
package access

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/dvjn/goldenportal/internal/auth"
)

var (
	errNotFound = errors.New("not found")
	errNoGrant  = errors.New("no role granted directly")
)

type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type Store struct{ db DB }

func NewStore(db DB) Store { return Store{db: db} }

// Grant is a role given directly to a user or a group.
type Grant struct {
	SubjectType string    `json:"subjectType"`
	SubjectID   string    `json:"subjectId"`
	Name        string    `json:"name"`
	Email       string    `json:"email,omitempty"`
	MemberCount *int      `json:"memberCount,omitempty"`
	Role        auth.Role `json:"role"`
	GrantedAt   time.Time `json:"grantedAt"`
}

// User and Group are what an admin can grant a role to.
type User struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
}

type Group struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MemberCount int    `json:"memberCount"`
}

// ListGrants returns every role given directly to a user or group. Roles
// people get through their groups are not listed.
func (s Store) ListGrants(ctx context.Context) ([]Grant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT rb.subject_type, rb.subject_id, rb.role, rb.created_at,
			coalesce(u.display_name, g.name), coalesce(u.email, ''),
			CASE WHEN g.id IS NOT NULL
				THEN (SELECT count(*) FROM group_members gm WHERE gm.group_id = g.id) END
		FROM role_bindings rb
		LEFT JOIN users u ON rb.subject_type = 'user' AND u.id = rb.subject_id
		LEFT JOIN groups g ON rb.subject_type = 'group' AND g.id = rb.subject_id
		WHERE u.id IS NOT NULL OR g.id IS NOT NULL
		ORDER BY lower(coalesce(u.display_name, g.name))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	grants := []Grant{}
	for rows.Next() {
		var (
			g       Grant
			members sql.NullInt64
		)
		if err := rows.Scan(&g.SubjectType, &g.SubjectID, &g.Role, &g.GrantedAt, &g.Name, &g.Email, &members); err != nil {
			return nil, err
		}
		if members.Valid {
			n := int(members.Int64)
			g.MemberCount = &n
		}
		grants = append(grants, g)
	}

	return grants, rows.Err()
}

func (s Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, display_name, email FROM users
		WHERE status = 'active'
		ORDER BY lower(display_name), email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.DisplayName, &u.Email); err != nil {
			return nil, err
		}
		users = append(users, u)
	}

	return users, rows.Err()
}

func (s Store) ListGroups(ctx context.Context) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id, g.name, (SELECT count(*) FROM group_members gm WHERE gm.group_id = g.id)
		FROM groups g
		ORDER BY lower(g.name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.MemberCount); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}

	return groups, rows.Err()
}

// Grant gives a user or group a role, replacing any role they had directly.
// It returns the subject's name for the audit log.
func (s Store) Grant(ctx context.Context, subjectType, subjectID string, role auth.Role) (string, error) {
	name, err := s.name(ctx, subjectType, subjectID)
	if err != nil {
		return "", err
	}

	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM role_bindings WHERE subject_type = $1 AND subject_id = $2`, subjectType, subjectID); err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO role_bindings (subject_type, subject_id, role) VALUES ($1, $2, $3)`,
		subjectType, subjectID, string(role))

	return name, err
}

// Revoke removes the role given directly to a user or group.
func (s Store) Revoke(ctx context.Context, subjectType, subjectID string) (string, error) {
	name, err := s.name(ctx, subjectType, subjectID)
	if err != nil {
		return "", err
	}

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM role_bindings WHERE subject_type = $1 AND subject_id = $2`, subjectType, subjectID)
	if err != nil {
		return "", err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", errNoGrant
	}

	return name, nil
}

// name returns a user's display name or a group's name.
func (s Store) name(ctx context.Context, subjectType, subjectID string) (string, error) {
	query := map[string]string{
		"user":  `SELECT display_name FROM users WHERE id = $1`,
		"group": `SELECT name FROM groups WHERE id = $1`,
	}[subjectType]

	var name string
	err := s.db.QueryRowContext(ctx, query, subjectID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}

	return name, err
}
