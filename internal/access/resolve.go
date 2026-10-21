package access

import (
	"context"
	"slices"
	"strings"

	"github.com/dvjn/goldport/internal/directory"
)

// BootstrapAdmins are the lowercased emails in GOLDPORT_BOOTSTRAP_ADMINS. They
// resolve to Admin without a grant, so a new portal has someone to grant the
// first roles.
type BootstrapAdmins []string

func ParseBootstrapAdmins(value string) BootstrapAdmins {
	var emails BootstrapAdmins
	for email := range strings.SplitSeq(value, ",") {
		if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
			emails = append(emails, email)
		}
	}

	return emails
}

func (b BootstrapAdmins) Contains(email string) bool {
	return email != "" && slices.Contains(b, strings.ToLower(email))
}

// Resolution is a user's effective role. CarriedBy holds the grants that give
// the role; Bootstrap is true when only GOLDPORT_BOOTSTRAP_ADMINS gives it.
type Resolution struct {
	Role      Role
	Bootstrap bool
	CarriedBy []Grant
}

// Resolve picks the highest of Member, the user's direct grant and the grants
// of the user's groups. A user who is not active has no role.
func Resolve(user directory.User, grants []Grant, bootstrap BootstrapAdmins) Resolution {
	if !user.Active {
		return Resolution{}
	}

	resolution := Resolution{Role: Member}
	for _, grant := range grants {
		switch {
		case grant.Role.Outranks(resolution.Role):
			resolution.Role, resolution.CarriedBy = grant.Role, []Grant{grant}
		case grant.Role == resolution.Role:
			resolution.CarriedBy = append(resolution.CarriedBy, grant)
		}
	}
	if Admin.Outranks(resolution.Role) && bootstrap.Contains(user.Email) {
		return Resolution{Role: Admin, Bootstrap: true}
	}

	return resolution
}

type grantsOfUser interface {
	ForUser(ctx context.Context, userID string) ([]Grant, error)
}

// Resolver reads the grants on every request. One indexed query is cheap, and
// unlike a cache it stays correct with several replicas and with SCIM changes.
type Resolver struct {
	grants    grantsOfUser
	bootstrap BootstrapAdmins
}

func NewResolver(grants grantsOfUser, bootstrap BootstrapAdmins) *Resolver {
	return &Resolver{grants: grants, bootstrap: bootstrap}
}

func (r *Resolver) Resolve(ctx context.Context, user directory.User) (Resolution, error) {
	grants, err := r.grants.ForUser(ctx, user.ID)
	if err != nil {
		return Resolution{}, err
	}

	return Resolve(user, grants, r.bootstrap), nil
}
