package access

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/httpx"
)

func Register(app *gofr.App) {
	app.GET("/api/v1/users", listUsers)
	app.PUT("/api/v1/users/{id}/role", setUserRole)
	app.GET("/api/v1/groups", listGroups)
	app.PUT("/api/v1/groups/{id}", updateGroup)
}

func listUsers(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	return NewStore(ctx.SQL).ListUsers(ctx)
}

func listGroups(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	return NewStore(ctx.SQL).ListGroups(ctx)
}

type roleRequest struct {
	// Role is "admin", "builder" or "member", or null to remove the binding.
	Role *string `json:"role"`
}

func setUserRole(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleAdmin)
	if err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "user")
	if err != nil {
		return nil, err
	}
	// Admins can't demote themselves, so the portal never loses its last admin by accident.
	if id == p.ID {
		return nil, httpx.Error{Code: http.StatusConflict, Message: "you can't change your own role; ask another admin"}
	}

	var req roleRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, badRequest("the request body is not valid JSON")
	}
	role, err := parseRole(req.Role)
	if err != nil {
		return nil, err
	}

	return nil, notFoundAs(NewStore(ctx.SQL).SetRole(ctx, "user", id, role), "user")
}

type groupRequest struct {
	Role   *string `json:"role"`
	IsTeam bool    `json:"isTeam"`
}

func updateGroup(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "group")
	if err != nil {
		return nil, err
	}

	var req groupRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, badRequest("the request body is not valid JSON")
	}
	role, err := parseRole(req.Role)
	if err != nil {
		return nil, err
	}

	store := NewStore(ctx.SQL)
	if err := notFoundAs(store.SetRole(ctx, "group", id, role), "group"); err != nil {
		return nil, err
	}

	return nil, store.SetTeam(ctx, id, req.IsTeam)
}

func parseRole(value *string) (*auth.Role, error) {
	if value == nil {
		return nil, nil
	}
	role, ok := auth.ParseRole(*value)
	if !ok {
		return nil, badRequest(`choose "admin", "builder", "member" or null for the role`)
	}

	return &role, nil
}

func pathID(ctx *gofr.Context, kind string) (string, error) {
	id := ctx.PathParam("id")
	if _, err := uuid.Parse(id); err != nil {
		return "", notFound(kind)
	}

	return id, nil
}

func notFoundAs(err error, kind string) error {
	if errors.Is(err, errNotFound) {
		return notFound(kind)
	}

	return err
}

func notFound(kind string) error {
	return httpx.Error{Code: http.StatusNotFound, Message: "no such " + kind}
}

func badRequest(message string) error {
	return httpx.Error{Code: http.StatusBadRequest, Message: message}
}
