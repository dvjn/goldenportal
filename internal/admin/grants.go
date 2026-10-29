package admin

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

var (
	errSubjectNotFound = httpx.Error{Code: http.StatusNotFound, Message: "the user or group does not exist"}
	errGrantNotFound   = httpx.Error{Code: http.StatusNotFound, Message: "this user or group has no grant"}
	errLastAdmin       = httpx.Error{
		Code:    http.StatusConflict,
		Message: "this is the last Admin grant, so it cannot lose Admin: grant Admin to another user or group first",
	}
	errGrantRole   = httpx.Error{Code: http.StatusBadRequest, Message: "role must be maintainer or admin"}
	errSubjectType = httpx.Error{Code: http.StatusBadRequest, Message: "the subject type must be user or group"}
)

type subjectResponse struct {
	Type        access.SubjectType `json:"type"`
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Email       string             `json:"email,omitempty"`
	MemberCount int                `json:"member_count"`
}

type grantResponse struct {
	ID        string          `json:"id"`
	Subject   subjectResponse `json:"subject"`
	Role      access.Role     `json:"role"`
	GrantedBy string          `json:"granted_by"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	LastAdmin bool            `json:"last_admin"`
}

type grantsResponse struct {
	Grants []grantResponse `json:"grants"`
}

func (s *Service) listGrants(ctx *gofr.Context) (any, error) {
	grants, err := s.grants.List(ctx)
	if err != nil {
		return nil, err
	}
	last, err := s.lastAdminGrant(ctx)
	if err != nil {
		return nil, err
	}

	response := grantsResponse{Grants: make([]grantResponse, 0, len(grants))}
	for _, grant := range grants {
		response.Grants = append(response.Grants, grantResponse{
			ID: grant.ID,
			Subject: subjectResponse{
				Type:        grant.Subject.Type,
				ID:          grant.Subject.ID,
				Name:        grant.SubjectName,
				Email:       grant.SubjectEmail,
				MemberCount: grant.MemberCount,
			},
			Role:      grant.Role,
			GrantedBy: grant.GrantedByEmail,
			CreatedAt: grant.CreatedAt,
			UpdatedAt: grant.UpdatedAt,
			LastAdmin: grant.ID == last,
		})
	}

	return response, nil
}

// lastAdminGrant returns the ID of the only grant that gives Admin to an
// active provisioned user, or an empty string when there is not exactly one.
func (s *Service) lastAdminGrant(ctx context.Context) (string, error) {
	carrying, err := s.grants.AdminCarrying(ctx)
	if len(carrying) != 1 {
		return "", err
	}

	return carrying[0], err
}

type setGrantRequest struct {
	Role access.Role `json:"role"`
}

func (s *Service) setGrant(ctx *gofr.Context) (any, error) {
	var request setGrantRequest
	if err := ctx.Bind(&request); err != nil || !slices.Contains(access.GrantRoles, request.Role) {
		return nil, errGrantRole
	}
	subject, name, err := s.subject(ctx)
	if err != nil {
		return nil, err
	}
	principal, _ := auth.PrincipalFrom(ctx)
	previous, err := s.grants.Set(ctx, subject, request.Role, principal.User.ID)
	if err != nil {
		return nil, grantError(err)
	}

	action := audit.GrantChanged
	if previous == access.NoRole {
		action = audit.GrantCreated
	}
	if err := s.audit.Record(ctx, grantEntry(principal.Actor(), action, subject, name, previous, request.Role)); err != nil {
		return nil, err
	}

	return s.listGrants(ctx)
}

func grantEntry(actor audit.Actor, action audit.Action, subject access.Subject, name string, previous, role access.Role) audit.Entry {
	return audit.Entry{
		Actor:    actor,
		Action:   action,
		Resource: audit.Resource{Kind: audit.ResourceKind(subject.Type), ID: subject.ID, Name: name},
		Details:  map[string]any{audit.ChangesKey: audit.Changes{"role": {Before: previous, After: role}}},
	}
}

func (s *Service) removeGrant(ctx *gofr.Context) (any, error) {
	subject, name, err := s.subject(ctx)
	if err != nil {
		return nil, err
	}
	principal, _ := auth.PrincipalFrom(ctx)
	previous, err := s.grants.Set(ctx, subject, access.NoRole, principal.User.ID)
	if err != nil {
		return nil, grantError(err)
	}
	if err := s.audit.Record(ctx, grantEntry(principal.Actor(), audit.GrantRemoved, subject, name, previous, access.NoRole)); err != nil {
		return nil, err
	}

	return s.listGrants(ctx)
}

// subject reads the subject from the path and returns it with its name.
func (s *Service) subject(ctx *gofr.Context) (access.Subject, string, error) {
	subject := access.Subject{Type: access.SubjectType(ctx.PathParam("type")), ID: ctx.PathParam("id")}
	var (
		name string
		err  error
	)
	switch subject.Type {
	case access.SubjectUser:
		var user directory.User
		user, err = s.directory.UserByID(ctx, subject.ID)
		name = cmp.Or(user.Name, user.UserName)
	case access.SubjectGroup:
		var group directory.Group
		group, err = s.directory.GroupByID(ctx, subject.ID)
		name = group.DisplayName
	default:
		return access.Subject{}, "", errSubjectType
	}
	if errors.Is(err, directory.ErrNotFound) {
		return access.Subject{}, "", errSubjectNotFound
	}

	return subject, name, err
}

func grantError(err error) error {
	switch {
	case errors.Is(err, access.ErrLastAdmin):
		return errLastAdmin
	case errors.Is(err, access.ErrNotFound):
		return errGrantNotFound
	case errors.Is(err, access.ErrSubjectNotFound):
		return errSubjectNotFound
	}

	return err
}

type subjectsResponse struct {
	Groups []subjectOption `json:"groups"`
	Users  []subjectOption `json:"users"`
}

// subjectOption is a user or group the grant picker offers, with the role
// its own grant gives, if any.
type subjectOption struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Email  string      `json:"email,omitempty"`
	Active bool        `json:"active"`
	Role   access.Role `json:"role,omitempty"`
}

// subjectsLimit caps the picker; a directory beyond it needs a search API.
const subjectsLimit = 1000

func (s *Service) listSubjects(ctx *gofr.Context) (any, error) {
	grants, err := s.grants.List(ctx)
	if err != nil {
		return nil, err
	}
	roles := map[access.Subject]access.Role{}
	for _, grant := range grants {
		roles[grant.Subject] = grant.Role
	}
	page := directory.Page{Limit: subjectsLimit}
	groups, _, err := s.directory.ListGroups(ctx, directory.GroupFilter{}, page)
	if err != nil {
		return nil, err
	}
	users, _, err := s.directory.ListUsers(ctx, directory.UserFilter{}, page)
	if err != nil {
		return nil, err
	}

	response := subjectsResponse{Groups: make([]subjectOption, 0, len(groups)), Users: make([]subjectOption, 0, len(users))}
	for _, group := range groups {
		response.Groups = append(response.Groups, subjectOption{
			ID:     group.ID,
			Name:   group.DisplayName,
			Active: true,
			Role:   roles[access.Subject{Type: access.SubjectGroup, ID: group.ID}],
		})
	}
	for _, user := range users {
		response.Users = append(response.Users, subjectOption{
			ID:     user.ID,
			Name:   cmp.Or(user.Name, user.UserName),
			Email:  user.Email,
			Active: user.Active,
			Role:   roles[access.Subject{Type: access.SubjectUser, ID: user.ID}],
		})
	}
	sortByName(response.Groups)
	sortByName(response.Users)

	return response, nil
}

func sortByName(options []subjectOption) {
	slices.SortFunc(options, func(a, b subjectOption) int { return cmp.Compare(a.Name, b.Name) })
}
