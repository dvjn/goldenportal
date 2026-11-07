package action

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/dvjn/goldenportal/internal/audit"
	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// These services are what the web API and MCP call. They check the
// caller's access themselves.

// Available is an action someone can run on an entity, with what it asks for.
type Available struct {
	Identifier  string               `json:"identifier"`
	Title       string               `json:"title"`
	Description string               `json:"description,omitempty"`
	Icon        string               `json:"icon,omitempty"`
	Color       string               `json:"color,omitempty"`
	Inputs      []blueprint.Property `json:"inputs"`
}

// published is a published action with its row ID.
type published struct {
	spec    Spec
	id      string
	version int
}

// publishedDocs are the published, valid documents of a kind: actions or workflows.
func publishedDocs(ctx context.Context, db configdoc.DB, kind string) ([]published, error) {
	store := configdoc.NewStore(db, kind)
	records, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []published
	for _, record := range records {
		if !record.Published() {
			continue
		}
		spec, issues := parseAny(record.PublishedSource)
		if configdoc.HasErrors(issues) {
			continue
		}
		id, err := store.PublishedID(ctx, record.Identifier)
		if err != nil {
			return nil, err
		}
		out = append(out, published{spec: spec, id: id, version: record.PublishedVersion})
	}

	return out, nil
}

func publishedActions(ctx *gofr.Context) ([]published, error) {
	return publishedDocs(ctx, ctx.SQL, Name)
}

// publishedDoc is a published document of a kind, by name.
func publishedDoc(ctx context.Context, db configdoc.DB, kind, name string) (published, error) {
	store := configdoc.NewStore(db, kind)
	record, err := store.Get(ctx, name)
	if errors.Is(err, configdoc.ErrNotFound) || (err == nil && !record.Published()) {
		noun := "action"
		if kind == WorkflowName {
			noun = "workflow"
		}
		return published{}, httpx.NotFound("no published " + noun + " " + name)
	}
	if err != nil {
		return published{}, err
	}
	spec, _ := parseAny(record.PublishedSource)
	id, err := store.PublishedID(ctx, name)

	return published{spec: spec, id: id, version: record.PublishedVersion}, err
}

func publishedAction(ctx *gofr.Context, name string) (published, error) {
	return publishedDoc(ctx, ctx.SQL, Name, name)
}

// target is an entity an action runs on, with what's needed to check filters on it.
type target struct {
	entity *entity.Entity
	specs  map[string]blueprint.Spec
	spec   blueprint.Spec
}

func loadTarget(ctx context.Context, db configdoc.DB, blueprintName, identifier string) (target, error) {
	specs, err := blueprint.Published(ctx, db)
	if err != nil {
		return target{}, err
	}
	spec, ok := specs[blueprintName]
	if !ok {
		return target{}, httpx.NotFound("no published blueprint " + blueprintName)
	}
	e, err := entity.NewStore(db).Get(ctx, blueprintName, identifier)
	if errors.Is(err, entity.ErrNotFound) {
		return target{}, httpx.NotFound(fmt.Sprintf("no %s %s", blueprintName, identifier))
	}

	return target{entity: e, specs: specs, spec: spec}, err
}

// matches reports whether the entity meets a filter, with $me as p.
func (t target) matches(ctx context.Context, db configdoc.DB, f *entity.Filter, p auth.Principal) (bool, error) {
	if f == nil {
		return true, nil
	}
	bound := meScope(p).ResolveFilter(f)
	page, err := entity.NewStore(db).List(ctx, entity.Query{
		Blueprint: t.spec, Blueprints: t.specs, Page: 1, PageSize: 1,
		Filter: &entity.Filter{And: []filter.Filter{*bound, {Property: "identifier", Op: filter.OpEq, Value: t.entity.Identifier}}},
	})
	if err != nil {
		return false, err
	}

	return page.Total > 0, nil
}

func meScope(p auth.Principal) Scope {
	groups := make([]any, len(p.Groups))
	for i, group := range p.Groups {
		groups[i] = group
	}

	return Scope{Me: map[string]any{"id": p.ID, "email": p.Email, "name": p.DisplayName, "role": string(p.Role), "groups": groups}}
}

// allowed reports whether p can run the action on the target: admins can
// run anything, others need its run access, and the entity must be in scope.
// Roles nest, as everywhere: run access for members includes builders.
func (a published) allowed(ctx context.Context, db configdoc.DB, t target, p auth.Principal) (bool, error) {
	if inScope, err := t.matches(ctx, db, a.spec.Scope, p); err != nil || !inScope {
		return false, err
	}
	if a.spec.Run.matches(p) {
		return true, nil
	}
	if a.spec.Run.Filter == nil {
		return false, nil
	}

	return t.matches(ctx, db, a.spec.Run.Filter, p)
}

// matches reports whether p has one of the roles or is in one of the
// groups. Admins always match, and roles nest.
func (a Access) matches(p auth.Principal) bool {
	return p.Can(auth.RoleAdmin) || slices.ContainsFunc(a.Roles, p.Can) ||
		slices.ContainsFunc(a.Groups, func(group string) bool { return slices.Contains(p.Groups, group) })
}

// ForEntity lists the actions the caller can run on an entity.
func ForEntity(ctx *gofr.Context, blueprintName, identifier string) ([]Available, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return nil, err
	}
	t, err := loadTarget(ctx, ctx.SQL, blueprintName, identifier)
	if err != nil {
		return nil, err
	}
	actions, err := publishedActions(ctx)
	if err != nil {
		return nil, err
	}
	available := []Available{}
	for _, a := range actions {
		if a.spec.Blueprint != blueprintName {
			continue
		}
		ok, err := a.allowed(ctx, ctx.SQL, t, p)
		if err != nil {
			return nil, err
		}
		if ok {
			s := a.spec
			available = append(available, Available{
				Identifier: s.Identifier, Title: s.Title, Description: s.Description, Icon: s.Icon, Color: s.Color,
				Inputs: s.Inputs,
			})
		}
	}

	return available, nil
}

// maxRows caps the entities RowActions answers for at once: a page of a table.
const maxRows = 100

// RowActions are the actions the caller can run on each of a page of
// entities: the actions once, and which of them each entity offers.
type RowActions struct {
	Actions  []Available         `json:"actions"`
	Entities map[string][]string `json:"entities"`
}

// ForEntities finds the actions the caller can run on each of a blueprint's
// entities named, with one query per action rather than per entity.
func ForEntities(ctx *gofr.Context, blueprintName string, identifiers []string) (RowActions, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return RowActions{}, err
	}
	if len(identifiers) > maxRows {
		return RowActions{}, httpx.BadRequest(fmt.Sprintf("ask about at most %d entities at once", maxRows))
	}
	out := RowActions{Actions: []Available{}, Entities: map[string][]string{}}
	if len(identifiers) == 0 {
		return out, nil
	}
	specs, err := blueprint.Published(ctx, ctx.SQL)
	if err != nil {
		return RowActions{}, err
	}
	spec, ok := specs[blueprintName]
	if !ok {
		return RowActions{}, httpx.NotFound("no published blueprint " + blueprintName)
	}
	actions, err := publishedActions(ctx)
	if err != nil {
		return RowActions{}, err
	}
	names := make([]any, len(identifiers))
	for i, identifier := range identifiers {
		names[i] = identifier
	}
	among := filter.Filter{Property: "identifier", Op: filter.OpIn, Values: names}
	me := meScope(p)
	matching := func(f *entity.Filter) ([]string, error) {
		parts := []filter.Filter{among}
		if f != nil {
			parts = append(parts, *me.ResolveFilter(f))
		}
		return entity.NewStore(ctx.SQL).Identifiers(ctx, specs, spec, &entity.Filter{And: parts}, maxRows)
	}

	for _, a := range actions {
		if a.spec.Blueprint != blueprintName {
			continue
		}
		run := a.spec.Run
		everyone := p.Can(auth.RoleAdmin) || slices.ContainsFunc(run.Roles, p.Can) ||
			slices.ContainsFunc(run.Groups, func(group string) bool { return slices.Contains(p.Groups, group) })
		if !everyone && run.Filter == nil {
			continue
		}
		inScope, err := matching(a.spec.Scope)
		if err != nil {
			return RowActions{}, err
		}
		allowed := inScope
		if !everyone {
			permitted, err := matching(run.Filter)
			if err != nil {
				return RowActions{}, err
			}
			allowed = slices.DeleteFunc(inScope, func(identifier string) bool { return !slices.Contains(permitted, identifier) })
		}
		if len(allowed) == 0 {
			continue
		}
		s := a.spec
		out.Actions = append(out.Actions, Available{
			Identifier: s.Identifier, Title: s.Title, Description: s.Description, Icon: s.Icon, Color: s.Color,
			Inputs: s.Inputs,
		})
		for _, identifier := range allowed {
			out.Entities[identifier] = append(out.Entities[identifier], s.Identifier)
		}
	}

	return out, nil
}

// RunRequest asks to run an action on an entity. A request sent again
// with the same Key gives the run the first made.
type RunRequest struct {
	Entity string         `json:"entity"`
	Inputs map[string]any `json:"inputs"`
	Key    string         `json:"idempotencyKey,omitempty"`
}

// InputError is what's wrong with the inputs given, by input.
type InputError struct{ Problems map[string][]string }

func (e InputError) Error() string {
	var parts []string
	for id, problems := range e.Problems {
		for _, problem := range problems {
			if id == "" {
				parts = append(parts, problem)
			} else {
				parts = append(parts, id+" "+problem)
			}
		}
	}
	slices.Sort(parts)

	return "check the inputs: " + strings.Join(parts, "; ")
}

func (e InputError) StatusCode() int { return http.StatusUnprocessableEntity }

func (e InputError) Response() map[string]any { return map[string]any{"problems": e.Problems} }

// Start runs an action on an entity as the caller, through via.
func Start(ctx *gofr.Context, name string, req RunRequest, via string) (Run, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return Run{}, err
	}
	a, err := publishedAction(ctx, name)
	if err != nil {
		return Run{}, err
	}
	run, err := prepare(ctx, ctx.SQL, a, req.Entity, req.Inputs, p)
	if err != nil {
		return Run{}, err
	}
	run.Via, run.key = via, req.Key

	return create(ctx, a, run, p)
}

// prepare checks p can run an action on an entity with the inputs given,
// and makes the run to start.
func prepare(ctx context.Context, db configdoc.DB, a published, identifier string, given map[string]any, p auth.Principal) (Run, error) {
	var t target
	if !a.spec.IsWorkflow() {
		var err error
		if t, err = loadTarget(ctx, db, a.spec.Blueprint, identifier); err != nil {
			return Run{}, err
		}
		ok, err := a.allowed(ctx, db, t, p)
		if err != nil {
			return Run{}, err
		}
		if !ok {
			return Run{}, httpx.Forbidden(fmt.Sprintf("you can't run %s on %s", a.spec.Title, t.entity.Title))
		}
	} else if !a.spec.Run.matches(p) {
		return Run{}, httpx.Forbidden("you can't run " + a.spec.Title)
	}
	if given == nil {
		given = map[string]any{}
	}
	inputs, problems := blueprint.ValidateInputs(a.spec.Inputs, given)
	if err := relationProblems(ctx, db, a.spec.Inputs, inputs, problems); err != nil {
		return Run{}, err
	}
	if len(problems) > 0 {
		return Run{}, InputError{problems}
	}

	run := Run{
		Kind: a.spec.docKind(), Action: a.spec.Identifier, Title: a.spec.Title, Version: a.version, Inputs: inputs,
		Requester:  Requester{ID: p.ID, Email: p.Email, Name: p.DisplayName, Role: string(p.Role), Groups: p.Groups},
		documentID: a.id, requesterID: p.ID,
	}
	if t.entity != nil {
		run.Entity = EntityRef{Blueprint: a.spec.Blueprint, Identifier: t.entity.Identifier, Title: t.entity.Title}
		run.entityID = t.entity.ID
	}

	return run, nil
}

// create records a run the caller asked for and starts it.
func create(ctx *gofr.Context, a published, run Run, p auth.Principal) (Run, error) {
	store := NewStore(ctx.SQL)
	id, created, err := store.Create(ctx, run, a.spec.Steps)
	if err != nil {
		return Run{}, err
	}
	if created {
		details := map[string]any{"run": id, "via": run.Via}
		if run.Entity.Identifier != "" {
			details["entity"] = run.Entity.Blueprint + "/" + run.Entity.Identifier
		}
		auth.Record(ctx, p, audit.ActionRun, audit.Target{Type: a.spec.docKind(), ID: a.spec.Identifier, Label: a.spec.Title}, details)
		NewRunner(ctx.SQL, ctx.Container.Logger, baseURL).Start(id)
	}

	return store.Get(ctx, id)
}

// StartWorkflow runs a workflow as the caller, through via.
func StartWorkflow(ctx *gofr.Context, name string, req RunRequest, via string) (Run, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return Run{}, err
	}
	a, err := publishedDoc(ctx, ctx.SQL, WorkflowName, name)
	if err != nil {
		return Run{}, err
	}
	run, err := prepare(ctx, ctx.SQL, a, "", req.Inputs, p)
	if err != nil {
		return Run{}, err
	}
	run.Via, run.key = via, req.Key

	return create(ctx, a, run, p)
}

// relationProblems adds a problem for each related entity that doesn't exist.
func relationProblems(ctx context.Context, db configdoc.DB, inputs []blueprint.Property, values map[string]any, problems map[string][]string) error {
	store := entity.NewStore(db)
	for _, in := range inputs {
		if !in.IsRelation() || values[in.ID] == nil {
			continue
		}
		var identifiers []string
		switch v := values[in.ID].(type) {
		case string:
			identifiers = []string{v}
		case []string:
			identifiers = v
		}
		for _, identifier := range identifiers {
			_, err := store.Get(ctx, in.Relation, identifier)
			if errors.Is(err, entity.ErrNotFound) {
				problems[in.ID] = append(problems[in.ID], fmt.Sprintf("names no %s %s", in.Relation, identifier))
				continue
			}
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// visible returns a run the caller may see: their own, any for builders,
// and those they can approve or did.
func visible(ctx *gofr.Context, id string) (Run, auth.Principal, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return Run{}, p, err
	}
	run, err := NewStore(ctx.SQL).Get(ctx, id)
	if errors.Is(err, ErrRunNotFound) {
		return Run{}, p, httpx.NotFound("no such run")
	}
	if err != nil {
		return Run{}, p, err
	}
	if _, run.CanDecide, err = approvable(ctx, run, p); err != nil {
		return Run{}, p, err
	}
	if run.requesterID != p.ID && !p.Can(auth.RoleBuilder) && !run.CanDecide && !decided(run, p) {
		return Run{}, p, httpx.NotFound("no such run")
	}

	return run, p, nil
}

func decided(run Run, p auth.Principal) bool {
	for _, step := range run.Steps {
		for _, d := range step.Decisions {
			if d.userID == p.ID {
				return true
			}
		}
	}

	return false
}

// approvable finds the approval step a run waits at, and whether p can
// decide it: anyone the step's approvers match, but not whoever asked for
// the run, nor someone who already decided.
func approvable(ctx *gofr.Context, run Run, p auth.Principal) (*waiting, bool, error) {
	if run.Status != RunWaiting {
		return nil, false, nil
	}
	i := slices.IndexFunc(run.Steps, func(s RunStep) bool { return s.Status == StepWaiting })
	if i < 0 {
		return nil, false, nil
	}
	source, err := NewStore(ctx.SQL).source(ctx, run.documentID, run.Version)
	if err != nil {
		return nil, false, err
	}
	spec, _ := parseAny(source)
	if i >= len(spec.Steps) || spec.Steps[i].Approval == nil {
		return nil, false, nil
	}
	w := &waiting{position: i, step: spec.Steps[i].Approval}
	if run.requesterID == p.ID || slices.ContainsFunc(run.Steps[i].Decisions, func(d Decision) bool { return d.userID == p.ID }) {
		return w, false, nil
	}
	approvers := w.step.Approvers
	if p.Can(auth.RoleAdmin) || slices.ContainsFunc(approvers.Roles, p.Can) ||
		slices.ContainsFunc(approvers.Groups, func(group string) bool { return slices.Contains(p.Groups, group) }) {
		return w, true, nil
	}
	if approvers.Filter == nil {
		return w, false, nil
	}
	t, err := loadTarget(ctx, ctx.SQL, run.Entity.Blueprint, run.Entity.Identifier)
	var notFound httpx.Error
	if errors.As(err, &notFound) {
		return w, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	ok, err := t.matches(ctx, ctx.SQL, approvers.Filter, p)

	return w, ok, err
}

// waiting is the approval step a run waits at.
type waiting struct {
	position int
	step     *ApprovalStep
}

// Decide approves or rejects the approval step a run waits at. Only people
// can: agents can't, so MCP offers no way to. Rejecting needs a comment.
func Decide(ctx *gofr.Context, id, decision, comment string) (Run, error) {
	run, p, err := visible(ctx, id)
	if err != nil {
		return Run{}, err
	}
	w, can, err := approvable(ctx, run, p)
	if err != nil {
		return Run{}, err
	}
	switch {
	case w == nil:
		return Run{}, httpx.Conflict("the run isn't waiting for approval")
	case run.requesterID == p.ID:
		return Run{}, httpx.Forbidden("someone else has to approve a run you asked for")
	case !can:
		return Run{}, httpx.Forbidden("you can't approve this run, or you already decided")
	}
	comment = strings.TrimSpace(comment)
	action := audit.ActionRunApprove
	switch decision {
	case Approve:
	case Reject:
		if comment == "" {
			return Run{}, httpx.BadRequest("say why you're rejecting it, as the comment")
		}
		action = audit.ActionRunReject
	default:
		return Run{}, httpx.BadRequest("the decision is approve or reject")
	}

	store := NewStore(ctx.SQL)
	resumed, err := store.decide(ctx, id, w.position, w.step.Require,
		Decision{Email: p.Email, Name: p.DisplayName, Decision: decision, Comment: comment, userID: p.ID})
	if errors.Is(err, ErrDecided) {
		return Run{}, httpx.Conflict("you already decided, or the step isn't waiting any more")
	}
	if err != nil {
		return Run{}, err
	}
	record(ctx, p, action, run, map[string]any{"step": run.Steps[w.position].Title, "comment": comment})
	runner := NewRunner(ctx.SQL, ctx.Container.Logger, baseURL)
	if resumed {
		runner.Start(id)
	} else {
		runner.Wake(ctx, id)
	}

	return GetRun(ctx, id)
}

// Approvals lists the runs waiting for the caller's approval, oldest first.
func Approvals(ctx *gofr.Context) ([]Run, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return nil, err
	}
	store := NewStore(ctx.SQL)
	page, err := store.List(ctx, RunsQuery{Status: RunWaiting, Page: 1, Size: maxWaiting})
	if err != nil {
		return nil, err
	}
	out := []Run{}
	for _, run := range slices.Backward(page.Items) {
		if run.Steps, err = store.steps(ctx, run.ID); err != nil {
			return nil, err
		}
		if _, can, err := approvable(ctx, run, p); err != nil {
			return nil, err
		} else if can {
			run.CanDecide = true
			out = append(out, run)
		}
	}

	return out, nil
}

// maxWaiting caps the waiting runs Approvals looks through.
const maxWaiting = 500

// GetRun reads a run with its steps.
func GetRun(ctx *gofr.Context, id string) (Run, error) {
	run, _, err := visible(ctx, id)
	return run, err
}

// RunsRequest asks for a page of runs. Members only ever see their own;
// builders see everyone's unless Mine is set.
type RunsRequest struct {
	Action string
	// Workflow narrows to one workflow's runs, and Parent to the runs
	// a workflow run started.
	Workflow   string
	Parent     string
	Blueprint  string
	Entity     string
	Status     string
	Search     string
	Mine       bool
	Page, Size int
}

// Runs lists runs, newest first.
func Runs(ctx *gofr.Context, req RunsRequest) (RunPage, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return RunPage{}, err
	}
	q := RunsQuery{
		Blueprint: req.Blueprint, Entity: req.Entity, Status: req.Status, Search: req.Search, Parent: req.Parent,
	}
	if q.Page, q.Size, err = httpx.CheckPaging(req.Page, req.Size); err != nil {
		return RunPage{}, err
	}
	if req.Mine || !p.Can(auth.RoleBuilder) {
		q.RequestedBy = p.ID
	}
	for kind, name := range map[string]string{Name: req.Action, WorkflowName: req.Workflow} {
		if name == "" {
			continue
		}
		id, err := configdoc.NewStore(ctx.SQL, kind).PublishedID(ctx, name)
		if errors.Is(err, configdoc.ErrNotFound) {
			return RunPage{Items: []Run{}}, nil
		}
		if err != nil {
			return RunPage{}, err
		}
		q.DocumentID = id
	}

	return NewStore(ctx.SQL).List(ctx, q)
}

// mayChange checks the caller asked for the run or builds the portal.
func mayChange(run Run, p auth.Principal) error {
	if run.requesterID != p.ID && !p.Can(auth.RoleBuilder) {
		return httpx.Forbidden("only whoever asked for the run, or a builder, can change it")
	}

	return nil
}

func record(ctx *gofr.Context, p auth.Principal, action string, run Run, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	details["run"] = run.ID
	auth.Record(ctx, p, action, audit.Target{Type: Name, ID: run.Action, Label: run.Title}, details)
}

// CancelRun stops a run that hasn't finished. A step already under way may
// still take effect on the other side.
func CancelRun(ctx *gofr.Context, id string) (Run, error) {
	run, p, err := visible(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if err := mayChange(run, p); err != nil {
		return Run{}, err
	}
	ok, err := NewStore(ctx.SQL).cancel(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if !ok {
		return Run{}, httpx.Conflict("the run has already finished")
	}
	record(ctx, p, audit.ActionRunCancel, run, nil)
	NewRunner(ctx.SQL, ctx.Container.Logger, baseURL).Wake(ctx, id)

	return NewStore(ctx.SQL).Get(ctx, id)
}

// RetryRun carries a failed or interrupted run on from the step it stopped
// at, with the same inputs and version, keeping what earlier steps gave.
func RetryRun(ctx *gofr.Context, id string) (Run, error) {
	run, p, err := visible(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if err := mayChange(run, p); err != nil {
		return Run{}, err
	}
	ok, err := NewStore(ctx.SQL).requeue(ctx, id, []string{RunFailed, RunInterrupted}, false, "", "")
	if err != nil {
		return Run{}, err
	}
	if !ok {
		return Run{}, httpx.Conflict("only failed or interrupted runs can be retried")
	}
	record(ctx, p, audit.ActionRunRetry, run, nil)
	NewRunner(ctx.SQL, ctx.Container.Logger, baseURL).Start(id)

	return NewStore(ctx.SQL).Get(ctx, id)
}

// Resolutions of an interrupted run.
const (
	// ResolveDone says the interrupted step took effect, so the run carries on after it.
	ResolveDone = "done"
	// ResolveFailed ends the run as failed.
	ResolveFailed = "failed"
)

// ResolveRun decides how an interrupted run's step went, which someone
// checked on the other side. Saying it took effect is for builders.
func ResolveRun(ctx *gofr.Context, id, outcome, comment string) (Run, error) {
	run, p, err := visible(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if err := mayChange(run, p); err != nil {
		return Run{}, err
	}
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return Run{}, httpx.BadRequest("say what you checked, as the comment")
	}
	store := NewStore(ctx.SQL)
	var ok bool
	switch outcome {
	case ResolveDone:
		if !p.Can(auth.RoleBuilder) {
			return Run{}, httpx.Forbidden("only builders can say an interrupted step took effect")
		}
		ok, err = store.requeue(ctx, id, []string{RunInterrupted}, true, p.ID, comment)
	case ResolveFailed:
		ok, err = store.resolveFailed(ctx, id, p.ID, comment)
	default:
		return Run{}, httpx.BadRequest("the outcome is done or failed")
	}
	if err != nil {
		return Run{}, err
	}
	if !ok {
		return Run{}, httpx.Conflict("only interrupted runs need resolving")
	}
	record(ctx, p, audit.ActionRunResolve, run, map[string]any{"outcome": outcome, "comment": comment})
	runner := NewRunner(ctx.SQL, ctx.Container.Logger, baseURL)
	if outcome == ResolveDone {
		runner.Start(id)
	} else {
		runner.Wake(ctx, id)
	}

	return store.Get(ctx, id)
}

// Offer is something the Self-Service catalog offers: a workflow, or an
// action, which runs on an entity people pick.
type Offer struct {
	Kind        string `json:"kind"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
	// Blueprint and BlueprintTitle say what an action runs on.
	Blueprint      string               `json:"blueprint,omitempty"`
	BlueprintTitle string               `json:"blueprintTitle,omitempty"`
	Inputs         []blueprint.Property `json:"inputs"`
}

// CatalogGroup is one group of the Self-Service catalog, workflows and
// actions together, by title.
type CatalogGroup struct {
	Title string  `json:"title"`
	Items []Offer `json:"items"`
}

// Catalog is the Self-Service catalog, by group, in order, with the
// ungrouped last.
type Catalog struct {
	Groups []CatalogGroup `json:"groups"`
}

// OtherGroup is where actions without a group show.
const OtherGroup = "Other"

// SelfServiceCatalog lists the workflows the caller can run, and the
// actions they can run on at least some entities: those whose run access
// matches them, or depends on the entity, which the server checks once
// they pick one.
func SelfServiceCatalog(ctx *gofr.Context) (Catalog, error) {
	p, err := auth.Require(ctx, auth.RoleMember)
	if err != nil {
		return Catalog{}, err
	}
	blueprints, err := blueprint.Published(ctx, ctx.SQL)
	if err != nil {
		return Catalog{}, err
	}
	byGroup := map[string][]Offer{}
	for _, kind := range []string{WorkflowName, Name} {
		docs, err := publishedDocs(ctx, ctx.SQL, kind)
		if err != nil {
			return Catalog{}, err
		}
		for _, d := range docs {
			s := d.spec
			if !s.Run.matches(p) && (s.IsWorkflow() || s.Run.Filter == nil) {
				continue
			}
			o := Offer{
				Kind: s.docKind(), Identifier: s.Identifier, Title: s.Title, Description: s.Description, Icon: s.Icon,
				Color: s.Color, Blueprint: s.Blueprint, Inputs: s.Inputs,
			}
			if bp, ok := blueprints[s.Blueprint]; ok {
				o.BlueprintTitle = bp.Title
			}
			group := cmp.Or(s.Group, OtherGroup)
			byGroup[group] = append(byGroup[group], o)
		}
	}

	out := Catalog{Groups: []CatalogGroup{}}
	for title, items := range byGroup {
		slices.SortFunc(items, func(a, b Offer) int { return cmp.Compare(a.Title, b.Title) })
		out.Groups = append(out.Groups, CatalogGroup{Title: title, Items: items})
	}
	slices.SortFunc(out.Groups, func(a, b CatalogGroup) int {
		if (a.Title == OtherGroup) != (b.Title == OtherGroup) {
			if a.Title == OtherGroup {
				return 1
			}
			return -1
		}
		return cmp.Compare(a.Title, b.Title)
	})

	return out, nil
}
