package connection

import (
	"strings"
	"testing"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
)

// withSources parses a connection and the blueprints whose sources it
// reads, as if they were all published.
func withSources(t *testing.T, connectionYAML string, blueprintYAMLs ...string) Spec {
	t.Helper()
	spec := mustParse(t, connectionYAML)
	published := map[string]blueprint.Spec{}
	for _, source := range blueprintYAMLs {
		bp, issues := blueprint.Parse(source)
		if configdoc.HasErrors(issues) {
			t.Fatalf("blueprint.Parse() issues = %+v", issues)
		}
		published[bp.Identifier] = bp
	}
	spec.AddSources(published)

	return spec
}

// sourced parses documents separated by ---: a connection, then the
// blueprints whose sources it reads.
func sourced(t *testing.T, documents string) Spec {
	t.Helper()
	docs := strings.Split(documents, "---\n")

	return withSources(t, docs[0], docs[1:]...)
}
