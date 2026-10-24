// Package auth signs people in through OIDC and resolves who is making each request.
package auth

import (
	"context"
	"net/http"

	"github.com/dvjn/goldenportal/internal/audit"
	"github.com/dvjn/goldenportal/internal/httpx"
)

type Role string

const (
	RoleMember  Role = "member"
	RoleBuilder Role = "builder"
	RoleAdmin   Role = "admin"
)

var roleRank = map[Role]int{RoleMember: 1, RoleBuilder: 2, RoleAdmin: 3}

// Principal is the signed-in user behind a request.
type Principal struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Role        Role   `json:"role"`
}

// Can reports whether the principal's role includes role. Roles nest:
// an admin can do everything a builder can, and a builder everything a member can.
func (p Principal) Can(role Role) bool {
	return roleRank[p.Role] >= roleRank[role]
}

// Actor describes the principal in the audit log.
func (p Principal) Actor() audit.Actor {
	return audit.Actor{ID: p.ID, Label: p.Email}
}

// ParseRole accepts the roles that can be bound to a user or group.
func ParseRole(value string) (Role, bool) {
	role := Role(value)
	_, ok := roleRank[role]

	return role, ok
}

// Require returns the signed-in principal if their role includes role, and
// otherwise an error a handler can return as is.
func Require(ctx context.Context, role Role) (Principal, error) {
	p, ok := FromContext(ctx)
	if !ok {
		return Principal{}, httpx.Error{Code: http.StatusUnauthorized, Message: "sign in to continue"}
	}
	if !p.Can(role) {
		return Principal{}, httpx.Error{Code: http.StatusForbidden, Message: "this needs the " + string(role) + " role"}
	}

	return p, nil
}

type authKey struct{}

// authInfo is how the request was signed in: by a session cookie or an API token.
type authInfo struct {
	principal   Principal
	sessionHash []byte
	viaToken    bool
}

func withAuth(ctx context.Context, info authInfo) context.Context {
	return context.WithValue(ctx, authKey{}, info)
}

func authFromContext(ctx context.Context) (authInfo, bool) {
	info, ok := ctx.Value(authKey{}).(authInfo)
	return info, ok
}

// FromContext returns the principal the auth middleware attached to ctx.
func FromContext(ctx context.Context) (Principal, bool) {
	info, ok := authFromContext(ctx)
	return info.principal, ok
}
