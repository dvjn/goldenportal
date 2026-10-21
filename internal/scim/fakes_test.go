package scim

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/dvjn/goldport/internal/directory"
)

// memoryDirectory follows the directory store's rules: unique user names and
// group names, and membership changes that ignore unknown users.
type memoryDirectory struct {
	mutex   sync.Mutex
	users   []directory.User
	groups  []directory.Group
	members map[string][]string
	nextID  int
}

func newMemoryDirectory() *memoryDirectory {
	return &memoryDirectory{members: map[string][]string{}}
}

func (d *memoryDirectory) id(prefix string) string {
	d.nextID++
	return prefix + "-" + strconv.Itoa(d.nextID)
}

func (d *memoryDirectory) userIndex(id string) int {
	return slices.IndexFunc(d.users, func(u directory.User) bool { return u.ID == id })
}

func (d *memoryDirectory) groupIndex(id string) int {
	return slices.IndexFunc(d.groups, func(g directory.Group) bool { return g.ID == id })
}

func (d *memoryDirectory) UserByID(_ context.Context, id string) (directory.User, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if i := d.userIndex(id); i >= 0 {
		return d.users[i], nil
	}

	return directory.User{}, directory.ErrNotFound
}

func (d *memoryDirectory) ListUsers(_ context.Context, filter directory.UserFilter, page directory.Page) ([]directory.User, int, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	var matched []directory.User
	for _, user := range d.users {
		if (filter.UserName == "" || user.UserName == filter.UserName) && (filter.ExternalID == "" || user.ExternalID == filter.ExternalID) {
			matched = append(matched, user)
		}
	}

	return window(matched, page), len(matched), nil
}

func (d *memoryDirectory) CreateUser(_ context.Context, user directory.User) (directory.User, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if slices.ContainsFunc(d.users, func(u directory.User) bool { return u.UserName == user.UserName }) {
		return directory.User{}, directory.ErrConflict
	}
	user.ID = d.id("u")
	d.users = append(d.users, user)

	return user, nil
}

func (d *memoryDirectory) ReplaceUser(_ context.Context, id string, user directory.User) (directory.User, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	i := d.userIndex(id)
	if i < 0 {
		return directory.User{}, directory.ErrNotFound
	}
	user.ID = id
	d.users[i] = user

	return user, nil
}

func (d *memoryDirectory) DeleteUser(_ context.Context, id string) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	i := d.userIndex(id)
	if i < 0 {
		return directory.ErrNotFound
	}
	d.users = slices.Delete(d.users, i, i+1)
	for group, members := range d.members {
		d.members[group] = slices.DeleteFunc(members, func(member string) bool { return member == id })
	}

	return nil
}

func (d *memoryDirectory) GroupByID(_ context.Context, id string) (directory.Group, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if i := d.groupIndex(id); i >= 0 {
		return d.groups[i], nil
	}

	return directory.Group{}, directory.ErrNotFound
}

func (d *memoryDirectory) ListGroups(_ context.Context, filter directory.GroupFilter, page directory.Page) ([]directory.Group, int, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	var matched []directory.Group
	for _, group := range d.groups {
		if (filter.DisplayName == "" || group.DisplayName == filter.DisplayName) && (filter.ExternalID == "" || group.ExternalID == filter.ExternalID) {
			matched = append(matched, group)
		}
	}

	return window(matched, page), len(matched), nil
}

func (d *memoryDirectory) CreateGroup(_ context.Context, group directory.Group) (directory.Group, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if slices.ContainsFunc(d.groups, func(g directory.Group) bool { return g.DisplayName == group.DisplayName }) {
		return directory.Group{}, directory.ErrConflict
	}
	group.ID = d.id("g")
	d.groups = append(d.groups, group)

	return group, nil
}

func (d *memoryDirectory) ReplaceGroup(_ context.Context, id string, group directory.Group) (directory.Group, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	i := d.groupIndex(id)
	if i < 0 {
		return directory.Group{}, directory.ErrNotFound
	}
	group.ID = id
	d.groups[i] = group

	return group, nil
}

func (d *memoryDirectory) DeleteGroup(_ context.Context, id string) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	i := d.groupIndex(id)
	if i < 0 {
		return directory.ErrNotFound
	}
	d.groups = slices.Delete(d.groups, i, i+1)
	delete(d.members, id)

	return nil
}

func (d *memoryDirectory) Members(_ context.Context, groupID string) ([]directory.Member, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	var members []directory.Member
	for _, id := range d.members[groupID] {
		members = append(members, directory.Member{UserID: id, UserName: d.users[d.userIndex(id)].UserName})
	}
	slices.SortFunc(members, func(a, b directory.Member) int { return strings.Compare(a.UserName, b.UserName) })

	return members, nil
}

func (d *memoryDirectory) SetMembers(ctx context.Context, groupID string, userIDs []string) error {
	d.mutex.Lock()
	d.members[groupID] = nil
	d.mutex.Unlock()

	return d.AddMembers(ctx, groupID, userIDs)
}

func (d *memoryDirectory) AddMembers(_ context.Context, groupID string, userIDs []string) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	for _, id := range userIDs {
		if d.userIndex(id) >= 0 && !slices.Contains(d.members[groupID], id) {
			d.members[groupID] = append(d.members[groupID], id)
		}
	}

	return nil
}

func (d *memoryDirectory) RemoveMembers(_ context.Context, groupID string, userIDs []string) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.members[groupID] = slices.DeleteFunc(d.members[groupID], func(id string) bool { return slices.Contains(userIDs, id) })

	return nil
}

func window[T any](items []T, page directory.Page) []T {
	start := min(page.Offset, len(items))
	end := min(start+page.Limit, len(items))

	return items[start:end]
}

type recordingSessions struct {
	revoked []string
}

func (r *recordingSessions) DeleteForUser(_ context.Context, userID string) error {
	r.revoked = append(r.revoked, userID)
	return nil
}

type staticTokens string

func (s staticTokens) Verify(_ context.Context, kind, token string) (bool, error) {
	return kind == "scim" && token == string(s), nil
}
