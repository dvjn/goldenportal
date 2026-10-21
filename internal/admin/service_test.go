package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dvjn/goldport/internal/access"
	"github.com/dvjn/goldport/internal/apitoken"
	"github.com/dvjn/goldport/internal/audit"
)

func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}

	return envelope.Data
}

func TestOnlyAdminsChangeAccess(t *testing.T) {
	cases := []struct {
		name   string
		as     string
		status int
	}{
		{"signed out", "", http.StatusUnauthorized},
		{"unknown session", "u-9", http.StatusUnauthorized},
		{"member", "u-4", http.StatusForbidden},
		{"maintainer through a group", "u-2", http.StatusForbidden},
		{"maintainer api token", "gpt_ci", http.StatusForbidden},
		{"admin through a group", "u-1", http.StatusOK},
		{"bootstrap admin", "u-3", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()

			response := h.do(t, http.MethodPut, "/api/v1/access/grants/group/g-2", tc.as, `{"role":"admin"}`)

			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if granted := len(h.grants.sets) == 1; granted != (tc.status == http.StatusOK) {
				t.Fatalf("grants set = %v", h.grants.sets)
			}
		})
	}
}

func TestSetGrantRecordsWhoGrantedWhat(t *testing.T) {
	h := newHarness()
	h.grants.previous = access.Maintainer

	response := h.do(t, http.MethodPut, "/api/v1/access/grants/group/g-2", "u-1", `{"role":"admin"}`)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	want := setCall{subject: access.Subject{Type: access.SubjectGroup, ID: "g-2"}, role: access.Admin, by: "u-1"}
	if len(h.grants.sets) != 1 || h.grants.sets[0] != want {
		t.Fatalf("sets = %+v, want %+v", h.grants.sets, want)
	}
	entry := h.audit.entries[0]
	if entry.Action != audit.GrantChanged || entry.Target.Name != "payments-team" || entry.Actor.Label != "alice@example.test" ||
		entry.Details["previous_role"] != access.Maintainer {
		t.Fatalf("audit = %+v", entry)
	}
}

func TestSetGrantRefusesBadInput(t *testing.T) {
	cases := []struct {
		name, path, body string
		status           int
	}{
		{"member is not a grant role", "/api/v1/access/grants/group/g-2", `{"role":"member"}`, http.StatusBadRequest},
		{"unknown subject type", "/api/v1/access/grants/team/g-2", `{"role":"admin"}`, http.StatusBadRequest},
		{"unknown group", "/api/v1/access/grants/group/g-9", `{"role":"admin"}`, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()

			if response := h.do(t, http.MethodPut, tc.path, "u-1", tc.body); response.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if len(h.grants.sets) != 0 || len(h.audit.entries) != 0 {
				t.Fatal("a refused request changed a grant or wrote the audit log")
			}
		})
	}
}

func TestRemovingTheLastAdminGrantIsRefused(t *testing.T) {
	h := newHarness()
	h.grants.setErr = access.ErrLastAdmin

	response := h.do(t, http.MethodDelete, "/api/v1/access/grants/group/g-1", "u-1", "")

	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "last Admin grant") {
		t.Fatalf("response = %d %s, want 409 naming the last Admin grant", response.Code, response.Body.String())
	}
	if len(h.audit.entries) != 0 {
		t.Fatalf("audit = %+v, want nothing recorded", h.audit.entries)
	}
}

func TestListGrantsMarksTheLastAdminGrant(t *testing.T) {
	h := newHarness()

	grants := decode[grantsResponse](t, h.do(t, http.MethodGet, "/api/v1/access/grants", "u-1", "").Body.Bytes())

	if len(grants.Grants) != 2 || !grants.Grants[0].LastAdmin || grants.Grants[1].LastAdmin {
		t.Fatalf("grants = %+v, want only platform-team marked", grants.Grants)
	}

	h.grants.carrying = []string{"r-1", "r-3"}
	grants = decode[grantsResponse](t, h.do(t, http.MethodGet, "/api/v1/access/grants", "u-1", "").Body.Bytes())
	if grants.Grants[0].LastAdmin {
		t.Fatal("platform-team is marked although another grant carries Admin")
	}
}

func TestCheckUserNamesTheGrantThatCarriesTheRole(t *testing.T) {
	h := newHarness()

	check := decode[checkResponse](t, h.do(t, http.MethodGet, "/api/v1/access/users/u-2", "u-1", "").Body.Bytes())

	if check.Role != access.Maintainer || len(check.CarriedBy) != 1 || check.CarriedBy[0].Name != "payments-team" ||
		len(check.Groups) != 1 || !check.LastSignInAt.Equal(at) || check.Bootstrap {
		t.Fatalf("check = %+v, want Maintainer through payments-team", check)
	}

	check = decode[checkResponse](t, h.do(t, http.MethodGet, "/api/v1/access/users/u-3", "u-1", "").Body.Bytes())
	if check.Role != access.Admin || !check.Bootstrap || len(check.CarriedBy) != 0 {
		t.Fatalf("check = %+v, want the bootstrap Admin", check)
	}
}

func TestRevokeSessionsEndsTheUsersSessions(t *testing.T) {
	h := newHarness()

	response := h.do(t, http.MethodDelete, "/api/v1/access/users/u-2/sessions", "u-1", "")

	if response.Code != http.StatusNoContent || len(h.sessions.revoked) != 1 || h.sessions.revoked[0] != "u-2" {
		t.Fatalf("response = %d, revoked = %v", response.Code, h.sessions.revoked)
	}
	if entry := h.audit.entries[0]; entry.Action != audit.SessionsRevoked || entry.Target.ID != "u-2" {
		t.Fatalf("audit = %+v", entry)
	}
}

func TestCreateTokenValidatesTheRequest(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
	}{
		{"admin token", `{"name":"ci","role":"admin","expires_in_days":30}`, http.StatusBadRequest},
		{"longer than a year", `{"name":"ci","role":"member","expires_in_days":366}`, http.StatusBadRequest},
		{"no expiry", `{"name":"ci","role":"member"}`, http.StatusBadRequest},
		{"blank name", `{"name":"  ","role":"member","expires_in_days":30}`, http.StatusBadRequest},
		{"maintainer for 90 days", `{"name":" ci ","role":"maintainer","expires_in_days":90}`, http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()

			response := h.do(t, http.MethodPost, "/api/v1/access/tokens", "u-1", tc.body)

			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if tc.status != http.StatusCreated {
				return
			}
			created := decode[secretResponse[tokenResponse]](t, response.Body.Bytes())
			if created.Secret != "gpt_new" || created.Token.Name != "ci" || h.tokens.created[0].CreatedBy != "u-1" {
				t.Fatalf("created = %+v, store saw %+v", created, h.tokens.created)
			}
			if entry := h.audit.entries[0]; entry.Action != audit.APITokenCreated || entry.Target.Name != "ci" {
				t.Fatalf("audit = %+v", entry)
			}
		})
	}
}

func TestRevokeTokenRecordsTheTokenName(t *testing.T) {
	h := newHarness()

	if response := h.do(t, http.MethodDelete, "/api/v1/access/tokens/t-1", "u-1", ""); response.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if response := h.do(t, http.MethodDelete, "/api/v1/access/tokens/t-2", "u-1", ""); response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an unknown token", response.Code)
	}
	if len(h.audit.entries) != 1 || h.audit.entries[0].Target.Name != "ci" {
		t.Fatalf("audit = %+v", h.audit.entries)
	}
}

func TestRotateSCIMTokenReturnsTheSecretOnce(t *testing.T) {
	h := newHarness()

	response := h.do(t, http.MethodPost, "/api/v1/scim/token", "u-1", "")

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	rotated := decode[secretResponse[scimTokenResponse]](t, response.Body.Bytes())
	if rotated.Secret != "gpt_scim" || h.tokens.rotated[0].Kind != apitoken.KindSCIM {
		t.Fatalf("rotated = %+v, store saw %+v", rotated, h.tokens.rotated)
	}
	if h.audit.entries[0].Action != audit.SCIMTokenRotated {
		t.Fatalf("audit = %+v", h.audit.entries)
	}
}

func TestSettingsReportTheBootstrapState(t *testing.T) {
	h := newHarness()
	h.grants.carrying = nil

	settings := decode[settingsResponse](t, h.do(t, http.MethodGet, "/api/v1/settings", "u-3", "").Body.Bytes())

	if settings.Bootstrap.AdminGranted || len(settings.Bootstrap.Admins) != 1 || settings.SessionMaxAgeSeconds != 36000 ||
		settings.OIDCClientSecretSet {
		t.Fatalf("settings = %+v", settings)
	}
	scim := decode[scimResponse](t, h.do(t, http.MethodGet, "/api/v1/scim", "u-3", "").Body.Bytes())
	if scim.Endpoint != "https://portal.example.test/scim/v2" || scim.Users != 4 || scim.Groups != 2 || !scim.LastPushAt.IsZero() {
		t.Fatalf("scim = %+v", scim)
	}
}

func TestListSubjectsShowsEachSubjectsOwnRole(t *testing.T) {
	h := newHarness()

	subjects := decode[subjectsResponse](t, h.do(t, http.MethodGet, "/api/v1/access/subjects", "u-1", "").Body.Bytes())

	if len(subjects.Groups) != 2 || subjects.Groups[0].Name != "payments-team" || subjects.Groups[0].Role != access.Maintainer {
		t.Fatalf("groups = %+v", subjects.Groups)
	}
	if len(subjects.Users) != 4 || subjects.Users[0].Role != access.NoRole {
		t.Fatalf("users = %+v", subjects.Users)
	}
}

func TestListAuditClampsThePage(t *testing.T) {
	h := newHarness()

	page := decode[auditResponse](t, h.do(t, http.MethodGet, "/api/v1/audit?offset=-4&limit=5000", "u-1", "").Body.Bytes())

	if page.Offset != 0 || page.Limit != maxAuditLimit {
		t.Fatalf("page = %+v, want offset 0 and limit %d", page, maxAuditLimit)
	}
}
