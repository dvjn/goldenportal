package admin

import (
	"cmp"
	"errors"
	"net/http"
	"time"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/auth"
	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

var errUserNotFound = httpx.Error{Code: http.StatusNotFound, Message: "the user does not exist"}

type checkedUser struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Active bool   `json:"active"`
}

type carrier struct {
	Type access.SubjectType `json:"type"`
	ID   string             `json:"id"`
	Name string             `json:"name"`
	Role access.Role        `json:"role"`
}

type namedGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// checkResponse explains a user's effective role. Role is empty for a user
// who is not active.
type checkResponse struct {
	User         checkedUser  `json:"user"`
	Role         access.Role  `json:"role"`
	Bootstrap    bool         `json:"bootstrap"`
	CarriedBy    []carrier    `json:"carried_by"`
	Groups       []namedGroup `json:"groups"`
	LastSignInAt time.Time    `json:"last_sign_in_at,omitzero"`
}

func (s *Service) checkUser(ctx *gofr.Context) (any, error) {
	user, err := s.directory.UserByID(ctx, ctx.PathParam("id"))
	if errors.Is(err, directory.ErrNotFound) {
		return nil, errUserNotFound
	}
	if err != nil {
		return nil, err
	}
	grants, err := s.grants.ForUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	groups, err := s.directory.GroupsOfUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	lastSignIn, err := s.directory.LastSignIn(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	resolution := access.Resolve(user, grants, s.settings.BootstrapAdmins)
	response := checkResponse{
		User:         checkedUser{ID: user.ID, Name: cmp.Or(user.Name, user.UserName), Email: user.Email, Active: user.Active},
		Role:         resolution.Role,
		Bootstrap:    resolution.Bootstrap,
		CarriedBy:    make([]carrier, 0, len(resolution.CarriedBy)),
		Groups:       make([]namedGroup, 0, len(groups)),
		LastSignInAt: lastSignIn,
	}
	for _, grant := range resolution.CarriedBy {
		response.CarriedBy = append(response.CarriedBy, carrier{
			Type: grant.Subject.Type, ID: grant.Subject.ID, Name: grant.SubjectName, Role: grant.Role,
		})
	}
	for _, group := range groups {
		response.Groups = append(response.Groups, namedGroup{ID: group.ID, Name: group.DisplayName})
	}

	return response, nil
}

func (s *Service) revokeSessions(ctx *gofr.Context) (any, error) {
	user, err := s.directory.UserByID(ctx, ctx.PathParam("id"))
	if errors.Is(err, directory.ErrNotFound) {
		return nil, errUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.sessions.DeleteForUser(ctx, user.ID); err != nil {
		return nil, err
	}
	principal, _ := auth.PrincipalFrom(ctx)
	if err := s.audit.Record(ctx, audit.Entry{
		Actor:    principal.Actor(),
		Action:   audit.SessionsRevoked,
		Resource: audit.Resource{Kind: audit.ResourceUser, ID: user.ID, Name: cmp.Or(user.Email, user.UserName)},
	}); err != nil {
		return nil, err
	}

	return nil, nil
}
