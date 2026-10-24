package access

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/audit"
	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/httpx"
)

func Register(app *gofr.App) {
	app.GET("/api/v1/users", listUsers)
	app.PUT("/api/v1/users/{id}/role", setUserRole)
	app.GET("/api/v1/groups", listGroups)
	app.PUT("/api/v1/groups/{id}", updateGroup)
	app.GET("/api/v1/audit", listAudit)
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

	label, err := NewStore(ctx.SQL).SetRole(ctx, "user", id, role)
	if err != nil {
		return nil, notFoundAs(err, "user")
	}

	auth.Record(ctx, p, audit.UserRole, audit.Target{Type: "user", ID: id, Label: label},
		map[string]any{"role": role})

	return nil, nil
}

type groupRequest struct {
	Role   *string `json:"role"`
	IsTeam bool    `json:"isTeam"`
}

func updateGroup(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleAdmin)
	if err != nil {
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
	label, err := store.SetRole(ctx, "group", id, role)
	if err != nil {
		return nil, notFoundAs(err, "group")
	}
	if err := store.SetTeam(ctx, id, req.IsTeam); err != nil {
		return nil, err
	}

	auth.Record(ctx, p, audit.GroupUpdate, audit.Target{Type: "group", ID: id, Label: label},
		map[string]any{"role": role, "isTeam": req.IsTeam})

	return nil, nil
}

var auditPageSizes = []int{10, 20, 50, 100}

// listAudit returns a page of audit events, newest first. It filters by free
// text (q), actions (a comma-separated action list) and a from/to date range,
// and pages with page and size.
func listAudit(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	q := audit.Query{Search: ctx.Param("q"), Page: 1, PageSize: auditPageSizes[0]}

	if value := ctx.Param("page"); value != "" {
		page, err := strconv.Atoi(value)
		if err != nil || page < 1 {
			return nil, badRequest("page must be a whole number from 1")
		}
		q.Page = page
	}
	if value := ctx.Param("size"); value != "" {
		size, err := strconv.Atoi(value)
		if err != nil || !slices.Contains(auditPageSizes, size) {
			return nil, badRequest("size must be 10, 20, 50 or 100")
		}
		q.PageSize = size
	}
	if value := ctx.Param("action"); value != "" {
		for action := range strings.SplitSeq(value, ",") {
			if !slices.Contains(audit.Actions, action) {
				return nil, badRequest("unknown action " + strconv.Quote(action))
			}
			q.Actions = append(q.Actions, action)
		}
	}

	var err error
	if q.From, err = dateParam(ctx, "from"); err != nil {
		return nil, err
	}
	if q.To, err = dateParam(ctx, "to"); err != nil {
		return nil, err
	}
	// to names the last day to include.
	if !q.To.IsZero() {
		q.To = q.To.AddDate(0, 0, 1)
	}

	return audit.List(ctx, ctx.SQL, q)
}

// dateParam reads a YYYY-MM-DD date, as the start of that day in UTC.
func dateParam(ctx *gofr.Context, name string) (time.Time, error) {
	value := ctx.Param(name)
	if value == "" {
		return time.Time{}, nil
	}
	date, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return time.Time{}, badRequest(name + " must be a date like 2026-10-24")
	}

	return date, nil
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
