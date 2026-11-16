package migrations

import (
	"slices"
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

func gpBlueprint(identifier, project string) string {
	return `kind: Blueprint
metadata: {identifier: pipeline, title: Pipeline, description: Reads ._gp.project as people wrote it}
spec:
  properties:
    repo: {type: string, description: 'from ._gp.project'}
  sources:
    # Every pipeline.
    - connection: gitlab
      resource: pipelines
      filter: '._gp.project != null'
      entity:
        identifier: ` + identifier + `
        title: ._gp.project
        properties:
          repo: ` + project + `
`
}

func TestRewriteGP(t *testing.T) {
	for _, test := range []struct{ identifier, project, wantIdentifier, wantProject string }{
		{`._gp.project + "#" + (.id | tostring)`, `._gp.project`, `._project + "#" + (.id | tostring)`, `._project`},
		{`.id`, `'._gp["project"]'`, `.id`, `._project`},
		{`.id`, `"._gp[\"project\"]"`, `.id`, `._project`},
		{`.id`, `'._gp."project"'`, `.id`, `._project`},
		{`.id`, `'.["_gp"].project'`, `.id`, `._project`},
		{`.id`, `'.["_gp"]["project"]'`, `.id`, `._project`},
		{`.id`, `._gp?.project`, `.id`, `._project?`},
		{`.id`, `._gp.project?`, `.id`, `._project?`},
		{`.id`, `._gp.latestPipeline.status`, `.id`, `._latestPipeline.status`},
		{`.id`, `'[._gp.ciConfig.jobs[]?.stage] | unique'`, `.id`, `[._ciConfig.jobs[]?.stage] | unique`},
		{`.id`, `'"\(._gp.project)/\(.ref)"'`, `.id`, `"\(._project)/\(.ref)"`},
		{`.id`, `|-` + "\n            ._gp.project\n            | ascii_downcase", `.id`, "._project\n| ascii_downcase"},
		// Paths further into the item, and _gp in strings, stay.
		{`.id`, `'.labels["_gp"]["x"]'`, `.id`, `.labels["_gp"]["x"]`},
		{`.id`, `.x._gp.y`, `.id`, `.x._gp.y`},
		{`.id`, `'"._gp.project \(._gp.project)"'`, `.id`, `"._gp.project \(._project)"`},
	} {
		var left []string
		got, err := rewriteGP(gpBlueprint(test.identifier, test.project), func(_, expression string) { left = append(left, expression) })
		if err != nil {
			t.Fatal(err)
		}
		spec, issues := blueprint.Parse(got)
		if configdoc.HasErrors(issues) || len(left) != 0 {
			t.Errorf("%s: issues %+v, left %q in\n%s", test.project, issues, left, got)
			continue
		}
		s := spec.Sources[0]
		if s.Entity.Identifier != test.wantIdentifier || s.Entity.Properties[0].Expression != test.wantProject ||
			s.Filter != "._project != null" || s.Entity.Title != "._project" {
			t.Errorf("%s: identifier %q, repo %q, want %q, %q in\n%s", test.project, s.Entity.Identifier, s.Entity.Properties[0].Expression,
				test.wantIdentifier, test.wantProject, got)
		}
		for _, kept := range []string{"# Every pipeline.", "Reads ._gp.project as people wrote it", "from ._gp.project"} {
			if !strings.Contains(got, kept) {
				t.Errorf("%s: lost %q in\n%s", test.project, kept, got)
			}
		}
	}
}

func TestRewriteGPLeavesWhatItCant(t *testing.T) {
	for _, project := range []string{
		`'._gp | .project'`,
		`'._gp as $x | $x.project'`,
		`'._gp | keys | join(",")'`,
		`'.["_gp"] | length'`,
	} {
		var left []string
		source := strings.ReplaceAll(gpBlueprint(".id", project), "._gp.project", "._project")
		got, err := rewriteGP(source, func(identifier, expression string) {
			left = append(left, identifier+": "+expression)
		})
		if err != nil || got != source {
			t.Errorf("%s: got %q, %v, want it as it was", project, got, err)
		}
		if want := "pipeline: " + strings.Trim(project, "'"); !slices.Equal(left, []string{want}) {
			t.Errorf("%s: left %q, want %q", project, left, want)
		}
	}
}

func TestRewriteGPRewritesWhatItCanBesideWhatItCant(t *testing.T) {
	var left []string
	got, err := rewriteGP(gpBlueprint("._gp.project", `'._gp.project + (._gp | keys | join(","))'`),
		func(_, expression string) { left = append(left, expression) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "identifier: ._project\n") || !slices.Equal(left, []string{`._project + (._gp | keys | join(","))`}) {
		t.Errorf("left %q in\n%s", left, got)
	}
}

func TestRewriteGPUnchanged(t *testing.T) {
	plain := strings.ReplaceAll(gpBlueprint(".id", "._project"), "'._gp.project != null'", ".id != null")
	plain = strings.Replace(plain, "title: ._gp.project", "title: .ref", 1)
	for _, text := range []string{
		// Prose names _gp, but no source reads it.
		plain,
		strings.Replace(plain, "repo: ._project", "repo: '.name | test(\"_gp\")'", 1),
		strings.Replace(plain, "repo: ._project", `repo: '"\(.id) _gp"'`, 1),
		strings.Replace(plain, "repo: ._project", "repo: ._gpx.project", 1),
		strings.Replace(plain, "repo: ._project", "repo: .my_gp.project", 1),
		strings.Replace(plain, "repo: ._project", "repo: '$x._gp.project'", 1),
		// Only blueprints have sources.
		"kind: Dashboard\nmetadata: {identifier: a, title: A}\nspec:\n  sources:\n    - entity: {identifier: ._gp.project}\n",
		"kind: Connection\nmetadata: {identifier: a, title: A}\nspec: {}\n",
		"not: [valid",
	} {
		got, err := rewriteGP(text, func(_, expression string) { t.Errorf("left %q", expression) })
		if err != nil || got != text {
			t.Errorf("got %q, %v, want %q", got, err, text)
		}
	}
}
