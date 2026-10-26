package configdoc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/dvjn/goldenportal/internal/audit"
	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/httpx"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

// ValidIdentifier reports whether s can name a document.
func ValidIdentifier(s string) bool { return identifierPattern.MatchString(s) }

// Register serves the documents of kind under /api/v1/<plural>. Members can
// read them; builders can change them.
func Register[S Spec](app *gofr.App, kind Kind[S]) {
	h := handlers[S]{kind: kind}
	kinds = append(kinds, h)
	base := "/api/v1/" + kind.Plural
	app.GET(base, h.list)
	app.POST(base+"/validate", h.validate)
	app.GET(base+"/{identifier}", h.get)
	app.DELETE(base+"/{identifier}", h.remove)
	app.PUT(base+"/{identifier}/draft", h.saveDraft)
	app.DELETE(base+"/{identifier}/draft", h.discardDraft)
	app.GET(base+"/{identifier}/draft/impact", h.impact)
	app.POST(base+"/{identifier}/draft/publish", h.publish)
	app.GET(base+"/{identifier}/versions", h.versions)
	app.GET(base+"/{identifier}/versions/{version}", h.version)
	app.GET(base+"/{identifier}/export", h.export)
}

type handlers[S Spec] struct{ kind Kind[S] }

func (h handlers[S]) store(ctx *gofr.Context) Store { return NewStore(ctx.SQL, h.kind.Name) }

type summary struct {
	Identifier string `json:"identifier"`
	Meta
	PublishedVersion int            `json:"publishedVersion"`
	HasDraft         bool           `json:"hasDraft"`
	Stats            map[string]int `json:"stats"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

func (h handlers[S]) list(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	records, err := h.store(ctx).List(ctx)
	if err != nil {
		return nil, err
	}

	summaries := make([]summary, 0, len(records))
	for _, record := range records {
		if !h.canRead(ctx, record) {
			continue
		}
		spec := h.current(record)
		p, _ := auth.FromContext(ctx)
		if h.kind.Readable != nil && !p.Can(auth.RoleBuilder) {
			spec, _ = h.kind.Parse(record.PublishedSource)
			record.DraftSource = ""
		}
		updated := record.PublishedAt
		if record.HasDraft() {
			updated = record.DraftUpdatedAt
		}
		summaries = append(summaries, summary{
			Identifier: record.Identifier, Meta: h.meta(record, spec),
			PublishedVersion: record.PublishedVersion, HasDraft: record.HasDraft(),
			Stats: spec.Stats(), UpdatedAt: updated,
		})
	}

	return summaries, nil
}

// current is the spec the document shows: the draft while there is one,
// since that's what builders are working on, and otherwise the published one.
func (h handlers[S]) current(record Record) S {
	source := record.PublishedSource
	if record.HasDraft() {
		source = record.DraftSource
	}
	spec, _ := h.kind.Parse(source)

	return spec
}

func (h handlers[S]) meta(record Record, spec S) Meta {
	meta := spec.Meta()
	if meta.Title == "" {
		meta.Title = record.Identifier
	}

	return meta
}

type sourceRequest struct {
	Source string `json:"source"`
}

type validation[S Spec] struct {
	Spec   S       `json:"spec"`
	Issues []Issue `json:"issues"`
}

func (h handlers[S]) bindSource(ctx *gofr.Context) (string, error) {
	var req sourceRequest
	if err := ctx.Bind(&req); err != nil {
		return "", badRequest(`send the ` + h.kind.Name + ` YAML as {"source": "..."}`)
	}

	return req.Source, nil
}

// validate checks a source without saving it.
func (h handlers[S]) validate(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleBuilder); err != nil {
		return nil, err
	}
	source, err := h.bindSource(ctx)
	if err != nil {
		return nil, err
	}
	spec, issues, err := h.check(ctx, source)
	if err != nil {
		return nil, err
	}

	return validation[S]{Spec: spec, Issues: issues}, nil
}

// check parses source and runs the kind's cross-document checks.
func (h handlers[S]) check(ctx *gofr.Context, source string) (S, []Issue, error) {
	spec, issues := h.kind.Parse(source)
	if HasErrors(issues) || h.kind.Check == nil {
		return spec, orEmpty(issues), nil
	}
	more, err := h.kind.Check(ctx, ctx.SQL, spec)
	if err != nil {
		return spec, nil, err
	}

	return spec, orEmpty(append(issues, more...)), nil
}

type publishedView[S Spec] struct {
	Version     int       `json:"version"`
	Source      string    `json:"source"`
	Spec        S         `json:"spec"`
	PublishedAt time.Time `json:"publishedAt"`
	PublishedBy string    `json:"publishedBy"`
}

type draftView[S Spec] struct {
	Source    string    `json:"source"`
	Spec      S         `json:"spec"`
	Issues    []Issue   `json:"issues"`
	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy string    `json:"updatedBy"`
}

type detail[S Spec] struct {
	Identifier string `json:"identifier"`
	Meta
	Published *publishedView[S] `json:"published"`
	Draft     *draftView[S]     `json:"draft"`
	CreatedAt time.Time         `json:"createdAt"`
}

func (h handlers[S]) get(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	record, err := h.load(ctx)
	if err != nil {
		return nil, err
	}

	p, _ := auth.FromContext(ctx)
	if h.kind.Readable != nil && !p.Can(auth.RoleBuilder) {
		record.DraftSource = ""
	}
	out := detail[S]{Identifier: record.Identifier, Meta: h.meta(record, h.current(record)), CreatedAt: record.CreatedAt}
	if record.Published() {
		published, _ := h.kind.Parse(record.PublishedSource)
		out.Published = &publishedView[S]{
			Version: record.PublishedVersion, Source: record.PublishedSource,
			Spec: published, PublishedAt: record.PublishedAt, PublishedBy: record.PublishedBy,
		}
	}
	if record.HasDraft() {
		draft, issues, err := h.check(ctx, record.DraftSource)
		if err != nil {
			return nil, err
		}
		out.Draft = &draftView[S]{
			Source: record.DraftSource, Spec: draft, Issues: issues,
			UpdatedAt: record.DraftUpdatedAt, UpdatedBy: record.DraftUpdatedBy,
		}
	}

	return out, nil
}

type saved struct {
	Created bool    `json:"created"`
	Issues  []Issue `json:"issues"`
}

// saveDraft stores a valid source as the draft, creating the document if
// it's new. Warnings don't stop it.
func (h handlers[S]) saveDraft(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleBuilder)
	if err != nil {
		return nil, err
	}
	identifier, err := h.identifierParam(ctx)
	if err != nil {
		return nil, err
	}
	source, err := h.bindSource(ctx)
	if err != nil {
		return nil, err
	}

	spec, issues, err := h.check(ctx, source)
	if err != nil {
		return nil, err
	}
	if err := h.invalid(issues); err != nil {
		return nil, err
	}
	if spec.Key() != identifier {
		return nil, httpx.Error{
			Code:    http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("the YAML's identifier is %q, not %q; identifiers can't change", spec.Key(), identifier),
		}
	}

	created, err := h.store(ctx).SaveDraft(ctx, identifier, source, p.ID)
	if err != nil {
		return nil, err
	}
	auth.Record(ctx, p, h.kind.Action(OpDraftSave), h.target(identifier, spec), map[string]any{"created": created})

	return saved{Created: created, Issues: issues}, nil
}

// discardDraft drops the draft, and the document too if it was never published.
func (h handlers[S]) discardDraft(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleBuilder)
	if err != nil {
		return nil, err
	}
	record, err := h.load(ctx)
	if err != nil {
		return nil, err
	}

	deleted, err := h.store(ctx).DiscardDraft(ctx, record.Identifier)
	if errors.Is(err, ErrNoDraft) {
		return nil, httpx.Error{Code: http.StatusNotFound, Message: "this " + h.kind.Name + " has no draft"}
	}
	if err != nil {
		return nil, err
	}
	auth.Record(ctx, p, h.kind.Action(OpDraftDiscard), h.target(record.Identifier, h.current(record)),
		map[string]any{"deleted": deleted})

	return nil, nil
}

type impactView struct {
	FirstVersion bool     `json:"firstVersion"`
	Changes      []Change `json:"changes"`
	Breaking     bool     `json:"breaking"`
	// Affected counts the data the breaking changes reach, such as entities.
	Affected int     `json:"affected"`
	Issues   []Issue `json:"issues"`
}

// impact previews what publishing the draft would change.
func (h handlers[S]) impact(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleBuilder); err != nil {
		return nil, err
	}
	record, err := h.load(ctx)
	if err != nil {
		return nil, err
	}
	view, _, err := h.draftImpact(ctx, record)
	if err != nil {
		return nil, err
	}

	return view, nil
}

func (h handlers[S]) draftImpact(ctx *gofr.Context, record Record) (impactView, S, error) {
	var zero S
	if !record.HasDraft() {
		return impactView{}, zero, httpx.Error{Code: http.StatusNotFound, Message: "this " + h.kind.Name + " has no draft"}
	}
	draft, issues, err := h.check(ctx, record.DraftSource)
	if err != nil {
		return impactView{}, zero, err
	}

	view := impactView{FirstVersion: !record.Published(), Issues: issues, Changes: []Change{}}
	if record.Published() && h.kind.Diff != nil {
		published, _ := h.kind.Parse(record.PublishedSource)
		view.Changes = h.kind.Diff(published, draft)
		view.Breaking = Breaking(view.Changes)
	}
	if view.Breaking && h.kind.Affected != nil {
		if view.Affected, err = h.kind.Affected(ctx, ctx.SQL, record.Identifier); err != nil {
			return impactView{}, zero, err
		}
	}

	return view, draft, nil
}

type publishRequest struct {
	AcknowledgeBreaking bool `json:"acknowledgeBreaking"`
}

type published struct {
	Version int `json:"version"`
}

// publish makes the draft the next version. Breaking changes need
// acknowledgeBreaking, so scripts can't break data by accident.
func (h handlers[S]) publish(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleBuilder)
	if err != nil {
		return nil, err
	}
	identifier, err := h.identifierParam(ctx)
	if err != nil {
		return nil, err
	}
	var req publishRequest
	// An empty body means no acknowledgement.
	_ = ctx.Bind(&req)

	tx, err := ctx.SQL.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	out, err := h.publishDraft(ctx, tx, p, identifier, req.AcknowledgeBreaking)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	h.recordPublish(ctx, p, out)

	return published{Version: out.Version}, nil
}

// publishDraft checks a draft and publishes it within tx. Checks see what
// ctx has pending, so drafts published together can refer to each other.
func (h handlers[S]) publishDraft(ctx *gofr.Context, tx DB, p auth.Principal, identifier string, acknowledgeBreaking bool) (Published, error) {
	record, err := NewStore(ctx.SQL, h.kind.Name).Get(ctx, identifier)
	if errors.Is(err, ErrNotFound) {
		return Published{}, h.notFound()
	}
	if err != nil {
		return Published{}, err
	}
	view, spec, err := h.draftImpact(ctx, record)
	if err != nil {
		return Published{}, err
	}
	if err := h.invalid(view.Issues); err != nil {
		return Published{}, err
	}
	if view.Breaking && !acknowledgeBreaking {
		return Published{}, httpx.Error{
			Code:    http.StatusConflict,
			Message: identifier + " has breaking changes; review the impact and publish with acknowledgeBreaking",
		}
	}

	v, err := NewStore(tx, h.kind.Name).Publish(ctx, record.Identifier, record.DraftSource, p.ID)
	if errors.Is(err, ErrDraftChanged) {
		return Published{}, httpx.Error{
			Code:    http.StatusConflict,
			Message: "the draft of " + identifier + " changed while publishing; review it and try again",
		}
	}
	if err != nil {
		return Published{}, err
	}
	if h.kind.Published != nil {
		var previous *S
		if record.Published() {
			spec, _ := h.kind.Parse(record.PublishedSource)
			previous = &spec
		}
		if err := h.kind.Published(ctx, tx, previous, spec); err != nil {
			return Published{}, err
		}
	}

	return Published{
		Kind: h.kind.Document, Name: identifier, Version: v, Breaking: view.Breaking,
		title: h.target(identifier, spec).Label, changes: len(view.Changes),
	}, nil
}

func (h handlers[S]) recordPublish(ctx *gofr.Context, p auth.Principal, out Published) {
	auth.Record(ctx, p, h.kind.Action(OpPublish), audit.Target{Type: h.kind.Name, ID: out.Name, Label: out.title},
		map[string]any{"version": out.Version, "changes": out.changes, "breaking": out.Breaking})
}

func (h handlers[S]) documentKind() string { return h.kind.Document }

// plan works out what applying source would do, checking it against
// whatever ctx has pending.
func (h handlers[S]) plan(ctx context.Context, db DB, source string) (Planned, error) {
	spec, issues := h.kind.Parse(source)
	out := Planned{Kind: h.kind.Document, Name: spec.Key(), Changes: []Change{}, source: source}
	if !HasErrors(issues) && h.kind.Check != nil {
		more, err := h.kind.Check(ctx, db, spec)
		if err != nil {
			return Planned{}, err
		}
		issues = append(issues, more...)
	}
	out.Issues = orEmpty(issues)
	if HasErrors(issues) {
		return out, nil
	}

	record, err := NewStore(db, h.kind.Name).Get(ctx, spec.Key())
	switch {
	case errors.Is(err, ErrNotFound):
		out.Action = ActionCreate
		return out, nil
	case err != nil:
		return Planned{}, err
	}
	current, _ := h.kind.Parse(h.currentSource(record))
	out.Action = ActionUpdate
	if sameSpec(current, spec) {
		out.Action = ActionUnchanged
	}
	if record.Published() && h.kind.Diff != nil {
		published, _ := h.kind.Parse(record.PublishedSource)
		out.Changes = h.kind.Diff(published, spec)
		out.Breaking = Breaking(out.Changes)
	}

	return out, nil
}

func (h handlers[S]) currentSource(record Record) string {
	if record.HasDraft() {
		return record.DraftSource
	}

	return record.PublishedSource
}

func (h handlers[S]) saveDraftAs(ctx *gofr.Context, db DB, p auth.Principal, name, source string) error {
	created, err := NewStore(db, h.kind.Name).SaveDraft(ctx, name, source, p.ID)
	if err != nil {
		return err
	}
	spec, _ := h.kind.Parse(source)
	auth.Record(ctx, p, h.kind.Action(OpDraftSave), h.target(name, spec), map[string]any{"created": created, "applied": true})

	return nil
}

func (h handlers[S]) publishedSources(ctx context.Context, db DB) (map[string]string, error) {
	sources, err := NewStore(db, h.kind.Name).PublishedSources(ctx)
	if err != nil {
		return nil, err
	}
	p, _ := auth.FromContext(ctx)
	if h.kind.Readable != nil && !p.Can(auth.RoleBuilder) {
		for name, source := range sources {
			spec, issues := h.kind.Parse(source)
			if HasErrors(issues) || !h.kind.Readable(spec, p) {
				delete(sources, name)
			}
		}
	}
	return sources, nil
}

func (h handlers[S]) draftSource(ctx context.Context, db DB, name string) (string, error) {
	record, err := NewStore(db, h.kind.Name).Get(ctx, name)
	if errors.Is(err, ErrNotFound) {
		return "", httpx.Error{Code: http.StatusNotFound, Message: "no such " + h.kind.Name + " " + name}
	}
	if err != nil {
		return "", err
	}
	if !record.HasDraft() {
		return "", httpx.Error{Code: http.StatusConflict, Message: name + " has no draft to publish"}
	}

	return record.DraftSource, nil
}

func (h handlers[S]) versions(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	record, err := h.load(ctx)
	if err != nil {
		return nil, err
	}

	return h.store(ctx).Versions(ctx, record.Identifier)
}

func (h handlers[S]) version(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	record, err := h.load(ctx)
	if err != nil {
		return nil, err
	}
	identifier := record.Identifier
	n, err := strconv.Atoi(ctx.PathParam("version"))
	if err != nil || n < 1 {
		return nil, httpx.Error{Code: http.StatusNotFound, Message: "no such version"}
	}

	v, err := h.store(ctx).Version(ctx, identifier, n)
	if errors.Is(err, ErrNotFound) {
		return nil, httpx.Error{Code: http.StatusNotFound, Message: "no such version"}
	}

	if err == nil && h.kind.Readable != nil {
		p, _ := auth.FromContext(ctx)
		spec, issues := h.kind.Parse(v.Source)
		if !p.Can(auth.RoleBuilder) && (HasErrors(issues) || !h.kind.Readable(spec, p)) {
			return nil, h.notFound()
		}
	}
	return v, err
}

// export returns the published YAML as written, or the draft with ?draft=true.
func (h handlers[S]) export(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleMember); err != nil {
		return nil, err
	}
	record, err := h.load(ctx)
	if err != nil {
		return nil, err
	}

	source := record.PublishedSource
	if ctx.Param("draft") == "true" {
		p, _ := auth.FromContext(ctx)
		if h.kind.Readable != nil && !p.Can(auth.RoleBuilder) {
			return nil, h.notFound()
		}
		source = record.DraftSource
	}
	if source == "" {
		return nil, httpx.Error{Code: http.StatusNotFound, Message: "there is nothing to export yet"}
	}

	return response.File{Content: []byte(source), ContentType: "application/yaml"}, nil
}

// remove deletes a document. The caller confirms by repeating the identifier
// as ?confirm=.
func (h handlers[S]) remove(ctx *gofr.Context) (any, error) {
	p, err := auth.Require(ctx, auth.RoleBuilder)
	if err != nil {
		return nil, err
	}
	record, err := h.load(ctx)
	if err != nil {
		return nil, err
	}
	if ctx.Param("confirm") != record.Identifier {
		return nil, badRequest("confirm the deletion by passing the " + h.kind.Name + "'s identifier as confirm")
	}
	if h.kind.Deletable != nil {
		if err := h.kind.Deletable(ctx, ctx.SQL, record.Identifier); err != nil {
			return nil, err
		}
	}

	if err := h.store(ctx).Delete(ctx, record.Identifier); err != nil {
		return nil, err
	}
	auth.Record(ctx, p, h.kind.Action(OpDelete), h.target(record.Identifier, h.current(record)), nil)

	return nil, nil
}

func (h handlers[S]) load(ctx *gofr.Context) (Record, error) {
	identifier, err := h.identifierParam(ctx)
	if err != nil {
		return Record{}, err
	}
	record, err := h.store(ctx).Get(ctx, identifier)
	if errors.Is(err, ErrNotFound) {
		return Record{}, h.notFound()
	}

	if err == nil && !h.canRead(ctx, record) {
		return Record{}, h.notFound()
	}
	return record, err
}

func (h handlers[S]) canRead(ctx context.Context, record Record) bool {
	if h.kind.Readable == nil {
		return true
	}
	p, _ := auth.FromContext(ctx)
	if p.Can(auth.RoleBuilder) {
		return true
	}
	if !record.Published() {
		return false
	}
	spec, issues := h.kind.Parse(record.PublishedSource)
	return !HasErrors(issues) && h.kind.Readable(spec, p)
}

func (h handlers[S]) identifierParam(ctx *gofr.Context) (string, error) {
	identifier := ctx.PathParam("identifier")
	if !ValidIdentifier(identifier) {
		return "", h.notFound()
	}

	return identifier, nil
}

// invalid turns errors among issues into a 422 naming the first few.
func (h handlers[S]) invalid(issues []Issue) error {
	var messages []string
	for _, issue := range issues {
		if issue.Severity != SeverityError {
			continue
		}
		message := issue.Message
		if issue.Line > 0 {
			message = fmt.Sprintf("line %d: %s", issue.Line, message)
		}
		messages = append(messages, message)
	}
	if len(messages) == 0 {
		return nil
	}
	if len(messages) > 3 {
		messages = append(messages[:3], fmt.Sprintf("and %d more", len(messages)-3))
	}

	return httpx.Error{
		Code:    http.StatusUnprocessableEntity,
		Message: "the " + h.kind.Name + " isn't valid: " + strings.Join(messages, "; "),
	}
}

func (h handlers[S]) target(identifier string, spec S) audit.Target {
	title := spec.Meta().Title
	if title == "" {
		title = identifier
	}

	return audit.Target{Type: h.kind.Name, ID: identifier, Label: title}
}

func (h handlers[S]) notFound() error {
	return httpx.Error{Code: http.StatusNotFound, Message: "no such " + h.kind.Name}
}

func orEmpty(issues []Issue) []Issue {
	if issues == nil {
		return []Issue{}
	}

	return issues
}

func badRequest(message string) error {
	return httpx.Error{Code: http.StatusBadRequest, Message: message}
}
