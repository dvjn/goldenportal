package scim

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gofr.dev/pkg/gofr/logging"
)

const testToken = "a-long-enough-scim-token-for-the-tests"

func serve(t *testing.T, s *Server, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	s.Routes(nil)(next).ServeHTTP(rec, req)

	return rec
}

func TestRoutesGuardTheEndpoint(t *testing.T) {
	logger := logging.NewMockLogger(logging.ERROR)
	on := New(testToken, "https://portal.example.com/", logger)
	off := New("", "https://portal.example.com", logger)

	tests := []struct {
		name   string
		server *Server
		path   string
		token  string
		want   int
	}{
		{"other paths pass through", on, "/api/v1/me", "", http.StatusTeapot},
		{"SCIM is off without a token", off, "/scim/v2/Users", testToken, http.StatusNotFound},
		{"requests need the token", on, "/scim/v2/Users", "", http.StatusUnauthorized},
		{"a wrong token is refused", on, "/scim/v2/Users", "wrong", http.StatusUnauthorized},
		{"discovery works with the token", on, "/scim/v2/ServiceProviderConfig", testToken, http.StatusOK},
		{"unknown endpoints are not found", on, "/scim/v2/Bulk", testToken, http.StatusNotFound},
		{"malformed ids are not found", on, "/scim/v2/Users/not-an-id", testToken, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, tt.server, http.MethodGet, tt.path, tt.token)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.want, rec.Body)
			}
			if tt.want != http.StatusTeapot && rec.Header().Get("Content-Type") != contentType {
				t.Fatalf("Content-Type = %q, want %q", rec.Header().Get("Content-Type"), contentType)
			}
		})
	}

	if got := on.URL(); got != "https://portal.example.com/scim/v2" {
		t.Errorf("URL() = %q", got)
	}
}

func TestParseFilter(t *testing.T) {
	f, ok := parseFilter(`userName eq "ada@example.com"`, userFilterAttributes)
	if !ok || f.attribute != "username" || f.value != "ada@example.com" {
		t.Fatalf("parseFilter(userName) = %+v, %v", f, ok)
	}
	f, ok = parseFilter(`displayName Eq "team \"a\""`, groupFilterAttributes)
	if !ok || f.attribute != "name" || f.value != `team "a"` {
		t.Fatalf("parseFilter(displayName) = %+v, %v", f, ok)
	}
	if f, ok := parseFilter("", userFilterAttributes); !ok || f != nil {
		t.Fatal("an empty filter should match everything")
	}
	for _, bad := range []string{`userName co "a"`, `title eq "x"`, `userName eq "a" and active eq true`} {
		if _, ok := parseFilter(bad, userFilterAttributes); ok {
			t.Errorf("parseFilter(%q) accepted a filter the portal doesn't answer", bad)
		}
	}
}

func TestUserInput(t *testing.T) {
	inactive := false
	in := userResource{
		UserName: " ada ",
		Name:     &name{GivenName: "Ada", FamilyName: "Lovelace"},
		Emails:   []email{{Value: "old@example.com"}, {Value: "ada@example.com", Primary: true}},
		Active:   &inactive,
	}.input()

	if in.UserName != "ada" || in.DisplayName != "Ada Lovelace" || in.Email != "ada@example.com" || in.Active {
		t.Fatalf("input() = %+v", in)
	}

	in = userResource{UserName: "grace@example.com"}.input()
	if in.Email != "grace@example.com" || in.DisplayName != "grace@example.com" || !in.Active {
		t.Fatalf("input() without details = %+v", in)
	}
}

func TestApplyUserOperation(t *testing.T) {
	doc := map[string]any{"userName": "ada", "active": true, "displayName": "Ada"}

	ops := []patchOperation{
		{Op: "replace", Path: "active", Value: "False"},
		{Op: "replace", Path: `emails[type eq "work"].value`, Value: "ada@new.example.com"},
		{Op: "replace", Value: map[string]any{"displayName": "Ada L."}},
		{Op: "add", Path: "phoneNumbers", Value: "ignored"},
	}
	for _, op := range ops {
		if err := applyUserOperation(doc, op); err != nil {
			t.Fatalf("applyUserOperation(%+v) = %v", op, err)
		}
	}

	raw, _ := json.Marshal(doc)
	var res userResource
	_ = json.Unmarshal(raw, &res)
	in := res.input()
	if in.Active || in.Email != "ada@new.example.com" || in.DisplayName != "Ada L." {
		t.Fatalf("after patch = %+v", in)
	}

	if err := applyUserOperation(doc, patchOperation{Op: "replace", Value: "not an object"}); err == nil {
		t.Fatal("a value without a path must be an object")
	}
}

func TestMemberReferences(t *testing.T) {
	id := "6f1c9d4e-7a3b-4c2d-9e8f-0a1b2c3d4e5f"
	refs := refsFrom([]any{map[string]any{"value": id}, map[string]any{"value": "nope"}, "junk"})
	if got := memberIDs(refs); len(got) != 1 || got[0] != id {
		t.Fatalf("memberIDs = %v, want only the valid id", got)
	}
	if got := memberIDs(refsFrom(map[string]any{"value": id})); len(got) != 1 {
		t.Fatalf("a single member object = %v", got)
	}
	if m := memberFilter.FindStringSubmatch(`members[value eq "` + id + `"]`); m == nil || m[1] != id {
		t.Fatalf("member filter = %v", m)
	}
}
