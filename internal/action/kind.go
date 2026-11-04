package action

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/integration"
)

// Kind serves actions as configuration documents.
var Kind = configdoc.Kind[Spec]{
	Name:      Name,
	Document:  DocumentKind,
	Plural:    "actions",
	Parse:     Parse,
	Check:     Check,
	Diff:      Diff,
	Deletable: deletable(Name),
}

// WorkflowDocs serves workflows as configuration documents.
var WorkflowDocs = configdoc.Kind[Spec]{
	Name:      WorkflowName,
	Document:  WorkflowKind,
	Plural:    "workflows",
	Parse:     ParseWorkflow,
	Check:     Check,
	Diff:      Diff,
	Deletable: deletable(WorkflowName),
}

// toFilter turns a filter written in YAML into the catalog's filter.
func toFilter(raw any) (*entity.Filter, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("filter is not valid: %w", err)
	}

	return entity.ParseFilter(string(encoded))
}

// Check checks the action against the blueprints and integrations it
// names. One not published yet is a warning: the action can't run until
// it is.
func Check(ctx context.Context, db configdoc.DB, spec Spec) ([]configdoc.Issue, error) {
	published, err := blueprint.Published(ctx, db)
	if err != nil {
		return nil, err
	}
	var issues []configdoc.Issue
	add := func(severity, path string, line int, format string, args ...any) {
		issues = append(issues, configdoc.Issue{Severity: severity, Path: path, Line: line, Message: fmt.Sprintf(format, args...)})
	}

	bp, ok := published[spec.Blueprint]
	switch {
	case spec.IsWorkflow():
		more, err := checkActionSteps(ctx, db, spec)
		if err != nil {
			return nil, err
		}
		issues = append(issues, more...)
	case !ok:
		add(configdoc.SeverityWarning, "spec.blueprint", spec.line, "no blueprint %q is published yet, so the action can't run until it is", spec.Blueprint)
	default:
		for path, f := range map[string]*entity.Filter{"spec.scope": spec.Scope, "spec.run.filter": spec.Run.Filter} {
			if f == nil {
				continue
			}
			if err := entity.CheckFilter(published, bp, *f); err != nil {
				add(configdoc.SeverityError, path, spec.line, "%v", err)
			}
		}
		refs, _ := refsIn(spec.values())
		for _, ref := range refs {
			if ref.Root != RootEntity || len(ref.Path) == 0 {
				continue
			}
			if _, ok := bp.Property(ref.Path[0]); !ok && !slices.Contains(blueprint.BuiltIns, ref.Path[0]) {
				add(configdoc.SeverityError, "spec.steps", spec.line, "%s: %s has no field %q", ref, bp.Identifier, ref.Path[0])
			}
		}
	}

	integrations := configdoc.NewStore(db, integration.Name)
	for i, step := range spec.Steps {
		path := fmt.Sprintf("spec.steps[%d]", i)
		switch {
		case step.Entity != nil:
			e := step.Entity
			record, err := integrations.Get(ctx, e.Integration)
			switch {
			case errors.Is(err, configdoc.ErrNotFound) || (err == nil && !record.Published()):
				add(configdoc.SeverityWarning, path+".entity.integration", step.line, "no integration %q is published yet, so this step fails until it is", e.Integration)
			case err != nil:
				return nil, err
			default:
				target, _ := integration.Parse(record.PublishedSource)
				if problem := integration.Writable(target); problem != "" {
					add(configdoc.SeverityError, path+".entity.integration", step.line, "%s", problem)
				}
			}
			target, ok := published[e.Blueprint]
			if !ok {
				add(configdoc.SeverityWarning, path+".entity.blueprint", step.line, "no blueprint %q is published yet, so this step fails until it is", e.Blueprint)
				continue
			}
			for key := range e.Properties {
				prop, ok := target.Property(key)
				switch {
				case !ok:
					add(configdoc.SeverityError, path+".entity.properties."+key, step.line, "%s has no property %q", target.Identifier, key)
				case prop.Derived():
					add(configdoc.SeverityError, path+".entity.properties."+key, step.line, "%s is worked out by the portal, so it can't be written", key)
				}
			}
		case step.Approval != nil && step.Approval.Approvers.Filter != nil && bp.Identifier != "":
			if err := entity.CheckFilter(published, bp, *step.Approval.Approvers.Filter); err != nil {
				add(configdoc.SeverityError, path+".approval.approvers.filter", step.line, "%v", err)
			}
		case step.Check != nil:
			target, ok := published[step.Check.Blueprint]
			if !ok {
				add(configdoc.SeverityWarning, path+".check.blueprint", step.line, "no blueprint %q is published yet, so this step fails until it is", step.Check.Blueprint)
				continue
			}
			if step.Check.Filter != nil {
				if err := entity.CheckFilter(published, target, *step.Check.Filter); err != nil {
					add(configdoc.SeverityError, path+".check.filter", step.line, "%v", err)
				}
			}
		}
	}

	return issues, nil
}

// values are every value a step or output can refer from.
func (s Spec) values() []any {
	var values []any
	for _, step := range s.Steps {
		switch {
		case step.HTTP != nil:
			values = append(values, step.HTTP.URL, step.HTTP.Body)
			for _, header := range step.HTTP.Headers {
				values = append(values, header.Value)
			}
		case step.Entity != nil:
			values = append(values, step.Entity.Identifier, step.Entity.Title, step.Entity.Properties)
		case step.Check != nil:
			values = append(values, step.Check.Message)
			eachFilterText(step.Check.Filter, func(text string) { values = append(values, text) })
		case step.Output != nil:
			values = append(values, step.Output.Summary)
			for _, link := range step.Output.Links {
				values = append(values, link.URL)
			}
		case step.Approval != nil:
			values = append(values, step.Approval.Message)
			for _, r := range step.Approval.Review {
				values = append(values, r.Markdown, r.JSON)
			}
		}
	}
	for _, output := range s.Outputs {
		values = append(values, output.Value)
	}

	return values
}

// Diff lists what publishing next over previous changes. Runs already
// made keep the version they ran, so nothing is breaking.
func Diff(previous, next Spec) []configdoc.Change {
	changes := []configdoc.Change{}
	add := func(section, id, change, format string, args ...any) {
		changes = append(changes, configdoc.Change{Section: section, ID: id, Change: change, Detail: fmt.Sprintf(format, args...)})
	}

	if previous.Blueprint != next.Blueprint {
		add("action", "", configdoc.ChangeChanged, "runs on %s instead of %s", next.Blueprint, previous.Blueprint)
	}
	if previous.Group != next.Group {
		add("action", "", configdoc.ChangeChanged, "shows under %s instead of %s in the Self-Service catalog", cmp.Or(next.Group, OtherGroup), cmp.Or(previous.Group, OtherGroup))
	}
	if !reflect.DeepEqual(previous.Scope, next.Scope) {
		add("action", "", configdoc.ChangeChanged, "changes which entities it applies to")
	}
	if !reflect.DeepEqual(previous.Run, next.Run) {
		add("action", "", configdoc.ChangeChanged, "changes who can run it")
	}
	if previous.Title != next.Title || previous.Description != next.Description || previous.Icon != next.Icon || previous.Color != next.Color {
		add("action", "", configdoc.ChangeChanged, "changes the title, description or icon")
	}

	before := map[string]blueprint.Property{}
	for _, in := range previous.Inputs {
		before[in.ID] = in
	}
	seen := map[string]bool{}
	for _, in := range next.Inputs {
		seen[in.ID] = true
		old, ok := before[in.ID]
		switch {
		case !ok:
			add("inputs", in.ID, configdoc.ChangeAdded, "asks for it")
		case !sameInput(old, in):
			add("inputs", in.ID, configdoc.ChangeChanged, "changes what it takes")
		}
	}
	for _, in := range previous.Inputs {
		if !seen[in.ID] {
			add("inputs", in.ID, configdoc.ChangeRemoved, "stops asking for it")
		}
	}
	if !sameSteps(previous.Steps, next.Steps) {
		add("steps", "", configdoc.ChangeChanged, "changes the steps from %d to %d", len(previous.Steps), len(next.Steps))
	}
	if !reflect.DeepEqual(previous.Outputs, next.Outputs) {
		add("outputs", "", configdoc.ChangeChanged, "changes what a run gives")
	}

	return changes
}

func sameInput(a, b blueprint.Property) bool {
	encodedA, _ := json.Marshal(a)
	encodedB, _ := json.Marshal(b)

	return string(encodedA) == string(encodedB)
}

func sameSteps(a, b []Step) bool {
	strip := func(steps []Step) []Step {
		out := slices.Clone(steps)
		for i := range out {
			out[i].line = 0
		}
		return out
	}

	return reflect.DeepEqual(strip(a), strip(b))
}

// deletable keeps a document while it has runs going, which need its versions.
func deletable(kind string) func(context.Context, configdoc.DB, string) error {
	return func(ctx context.Context, db configdoc.DB, identifier string) error {
		var running int
		err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM runs r JOIN config_documents d ON d.id = r.document_id
			WHERE d.kind = $1 AND d.identifier = $2 AND r.status IN ('queued', 'running', 'waiting')`, kind, identifier).Scan(&running)
		if err != nil {
			return err
		}
		if running > 0 {
			return httpx.Error{Code: http.StatusConflict, Message: fmt.Sprintf("%d runs of it are still going; cancel them or let them finish first", running)}
		}

		return nil
	}
}

// checkActionSteps checks the actions a workflow runs: one not published
// is a warning, and inputs it gives that the action doesn't take an error.
func checkActionSteps(ctx context.Context, db configdoc.DB, spec Spec) ([]configdoc.Issue, error) {
	var issues []configdoc.Issue
	store := configdoc.NewStore(db, Name)
	for i, step := range spec.Steps {
		if step.Action == nil {
			continue
		}
		path := fmt.Sprintf("spec.steps[%d].action", i)
		record, err := store.Get(ctx, step.Action.Name)
		if errors.Is(err, configdoc.ErrNotFound) || (err == nil && !record.Published()) {
			issues = append(issues, configdoc.Issue{
				Severity: configdoc.SeverityWarning, Path: path + ".name", Line: step.line,
				Message: fmt.Sprintf("no action %q is published yet, so this step fails until it is", step.Action.Name),
			})
			continue
		}
		if err != nil {
			return nil, err
		}
		target, _ := Parse(record.PublishedSource)
		for key := range step.Action.Inputs {
			if _, ok := target.Input(key); !ok {
				issues = append(issues, configdoc.Issue{
					Severity: configdoc.SeverityError, Path: path + ".inputs." + key, Line: step.line,
					Message: fmt.Sprintf("%s takes no input %q", target.Title, key),
				})
			}
		}
		for _, in := range target.Inputs {
			if _, given := step.Action.Inputs[in.ID]; in.Required && !given {
				issues = append(issues, configdoc.Issue{
					Severity: configdoc.SeverityError, Path: path + ".inputs", Line: step.line,
					Message: fmt.Sprintf("%s needs input %q", target.Title, in.ID),
				})
			}
		}
	}

	return issues, nil
}
