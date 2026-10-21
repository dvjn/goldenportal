package access

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gofr.dev/pkg/gofr/container"
)

var (
	ErrNotFound        = errors.New("not found")
	ErrSubjectNotFound = errors.New("the user or group does not exist")
	ErrLastAdmin       = errors.New("this is the last Admin grant")
)

type SubjectType string

const (
	SubjectUser  SubjectType = "user"
	SubjectGroup SubjectType = "group"
)

type Subject struct {
	Type SubjectType
	ID   string
}

// Grant gives a user, or every member of a group, a role above Member.
type Grant struct {
	ID             string
	Subject        Subject
	SubjectName    string
	SubjectEmail   string
	MemberCount    int
	Role           Role
	GrantedBy      string
	GrantedByEmail string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Grants struct {
	db container.DB
}

func NewGrants(db container.DB) *Grants {
	return &Grants{db: db}
}

const grantSelect = `SELECT g.id, g.subject_type, g.subject_id,
		COALESCE(NULLIF(u.name, ''), u.user_name, gr.display_name) AS subject_name, COALESCE(u.email, ''),
		(SELECT COUNT(*) FROM group_members m WHERE m.group_id = g.group_id),
		g.role, COALESCE(g.granted_by::text, ''), COALESCE(b.email, ''), g.created_at, g.updated_at
	FROM role_grants g
	LEFT JOIN users u ON u.id = g.user_id
	LEFT JOIN groups gr ON gr.id = g.group_id
	LEFT JOIN users b ON b.id = g.granted_by`

// adminCarryingGrants are the Admin grants that give Admin to at least one
// active user that SCIM provisioned.
const adminCarryingGrants = `SELECT g.id FROM role_grants g
	WHERE g.role = 'admin' AND EXISTS (
		SELECT 1 FROM users u
		WHERE u.active AND u.external_id IS NOT NULL
			AND (u.id = g.user_id OR u.id IN (SELECT m.user_id FROM group_members m WHERE m.group_id = g.group_id))
	)`

const (
	queryListGrants   = grantSelect + ` ORDER BY g.role = 'admin' DESC, g.subject_type, subject_name`
	queryGrantsOfUser = grantSelect + ` WHERE g.user_id = $1
		OR g.group_id IN (SELECT m.group_id FROM group_members m WHERE m.user_id = $1)
		ORDER BY g.subject_type DESC, subject_name`
	queryAdminCarryingGrants = adminCarryingGrants
	queryLockGrants          = `LOCK TABLE role_grants IN SHARE ROW EXCLUSIVE MODE`
	querySubjectRole         = `SELECT id, role FROM role_grants WHERE subject_type = $1 AND subject_id = $2`
	queryIsLastAdminGrant    = `WITH carrying AS (` + adminCarryingGrants + `)
		SELECT EXISTS (SELECT 1 FROM carrying WHERE id = $1) AND NOT EXISTS (SELECT 1 FROM carrying WHERE id <> $1)`
	queryUpsertGrant = `INSERT INTO role_grants (user_id, group_id, role, granted_by)
		VALUES (CASE WHEN $1 = 'user' THEN $2::uuid END, CASE WHEN $1 = 'group' THEN $2::uuid END, $3, NULLIF($4, '')::uuid)
		ON CONFLICT (subject_type, subject_id)
		DO UPDATE SET role = EXCLUDED.role, granted_by = EXCLUDED.granted_by, updated_at = NOW()`
	queryDeleteGrant = `DELETE FROM role_grants WHERE id = $1`
)

func (s *Grants) List(ctx context.Context) ([]Grant, error) {
	return s.list(ctx, queryListGrants)
}

// ForUser returns the user's direct grant and the grants of the user's groups.
func (s *Grants) ForUser(ctx context.Context, userID string) ([]Grant, error) {
	return s.list(ctx, queryGrantsOfUser, userID)
}

// AdminCarrying returns the IDs of the Admin grants that give Admin to an
// active provisioned user. When it returns one ID, that grant is the last one.
func (s *Grants) AdminCarrying(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, queryAdminCarryingGrants)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// Set gives the subject the role, or removes its grant when the role is
// NoRole, and returns the role it held before. It refuses to take Admin away
// from the last grant that gives Admin to an active provisioned user. The
// table lock makes two concurrent removals see each other.
func (s *Grants) Set(ctx context.Context, subject Subject, role Role, grantedBy string) (Role, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return NoRole, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, queryLockGrants); err != nil {
		return NoRole, err
	}
	var (
		id       string
		previous Role
	)
	err = tx.QueryRowContext(ctx, querySubjectRole, subject.Type, subject.ID).Scan(&id, &previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return NoRole, classify(err)
	}
	if previous == NoRole && role == NoRole {
		return NoRole, ErrNotFound
	}
	if previous == Admin && role != Admin {
		var last bool
		if err := tx.QueryRowContext(ctx, queryIsLastAdminGrant, id).Scan(&last); err != nil {
			return NoRole, err
		}
		if last {
			return NoRole, ErrLastAdmin
		}
	}

	if role == NoRole {
		_, err = tx.ExecContext(ctx, queryDeleteGrant, id)
	} else {
		_, err = tx.ExecContext(ctx, queryUpsertGrant, subject.Type, subject.ID, role, grantedBy)
	}
	if err != nil {
		return NoRole, classify(err)
	}

	return previous, tx.Commit()
}

func (s *Grants) list(ctx context.Context, query string, args ...any) ([]Grant, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()

	grants := []Grant{}
	for rows.Next() {
		var grant Grant
		if err := rows.Scan(&grant.ID, &grant.Subject.Type, &grant.Subject.ID, &grant.SubjectName, &grant.SubjectEmail,
			&grant.MemberCount, &grant.Role, &grant.GrantedBy, &grant.GrantedByEmail,
			&grant.CreatedAt, &grant.UpdatedAt); err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}

	return grants, rows.Err()
}

// https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	foreignKeyViolation       = "23503"
	invalidTextRepresentation = "22P02"
)

func classify(err error) error {
	var state interface{ SQLState() string }
	if errors.As(err, &state) && (state.SQLState() == foreignKeyViolation || state.SQLState() == invalidTextRepresentation) {
		return ErrSubjectNotFound
	}

	return err
}
