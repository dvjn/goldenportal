package connection

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

// TestGitLabPullReadsRunners checks a sync reads each runner the attempts
// it reads ran on once, however many projects share it, and gives it as
// the newest attempt on it found it, without its IP address or anything of
// its token, even when it reads no attempts for their own sake.
func TestGitLabPullReadsRunners(t *testing.T) {
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabYAML, f.url())
	pulled, failed := pullGitLab(t, config, gitlabRunners)
	if len(failed) != 0 || len(pulled[gitlabRunners]) != 1 || len(pulled[gitlabJobAttempts]) != 0 {
		t.Fatalf("runners = %v, attempts = %d, failed = %v; want runner 1 alone", pulled[gitlabRunners], len(pulled[gitlabJobAttempts]), failed)
	}
	if asked := f.sent(http.MethodGet, "/runners/1"); len(asked) != 1 {
		t.Errorf("runner 1 was read %d times, want once, though all three projects' attempts ran on it", len(asked))
	}
	runner := pulled[gitlabRunners][0]
	for _, field := range []string{"ip_address", "token_expires_at", "uuid"} {
		if _, ok := runner[field]; ok {
			t.Errorf("runner 1 carries %s", field)
		}
	}
	if bad := nonJSON(runner); bad != "" {
		t.Errorf("runner 1 carries %s, which jq can't read", bad)
	}
	got, _ := json.Marshal(extras(runner, gitlabRunnerNames...))
	if runner["version"] != "18.6.0" || !reflect.DeepEqual(runner["tag_list"], []any{"docker"}) || runner["maximum_timeout"] != float64(3600) ||
		valueAt(runner, "created_by", "username") != "ada" || valueAt(runner, "created_by", "avatar_url") != nil ||
		string(got) != `{"groups":[],"lastJobAt":"`+gitlabT3+`","projects":[],"title":"runner 1","url":null}` {
		t.Errorf("runner 1 = %v, want it as read on its own, with when its newest attempt was", runner)
	}
}

// TestGitLabRunnerFallsBack checks a runner GitLab refuses or doesn't have
// is as its newest attempt found it, and one it fails to answer about
// fails runners alone, leaving it out.
func TestGitLabRunnerFallsBack(t *testing.T) {
	f := newGitLabFixture(t)
	_, config := gitlabSpec(t, gitlabProjectYAML("acme/api"), f.url())
	want := `{"_groups":[],"_lastJobAt":"` + gitlabT3 + `","_projects":[],"_title":"runner 1","_url":null,` +
		`"architecture":"amd64","description":"runner 1","id":1,"is_shared":true,"online":true,"platform":"linux",` +
		`"runner_type":"instance_type","status":"online","version":"18.5.0"}`
	for name, refuse := range map[string]func(){
		"not found": func() { f.answer("/runners/1", gitlabNotFound) },
		"forbidden": func() { f.deny("/runners/1") },
	} {
		refuse()
		pulled, failed := pullGitLab(t, config, gitlabRunners, gitlabJobAttempts)
		f.allow()
		if len(failed) != 0 || len(pulled[gitlabRunners]) != 1 {
			t.Errorf("%s: runners = %v, failed = %v", name, pulled[gitlabRunners], failed)
			continue
		}
		if got, _ := json.Marshal(pulled[gitlabRunners][0]); string(got) != want {
			t.Errorf("%s: runner 1 = %s\nwant %s", name, got, want)
		}
	}

	shortenGitLabWaits(t)
	f.answer("/runners/1", gitlabServerError, gitlabServerError, gitlabServerError, gitlabServerError, gitlabServerError)
	pulled, failed := pullGitLab(t, config, gitlabRunners, gitlabJobAttempts)
	if failed[gitlabRunners] == nil || failed[gitlabJobAttempts] != nil || len(pulled[gitlabRunners]) != 0 || len(pulled[gitlabJobAttempts]) == 0 {
		t.Errorf("runners = %v, failed = %v; want runners failed, without runner 1, and attempts read", pulled[gitlabRunners], failed)
	}
}

// TestGitLabRunnerObject checks a runner's title, the groups and projects it
// belongs to, and its page: in its group or the first project it was
// registered in, from where GitLab says they are, and none for an
// instance's.
func TestGitLabRunnerObject(t *testing.T) {
	seen := &gitlabRunnerSeen{
		runner:    map[string]any{"id": float64(7), "description": "", "runner_type": "group_type", "is_shared": false, "ip_address": "10.0.0.1"},
		manager:   map[string]any{"version": "18.5.0", "platform": "linux", "system_id": "r_1"},
		lastJobAt: gitlabT3,
	}
	group := map[string]any{"id": float64(10), "name": "Acme", "web_url": "https://gitlab.example.com/groups/acme/platform"}
	project := func(path string) map[string]any {
		return map[string]any{"id": float64(1), "path_with_namespace": path, "web_url": "https://code.example.com/gitlab/" + path}
	}
	for _, test := range []struct {
		name   string
		detail map[string]any
		want   string
	}{
		{
			"group",
			map[string]any{"id": float64(7), "description": "", "name": nil, "runner_type": "group_type", "groups": []any{group}, "projects": []any{}},
			`{"groups":["acme/platform"],"lastJobAt":"` + gitlabT3 + `","projects":[],"title":"Group runner 7","url":"https://gitlab.example.com/groups/acme/platform/-/runners/7"}`,
		},
		{
			"project",
			map[string]any{
				"id": float64(8), "description": " ", "name": "builder", "runner_type": "project_type", "groups": []any{},
				"projects": []any{project("acme/api"), project("octo/tools")},
			},
			`{"groups":[],"lastJobAt":"` + gitlabT3 + `","projects":["acme/api","octo/tools"],"title":"builder","url":"https://code.example.com/gitlab/acme/api/-/runners/8"}`,
		},
		{
			"instance",
			map[string]any{"id": float64(9), "description": "shared-linux ", "runner_type": "instance_type", "groups": []any{}, "projects": []any{}},
			`{"groups":[],"lastJobAt":"` + gitlabT3 + `","projects":[],"title":"shared-linux","url":null}`,
		},
		{"unread", nil, `{"groups":[],"lastJobAt":"` + gitlabT3 + `","projects":[],"title":"Group runner 7","url":null}`},
	} {
		runner := gitlabRunnerObject(test.detail, seen)
		if got, _ := json.Marshal(extras(runner, gitlabRunnerNames...)); string(got) != test.want {
			t.Errorf("%s: added %s\nwant %s", test.name, got, test.want)
		}
		if _, ok := runner["groups"]; ok {
			t.Errorf("%s: runner = %v, want its groups only by path", test.name, runner)
		}
	}
	if unread := gitlabRunnerObject(nil, seen); unread["version"] != "18.5.0" || unread["system_id"] != nil || unread["ip_address"] != nil {
		t.Errorf("unread runner = %v, want its manager's version, and neither its manager's ID nor its IP address", unread)
	}
}
