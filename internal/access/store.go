// Package access lets admins see users and groups and bind roles to them.
package access

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/auth"
)

var errNotFound = errors.New("not found")

type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type Store struct{ db DB }

func NewStore(db DB) Store { return Store{db: db} }

type User struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"displayName"`
	Status      string     `json:"status"`
	LastLoginAt *time.Time `json:"lastLoginAt"`
	// Role is bound to the user directly; nil means none.
	Role *auth.Role `json:"role"`
	// EffectiveRole also counts roles bound to the user's groups.
	EffectiveRole auth.Role `json:"effectiveRole"`
	Groups        []string  `json:"groups"`
}

type Group struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	IsTeam      bool       `json:"isTeam"`
	MemberCount int        `json:"memberCount"`
	Role        *auth.Role `json:"role"`
}

func (s Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.email, u.display_name, u.status, u.last_login_at,
			(SELECT rb.role FROM role_bindings rb
				WHERE rb.subject_type = 'user' AND rb.subject_id = u.id
				ORDER BY CASE rb.role WHEN 'admin' THEN 3 WHEN 'builder' THEN 2 ELSE 1 END DESC
				LIMIT 1),
			`+auth.EffectiveRoleSQL+`,
			coalesce((SELECT array_agg(g.name ORDER BY g.name) FROM group_members gm
				JOIN groups g ON g.id = gm.group_id WHERE gm.user_id = u.id), '{}')
		FROM users u
		ORDER BY lower(u.display_name), u.email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var (
			u    User
			role sql.NullString
		)
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Status, &u.LastLoginAt,
			&role, &u.EffectiveRole, pq.Array(&u.Groups)); err != nil {
			return nil, err
		}
		u.Role = nullRole(role)
		users = append(users, u)
	}

	return users, rows.Err()
}

func (s Store) ListGroups(ctx context.Context) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id, g.name, g.is_team,
			(SELECT count(*) FROM group_members gm WHERE gm.group_id = g.id),
			(SELECT rb.role FROM role_bindings rb
				WHERE rb.subject_type = 'group' AND rb.subject_id = g.id
				ORDER BY CASE rb.role WHEN 'admin' THEN 3 WHEN 'builder' THEN 2 ELSE 1 END DESC
				LIMIT 1)
		FROM groups g
		ORDER BY lower(g.name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := []Group{}
	for rows.Next() {
		var (
			g    Group
			role sql.NullString
		)
		if err := rows.Scan(&g.ID, &g.Name, &g.IsTeam, &g.MemberCount, &role); err != nil {
			return nil, err
		}
		g.Role = nullRole(role)
		groups = append(groups, g)
	}

	return groups, rows.Err()
}

// SetRole replaces the role bound directly to a user or group; nil removes it.
func (s Store) SetRole(ctx context.Context, subjectType, subjectID string, role *auth.Role) error {
	if err := s.exists(ctx, subjectType, subjectID); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM role_bindings WHERE subject_type = $1 AND subject_id = $2`, subjectType, subjectID); err != nil {
		return err
	}
	if role == nil {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO role_bindings (subject_type, subject_id, role) VALUES ($1, $2, $3)`,
		subjectType, subjectID, string(*role))

	return err
}

func (s Store) SetTeam(ctx context.Context, groupID string, isTeam bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE groups SET is_team = $2 WHERE id = $1`, groupID, isTeam)
	if err != nil {
		return err
	}

	return requireRow(result)
}

func (s Store) exists(ctx context.Context, subjectType, subjectID string) error {
	table := map[string]string{"user": "users", "group": "groups"}[subjectType]

	var exists bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM `+table+` WHERE id = $1)`, subjectID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errNotFound
	}

	return nil
}

func requireRow(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotFound
	}

	return nil
}

func nullRole(value sql.NullString) *auth.Role {
	if !value.Valid {
		return nil
	}
	role := auth.Role(value.String)

	return &role
}
