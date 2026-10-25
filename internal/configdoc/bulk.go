package configdoc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// bulkKind is what apply, publish and export need from a kind, whatever its spec type.
type bulkKind interface {
	documentKind() string
	plan(ctx context.Context, db DB, source string) (Planned, error)
	saveDraftAs(ctx *gofr.Context, db DB, p auth.Principal, name, source string) error
	publishDraft(ctx *gofr.Context, db DB, p auth.Principal, name string, acknowledgeBreaking bool) (Published, error)
	publishedSources(ctx context.Context, db DB) (map[string]string, error)
	draftSource(ctx context.Context, db DB, name string) (string, error)
	recordPublish(ctx *gofr.Context, p auth.Principal, published Published)
}

// kinds are the registered kinds, in the order they're registered, which
// is the order they depend on each other: blueprints before integrations.
var kinds []bulkKind

func kindFor(document string) (bulkKind, bool) {
	for _, k := range kinds {
		if k.documentKind() == document {
			return k, true
		}
	}

	return nil, false
}

func kindOrder(document string) int {
	for i, k := range kinds {
		if k.documentKind() == document {
			return i
		}
	}

	return len(kinds)
}

// RegisterBulk serves apply, publish and export across every kind. Call it
// once, after registering the kinds.
func RegisterBulk(app *gofr.App) {
	app.POST("/api/v1/apply", apply)
	app.POST("/api/v1/publish", publishMany)
	app.GET("/api/v1/export", exportAll)
}

type pendingKey struct{}

// WithPending makes documents being applied or published together visible
// to each other's checks, by kind and name, as if they were published.
func WithPending(ctx context.Context, pending map[string]map[string]string) context.Context {
	return context.WithValue(ctx, pendingKey{}, pending)
}

// PendingSources returns the sources of a kind that are pending in ctx, by name.
func PendingSources(ctx context.Context, kind string) map[string]string {
	pending, _ := ctx.Value(pendingKey{}).(map[string]map[string]string)
	return pending[kind]
}

// Planned is what applying one document would do.
type Planned struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Action is create, update or unchanged, against the draft or, without
	// one, the published version.
	Action   string   `json:"action"`
	Issues   []Issue  `json:"issues"`
	Changes  []Change `json:"changes"`
	Breaking bool     `json:"breaking"`
	source   string
}

const (
	ActionCreate    = "create"
	ActionUpdate    = "update"
	ActionUnchanged = "unchanged"
)

type applyRequest struct {
	Source string `json:"source"`
}

type applied struct {
	DryRun    bool      `json:"dryRun"`
	Valid     bool      `json:"valid"`
	Documents []Planned `json:"documents"`
}

// apply checks a multi-document file as a set and, unless dryRun=true,
// saves a draft of every document that changed, all or nothing. Documents
// can refer to each other, so a blueprint and the integration that writes
// it can arrive together.
func apply(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleBuilder)
	if err != nil {
		return nil, err
	}
	var req applyRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, badRequest(`send the YAML as {"source": "..."}`)
	}
	dryRun := ctx.Param("dryRun") == "true"

	chunks := SplitDocuments(req.Source)
	if len(chunks) == 0 {
		return nil, badRequest("there are no documents to apply")
	}

	type entry struct {
		kind  bulkKind
		chunk Chunk
		name  string
	}
	entries := make([]entry, 0, len(chunks))
	pending := map[string]map[string]string{}
	result := applied{DryRun: dryRun, Valid: true, Documents: []Planned{}}
	seen := map[string]bool{}

	for _, chunk := range chunks {
		document, name := peek(chunk.Source)
		k, ok := kindFor(document)
		if !ok {
			result.Valid = false
			result.Documents = append(result.Documents, Planned{
				Kind: document, Name: name, Changes: []Change{},
				Issues: shift([]Issue{{
					Severity: SeverityError, Path: "kind", Line: 1,
					Message: fmt.Sprintf("unknown kind %q; use %s", document, strings.Join(documentKinds(), " or ")),
				}}, chunk.Line),
			})
			continue
		}
		key := document + "/" + name
		if name != "" && seen[key] {
			result.Valid = false
			result.Documents = append(result.Documents, Planned{
				Kind: document, Name: name, Changes: []Change{},
				Issues: shift([]Issue{{
					Severity: SeverityError, Path: "metadata.name", Line: 1,
					Message: fmt.Sprintf("%s %s is in the file twice", document, name),
				}}, chunk.Line),
			})
			continue
		}
		seen[key] = true
		entries = append(entries, entry{kind: k, chunk: chunk, name: name})
		if name != "" {
			if pending[document] == nil {
				pending[document] = map[string]string{}
			}
			pending[document][name] = chunk.Source
		}
	}

	checkCtx := WithPending(ctx, pending)
	for _, e := range entries {
		planned, err := e.kind.plan(checkCtx, ctx.SQL, e.chunk.Source)
		if err != nil {
			return nil, err
		}
		planned.Issues = shift(planned.Issues, e.chunk.Line)
		if HasErrors(planned.Issues) {
			result.Valid = false
		}
		result.Documents = append(result.Documents, planned)
	}

	if dryRun {
		return result, nil
	}
	if !result.Valid {
		return nil, httpx.Error{
			Code:    http.StatusUnprocessableEntity,
			Message: "some documents have problems; apply with dryRun=true to see them",
		}
	}

	tx, err := ctx.SQL.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	for i, e := range entries {
		if result.Documents[i].Action == ActionUnchanged {
			continue
		}
		if err := e.kind.saveDraftAs(ctx, tx, p, result.Documents[i].Name, e.chunk.Source); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return result, nil
}

// peek reads a document's kind and name, to route it before parsing it fully.
func peek(source string) (kind, name string) {
	root, _ := ParseYAML(source)
	if root == nil {
		return "", ""
	}
	var envelope struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}
	_ = root.Decode(&envelope)

	return envelope.Kind, envelope.Metadata.Name
}

// shift moves issue lines from a document's own lines to the whole file's.
func shift(issues []Issue, line int) []Issue {
	for i := range issues {
		if issues[i].Line > 0 {
			issues[i].Line += line - 1
		}
	}

	return issues
}

func documentKinds() []string {
	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, k.documentKind())
	}

	return names
}

// DocumentRef names a document.
type DocumentRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type publishManyRequest struct {
	Documents           []DocumentRef `json:"documents"`
	AcknowledgeBreaking bool          `json:"acknowledgeBreaking"`
}

// Published is a document that was published, and its new version.
type Published struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Version  int    `json:"version"`
	Breaking bool   `json:"breaking"`
	title    string
	changes  int
}

// publishMany publishes several drafts in one transaction, blueprints before
// the integrations that write them. Their drafts see each other when checked.
func publishMany(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleBuilder)
	if err != nil {
		return nil, err
	}
	var req publishManyRequest
	if err := ctx.Bind(&req); err != nil || len(req.Documents) == 0 {
		return nil, badRequest(`send the drafts to publish as {"documents": [{"kind": "Blueprint", "name": "service"}]}`)
	}

	refs := append([]DocumentRef(nil), req.Documents...)
	for _, ref := range refs {
		if _, ok := kindFor(ref.Kind); !ok {
			return nil, badRequest(fmt.Sprintf("unknown kind %q; use %s", ref.Kind, strings.Join(documentKinds(), " or ")))
		}
	}
	sortRefs(refs)

	pending := map[string]map[string]string{}
	for _, ref := range refs {
		k, _ := kindFor(ref.Kind)
		source, err := k.draftSource(ctx, ctx.SQL, ref.Name)
		if err != nil {
			return nil, err
		}
		if pending[ref.Kind] == nil {
			pending[ref.Kind] = map[string]string{}
		}
		pending[ref.Kind][ref.Name] = source
	}

	tx, err := ctx.SQL.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	checkCtx := *ctx
	checkCtx.Context = WithPending(ctx.Context, pending)
	out := make([]Published, 0, len(refs))
	for _, ref := range refs {
		k, _ := kindFor(ref.Kind)
		published, err := k.publishDraft(&checkCtx, tx, p, ref.Name, req.AcknowledgeBreaking)
		if err != nil {
			return nil, err
		}
		out = append(out, published)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, published := range out {
		k, _ := kindFor(published.Kind)
		k.recordPublish(ctx, p, published)
	}

	return out, nil
}

func sortRefs(refs []DocumentRef) {
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && kindOrder(refs[j].Kind) < kindOrder(refs[j-1].Kind); j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

// exportAll returns every published document as one multi-document YAML
// file, in dependency order, so applying it rebuilds the configuration.
func exportAll(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}

	var documents []string
	for _, k := range kinds {
		sources, err := k.publishedSources(ctx, ctx.SQL)
		if err != nil {
			return nil, err
		}
		for _, name := range sortedKeys(sources) {
			documents = append(documents, strings.TrimRight(sources[name], "\n")+"\n")
		}
	}

	return response.File{Content: []byte(strings.Join(documents, "---\n")), ContentType: "application/yaml"}, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}

	return keys
}

// sameSpec reports whether two specs mean the same, whatever their formatting.
func sameSpec(a, b any) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)

	return errA == nil && errB == nil && string(ja) == string(jb)
}
