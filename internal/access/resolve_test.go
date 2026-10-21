package access

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/dvjn/goldport/internal/directory"
)

var bob = directory.User{ID: "u-2", Email: "bob@example.test", Active: true}

func grant(id string, subjectType SubjectType, role Role) Grant {
	return Grant{ID: id, Subject: Subject{Type: subjectType, ID: id}, Role: role}
}

func TestResolve(t *testing.T) {
	direct := grant("direct", SubjectUser, Maintainer)
	payments := grant("payments", SubjectGroup, Maintainer)
	platform := grant("platform", SubjectGroup, Admin)
	inactive := bob
	inactive.Active = false

	cases := []struct {
		name      string
		user      directory.User
		grants    []Grant
		bootstrap BootstrapAdmins
		want      Resolution
	}{
		{"no grant", bob, nil, nil, Resolution{Role: Member}},
		{"direct grant", bob, []Grant{direct}, nil, Resolution{Role: Maintainer, CarriedBy: []Grant{direct}}},
		{"group grant", bob, []Grant{payments}, nil, Resolution{Role: Maintainer, CarriedBy: []Grant{payments}}},
		{
			"highest wins", bob,
			[]Grant{direct, platform, payments},
			nil,
			Resolution{Role: Admin, CarriedBy: []Grant{platform}},
		},
		{
			"equal grants all carry the role", bob,
			[]Grant{direct, payments},
			nil,
			Resolution{Role: Maintainer, CarriedBy: []Grant{direct, payments}},
		},
		{"inactive user", inactive, []Grant{platform}, nil, Resolution{}},
		{"inactive bootstrap admin", inactive, nil, BootstrapAdmins{"bob@example.test"}, Resolution{}},
		{"bootstrap admin", bob, []Grant{direct}, BootstrapAdmins{"bob@example.test"}, Resolution{Role: Admin, Bootstrap: true}},
		{
			"bootstrap admin with an admin grant", bob,
			[]Grant{platform},
			BootstrapAdmins{"bob@example.test"},
			Resolution{Role: Admin, CarriedBy: []Grant{platform}},
		},
		{"someone else is the bootstrap admin", bob, nil, BootstrapAdmins{"alice@example.test"}, Resolution{Role: Member}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Resolve(tc.user, tc.grants, tc.bootstrap); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Resolve() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseBootstrapAdmins(t *testing.T) {
	admins := ParseBootstrapAdmins(" Alice@Example.test, ,bob@example.test,")

	if !reflect.DeepEqual(admins, BootstrapAdmins{"alice@example.test", "bob@example.test"}) {
		t.Fatalf("ParseBootstrapAdmins() = %v", admins)
	}
	if !admins.Contains("ALICE@example.TEST") || admins.Contains("cara@example.test") || admins.Contains("") {
		t.Fatal("Contains() does not match emails case-insensitively")
	}
	if len(ParseBootstrapAdmins("")) != 0 {
		t.Fatal("an empty setting lists bootstrap admins")
	}
}

type stubGrants struct {
	grants []Grant
	err    error
}

func (s stubGrants) ForUser(context.Context, string) ([]Grant, error) { return s.grants, s.err }

func TestResolverReadsTheUsersGrants(t *testing.T) {
	platform := grant("platform", SubjectGroup, Admin)

	resolution, err := NewResolver(stubGrants{grants: []Grant{platform}}, nil).Resolve(context.Background(), bob)
	if err != nil || resolution.Role != Admin {
		t.Fatalf("Resolve() = %+v, %v, want Admin", resolution, err)
	}

	failure := errors.New("database down")
	if _, err := NewResolver(stubGrants{err: failure}, nil).Resolve(context.Background(), bob); !errors.Is(err, failure) {
		t.Fatalf("Resolve() error = %v, want the store failure", err)
	}
}
