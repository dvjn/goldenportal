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

type principalKey struct{}

type sessionKey struct{}

func withPrincipal(ctx context.Context, p Principal, sessionHash []byte) context.Context {
	ctx = context.WithValue(ctx, principalKey{}, p)
	return context.WithValue(ctx, sessionKey{}, sessionHash)
}

// FromContext returns the principal the auth middleware attached to ctx.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

func sessionFromContext(ctx context.Context) []byte {
	hash, _ := ctx.Value(sessionKey{}).([]byte)
	return hash
}
