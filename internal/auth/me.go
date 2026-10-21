package auth

import (
	"context"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/directory"
	"gofr.dev/pkg/gofr"
)

type meResponse struct {
	User        meUser              `json:"user"`
	Groups      []meGroup           `json:"groups"`
	Role        access.Role         `json:"role"`
	Permissions []access.Permission `json:"permissions"`
	Bootstrap   bool                `json:"bootstrap"`
}

type meUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type meGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Service) me(ctx *gofr.Context) (any, error) {
	principal, _ := PrincipalFrom(ctx)
	groups, err := s.groupsOf(ctx, principal)
	if err != nil {
		return nil, err
	}

	response := meResponse{
		User:        meUser{ID: principal.User.ID, Email: principal.User.Email, Name: principal.User.Name},
		Groups:      make([]meGroup, 0, len(groups)),
		Role:        principal.Access.Role,
		Permissions: principal.Access.Role.Permissions(),
		Bootstrap:   principal.Access.Bootstrap,
	}
	for _, group := range groups {
		response.Groups = append(response.Groups, meGroup{ID: group.ID, Name: group.DisplayName})
	}

	return response, nil
}

// groupsOf returns no groups for an API token, which is no user.
func (s *Service) groupsOf(ctx context.Context, principal Principal) ([]directory.Group, error) {
	if principal.User.ID == "" {
		return nil, nil
	}

	return s.directory.GroupsOfUser(ctx, principal.User.ID)
}
