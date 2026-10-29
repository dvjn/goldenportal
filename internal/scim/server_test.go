package scim

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"gofr.dev/pkg/gofr/logging"
)

const testToken = "scim-test-token"

type scimClient struct {
	t        *testing.T
	server   *httptest.Server
	store    *memoryDirectory
	sessions *recordingSessions
	audit    *memoryAudit
}

func newSCIMClient(t *testing.T) *scimClient {
	t.Helper()
	store := newMemoryDirectory()
	sessions := &recordingSessions{}
	log := &memoryAudit{}
	scim := NewServer(store, sessions, staticTokens(testToken), log, logging.NewMockLogger(logging.ERROR))
	server := httptest.NewServer(scim.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})))
	t.Cleanup(server.Close)

	return &scimClient{t: t, server: server, store: store, sessions: sessions, audit: log}
}

// do sends a request the way Authentik's SCIM client does and decodes the
// JSON response into a generic map.
func (c *scimClient) do(method, path, token string, body any) (int, map[string]any) {
	c.t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		payload = bytes.NewReader(encoded)
	}
	request, _ := http.NewRequest(method, c.server.URL+"/scim/v2"+path, payload)
	request.Header.Set("Accept", "application/scim+json")
	request.Header.Set("Content-Type", "application/scim+json")
	request.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()

	var decoded map[string]any
	raw, _ := io.ReadAll(response.Body)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			c.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
		if got := response.Header.Get("Content-Type"); got != "application/scim+json" {
			c.t.Fatalf("%s %s: Content-Type = %q", method, path, got)
		}
	}

	return response.StatusCode, decoded
}

func (c *scimClient) expect(method, path string, body any, status int) map[string]any {
	c.t.Helper()
	got, decoded := c.do(method, path, testToken, body)
	if got != status {
		c.t.Fatalf("%s %s: status = %d, want %d: %v", method, path, got, status, decoded)
	}

	return decoded
}

func authentikUser(userName, name, email string, active bool) map[string]any {
	return map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		"userName":    userName,
		"name":        map[string]string{"formatted": name + " ", "givenName": name, "familyName": " "},
		"displayName": name,
		"active":      active,
		"emails":      []map[string]any{{"value": email, "type": "other", "primary": true}},
		"externalId":  "uid-" + userName,
	}
}

func memberValues(group map[string]any) []string {
	var values []string
	for _, member := range group["members"].([]any) {
		values = append(values, member.(map[string]any)["value"].(string))
	}
	slices.Sort(values)

	return values
}

func TestServiceProviderConfigAdvertisesPatchAndFilter(t *testing.T) {
	client := newSCIMClient(t)

	config := client.expect(http.MethodGet, "/ServiceProviderConfig", nil, http.StatusOK)

	if config["patch"].(map[string]any)["supported"] != true || config["filter"].(map[string]any)["supported"] != true {
		t.Fatalf("config = %v, want patch and filter supported", config)
	}
	if config["bulk"].(map[string]any)["maxOperations"] != float64(0) {
		t.Fatalf("bulk = %v, want maxOperations 0", config["bulk"])
	}
}

func TestRequestsWithoutAValidTokenAreRefused(t *testing.T) {
	client := newSCIMClient(t)

	for _, token := range []string{"", "wrong"} {
		status, body := client.do(http.MethodGet, "/ServiceProviderConfig", token, nil)
		if status != http.StatusUnauthorized || body["status"] != "401" {
			t.Fatalf("token %q: status = %d, body = %v, want 401", token, status, body)
		}
	}
}

func TestAuthentikUserSync(t *testing.T) {
	client := newSCIMClient(t)

	created := client.expect(http.MethodPost, "/Users", authentikUser("alice", "Alice", "alice@example.test", true), http.StatusCreated)
	aliceID := created["id"].(string)
	stored := client.store.users[0]
	if stored.ExternalID != "uid-alice" || stored.Email != "alice@example.test" || stored.Name != "Alice" || !stored.Active {
		t.Fatalf("stored user = %+v", stored)
	}

	duplicate := client.expect(http.MethodPost, "/Users", authentikUser("alice", "Alice", "alice@example.test", true), http.StatusConflict)
	if duplicate["scimType"] != "uniqueness" || duplicate["status"] != "409" {
		t.Fatalf("duplicate = %v", duplicate)
	}
	found := client.expect(http.MethodGet, "/Users?"+url.Values{"filter": {`userName eq "alice"`}}.Encode(), nil, http.StatusOK)
	resources := found["Resources"].([]any)
	if found["totalResults"] != float64(1) || resources[0].(map[string]any)["id"] != aliceID {
		t.Fatalf("filter lookup = %v, want alice", found)
	}

	replaced := client.expect(http.MethodPut, "/Users/"+aliceID, authentikUser("alice", "Alice Adams", "alice@example.test", true), http.StatusOK)
	if replaced["displayName"] != "Alice Adams" {
		t.Fatalf("replaced = %v", replaced)
	}
	if len(client.sessions.revoked) != 0 {
		t.Fatalf("revoked = %v for an active user", client.sessions.revoked)
	}

	client.expect(http.MethodPut, "/Users/"+aliceID, authentikUser("alice", "Alice Adams", "alice@example.test", false), http.StatusOK)
	if !slices.Equal(client.sessions.revoked, []string{aliceID}) {
		t.Fatalf("revoked = %v, want alice's sessions", client.sessions.revoked)
	}

	client.expect(http.MethodDelete, "/Users/"+aliceID, nil, http.StatusNoContent)
	client.expect(http.MethodGet, "/Users/"+aliceID, nil, http.StatusNotFound)
}

func TestAuthentikGroupSync(t *testing.T) {
	client := newSCIMClient(t)
	alice := client.expect(http.MethodPost, "/Users", authentikUser("alice", "Alice", "alice@example.test", true), http.StatusCreated)["id"].(string)
	bob := client.expect(http.MethodPost, "/Users", authentikUser("bob", "Bob", "bob@example.test", true), http.StatusCreated)["id"].(string)

	group := client.expect(http.MethodPost, "/Groups", map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
		"displayName": "platform-team",
		"externalId":  "group-pk-1",
	}, http.StatusCreated)
	groupPath := "/Groups/" + group["id"].(string)
	if len(group["members"].([]any)) != 0 {
		t.Fatalf("new group members = %v, want none", group["members"])
	}

	client.expect(http.MethodPost, "/Groups", map[string]any{"displayName": "platform-team"}, http.StatusConflict)
	found := client.expect(http.MethodGet, "/Groups?"+url.Values{"filter": {`displayName eq "platform-team"`}}.Encode(), nil, http.StatusOK)
	if found["Resources"].([]any)[0].(map[string]any)["id"] != group["id"] {
		t.Fatalf("filter lookup = %v", found)
	}

	client.expect(http.MethodPatch, groupPath, map[string]any{
		"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": []map[string]any{
			{"op": "add", "path": "members", "value": []map[string]string{{"value": alice}}},
			{"op": "add", "path": "members", "value": []map[string]string{{"value": bob}}},
		},
	}, http.StatusOK)
	members := memberValues(client.expect(http.MethodGet, groupPath, nil, http.StatusOK))
	if !slices.Equal(members, []string{alice, bob}) {
		t.Fatalf("members = %v, want alice and bob", members)
	}

	patched := client.expect(http.MethodPatch, groupPath, map[string]any{
		"schemas":    []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": []map[string]any{{"op": "remove", "path": `members[value eq "` + bob + `"]`}},
	}, http.StatusOK)
	if !slices.Equal(memberValues(patched), []string{alice}) {
		t.Fatalf("members = %v, want alice", memberValues(patched))
	}

	renamed := client.expect(http.MethodPatch, groupPath, map[string]any{
		"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": []map[string]any{{
			"op": "replace",
			"value": map[string]any{
				"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
				"displayName": "platform",
				"externalId":  "group-pk-1",
			},
		}},
	}, http.StatusOK)
	if renamed["displayName"] != "platform" || !slices.Equal(memberValues(renamed), []string{alice}) {
		t.Fatalf("renamed = %v", renamed)
	}

	client.expect(http.MethodPatch, groupPath, map[string]any{
		"Operations": []map[string]any{{"op": "remove", "path": "members"}},
	}, http.StatusOK)
	if members := client.store.members[group["id"].(string)]; len(members) != 0 {
		t.Fatalf("members = %v, want none", members)
	}

	client.expect(http.MethodDelete, groupPath, nil, http.StatusNoContent)
	client.expect(http.MethodGet, groupPath, nil, http.StatusNotFound)
}

func TestBadRequestsGetSCIMErrors(t *testing.T) {
	client := newSCIMClient(t)
	group := client.expect(http.MethodPost, "/Groups", map[string]any{"displayName": "contractors"}, http.StatusCreated)

	cases := []struct {
		name     string
		method   string
		path     string
		body     any
		status   int
		scimType string
	}{
		{"filter with another operator", http.MethodGet, "/Users?" + url.Values{"filter": {`userName co "al"`}}.Encode(), nil, http.StatusBadRequest, "invalidFilter"},
		{"filter on an unsupported attribute", http.MethodGet, "/Users?" + url.Values{"filter": {`emails eq "a@example.test"`}}.Encode(), nil, http.StatusBadRequest, "invalidFilter"},
		{"user without a user name", http.MethodPost, "/Users", map[string]any{"displayName": "Nobody"}, http.StatusBadRequest, "invalidValue"},
		{"unknown patch operation", http.MethodPatch, "/Groups/" + group["id"].(string), map[string]any{
			"Operations": []map[string]any{{"op": "move", "path": "members"}},
		}, http.StatusBadRequest, "invalidSyntax"},
		{"patch on an unknown path", http.MethodPatch, "/Groups/" + group["id"].(string), map[string]any{
			"Operations": []map[string]any{{"op": "replace", "path": "owner", "value": "x"}},
		}, http.StatusBadRequest, "invalidPath"},
		{"unknown endpoint", http.MethodGet, "/Bulk", nil, http.StatusNotFound, ""},
		{"malformed body", http.MethodPost, "/Users", "not an object", http.StatusBadRequest, "invalidSyntax"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := client.do(tc.method, tc.path, testToken, tc.body)
			scimType, _ := body["scimType"].(string)
			if status != tc.status || scimType != tc.scimType {
				t.Fatalf("status = %d, body = %v, want %d %q", status, body, tc.status, tc.scimType)
			}
			if !strings.Contains(body["schemas"].([]any)[0].(string), "Error") {
				t.Fatalf("schemas = %v, want the error schema", body["schemas"])
			}
		})
	}
}

func TestListsPageWithStartIndexAndCount(t *testing.T) {
	client := newSCIMClient(t)
	for _, name := range []string{"alice", "bob", "cara"} {
		client.expect(http.MethodPost, "/Users", authentikUser(name, name, name+"@example.test", true), http.StatusCreated)
	}

	page := client.expect(http.MethodGet, "/Users?startIndex=2&count=1", nil, http.StatusOK)

	if page["totalResults"] != float64(3) || page["itemsPerPage"] != float64(1) || page["startIndex"] != float64(2) {
		t.Fatalf("page = %v", page)
	}
	if page["Resources"].([]any)[0].(map[string]any)["userName"] != "bob" {
		t.Fatalf("resources = %v, want bob", page["Resources"])
	}
}
