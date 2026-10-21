package access

import (
	"slices"
	"testing"
)

func TestEveryRoleHoldsOnlyRegisteredPermissions(t *testing.T) {
	for role, permissions := range rolePermissions {
		for _, permission := range permissions {
			if !slices.Contains(Permissions, permission) {
				t.Errorf("%s holds %q, which is not registered", role, permission)
			}
		}
	}
}

func TestAdminHoldsEveryRegisteredPermission(t *testing.T) {
	for _, permission := range Permissions {
		if !Admin.Can(permission) {
			t.Errorf("Admin cannot %q", permission)
		}
	}
}

func TestEachRoleHoldsEverythingTheRoleBelowHolds(t *testing.T) {
	for i := 1; i < len(ranked)-1; i++ {
		lower, higher := ranked[i], ranked[i+1]
		for _, permission := range lower.Permissions() {
			if !higher.Can(permission) {
				t.Errorf("%s holds %q but %s does not", lower, permission, higher)
			}
		}
	}
}

func TestRolePermissions(t *testing.T) {
	cases := []struct {
		role       Role
		permission Permission
		want       bool
	}{
		{NoRole, CatalogRead, false},
		{Member, CatalogRead, true},
		{Member, CatalogWrite, false},
		{Maintainer, CatalogWrite, true},
		{Maintainer, IntegrationsTokens, true},
		{Maintainer, AccessWrite, false},
		{Maintainer, SettingsRead, false},
		{Admin, AccessWrite, true},
	}
	for _, tc := range cases {
		if got := tc.role.Can(tc.permission); got != tc.want {
			t.Errorf("%q.Can(%q) = %t, want %t", tc.role, tc.permission, got, tc.want)
		}
	}
}

func TestPermissionsReturnsACopy(t *testing.T) {
	permissions := Member.Permissions()
	permissions[0] = AccessWrite

	if Member.Can(AccessWrite) {
		t.Fatal("changing the returned slice changed the role")
	}
}

func TestOutranks(t *testing.T) {
	if !Admin.Outranks(Maintainer) || !Maintainer.Outranks(Member) || !Member.Outranks(NoRole) {
		t.Fatal("the ranking is not Admin > Maintainer > Member > NoRole")
	}
	if Member.Outranks(Member) || Maintainer.Outranks(Admin) {
		t.Fatal("a role outranks itself or a higher role")
	}
}
