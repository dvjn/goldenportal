package httpx

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gofr.dev/pkg/gofr"
)

type recordingRegistrar struct {
	registered []string
}

func (r *recordingRegistrar) GET(pattern string, _ gofr.Handler) {
	r.record(http.MethodGet, pattern)
}

func (r *recordingRegistrar) POST(pattern string, _ gofr.Handler) {
	r.record(http.MethodPost, pattern)
}

func (r *recordingRegistrar) PUT(pattern string, _ gofr.Handler) {
	r.record(http.MethodPut, pattern)
}

func (r *recordingRegistrar) PATCH(pattern string, _ gofr.Handler) {
	r.record(http.MethodPatch, pattern)
}

func (r *recordingRegistrar) DELETE(pattern string, _ gofr.Handler) {
	r.record(http.MethodDelete, pattern)
}

func (r *recordingRegistrar) record(method, pattern string) {
	r.registered = append(r.registered, method+" "+pattern)
}

func noopHandler(*gofr.Context) (any, error) { return nil, nil }

func TestRoutesRegistersEveryMethodAndRemembersItsAccess(t *testing.T) {
	app := &recordingRegistrar{}
	routes := NewRoutes(app)

	routes.GET("/api/v1/items", Public(), noopHandler)
	routes.POST("/api/v1/items", Authenticated(), noopHandler)
	routes.PUT("/api/v1/items/{id}", Authenticated(), noopHandler)
	routes.PATCH("/api/v1/items/{id}", Authenticated(), noopHandler)
	routes.DELETE("/api/v1/items/{id}", Authenticated(), noopHandler)

	want := []string{
		"GET /api/v1/items",
		"POST /api/v1/items",
		"PUT /api/v1/items/{id}",
		"PATCH /api/v1/items/{id}",
		"DELETE /api/v1/items/{id}",
	}
	if strings.Join(app.registered, "\n") != strings.Join(want, "\n") {
		t.Fatalf("registered = %v, want %v", app.registered, want)
	}

	cases := []struct {
		method          string
		template        string
		declared        bool
		requiresSession bool
	}{
		{http.MethodGet, "/api/v1/items", true, false},
		{http.MethodPost, "/api/v1/items", true, true},
		{http.MethodDelete, "/api/v1/items/{id}", true, true},
		{http.MethodDelete, "/api/v1/items", false, false},
		{http.MethodGet, "/", false, false},
	}
	for _, tc := range cases {
		access, ok := routes.Lookup(tc.method, tc.template)
		if ok != tc.declared || access.RequiresSession() != tc.requiresSession {
			t.Errorf("Lookup(%s %s) = (session %t, %t), want (session %t, %t)",
				tc.method, tc.template, access.RequiresSession(), ok, tc.requiresSession, tc.declared)
		}
	}
}

func TestOnlyRoutesRegistersHandlersOnTheGofrApp(t *testing.T) {
	root := moduleRoot(t)
	packages := listPackages(t, root)
	exports := map[string]string{}
	for _, pkg := range packages {
		exports[pkg.importPath] = pkg.export
	}
	lookup := func(path string) (io.ReadCloser, error) { return os.Open(exports[path]) }
	allowed := filepath.Join(root, "internal", "httpx", "routes.go")
	methods := map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}

	checked := 0
	for _, pkg := range packages {
		if !pkg.inModule || len(pkg.files) == 0 {
			continue
		}
		fset := token.NewFileSet()
		files := parseFiles(t, fset, pkg)
		info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
		config := types.Config{Importer: importer.ForCompiler(fset, "gc", lookup)}
		if _, err := config.Check(pkg.importPath, fset, files, info); err != nil {
			t.Fatalf("type-check %s: %v", pkg.importPath, err)
		}
		checked++

		for selector, selection := range info.Selections {
			if !methods[selector.Sel.Name] || types.TypeString(selection.Recv(), nil) != "*gofr.dev/pkg/gofr.App" {
				continue
			}
			position := fset.Position(selector.Pos())
			if position.Filename != allowed {
				t.Errorf("%s: register routes through httpx.Routes, not gofr.App.%s", position, selector.Sel.Name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no packages were checked")
	}
}

type listedPackage struct {
	importPath string
	dir        string
	export     string
	files      []string
	inModule   bool
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	output, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}

	return filepath.Dir(strings.TrimSpace(string(output)))
}

func listPackages(t *testing.T, root string) []listedPackage {
	t.Helper()
	command := exec.Command("go", "list", "-export", "-deps",
		"-f", `{{.ImportPath}}|{{.Dir}}|{{.Export}}|{{join .GoFiles ","}}|{{with .Module}}{{.Main}}{{end}}`,
		"./cmd/...", "./internal/...")
	command.Dir = root
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}

	var packages []listedPackage
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "|")
		packages = append(packages, listedPackage{
			importPath: fields[0],
			dir:        fields[1],
			export:     fields[2],
			files:      strings.FieldsFunc(fields[3], func(r rune) bool { return r == ',' }),
			inModule:   fields[4] == "true",
		})
	}

	return packages
}

func parseFiles(t *testing.T, fset *token.FileSet, pkg listedPackage) []*ast.File {
	t.Helper()
	files := make([]*ast.File, 0, len(pkg.files))
	for _, name := range pkg.files {
		file, err := parser.ParseFile(fset, filepath.Join(pkg.dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, file)
	}

	return files
}
