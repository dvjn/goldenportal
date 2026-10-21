package auth

import (
	"context"

	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

type userDirectory interface {
	GroupsOfUser(ctx context.Context, userID string) ([]directory.Group, error)
}

type Service struct {
	config    Config
	directory userDirectory
}

func NewService(config Config, directory userDirectory) *Service {
	return &Service{config: config, directory: directory}
}

func (s *Service) Register(routes *httpx.Routes) {
	routes.GET("/api/v1/auth/me", httpx.Authenticated(), s.me)
}

type meResponse struct {
	User   meUser    `json:"user"`
	Groups []meGroup `json:"groups"`
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
	groups, err := s.directory.GroupsOfUser(ctx, principal.User.ID)
	if err != nil {
		return nil, err
	}

	response := meResponse{
		User:   meUser{ID: principal.User.ID, Email: principal.User.Email, Name: principal.User.Name},
		Groups: make([]meGroup, 0, len(groups)),
	}
	for _, group := range groups {
		response.Groups = append(response.Groups, meGroup{ID: group.ID, Name: group.DisplayName})
	}

	return response, nil
}
