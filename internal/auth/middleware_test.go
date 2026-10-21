package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/directory"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr"
)

func aliceDirectory() *memoryDirectory {
	return &memoryDirectory{
		users: []directory.User{{
			ID: "u-1", ExternalID: "sub-alice", UserName: "alice", Email: "alice@example.test", Name: "Alice", Active: true,
		}},
		groups: map[string][]directory.Group{"u-1": {
			{ID: "g-1", DisplayName: "all-engineers"},
			{ID: "g-2", DisplayName: "platform-team"},
		}},
	}
}

func TestMiddlewareFailsClosed(t *testing.T) {
	whoAmI := func(ctx *gofr.Context) (any, error) {
		principal, _ := PrincipalFrom(ctx)
		return principal.User.ID, nil
	}
	register := func(routes *httpx.Routes) {
		routes.GET("/api/v1/open", httpx.Public(), whoAmI)
		routes.GET("/api/v1/private", httpx.Authenticated(), whoAmI)
		routes.GET("/api/v1/grants", httpx.Requires(access.AccessWrite), whoAmI)
	}
	admin := memoryGrants{"u-1": {{ID: "r-1", Subject: access.Subject{Type: access.SubjectGroup, ID: "g-2"}, Role: access.Admin}}}
	const (
		required  = `{"error":{"message":"authentication required"}}`
		forbidden = `{"error":{"message":"this needs the access.write permission, ask an admin if you need access"}}`
	)

	cases := []struct {
		name     string
		path     string
		cookie   string
		grants   memoryGrants
		failure  error
		status   int
		response string
	}{
		{"public route without a session", "/api/v1/open", "", nil, nil, http.StatusOK, `{"data":""}`},
		{"authenticated route without a cookie", "/api/v1/private", "", nil, nil, http.StatusUnauthorized, required},
		{"authenticated route with an unknown token", "/api/v1/private", "stale", nil, nil, http.StatusUnauthorized, required},
		{"authenticated route with a live session", "/api/v1/private", "live", nil, nil, http.StatusOK, `{"data":"u-1"}`},
		{"permission route without a cookie", "/api/v1/grants", "", admin, nil, http.StatusUnauthorized, required},
		{"permission route without the permission", "/api/v1/grants", "live", nil, nil, http.StatusForbidden, forbidden},
		{"permission route with the permission", "/api/v1/grants", "live", admin, nil, http.StatusOK, `{"data":"u-1"}`},
		{"undeclared api route", "/api/v1/missing", "live", nil, nil, http.StatusUnauthorized, required},
		{
			"session store failure", "/api/v1/private", "live", nil, errors.New("database down"),
			http.StatusInternalServerError, `{"error":{"message":"could not check the session"}}`,
		},
		{"non api path", "/somewhere", "", nil, nil, http.StatusNotFound, "404 page not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessions := newMemorySessions(aliceDirectory())
			sessions.userByToken["live"] = "u-1"
			sessions.err = tc.failure
			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.cookie != "" {
				request.AddCookie(&http.Cookie{Name: testCookies.Session, Value: tc.cookie})
			}
			recorder := httptest.NewRecorder()

			newTestRouter(sessions, tc.grants, register).ServeHTTP(recorder, request)

			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.status)
			}
			if got := strings.TrimSuffix(recorder.Body.String(), "\n"); got != tc.response {
				t.Fatalf("body = %q, want %q", got, tc.response)
			}
		})
	}
}

func TestMeReturnsTheUserAndTheirGroups(t *testing.T) {
	users := aliceDirectory()
	sessions := newMemorySessions(users)
	sessions.userByToken["live"] = "u-1"
	service := NewService(Config{Cookies: testCookies}, nil, sessions, users, &memoryAudit{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: testCookies.Session, Value: "live"})
	recorder := httptest.NewRecorder()

	newTestRouter(sessions, nil, service.Register).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Data meResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := meResponse{
		User:        meUser{ID: "u-1", Email: "alice@example.test", Name: "Alice"},
		Groups:      []meGroup{{ID: "g-1", Name: "all-engineers"}, {ID: "g-2", Name: "platform-team"}},
		Role:        access.Member,
		Permissions: []access.Permission{access.CatalogRead},
	}
	if !reflect.DeepEqual(body.Data, want) {
		t.Fatalf("me = %+v, want %+v", body.Data, want)
	}
}
