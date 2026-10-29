package scim

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/dvjn/goldport/internal/audit"
)

func (c *scimClient) lastEntry(wantCount int) audit.Entry {
	c.t.Helper()
	entries := c.audit.recorded()
	if len(entries) != wantCount {
		c.t.Fatalf("audit has %d entries, want %d: %+v", len(entries), wantCount, entries)
	}

	return entries[len(entries)-1]
}

func (c *scimClient) expectEntry(wantCount int, action audit.Action, resource audit.Resource, changes audit.Changes) {
	c.t.Helper()
	entry := c.lastEntry(wantCount)
	if entry.Actor != audit.SCIMProvisioner || entry.Action != action || entry.Resource != resource ||
		!reflect.DeepEqual(entry.Details[audit.ChangesKey], changes) {
		c.t.Fatalf("entry = %+v, want %s on %+v with %+v", entry, action, resource, changes)
	}
}

func TestUserSyncRecordsWhatChanged(t *testing.T) {
	client := newSCIMClient(t)

	created := client.expect(http.MethodPost, "/Users", authentikUser("alice", "Alice", "alice@example.test", true), http.StatusCreated)
	aliceID := created["id"].(string)
	resource := audit.Resource{Kind: audit.ResourceUser, ID: aliceID, Name: "alice@example.test"}
	client.expectEntry(1, audit.UserProvisioned, resource, audit.Changes{
		"user_name": {Before: "", After: "alice"},
		"email":     {Before: "", After: "alice@example.test"},
		"name":      {Before: "", After: "Alice"},
		"active":    {Before: false, After: true},
	})

	client.expect(http.MethodPut, "/Users/"+aliceID, authentikUser("alice", "Alice", "alice@example.test", true), http.StatusOK)
	client.lastEntry(1)

	client.expect(http.MethodPut, "/Users/"+aliceID, authentikUser("alice", "Alice Adams", "alice@example.test", true), http.StatusOK)
	client.expectEntry(2, audit.UserUpdated, resource, audit.Changes{
		"name": {Before: "Alice", After: "Alice Adams"},
	})

	client.expect(http.MethodPut, "/Users/"+aliceID, authentikUser("alice", "Alice Adams", "alice@example.test", false), http.StatusOK)
	client.expectEntry(3, audit.UserUpdated, resource, audit.Changes{
		"active": {Before: true, After: false},
	})

	client.expect(http.MethodDelete, "/Users/"+aliceID, nil, http.StatusNoContent)
	client.expectEntry(4, audit.UserDeprovisioned, resource, audit.Changes{
		"user_name": {Before: "alice", After: ""},
		"email":     {Before: "alice@example.test", After: ""},
		"name":      {Before: "Alice Adams", After: ""},
	})
}

func TestGroupSyncRecordsWhatChanged(t *testing.T) {
	client := newSCIMClient(t)
	alice := client.expect(http.MethodPost, "/Users", authentikUser("alice", "Alice", "alice@example.test", true), http.StatusCreated)["id"].(string)
	bob := client.expect(http.MethodPost, "/Users", authentikUser("bob", "Bob", "bob@example.test", true), http.StatusCreated)["id"].(string)

	group := client.expect(http.MethodPost, "/Groups", map[string]any{"displayName": "platform-team"}, http.StatusCreated)
	groupID := group["id"].(string)
	groupPath := "/Groups/" + groupID
	resource := audit.Resource{Kind: audit.ResourceGroup, ID: groupID, Name: "platform-team"}
	client.expectEntry(3, audit.GroupProvisioned, resource, audit.Changes{
		"display_name": {Before: "", After: "platform-team"},
	})

	addMembers := func(ids ...string) map[string]any {
		var values []map[string]string
		for _, id := range ids {
			values = append(values, map[string]string{"value": id})
		}

		return map[string]any{"Operations": []map[string]any{{"op": "add", "path": "members", "value": values}}}
	}
	client.expect(http.MethodPatch, groupPath, addMembers(bob, alice), http.StatusOK)
	client.expectEntry(4, audit.GroupUpdated, resource, audit.Changes{
		"members": {Before: []string{}, After: []string{"alice", "bob"}},
	})

	client.expect(http.MethodPatch, groupPath, addMembers(alice), http.StatusOK)
	client.lastEntry(4)

	client.expect(http.MethodPatch, groupPath, map[string]any{
		"Operations": []map[string]any{{"op": "remove", "path": `members[value eq "` + bob + `"]`}},
	}, http.StatusOK)
	client.expectEntry(5, audit.GroupUpdated, resource, audit.Changes{
		"members": {Before: []string{"alice", "bob"}, After: []string{"alice"}},
	})

	client.expect(http.MethodPut, groupPath, map[string]any{
		"displayName": "platform",
		"members":     []map[string]string{{"value": alice}},
	}, http.StatusOK)
	renamed := audit.Resource{Kind: audit.ResourceGroup, ID: groupID, Name: "platform"}
	client.expectEntry(6, audit.GroupUpdated, renamed, audit.Changes{
		"display_name": {Before: "platform-team", After: "platform"},
	})

	client.expect(http.MethodPut, groupPath, map[string]any{
		"displayName": "platform",
		"members":     []map[string]string{{"value": alice}},
	}, http.StatusOK)
	client.lastEntry(6)

	client.expect(http.MethodDelete, groupPath, nil, http.StatusNoContent)
	client.expectEntry(7, audit.GroupDeleted, renamed, audit.Changes{
		"display_name": {Before: "platform", After: ""},
		"members":      {Before: []string{"alice"}, After: []string(nil)},
	})
}
