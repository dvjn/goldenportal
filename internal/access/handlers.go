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
	app.GET("/api/v1/access", listGrants)
	app.PUT("/api/v1/access/{type}/{id}", grant)
	app.DELETE("/api/v1/access/{type}/{id}", revoke)
	app.GET("/api/v1/users", listUsers)
	app.GET("/api/v1/groups", listGroups)
	app.GET("/api/v1/audit", listAudit)
}

func listGrants(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	return NewStore(ctx.SQL).ListGrants(ctx)
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

type grantRequest struct {
	Role string `json:"role"`
}

func grant(ctx *gofr.Context) (any, error) {
	p, subjectType, id, err := subject(ctx)
	if err != nil {
		return nil, err
	}

	var req grantRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, badRequest("the request body is not valid JSON")
	}
	role, ok := auth.ParseRole(req.Role)
	if !ok {
		return nil, badRequest(`choose "admin", "builder" or "member" for the role`)
	}

	name, err := NewStore(ctx.SQL).Grant(ctx, subjectType, id, role)
	if err != nil {
		return nil, notFoundAs(err, subjectType)
	}

	auth.Record(ctx, p, audit.AccessGrant, audit.Target{Type: subjectType, ID: id, Label: name},
		map[string]any{"role": role})

	return nil, nil
}

func revoke(ctx *gofr.Context) (any, error) {
	p, subjectType, id, err := subject(ctx)
	if err != nil {
		return nil, err
	}

	name, err := NewStore(ctx.SQL).Revoke(ctx, subjectType, id)
	if errors.Is(err, errNoGrant) {
		return nil, httpx.Error{Code: http.StatusNotFound, Message: "this " + subjectType + " has no role granted directly"}
	}
	if err != nil {
		return nil, notFoundAs(err, subjectType)
	}

	auth.Record(ctx, p, audit.AccessRevoke, audit.Target{Type: subjectType, ID: id, Label: name}, nil)

	return nil, nil
}

// subject checks the caller is an admin and reads the user or group from the path.
func subject(ctx *gofr.Context) (auth.Principal, string, string, error) {
	p, err := auth.Require(ctx, auth.RoleAdmin)
	if err != nil {
		return auth.Principal{}, "", "", err
	}

	subjectType := ctx.PathParam("type")
	if subjectType != "user" && subjectType != "group" {
		return auth.Principal{}, "", "", httpx.Error{Code: http.StatusNotFound, Message: `grant access to a "user" or a "group"`}
	}
	id := ctx.PathParam("id")
	if _, err := uuid.Parse(id); err != nil {
		return auth.Principal{}, "", "", notFound(subjectType)
	}
	// Admins can't change their own access, so the portal never loses its last admin by accident.
	if subjectType == "user" && id == p.ID {
		return auth.Principal{}, "", "", httpx.Error{Code: http.StatusConflict, Message: "you can't change your own access; ask another admin"}
	}

	return p, subjectType, id, nil
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
