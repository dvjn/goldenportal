package action

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// fanOut starts an action on each entity an action step names, as whoever
// asked for the workflow, and waits for them. Starting again, after
// a retry or a lost instance, starts only those not already going or done.
func (rn Runner) fanOut(ctx context.Context, store Store, run Run, position int, step Step, scope Scope) error {
	if held, err := store.renew(ctx, run.ID, lease); err != nil || !held {
		return errors.Join(err, context.Canceled)
	}
	if err := store.startStep(ctx, run.ID, position); err != nil {
		return err
	}
	fail := func(format string, args ...any) error {
		message := fmt.Sprintf(format, args...)
		if err := store.finishStep(ctx, run.ID, position, StepFailed, nil, message, nil); err != nil {
			return err
		}
		return store.finish(ctx, run.ID, RunFailed, step.Title+": "+message, nil)
	}

	a := step.Action
	entities := identifiers(scope.Resolve(a.Entities))
	switch {
	case len(entities) == 0:
		return fail("no entities to run %s on", a.Name)
	case len(entities) > maxFanOut:
		return fail("it can run %s on at most %d entities at once, not %d", a.Name, maxFanOut, len(entities))
	}
	target, err := publishedDoc(ctx, rn.db, Name, a.Name)
	var notFound httpx.Error
	if errors.As(err, &notFound) {
		return fail("%s", notFound.Message)
	}
	if err != nil {
		return err
	}
	existing, err := store.children(ctx, run.ID, position)
	if err != nil {
		return err
	}
	inputs, _ := scope.Resolve(a.Inputs).(map[string]any)
	requester := auth.Principal{
		ID: run.Requester.ID, Email: run.Requester.Email, DisplayName: run.Requester.Name,
		Role: auth.Role(run.Requester.Role), Groups: run.Requester.Groups,
	}

	// Refusals are recorded as runs that failed straight away, so the step
	// counts them and people see why.
	refused := map[string]string{}
	for _, identifier := range entities {
		if child, ok := existing[identifier]; ok && child.Status != RunFailed && child.Status != RunCancelled &&
			child.Status != RunRejected && child.Status != RunInterrupted {
			continue
		}
		child, err := prepare(ctx, rn.db, target, identifier, inputs, requester)
		var refusal httpx.Error
		var invalid InputError
		switch {
		case errors.As(err, &refusal):
			refused[identifier] = refusal.Message
			continue
		case errors.As(err, &invalid):
			refused[identifier] = invalid.Error()
			continue
		case err != nil:
			return err
		}
		child.Via, child.Parent, child.parentPosition = run.Via, run.ID, position
		id, _, err := store.Create(ctx, child, target.spec.Steps)
		if err != nil {
			return err
		}
		rn.Start(id)
	}
	if len(refused) > 0 {
		if err := store.setRefused(ctx, run.ID, position, refused); err != nil {
			return err
		}
	}
	if err := store.waitChildren(ctx, run.ID, position); err != nil {
		return err
	}
	rn.settle(ctx, store, run.ID)

	return nil
}

// identifiers reads an action step's entities: one identifier or a list.
func identifiers(value any) []string {
	var out []string
	add := func(v any) {
		if text, ok := v.(string); ok && text != "" && !slices.Contains(out, text) {
			out = append(out, text)
		}
	}
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			add(item)
		}
	case []string:
		for _, item := range v {
			add(item)
		}
	default:
		add(v)
	}

	return out
}

// Wake tells the workflow that started a run, if any, that it ended.
func (rn Runner) Wake(ctx context.Context, id string) {
	store := NewStore(rn.db)
	run, err := store.Get(ctx, id)
	if err != nil || run.Parent == "" || !run.Finished() {
		return
	}
	rn.settle(ctx, store, run.Parent)
}

// settle ends the action step a run waits at once every run it started has
// ended, then carries the run on.
func (rn Runner) settle(ctx context.Context, store Store, id string) {
	if err := rn.trySettle(ctx, store, id); err != nil {
		rn.logger.Errorf("Couldn't check on the runs %s started: %v", id, err)
	}
}

func (rn Runner) trySettle(ctx context.Context, store Store, id string) error {
	run, err := store.Get(ctx, id)
	if err != nil || run.Status != RunWaiting {
		return err
	}
	position := slices.IndexFunc(run.Steps, func(s RunStep) bool { return s.Status == StepWaiting && s.Type == StepAction })
	if position < 0 {
		return nil
	}
	source, err := store.source(ctx, run.documentID, run.Version)
	if err != nil {
		return err
	}
	spec, _ := parseAny(source)
	step := spec.Steps[position]
	children, err := store.children(ctx, id, position)
	if err != nil {
		return err
	}
	detail, _ := run.Steps[position].Detail.(map[string]any)
	refused, _ := detail["refused"].(map[string]any)

	runs := []map[string]any{}
	succeeded, failed := 0, 0
	for _, child := range children {
		if !child.Finished() {
			return nil
		}
		runs = append(runs, map[string]any{
			"id": child.ID, "entity": child.Entity.Identifier, "title": child.Entity.Title,
			"status": child.Status, "outputs": child.Outputs, "error": child.Error,
		})
		if child.Status == RunSucceeded {
			succeeded++
		} else {
			failed++
		}
	}
	for identifier, reason := range refused {
		if _, ran := children[identifier]; !ran {
			runs = append(runs, map[string]any{"entity": identifier, "status": RunFailed, "error": reason})
			failed++
		}
	}
	slices.SortFunc(runs, func(a, b map[string]any) int {
		return compareText(a["entity"], b["entity"])
	})
	output := map[string]any{"runs": runs, "succeeded": float64(succeeded), "failed": float64(failed)}
	failure := ""
	if failed > step.Action.MaxFailures {
		failure = fmt.Sprintf("%d of %d runs of %s failed", failed, failed+succeeded, step.Action.Name)
	}
	resumed, err := store.endChildStep(ctx, id, position, output, failure, step.Continue)
	if err != nil {
		return err
	}
	if resumed {
		rn.Start(id)
	} else if failure != "" {
		rn.Wake(ctx, id)
	}

	return nil
}

func compareText(a, b any) int {
	x, _ := a.(string)
	y, _ := b.(string)
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}

	return 0
}
