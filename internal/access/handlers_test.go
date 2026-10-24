package access

import (
	"testing"

	"github.com/dvjn/goldenportal/internal/auth"
)

func TestParseRole(t *testing.T) {
	builder := "builder"
	got, err := parseRole(&builder)
	if err != nil || got == nil || *got != auth.RoleBuilder {
		t.Fatalf("parseRole(builder) = %v, %v; want builder", got, err)
	}

	got, err = parseRole(nil)
	if err != nil || got != nil {
		t.Fatalf("parseRole(nil) = %v, %v; want no role", got, err)
	}

	owner := "owner"
	if _, err := parseRole(&owner); err == nil {
		t.Fatal("parseRole(owner) accepted a role that doesn't exist")
	}
}
