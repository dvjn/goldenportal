package scim

import (
	"cmp"
	"context"
	"slices"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/directory"
)

type auditLog interface {
	Record(ctx context.Context, entry audit.Entry) error
}

// groupState is a group with the user names of its members.
type groupState struct {
	group   directory.Group
	members []string
}

func (s *Server) groupState(ctx context.Context, group directory.Group) (groupState, error) {
	members, err := s.directory.Members(ctx, group.ID)
	names := make([]string, 0, len(members))
	for _, member := range members {
		names = append(names, member.UserName)
	}
	slices.Sort(names)

	return groupState{group: group, members: names}, err
}

// recordUser records a change from one state of a user to another, and
// nothing when the identity provider sent the user as it was. A provisioned
// user changes from the zero user and a deprovisioned one to it.
func (s *Server) recordUser(ctx context.Context, action audit.Action, previous, current directory.User) error {
	changes := audit.Changes{}
	addChange(changes, "user_name", previous.UserName, current.UserName)
	addChange(changes, "email", previous.Email, current.Email)
	addChange(changes, "name", previous.Name, current.Name)
	addChange(changes, "active", previous.Active, current.Active)
	if len(changes) == 0 {
		return nil
	}
	user := cmp.Or(current, previous)

	return s.audit.Record(ctx, audit.Entry{
		Actor:    audit.SCIMProvisioner,
		Action:   action,
		Resource: audit.Resource{Kind: audit.ResourceUser, ID: user.ID, Name: cmp.Or(user.Email, user.UserName)},
		Details:  map[string]any{audit.ChangesKey: changes},
	})
}

// recordGroup records a change from one state of a group to another, the
// same way recordUser does for a user.
func (s *Server) recordGroup(ctx context.Context, action audit.Action, previous, current groupState) error {
	changes := audit.Changes{}
	addChange(changes, "display_name", previous.group.DisplayName, current.group.DisplayName)
	if !slices.Equal(previous.members, current.members) {
		changes["members"] = audit.Change{Before: previous.members, After: current.members}
	}
	if len(changes) == 0 {
		return nil
	}
	group := cmp.Or(current.group, previous.group)

	return s.audit.Record(ctx, audit.Entry{
		Actor:    audit.SCIMProvisioner,
		Action:   action,
		Resource: audit.Resource{Kind: audit.ResourceGroup, ID: group.ID, Name: group.DisplayName},
		Details:  map[string]any{audit.ChangesKey: changes},
	})
}

func addChange[T comparable](changes audit.Changes, field string, before, after T) {
	if before != after {
		changes[field] = audit.Change{Before: before, After: after}
	}
}
