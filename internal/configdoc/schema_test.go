package configdoc_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/gorilla/mux"
	"go.yaml.in/yaml/v3"
	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/dashboard"
	"github.com/dvjn/goldenportal/internal/integration"
	"github.com/dvjn/goldenportal/internal/scorecard"
	"github.com/dvjn/goldenportal/internal/widget"
)

var parsers = map[string]func(string) []configdoc.Issue{
	"Blueprint":   func(s string) []configdoc.Issue { _, issues := blueprint.Parse(s); return issues },
	"Integration": func(s string) []configdoc.Issue { _, issues := integration.Parse(s); return issues },
	"Scorecard":   func(s string) []configdoc.Issue { _, issues := scorecard.Parse(s); return issues },
	"Dashboard":   func(s string) []configdoc.Issue { _, issues := dashboard.Parse(s); return issues },
}

func resolved(t *testing.T, kind string) *jsonschema.Resolved {
	t.Helper()
	raw, ok := configdoc.Schema(kind)
	if !ok {
		t.Fatalf("Schema(%q) is missing", kind)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("%s schema: %v", kind, err)
	}
	rs, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("%s schema doesn't resolve: %v", kind, err)
	}

	return rs
}

func TestSchema(t *testing.T) {
	if len(parsers) != len(configdoc.SchemaKinds) {
		t.Fatalf("SchemaKinds = %v; the tests parse %d kinds", configdoc.SchemaKinds, len(parsers))
	}
	for _, kind := range configdoc.SchemaKinds {
		resolved(t, kind)
		for _, name := range []string{strings.ToLower(kind), strings.ToLower(kind) + "s", strings.ToUpper(kind)} {
			if _, ok := configdoc.Schema(name); !ok {
				t.Errorf("Schema(%q) is missing", name)
			}
		}
	}
	if _, ok := configdoc.Schema("entity"); ok {
		t.Error(`Schema("entity") is found`)
	}
}

type routes map[string]gofr.Handler

func (r routes) GET(path string, h gofr.Handler)        { r[path] = h }
func (routes) POST(string, gofr.Handler)                {}
func (routes) PUT(string, gofr.Handler)                 {}
func (routes) DELETE(string, gofr.Handler)              {}
func (routes) AddCronJob(string, string, gofr.CronFunc) {}

func TestSchemaHandlers(t *testing.T) {
	r := routes{}
	configdoc.RegisterSchemas(r)
	call := func(path string, vars map[string]string, signedIn bool) (any, error) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if signedIn {
			req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{Role: auth.RoleMember}))
		}
		req = mux.SetURLVars(req, vars)
		route := "schemas"
		if vars != nil {
			route += "/{kind}"
		}
		return r[route](&gofr.Context{Context: req.Context(), Request: gofrHTTP.NewRequest(req)})
	}

	list, err := call("/api/web/schemas", nil, true)
	if summaries, ok := list.([]configdoc.SchemaSummary); err != nil || !ok || len(summaries) != len(configdoc.SchemaKinds) || summaries[0].Description == "" {
		t.Errorf("list = %+v, %v", list, err)
	}
	got, err := call("/api/web/schemas/blueprints", map[string]string{"kind": "blueprints"}, true)
	if want, _ := configdoc.Schema("Blueprint"); err != nil || !bytes.Equal(got.(json.RawMessage), want) {
		t.Errorf("get = %v", err)
	}
	if _, err := call("/api/web/schemas/entity", map[string]string{"kind": "entity"}, true); httpStatus(err) != http.StatusNotFound {
		t.Errorf("unknown kind = %v, want not found", err)
	}
	if _, err := call("/api/web/schemas/blueprints", map[string]string{"kind": "blueprints"}, false); httpStatus(err) != http.StatusUnauthorized {
		t.Errorf("signed out = %v, want unauthorized", err)
	}
}

func httpStatus(err error) int {
	var status interface{ StatusCode() int }
	if errors.As(err, &status) {
		return status.StatusCode()
	}

	return 0
}

// TestSchemasAcceptParsedDocuments checks every example document the
// parsers accept, from the seed data and the tests, against its schema, and
// that the schema rejects those the parsers reject for an unknown field.
func TestSchemasAcceptParsedDocuments(t *testing.T) {
	sources := seedDocuments(t)
	sources = append(sources, stringLiterals(t, "..")...)
	kindPattern := regexp.MustCompile(`(?m)^kind:\s*(\w+)\s*$`)
	accepted := map[string]int{}
	for _, source := range sources {
		match := kindPattern.FindStringSubmatch(source.text)
		if match == nil {
			continue
		}
		parse, ok := parsers[match[1]]
		if !ok {
			continue
		}
		issues := parse(source.text)
		unknown := slices.ContainsFunc(issues, func(issue configdoc.Issue) bool {
			return strings.HasPrefix(issue.Message, "unknown field")
		})
		if configdoc.HasErrors(issues) && !unknown {
			continue
		}
		instance, err := toJSON(source.text)
		if err != nil {
			t.Errorf("%s: %v", source.at, err)
			continue
		}
		err = resolved(t, match[1]).Validate(instance)
		switch {
		case unknown && err == nil:
			t.Errorf("%s: the parser rejects an unknown field the %s schema allows: %+v", source.at, match[1], issues)
		case !unknown && err != nil:
			t.Errorf("%s: the parser accepts what the %s schema rejects: %v", source.at, match[1], err)
		case !unknown:
			accepted[match[1]]++
		}
	}
	for _, kind := range configdoc.SchemaKinds {
		if accepted[kind] == 0 {
			t.Errorf("no %s document was checked", kind)
		}
	}
}

type exampleSource struct{ at, text string }

func seedDocuments(t *testing.T) []exampleSource {
	t.Helper()
	data, err := os.ReadFile("../../deps/seed.sql")
	if err != nil {
		t.Fatal(err)
	}
	var sources []exampleSource
	for _, match := range regexp.MustCompile(`(?s)\$yaml\$(.*?)\$yaml\$`).FindAllStringSubmatch(string(data), -1) {
		sources = append(sources, exampleSource{"seed.sql", match[1]})
	}

	return sources
}

// stringLiterals are the string literals in the tests under dir, which hold
// the documents and widgets they parse.
func stringLiterals(t *testing.T, dir string) []exampleSource {
	t.Helper()
	var sources []exampleSource
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if text, err := strconv.Unquote(lit.Value); err == nil {
				sources = append(sources, exampleSource{fset.Position(lit.Pos()).String(), text})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return sources
}

// TestWidgetSchemaAcceptsParsedWidgets does for the widget tests' widgets
// what TestSchemasAcceptParsedDocuments does for documents, reading them on
// an entity page, where widgets can use the most.
func TestWidgetSchemaAcceptsParsedWidgets(t *testing.T) {
	root := rawSchema(t, "Blueprint")
	raw, err := json.Marshal(map[string]any{"$defs": root["$defs"], "$ref": "#/$defs/widgets"})
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	rs, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	accepted := 0
	for _, source := range stringLiterals(t, "../widget") {
		var node yaml.Node
		if yaml.Unmarshal([]byte(source.text), &node) != nil || len(node.Content) == 0 || !widgetMap(node.Content[0]) {
			continue
		}
		r := &configdoc.Reader{}
		widget.Parse(r, node.Content[0], "widgets", widget.Scope{Blueprint: "team", Entity: true})
		unknown := slices.ContainsFunc(r.Issues, func(issue configdoc.Issue) bool {
			return strings.HasPrefix(issue.Message, "unknown field") || strings.Contains(issue.Message, "no longer take")
		})
		if configdoc.HasErrors(r.Issues) && !unknown {
			continue
		}
		instance, err := toJSON(source.text)
		if err != nil {
			t.Errorf("%s: %v", source.at, err)
			continue
		}
		err = rs.Validate(instance)
		switch {
		case unknown && err == nil:
			t.Errorf("%s: the parser rejects a field the widget schema allows: %+v", source.at, r.Issues)
		case !unknown && err != nil:
			t.Errorf("%s: the parser accepts what the widget schema rejects: %v", source.at, err)
		case !unknown:
			accepted++
		}
	}
	if accepted == 0 {
		t.Error("no widgets were checked")
	}
}

// widgetMap reports whether node is a mapping of widgets keyed by identifier.
func widgetMap(node *yaml.Node) bool {
	if node.Kind != yaml.MappingNode || len(node.Content) == 0 {
		return false
	}
	for i := 1; i < len(node.Content); i += 2 {
		if configdoc.Field(node.Content[i], "type") == nil {
			return false
		}
	}

	return true
}

func toJSON(source string) (any, error) {
	var value any
	if err := yaml.Unmarshal([]byte(source), &value); err != nil {
		return nil, err
	}
	data, err := json.Marshal(withoutNulls(value))
	if err != nil {
		return nil, err
	}
	var instance any
	err = json.Unmarshal(data, &instance)

	return instance, err
}

// withoutNulls leaves out keys set to null, as the parsers do, and makes
// every key a string, as JSON's are.
func withoutNulls(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			if item != nil {
				out[key] = withoutNulls(item)
			}
		}
		return out
	case map[any]any:
		out := map[string]any{}
		for key, item := range v {
			if item != nil {
				out[fmt.Sprint(key)] = withoutNulls(item)
			}
		}
		return out
	case []any:
		for i, item := range v {
			v[i] = withoutNulls(item)
		}
	}

	return value
}

// TestSchemaFieldsMatchParsers checks each mapping the parsers read with
// Fields takes the same keys in its schema, and no others.
func TestSchemaFieldsMatchParsers(t *testing.T) {
	type mapping struct {
		kind, pointer string
		fields        []string
		// except are keys the parser takes only to explain they're wrong.
		except []string
	}
	mappings := []mapping{
		{"Blueprint", "/$defs/metadata", configdoc.MetadataFields, nil},
		{"Integration", "/properties/metadata", configdoc.MetadataFields, []string{"icon", "color"}},
		{"Blueprint", "/$defs/spec", blueprint.SpecFields, nil},
		{"Blueprint", "/$defs/property", blueprint.PropertyFields, nil},
		{"Blueprint", "/$defs/items", blueprint.ItemsFields, nil},
		{"Blueprint", "/$defs/display", blueprint.DisplayFields, []string{"column"}},
		{"Blueprint", "/$defs/catalog", blueprint.CatalogFields, nil},
		{"Blueprint", "/$defs/entity", blueprint.EntityFields, nil},
		{"Blueprint", "/$defs/header", blueprint.HeaderFields, nil},
		{"Blueprint", "/$defs/section", blueprint.SectionFields, nil},
		{"Blueprint", "/$defs/tab", widget.TabFields, nil},
		{"Blueprint", "/$defs/query", widget.QueryFields, nil},
		{"Blueprint", "/$defs/value", widget.ValueFields, nil},
		{"Blueprint", "/$defs/link", widget.LinkFields, nil},
		{"Blueprint", "/$defs/rowLink", widget.RowLinkFields, nil},
		{"Blueprint", "/$defs/column/oneOf/1", widget.ColumnFields, nil},
		{"Integration", "/$defs/spec", integration.SpecFields, nil},
		{"Integration", "/$defs/connector", integration.ConnectorFields, nil},
		{"Integration", "/$defs/sync", integration.SyncFields, nil},
		{"Integration", "/$defs/mapping", integration.MappingFields, nil},
		{"Integration", "/$defs/entityMapping", integration.EntityFields, nil},
		{"Integration", "/$defs/httpConfig", integration.HTTPConfigFields, nil},
		{"Integration", "/$defs/httpResource", integration.HTTPResourceFields, nil},
		{"Integration", "/$defs/httpResource/properties/headers/additionalProperties/oneOf/1", integration.HTTPHeaderFields, nil},
		{"Scorecard", "/$defs/spec", scorecard.SpecFields, nil},
		{"Scorecard", "/$defs/tier", scorecard.TierFields, nil},
		{"Scorecard", "/$defs/criterion", scorecard.CriterionFields, nil},
		{"Dashboard", "/$defs/spec", dashboard.SpecFields, nil},
		{"Dashboard", "/$defs/input", dashboard.InputFields, nil},
		{"Dashboard", "/$defs/defaultQuery", dashboard.DefaultFields, nil},
		{"Dashboard", "/$defs/defaultQuery/properties/query", widget.QueryFields, nil},
		{"Dashboard", "/$defs/audience", dashboard.AudienceFields, nil},
	}
	for _, kind := range configdoc.SchemaKinds {
		mappings = append(mappings, mapping{kind, "", configdoc.DocumentFields, nil})
	}
	var widgetTypes []string
	for _, widgetType := range widget.Types() {
		mappings = append(mappings, mapping{"Blueprint", "/$defs/" + widgetType + "Widget", widget.TypeFields(widgetType), nil})
	}
	for _, m := range mappings {
		root := rawSchema(t, m.kind)
		node := resolvePointer(t, root, m.pointer)
		want := slices.DeleteFunc(slices.Clone(m.fields), func(f string) bool { return slices.Contains(m.except, f) })
		slices.Sort(want)
		got := propertyNames(t, root, node)
		if !slices.Equal(got, want) {
			t.Errorf("%s %s takes %v; the parser takes %v", m.kind, m.pointer, got, want)
		}
		if !closed(t, root, node) {
			t.Errorf("%s %s allows other keys, which the parser rejects", m.kind, m.pointer)
		}
	}

	root := rawSchema(t, "Blueprint")
	for _, branch := range resolvePointer(t, root, "/$defs/widget/oneOf").([]any) {
		node := deref(t, root, branch.(map[string]any))
		widgetTypes = append(widgetTypes, node["properties"].(map[string]any)["type"].(map[string]any)["const"].(string))
	}
	slices.Sort(widgetTypes)
	if !slices.Equal(widgetTypes, widget.Types()) {
		t.Errorf("widget schemas cover %v; the parser takes %v", widgetTypes, widget.Types())
	}
}

func rawSchema(t *testing.T, kind string) map[string]any {
	t.Helper()
	raw, _ := configdoc.Schema(kind)
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}

	return root
}

func resolvePointer(t *testing.T, root any, pointer string) any {
	t.Helper()
	node := root
	for _, token := range strings.Split(pointer, "/")[1:] {
		switch v := node.(type) {
		case map[string]any:
			node = v[token]
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i >= len(v) {
				t.Fatalf("no %s in the schema", pointer)
			}
			node = v[i]
		}
		if node == nil {
			t.Fatalf("no %s in the schema", pointer)
		}
	}

	return node
}

func deref(t *testing.T, root, node map[string]any) map[string]any {
	t.Helper()
	ref, ok := node["$ref"].(string)
	if !ok {
		return node
	}

	return resolvePointer(t, root, strings.TrimPrefix(ref, "#")).(map[string]any)
}

// propertyNames lists the keys a schema takes, across the branches of a
// union, leaving out those it sets to false.
func propertyNames(t *testing.T, root map[string]any, node any) []string {
	t.Helper()
	schema, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	var names []string
	if ref, ok := schema["$ref"].(string); ok {
		names = append(names, propertyNames(t, root, resolvePointer(t, root, strings.TrimPrefix(ref, "#")))...)
	}
	for _, key := range []string{"allOf", "oneOf", "anyOf"} {
		branches, _ := schema[key].([]any)
		for _, branch := range branches {
			names = append(names, propertyNames(t, root, branch)...)
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	for name, value := range properties {
		if value != false {
			names = append(names, name)
		}
	}
	names = slices.DeleteFunc(names, func(name string) bool { return properties[name] == false })
	slices.Sort(names)

	return slices.Compact(names)
}

// closed reports whether a schema, or every branch of a union, rejects keys
// it doesn't list.
func closed(t *testing.T, root map[string]any, node any) bool {
	t.Helper()
	schema, ok := node.(map[string]any)
	if !ok {
		return false
	}
	if schema["additionalProperties"] == false {
		return true
	}
	if ref, ok := schema["$ref"].(string); ok {
		return closed(t, root, resolvePointer(t, root, strings.TrimPrefix(ref, "#")))
	}
	branches, _ := schema["oneOf"].([]any)
	for _, branch := range branches {
		if !closed(t, root, branch) {
			return false
		}
	}

	return len(branches) > 0
}
