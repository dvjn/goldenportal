// Package auth signs people in through OIDC and resolves who is making each request.
package auth

import "context"

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
