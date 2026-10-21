// Package access decides what a principal may do: the system roles, the
// permissions each role holds, and the grants that give users a role.
package access

import (
	"slices"

	"github.com/dvjn/goldport/internal/httpx"
)

type Permission = httpx.Permission

const (
	SettingsRead       Permission = "settings.read"
	AccessRead         Permission = "access.read"
	AccessWrite        Permission = "access.write"
	AuditRead          Permission = "audit.read"
	CatalogRead        Permission = "catalog.read"
	CatalogWrite       Permission = "catalog.write"
	CatalogModel       Permission = "catalog.model"
	IntegrationsSync   Permission = "integrations.sync"
	IntegrationsTokens Permission = "integrations.tokens"
)

// Permissions is the registry of every permission a route or a page checks.
var Permissions = []Permission{
	SettingsRead,
	AccessRead,
	AccessWrite,
	AuditRead,
	CatalogRead,
	CatalogWrite,
	CatalogModel,
	IntegrationsSync,
	IntegrationsTokens,
}

// Role is a system role. NoRole belongs to users who cannot sign in.
type Role string

const (
	NoRole     Role = ""
	Member     Role = "member"
	Maintainer Role = "maintainer"
	Admin      Role = "admin"
)

var ranked = []Role{NoRole, Member, Maintainer, Admin}

var rolePermissions = map[Role][]Permission{
	Member:     {CatalogRead},
	Maintainer: {CatalogRead, CatalogWrite, CatalogModel, IntegrationsSync, IntegrationsTokens},
	Admin:      Permissions,
}

// GrantRoles are the roles a grant can give. Member needs no grant.
var GrantRoles = []Role{Maintainer, Admin}

// TokenRoles are the roles an API token can hold. No machine is an Admin.
var TokenRoles = []Role{Member, Maintainer}

func (r Role) Permissions() []Permission {
	return append([]Permission{}, rolePermissions[r]...)
}

func (r Role) Can(permission Permission) bool {
	return slices.Contains(rolePermissions[r], permission)
}

func (r Role) Outranks(other Role) bool {
	return slices.Index(ranked, r) > slices.Index(ranked, other)
}
