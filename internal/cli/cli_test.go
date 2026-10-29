package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const team = "kind: Blueprint\nidentifier: team\ntitle: Team\n"

type result struct {
	code int
	out  string
	err  string
}

func run(t *testing.T, input string, env map[string]string, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(args, IO{In: strings.NewReader(input), Out: &out, Err: &errOut, Getenv: func(name string) string { return env[name] }})

	return result{code: code, out: out.String(), err: errOut.String()}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func TestValidateTheExamples(t *testing.T) {
	got := run(t, "", nil, "validate", "-f", "../../examples/definitions")

	if got.code != 0 || !strings.Contains(got.out, "6 definitions in 2 files, all valid") {
		t.Fatalf("validate = %+v", got)
	}
}

func TestValidateReportsEachProblemAtItsFileAndLine(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"team.yaml":          team,
		"nested/service.yml": "kind: Blueprint\nidentifier: service\ntitle: Service\nrelations:\n  owner:\n    title: Owner\n    target: group\n",
		"notes.txt":          "not a definition",
	})

	got := run(t, "", nil, "validate", "-f", dir)

	want := filepath.Join(dir, "nested/service.yml") + `:7:13: Blueprint service: relations.owner.target: no blueprint has the identifier "group"`
	if got.code != 1 || !strings.Contains(got.err, want) || !strings.Contains(got.err, "found 1 problem") {
		t.Fatalf("validate = %+v, want %s", got, want)
	}

	twice := writeFiles(t, map[string]string{"a.yaml": team, "b.yaml": team})
	got = run(t, "", nil, "validate", "-f", twice)
	want = filepath.Join(twice, "b.yaml") + ":1:5: Blueprint team: also defined in " + filepath.Join(twice, "a.yaml")
	if got.code != 1 || !strings.Contains(got.err, want) {
		t.Fatalf("validate of a duplicate = %+v, want %s", got, want)
	}
}

func TestUnknownCommandPrintsTheUsage(t *testing.T) {
	if got := run(t, "", nil, "serve"); got.code != 2 || !strings.Contains(got.err, "Usage:") {
		t.Fatalf("run = %+v", got)
	}
	if got := run(t, "", nil, "validate"); got.code != 2 {
		t.Fatalf("validate without -f = %+v", got)
	}
}

// portal answers the plan and apply calls the way the API does.
type portal struct {
	planned string
	applied []map[string]string
}

func (p *portal) serve(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gpt_ci" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"authentication required"}}`)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/api/v1/definitions/plan":
			p.planned = body["source"]
			_, _ = io.WriteString(w, `{"data":{"id":"plan-1","summary":{"added":1,"changed":1,"removed":0},
				"changes":[{"kind":"Blueprint","identifier":"team","action":"add"},
					{"kind":"Blueprint","identifier":"service","action":"change","fields":[{"path":"properties.tier.enum","before":["1"],"after":["1","2"]}]}],
				"impact":{"deleted_entities":[],"cleared_values":[{"blueprint":"service","name":"tier","count":3}],"dropped_links":[]}}}`)
		case "/api/v1/definitions/apply":
			p.applied = append(p.applied, body)
			_, _ = io.WriteString(w, `{"data":{"number":7}}`)
		case "/api/v1/definitions/export":
			_, _ = io.WriteString(w, `{"data":{"revision":7,"yaml":"kind: Blueprint\n"}}`)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	}))
}

func TestApplyPlansTheFilesAndAppliesAfterConfirmation(t *testing.T) {
	p := &portal{}
	server := p.serve(t)
	defer server.Close()
	env := map[string]string{"GOLDPORT_URL": server.URL + "/", "GOLDPORT_TOKEN": "gpt_ci"}
	dir := writeFiles(t, map[string]string{"a.yaml": team, "b.yaml": strings.Replace(team, "team", "group", 2)})

	declined := run(t, "no\n", env, "apply", "-f", dir)
	if declined.code != 0 || len(p.applied) != 0 || !strings.Contains(declined.out, "Nothing applied.") {
		t.Fatalf("declined apply = %+v", declined)
	}
	if p.planned != team+"---\n"+strings.Replace(team, "team", "group", 2) {
		t.Fatalf("planned source = %q, want both files as one source", p.planned)
	}
	for _, line := range []string{
		"Plan: 1 to add, 1 to change, 0 to remove.",
		"+ Blueprint team",
		`    properties.tier.enum: ["1"] -> ["1","2"]`,
		"  3 values of service.tier cleared",
	} {
		if !strings.Contains(declined.out, line) {
			t.Errorf("output =\n%s\nwant %q", declined.out, line)
		}
	}

	confirmed := run(t, "yes\n", env, "apply", "-f", dir, "-m", "from CI")
	if confirmed.code != 0 || !strings.Contains(confirmed.out, "Applied revision 7.") {
		t.Fatalf("confirmed apply = %+v", confirmed)
	}
	if run(t, "", env, "apply", "-f", dir, "--yes").code != 0 || len(p.applied) != 2 || p.applied[0]["message"] != "from CI" {
		t.Fatalf("applied = %+v", p.applied)
	}
}

func TestApplyNeedsTheServerAndAToken(t *testing.T) {
	p := &portal{}
	server := p.serve(t)
	defer server.Close()
	dir := writeFiles(t, map[string]string{"a.yaml": team})

	if got := run(t, "", nil, "apply", "-f", dir); got.code != 1 || !strings.Contains(got.err, "set GOLDPORT_URL") {
		t.Fatalf("apply without env = %+v", got)
	}
	got := run(t, "", map[string]string{"GOLDPORT_URL": server.URL, "GOLDPORT_TOKEN": "wrong"}, "apply", "-f", dir)
	if got.code != 1 || !strings.Contains(got.err, "authentication required") {
		t.Fatalf("apply with a wrong token = %+v", got)
	}
}

func TestExportWritesTheYAML(t *testing.T) {
	p := &portal{}
	server := p.serve(t)
	defer server.Close()
	env := map[string]string{"GOLDPORT_URL": server.URL, "GOLDPORT_TOKEN": "gpt_ci"}
	output := filepath.Join(t.TempDir(), "definitions.yaml")

	if got := run(t, "", env, "export", "-o", output); got.code != 0 || !strings.Contains(got.err, "Wrote revision 7") {
		t.Fatalf("export = %+v", got)
	}
	if text, _ := os.ReadFile(output); string(text) != "kind: Blueprint\n" {
		t.Fatalf("exported = %q", text)
	}
	if got := run(t, "", env, "export", "-o", "-"); got.out != "kind: Blueprint\n" {
		t.Fatalf("export to stdout = %+v", got)
	}
}
