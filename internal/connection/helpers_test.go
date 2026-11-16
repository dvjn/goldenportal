package connection

import (
	"encoding/json"
	"slices"
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

// jsonKeys adds the keys of every object in v, at any depth, as JSON gives
// them.
func jsonKeys(into map[string]bool, v any) {
	b, _ := json.Marshal(v)
	var decoded any
	_ = json.Unmarshal(b, &decoded)
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for key, value := range v {
				into[key] = true
				walk(value)
			}
		case []any:
			for _, value := range v {
				walk(value)
			}
		}
	}
	walk(decoded)
}

// checkAdded checks each item carries, of the fields starting with _ that
// the system didn't serve, exactly those added names its resource gets,
// and that the system serves none of the added names itself.
func checkAdded(t *testing.T, added map[string][]string, items map[string][]map[string]any, served map[string]bool) {
	t.Helper()
	for resource, names := range added {
		for _, name := range names {
			if served["_"+name] {
				t.Errorf("%s: the system gives _%s itself, which the portal adds", resource, name)
			}
		}
	}
	for resource, objects := range items {
		want := []string{}
		for _, name := range added[resource] {
			want = append(want, "_"+name)
		}
		slices.Sort(want)
		for _, object := range objects {
			got := []string{}
			for key := range object {
				if strings.HasPrefix(key, "_") && !served[key] {
					got = append(got, key)
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("%s %v carries %v, want %v", resource, object["id"], got, want)
			}
		}
	}
}
