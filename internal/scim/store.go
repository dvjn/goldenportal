package scim

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/lib/pq"
)

var (
	errNotFound = errors.New("not found")
	errConflict = errors.New("already exists")
)

type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type store struct{ db DB }

type user struct {
	ID          string
	UserName    string
	ExternalID  string
	DisplayName string
	Email       string
	Active      bool
	Created     time.Time
	Updated     time.Time
	Groups      []ref
}

type group struct {
	ID         string
	Name       string
	ExternalID string
	Created    time.Time
	Updated    time.Time
	Members    []ref
}

// A user's SCIM userName is the one their identity provider set, or their email
// for people who only ever signed in.
const selectUsers = `
	SELECT u.id, coalesce(u.username, u.email), coalesce(u.external_id, ''), u.display_name, u.email,
		u.status = 'active', u.created_at, u.updated_at, count(*) OVER ()
	FROM users u`

var userColumns = map[string]string{
	"username":    "lower(coalesce(u.username, u.email)) = lower($1)",
	"external_id": "u.external_id = $1",
	"email":       "lower(u.email) = lower($1)",
	"id":          "u.id::text = $1",
}

func (s store) listUsers(ctx context.Context, f *filter, start, count int) ([]user, int, error) {
	where, args := "", []any{}
	if f != nil {
		where = " WHERE " + userColumns[f.attribute]
		args = append(args, f.value)
	}
	args = append(args, count, start-1)
	query := selectUsers + where +
		` ORDER BY u.created_at, u.id LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var (
		users []user
		total int
	)
	for rows.Next() {
		var u user
		if err := rows.Scan(&u.ID, &u.UserName, &u.ExternalID, &u.DisplayName, &u.Email,
			&u.Active, &u.Created, &u.Updated, &total); err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if total == 0 && start > 1 {
		return nil, s.countUsers(ctx, f), nil
	}

	for i := range users {
		if users[i].Groups, err = s.userGroups(ctx, users[i].ID); err != nil {
			return nil, 0, err
		}
	}

	return users, total, nil
}

func (s store) countUsers(ctx context.Context, f *filter) int {
	where, args := "", []any{}
	if f != nil {
		where = " WHERE " + userColumns[f.attribute]
		args = append(args, f.value)
	}
	var total int
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM users u`+where, args...).Scan(&total)

	return total
}

func (s store) getUser(ctx context.Context, id string) (user, error) {
	users, _, err := s.listUsers(ctx, &filter{attribute: "id", value: id}, 1, 1)
	if err != nil {
		return user{}, err
	}
	if len(users) == 0 {
		return user{}, errNotFound
	}

	return users[0], nil
}

func (s store) userGroups(ctx context.Context, userID string) ([]ref, error) {
	return s.refs(ctx, `
		SELECT g.id, g.name FROM group_members gm
		JOIN groups g ON g.id = gm.group_id
		WHERE gm.user_id = $1 ORDER BY lower(g.name)`, userID)
}

// createUser provisions a user. Someone who already signed in with the same
// email is adopted rather than duplicated.
func (s store) createUser(ctx context.Context, in userInput) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `
		UPDATE users SET username = $1, external_id = nullif($2, ''), display_name = $3, email = $4,
			status = $5, provisioned = true, updated_at = now()
		WHERE id = (
			SELECT id FROM users
			WHERE NOT provisioned AND email <> '' AND lower(email) = lower($4)
			ORDER BY created_at LIMIT 1
		)
		RETURNING id`, in.UserName, in.ExternalID, in.DisplayName, in.Email, status(in.Active)).Scan(&id)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		return id, uniqueness(err)
	}

	err = s.db.QueryRowContext(ctx, `
		INSERT INTO users (username, external_id, display_name, email, status, provisioned)
		VALUES ($1, nullif($2, ''), $3, $4, $5, true)
		RETURNING id`, in.UserName, in.ExternalID, in.DisplayName, in.Email, status(in.Active)).Scan(&id)

	return id, uniqueness(err)
}

// replaceUser stores a user's provisioned details and reports whether their
// active state changed, so sessions and tokens can be cut off.
func (s store) replaceUser(ctx context.Context, id string, in userInput) (bool, error) {
	current, err := s.getUser(ctx, id)
	if err != nil {
		return false, err
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE users SET username = $2, external_id = nullif($3, ''), display_name = $4, email = $5,
			status = $6, provisioned = true, updated_at = now()
		WHERE id = $1`, id, in.UserName, in.ExternalID, in.DisplayName, in.Email, status(in.Active))
	if err := uniqueness(err); err != nil {
		return false, err
	}
	if err := requireRow(result); err != nil {
		return false, err
	}

	if current.Active && !in.Active {
		return true, s.cutOff(ctx, id)
	}

	return current.Active != in.Active, nil
}

// deactivateUser is what deleting a user means: they can't sign in, and their
// sessions and API tokens stop working at once. Their history stays.
func (s store) deactivateUser(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE users SET status = 'deactivated', updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if err := requireRow(result); err != nil {
		return err
	}

	return s.cutOff(ctx, id)
}

func (s store) cutOff(ctx context.Context, userID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)

	return err
}

const selectGroups = `
	SELECT g.id, g.name, coalesce(g.external_id, ''), g.created_at, g.updated_at, count(*) OVER ()
	FROM groups g`

var groupColumns = map[string]string{
	"name":        "lower(g.name) = lower($1)",
	"external_id": "g.external_id = $1",
	"id":          "g.id::text = $1",
}

func (s store) listGroups(ctx context.Context, f *filter, start, count int, withMembers bool) ([]group, int, error) {
	where, args := "", []any{}
	if f != nil {
		where = " WHERE " + groupColumns[f.attribute]
		args = append(args, f.value)
	}
	args = append(args, count, start-1)
	query := selectGroups + where +
		` ORDER BY lower(g.name), g.id LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var (
		groups []group
		total  int
	)
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.ID, &g.Name, &g.ExternalID, &g.Created, &g.Updated, &total); err != nil {
			return nil, 0, err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	if withMembers {
		for i := range groups {
			if groups[i].Members, err = s.members(ctx, groups[i].ID); err != nil {
				return nil, 0, err
			}
		}
	}

	return groups, total, nil
}

func (s store) getGroup(ctx context.Context, id string) (group, error) {
	groups, _, err := s.listGroups(ctx, &filter{attribute: "id", value: id}, 1, 1, true)
	if err != nil {
		return group{}, err
	}
	if len(groups) == 0 {
		return group{}, errNotFound
	}

	return groups[0], nil
}

func (s store) members(ctx context.Context, groupID string) ([]ref, error) {
	return s.refs(ctx, `
		SELECT u.id, u.display_name FROM group_members gm
		JOIN users u ON u.id = gm.user_id
		WHERE gm.group_id = $1 ORDER BY lower(u.display_name)`, groupID)
}

// createGroup provisions a group. A group the portal already knows by name,
// from the sign-in groups claim, is adopted rather than duplicated.
func (s store) createGroup(ctx context.Context, name, externalID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `
		UPDATE groups SET external_id = nullif($2, ''), provisioned = true, updated_at = now()
		WHERE NOT provisioned AND lower(name) = lower($1)
		RETURNING id`, name, externalID).Scan(&id)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		return id, err
	}

	err = s.db.QueryRowContext(ctx, `
		INSERT INTO groups (name, external_id, provisioned) VALUES ($1, nullif($2, ''), true)
		RETURNING id`, name, externalID).Scan(&id)

	return id, uniqueness(err)
}

func (s store) updateGroup(ctx context.Context, id, name, externalID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE groups SET name = $2, external_id = nullif($3, ''), provisioned = true, updated_at = now()
		WHERE id = $1`, id, name, externalID)
	if err := uniqueness(err); err != nil {
		return err
	}

	return requireRow(result)
}

// setMembers makes the group's members exactly userIDs. Unknown ids are ignored.
func (s store) setMembers(ctx context.Context, groupID string, userIDs []string) error {
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM group_members WHERE group_id = $1 AND NOT (user_id::text = ANY($2))`,
		groupID, pq.Array(userIDs)); err != nil {
		return err
	}

	return s.addMembers(ctx, groupID, userIDs)
}

func (s store) addMembers(ctx context.Context, groupID string, userIDs []string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO group_members (group_id, user_id)
		SELECT $1, id FROM users WHERE id::text = ANY($2)
		ON CONFLICT DO NOTHING`, groupID, pq.Array(userIDs))

	return err
}

func (s store) removeMembers(ctx context.Context, groupID string, userIDs []string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM group_members WHERE group_id = $1 AND user_id::text = ANY($2)`,
		groupID, pq.Array(userIDs))

	return err
}

func (s store) touchGroup(ctx context.Context, groupID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE groups SET updated_at = now() WHERE id = $1`, groupID)
	return err
}

// deleteGroup removes a group, its memberships and the roles granted to it.
func (s store) deleteGroup(ctx context.Context, id string) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `DELETE FROM groups WHERE id = $1 RETURNING name`, id).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}
	if err != nil {
		return "", err
	}

	_, err = s.db.ExecContext(ctx, `
		DELETE FROM role_bindings WHERE subject_type = 'group' AND subject_id = $1`, id)

	return name, err
}

func (s store) refs(ctx context.Context, query string, args ...any) ([]ref, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	refs := []ref{}
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.Value, &r.Display); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}

	return refs, rows.Err()
}

func status(active bool) string {
	if active {
		return "active"
	}

	return "deactivated"
}

func uniqueness(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return errConflict
	}

	return err
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
