// Package auth signs people in through OIDC and resolves who is making each request.
package auth

import (
	"context"

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
	// Groups are the names of the identity provider's groups they're in,
	// sorted, so widgets can compare with them as $me.groups.
	Groups []string `json:"groups"`
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
		return Principal{}, httpx.Unauthorized("sign in to continue")
	}
	if !p.Can(role) {
		return Principal{}, httpx.Forbidden("this needs the " + string(role) + " role")
	}

	return p, nil
}

type authKey struct{}

// authInfo is who the request was signed in as and, from a browser, their
// session.
type authInfo struct {
	principal   Principal
	sessionHash []byte
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

// WithPrincipal signs in a call the server makes on p's behalf, such as an
// MCP tool's, which the client authorized with its own token rather than a
// session.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return withAuth(ctx, authInfo{principal: p})
}

// InSession reports whether the request was signed in with a browser
// session, rather than a token: consenting on someone's behalf, for one,
// is for them to do in person.
func InSession(ctx context.Context) bool {
	info, _ := authFromContext(ctx)
	return info.sessionHash != nil
}
