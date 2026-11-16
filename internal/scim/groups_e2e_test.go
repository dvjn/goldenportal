//go:build e2e

package scim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/access"
	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/pgtest"
)

// newSCIM serves SCIM on a fresh database, with group parents counting as
// they do while SCIM manages groups.
func newSCIM(t *testing.T) (scimClient, *gofrSQL.DB) {
	t.Helper()
	auth.UseGroupParents(true)
	t.Cleanup(func() { auth.UseGroupParents(false) })
	db := pgtest.New(t)

	return scimClient{t: t, s: New(testToken, "https://portal.example.com", logging.NewMockLogger(logging.ERROR)), db: db}, db
}

type scimClient struct {
	t  *testing.T
	s  *Server
	db *gofrSQL.DB
}

func (c scimClient) send(method, path string, body any) *httptest.ResponseRecorder {
	c.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, prefix+path, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	c.s.Routes(c.db)(http.NotFoundHandler()).ServeHTTP(rec, req)

	return rec
}

func (c scimClient) do(method, path string, body any) map[string]any {
	c.t.Helper()
	rec := c.send(method, path, body)
	if rec.Code >= 300 {
		c.t.Fatalf("%s %s = %d: %s", method, path, rec.Code, rec.Body)
	}
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)

	return out
}

func (c scimClient) user(name string) string {
	c.t.Helper()
	res := c.do(http.MethodPost, "/Users", map[string]any{"schemas": []string{schemaUser}, "userName": name + "@example.com"})
	id, _ := res["id"].(string)

	return id
}

func (c scimClient) group(name, externalID string, parents ...string) string {
	c.t.Helper()
	res := c.do(http.MethodPost, "/Groups", groupBody(name, externalID, parents...))
	id, _ := res["id"].(string)

	return id
}

func (c scimClient) parents(groupID string) []string {
	c.t.Helper()
	return parentsOf(c.t, c.do(http.MethodGet, "/Groups/"+groupID, nil))
}

func (c scimClient) patch(groupID string, ops ...map[string]any) {
	c.t.Helper()
	c.do(http.MethodPatch, "/Groups/"+groupID, map[string]any{"schemas": []string{schemaPatchOp}, "Operations": ops})
}

func groupBody(name, externalID string, parents ...string) map[string]any {
	refs := []map[string]string{}
	for _, p := range parents {
		refs = append(refs, map[string]string{"externalId": p})
	}

	return map[string]any{
		"schemas": []string{schemaGroup}, "displayName": name, "externalId": externalID,
		schemaGroupParents: map[string]any{"parents": refs},
	}
}

func parentsOf(t *testing.T, res map[string]any) []string {
	t.Helper()
	schemas, _ := res["schemas"].([]any)
	if !slices.Contains(schemas, any(schemaGroupParents)) {
		t.Fatalf("schemas = %v, want the parents extension", schemas)
	}
	extension, ok := res[schemaGroupParents].(map[string]any)
	if !ok {
		t.Fatalf("group %v has no parents extension", res)
	}
	ids := []string{}
	refs, _ := extension["parents"].([]any)
	for _, r := range refs {
		id, _ := r.(map[string]any)["externalId"].(string)
		ids = append(ids, id)
	}

	return ids
}

func memberValues(res map[string]any) []string {
	ids := []string{}
	members, _ := res["members"].([]any)
	for _, m := range members {
		id, _ := m.(map[string]any)["value"].(string)
		ids = append(ids, id)
	}

	return ids
}

func searchCount(t *testing.T, db *gofrSQL.DB, name string) int {
	t.Helper()
	found, err := access.NewStore(db).Search(t.Context(), name, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range found {
		if s.Type == "group" && s.Name == name && s.MemberCount != nil {
			return *s.MemberCount
		}
	}
	t.Fatalf("search for %q found %+v", name, found)

	return 0
}

func principal(t *testing.T, db *gofrSQL.DB, userID string) auth.Principal {
	t.Helper()
	store := auth.NewStore(db)
	hash := auth.HashSecret(auth.NewSecret())
	if err := store.CreateSession(t.Context(), userID, hash); err != nil {
		t.Fatal(err)
	}
	p, err := store.SessionPrincipal(t.Context(), hash)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

// Some identity providers send only a group's direct members, with each
// group's parents, so membership is counted up the hierarchy here, whatever
// order the groups arrive in.
func TestNestedGroups(t *testing.T) {
	c, db := newSCIM(t)
	ada, grace := c.user("ada"), c.user("grace")

	lead := c.group("discovery-devops-lead", "11", "10", "404")
	c.patch(lead, map[string]any{"op": "add", "path": "members", "value": []map[string]string{{"value": ada}}})
	if p := principal(t, db, ada); !slices.Equal(p.Groups, []string{"discovery-devops-lead"}) {
		t.Fatalf("before the parent arrives, groups = %v", p.Groups)
	}

	discovery := c.group("discovery", "10", "1")
	engineering := c.group("engineering", "1")
	c.patch(discovery, map[string]any{"op": "add", "path": "members", "value": []map[string]string{{"value": grace}}})
	if _, err := access.NewStore(db).Grant(t.Context(), "group", engineering, auth.RoleBuilder); err != nil {
		t.Fatal(err)
	}

	p := principal(t, db, ada)
	if p.Role != auth.RoleBuilder {
		t.Errorf("role through a grandparent = %s, want builder", p.Role)
	}
	if want := []string{"discovery", "discovery-devops-lead", "engineering"}; !slices.Equal(p.Groups, want) {
		t.Errorf("groups = %v, want %v", p.Groups, want)
	}

	grants, err := access.NewStore(db).ListGrants(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 || grants[0].MemberCount == nil || *grants[0].MemberCount != 2 {
		t.Errorf("grants = %+v, want engineering reaching 2 people", grants)
	}
	if got := searchCount(t, db, "engineering"); got != 2 {
		t.Errorf("searched engineering has %d members, want 2 as its grant shows", got)
	}

	// Parents left from an earlier SCIM setup count for nothing without it.
	auth.UseGroupParents(false)
	if p := principal(t, db, ada); p.Role != auth.RoleMember || !slices.Equal(p.Groups, []string{"discovery-devops-lead"}) {
		t.Errorf("without SCIM, ada = %s %v", p.Role, p.Groups)
	}
	if got := searchCount(t, db, "engineering"); got != 0 {
		t.Errorf("without SCIM, engineering has %d members", got)
	}
	auth.UseGroupParents(true)

	res := c.do(http.MethodGet, "/Groups/"+discovery, nil)
	if got := memberValues(res); !slices.Equal(got, []string{grace}) {
		t.Errorf("SCIM members of discovery = %v, want only its direct member", got)
	}
	if got := parentsOf(t, res); !slices.Equal(got, []string{"1"}) {
		t.Errorf("parents of discovery = %v", got)
	}
	if got := c.parents(lead); !slices.Equal(got, []string{"10", "404"}) {
		t.Errorf("parents of the lead group = %v, want unresolved ones kept", got)
	}
	list := c.do(http.MethodGet, "/Groups?filter="+url.QueryEscape(`displayName eq "engineering"`), nil)
	if resources, _ := list["Resources"].([]any); len(resources) != 1 ||
		len(parentsOf(t, resources[0].(map[string]any))) != 0 {
		t.Errorf("listed engineering = %v", list)
	}

	// A cycle back to the child neither loops nor adds anyone.
	c.patch(engineering, map[string]any{"op": "replace", "value": map[string]any{
		"schemas": []string{schemaGroup}, "displayName": "engineering", "externalId": "1",
		schemaGroupParents: map[string]any{"parents": []map[string]string{{"externalId": "11"}}},
	}})
	if got := c.parents(engineering); !slices.Equal(got, []string{"11"}) {
		t.Fatalf("parents after a no-path replace = %v", got)
	}
	if p := principal(t, db, grace); p.Role != auth.RoleBuilder ||
		!slices.Equal(p.Groups, []string{"discovery", "discovery-devops-lead", "engineering"}) {
		t.Errorf("in a cycle, grace = %s %v", p.Role, p.Groups)
	}

	// Taking the lead group out of discovery takes ada out of what's above.
	body := groupBody("discovery-devops-lead", "11")
	body["members"] = []map[string]string{{"value": ada}}
	c.do(http.MethodPut, "/Groups/"+lead, body)
	if p := principal(t, db, ada); p.Role != auth.RoleMember || !slices.Equal(p.Groups, []string{"discovery-devops-lead"}) {
		t.Errorf("after the replace, ada = %s %v", p.Role, p.Groups)
	}
}

func TestPatchGroupParents(t *testing.T) {
	c, _ := newSCIM(t)
	id := c.group("team", "7")

	steps := []struct {
		name string
		op   map[string]any
		want []string
	}{
		{"add by path", map[string]any{
			"op": "add", "path": parentsPath, "value": []map[string]string{{"externalId": "1"}, {"externalId": "2"}},
		}, []string{"1", "2"}},
		{"add one object", map[string]any{
			"op": "add", "path": parentsPath, "value": map[string]string{"externalId": "3"},
		}, []string{"1", "2", "3"}},
		{"remove by filter", map[string]any{
			"op": "remove", "path": parentsPath + `[externalId eq "2"]`,
		}, []string{"1", "3"}},
		{"remove by value", map[string]any{
			"op": "remove", "path": parentsPath, "value": []map[string]string{{"externalId": "1"}},
		}, []string{"3"}},
		{"replace by path", map[string]any{
			"op": "Replace", "path": parentsPath, "value": []map[string]string{{"externalId": "4"}, {"externalId": "5"}},
		}, []string{"4", "5"}},
		{"replace the extension", map[string]any{
			"op": "replace", "path": schemaGroupParents, "value": map[string]any{"parents": []map[string]string{{"externalId": "6"}}},
		}, []string{"6"}},
		{"replace without a path", map[string]any{
			"op": "replace", "value": map[string]any{
				"displayName": "team", schemaGroupParents: map[string]any{"parents": []map[string]string{{"externalId": "8"}}},
			},
		}, []string{"8"}},
		{"another attribute leaves parents", map[string]any{
			"op": "replace", "value": map[string]any{"displayName": "team 2"},
		}, []string{"8"}},
		{"remove all", map[string]any{"op": "remove", "path": parentsPath}, []string{}},
	}
	for _, step := range steps {
		c.patch(id, step.op)
		if got := c.parents(id); !slices.Equal(got, step.want) {
			t.Fatalf("%s: parents = %v, want %v", step.name, got, step.want)
		}
	}

	c.do(http.MethodPut, "/Groups/"+id, groupBody("team", "7", "9"))
	if got := c.parents(id); !slices.Equal(got, []string{"9"}) {
		t.Fatalf("after PUT, parents = %v", got)
	}
}

// authentikUpdate is the PATCH an identity provider's general update sends:
// one no-path replace of every mapped attribute, with a top-level fingerprint
// of the parents so its change detection sees a parent removed.
func authentikUpdate(name, externalID, parentsKey string, parents ...string) map[string]any {
	refs := []map[string]string{}
	for _, p := range parents {
		refs = append(refs, map[string]string{"externalId": p})
	}

	return map[string]any{"op": "replace", "value": map[string]any{
		"schemas":          []string{schemaGroup},
		"displayName":      name,
		"externalId":       externalID,
		"parentsKey":       parentsKey,
		schemaGroupParents: map[string]any{"parents": refs},
	}}
}

// noUnknownAttributes fails if a group resource carries attributes the
// portal doesn't keep.
func noUnknownAttributes(t *testing.T, res map[string]any) {
	t.Helper()
	known := []string{"schemas", "id", "externalId", "displayName", "members", "meta", schemaGroupParents}
	for key := range res {
		if !slices.Contains(known, key) {
			t.Errorf("group returned the unknown attribute %q", key)
		}
	}
	extension, _ := res[schemaGroupParents].(map[string]any)
	for key := range extension {
		if key != "parents" {
			t.Errorf("group extension returned the unknown attribute %q", key)
		}
	}
}

func TestUnknownGroupAttributesAreIgnored(t *testing.T) {
	c, _ := newSCIM(t)

	body := groupBody("team", "7", "1", "2")
	body["parentsKey"] = "1,2"
	body[schemaGroupParents].(map[string]any)["level"] = 3
	res := c.do(http.MethodPost, "/Groups", body)
	id, _ := res["id"].(string)
	noUnknownAttributes(t, res)
	if got := parentsOf(t, res); !slices.Equal(got, []string{"1", "2"}) {
		t.Fatalf("after POST, parents = %v", got)
	}

	body = groupBody("team", "7", "2", "3")
	body["parentsKey"] = "2,3"
	body[schemaGroupParents].(map[string]any)["level"] = 3
	noUnknownAttributes(t, c.do(http.MethodPut, "/Groups/"+id, body))

	ops := []map[string]any{
		authentikUpdate("team", "7", "2,3", "2", "3"),
		{"op": "add", "value": map[string]any{"parentsKey": "2,3", "nickname": "t"}},
		{"op": "replace", "path": "parentsKey", "value": "2,3"},
		{"op": "remove", "path": "parentsKey"},
		{"op": "replace", "path": schemaGroupParents, "value": map[string]any{
			"parents": []map[string]string{{"externalId": "2"}, {"externalId": "3"}}, "level": 3,
		}},
		{"op": "replace", "path": schemaGroupParents + ":level", "value": 3},
		{"op": "remove", "path": schemaGroupParents + ":level"},
	}
	for _, op := range ops {
		c.patch(id, op)
		res := c.do(http.MethodGet, "/Groups/"+id, nil)
		noUnknownAttributes(t, res)
		if got := parentsOf(t, res); !slices.Equal(got, []string{"2", "3"}) {
			t.Fatalf("after %v, parents = %v", op, got)
		}
	}
}

// A parent removed upstream reaches the portal: the fingerprint makes the
// identity provider send the update, and a replace sets the parent list
// rather than merging into it.
func TestParentRemovalWithAFingerprint(t *testing.T) {
	c, _ := newSCIM(t)
	id := c.group("team", "7", "1", "2")

	c.patch(id, authentikUpdate("team", "7", "1,2,changed", "1", "2"))
	if got := c.parents(id); !slices.Equal(got, []string{"1", "2"}) {
		t.Fatalf("after only the fingerprint changed, parents = %v", got)
	}

	c.patch(id, authentikUpdate("team", "7", "1", "1"))
	if got := c.parents(id); !slices.Equal(got, []string{"1"}) {
		t.Fatalf("after a parent was removed, parents = %v", got)
	}
}

// A no-path replace may list the name and external id in any order; both
// changes land.
func TestPatchRenamesAndReidentifiesAtOnce(t *testing.T) {
	c, _ := newSCIM(t)
	id := c.group("team", "7")

	for i := range 10 {
		name, externalID := fmt.Sprintf("team %d", i), fmt.Sprintf("x%d", i)
		c.patch(id, map[string]any{"op": "replace", "value": map[string]any{
			"displayName": name, "externalId": externalID,
		}})
		res := c.do(http.MethodGet, "/Groups/"+id, nil)
		if res["displayName"] != name || res["externalId"] != externalID {
			t.Fatalf("after renaming to %q, %q: %v, %v", name, externalID, res["displayName"], res["externalId"])
		}
	}
}

// Create and replace read parents as PATCH does: one object, and elements
// without a string externalId are skipped.
func TestGroupBodiesReadParentsLeniently(t *testing.T) {
	c, _ := newSCIM(t)
	body := groupBody("team", "7")
	body[schemaGroupParents] = map[string]any{"parents": map[string]any{"externalId": "1"}}
	res := c.do(http.MethodPost, "/Groups", body)
	id, _ := res["id"].(string)
	if got := parentsOf(t, res); !slices.Equal(got, []string{"1"}) {
		t.Fatalf("after POST with one parent object, parents = %v", got)
	}

	body[schemaGroupParents] = map[string]any{"parents": []any{"junk", map[string]any{"externalId": 5}, map[string]any{"externalId": "2"}}}
	if got := parentsOf(t, c.do(http.MethodPut, "/Groups/"+id, body)); !slices.Equal(got, []string{"2"}) {
		t.Fatalf("after PUT with some malformed parents, parents = %v", got)
	}

	body[schemaGroupParents] = map[string]any{"parents": []any{}}
	if got := parentsOf(t, c.do(http.MethodPut, "/Groups/"+id, body)); len(got) != 0 {
		t.Fatalf("after PUT with an empty list, parents = %v", got)
	}
}

// A parents value with no parent in it is refused rather than read as none,
// so a broken mapping can't drop every inherited role.
func TestMalformedParentsAreRefused(t *testing.T) {
	c, _ := newSCIM(t)
	id := c.group("team", "7", "1")

	refused := func(what string, rec *httptest.ResponseRecorder) {
		t.Helper()
		var res errorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if rec.Code != http.StatusBadRequest || res.ScimType != "invalidValue" {
			t.Errorf("%s = %d %s, want 400 invalidValue", what, rec.Code, rec.Body)
		}
		if got := c.parents(id); !slices.Equal(got, []string{"1"}) {
			t.Fatalf("after %s, parents = %v", what, got)
		}
	}
	for _, value := range []any{"1", []any{map[string]any{"value": "1"}}, map[string]any{}, []any{"junk"}} {
		body := groupBody("team 2", "8")
		body[schemaGroupParents] = map[string]any{"parents": value}
		refused(fmt.Sprintf("POST with parents %v", value), c.send(http.MethodPost, "/Groups", body))
		refused(fmt.Sprintf("PUT with parents %v", value), c.send(http.MethodPut, "/Groups/"+id, body))

		for _, op := range []map[string]any{
			{"op": "replace", "path": parentsPath, "value": value},
			{"op": "add", "path": parentsPath, "value": value},
			{"op": "replace", "path": schemaGroupParents, "value": map[string]any{"parents": value}},
			{"op": "replace", "value": map[string]any{schemaGroupParents: map[string]any{"parents": value}}},
		} {
			refused(fmt.Sprintf("PATCH %v", op), c.send(http.MethodPatch, "/Groups/"+id, map[string]any{
				"schemas": []string{schemaPatchOp}, "Operations": []map[string]any{op},
			}))
		}
	}
	if list := c.do(http.MethodGet, "/Groups?filter="+url.QueryEscape(`displayName eq "team 2"`), nil); list["totalResults"] != float64(0) {
		t.Errorf("a refused POST created a group: %v", list)
	}
}
