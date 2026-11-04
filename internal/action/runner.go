package action

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/integration"
)

const (
	// lease is how long an instance holds a run without renewing it. Once
	// it runs out, as when the instance stops, another takes the run on.
	lease = time.Minute
	// renewEvery is also how soon a step under way notices it was cancelled.
	renewEvery = 5 * time.Second
)

// SweepSchedule is how often each instance looks for runs no instance
// holds, as a cron expression: new ones not started yet, and ones whose
// instance stopped.
const SweepSchedule = "* * * * *"

// Runner carries runs out. Every instance has one; they share the work
// through the database.
type Runner struct {
	db     integration.TxDB
	logger logging.Logger
	// baseURL is the portal's public URL, for links to runs and entities.
	baseURL string
}

func NewRunner(db integration.TxDB, logger logging.Logger, baseURL string) Runner {
	return Runner{db: db, logger: logger, baseURL: strings.TrimSuffix(baseURL, "/")}
}

// Start takes on a run straight away, rather than at the next sweep.
func (rn Runner) Start(id string) {
	go func() {
		ctx := context.Background()
		claimed, ok, err := NewStore(rn.db).claim(ctx, id, lease)
		if err != nil {
			rn.logger.Errorf("Couldn't take on run %s: %v", id, err)
			return
		}
		if ok {
			rn.execute(ctx, claimed)
		}
	}()
}

// Sweep ends approvals nobody gave in time, then takes on every run
// waiting for an instance.
func (rn Runner) Sweep(ctx context.Context) {
	store := NewStore(rn.db)
	if err := store.expire(ctx); err != nil {
		rn.logger.Errorf("Couldn't end overdue approvals: %v", err)
	}
	// A run can end without its workflow hearing, as when an instance
	// stops just then, so check on those still waiting.
	if parents, err := store.waitingParents(ctx); err != nil {
		rn.logger.Errorf("Couldn't look for workflows waiting on runs: %v", err)
	} else {
		for _, parent := range parents {
			rn.settle(ctx, store, parent)
		}
	}
	for {
		id, ok, err := store.claim(ctx, "", lease)
		if err != nil {
			rn.logger.Errorf("Couldn't look for runs: %v", err)
			return
		}
		if !ok {
			return
		}
		go rn.execute(context.Background(), id)
	}
}

// execute carries a claimed run on from its first step that isn't done,
// renewing the lease while it goes. It stops when the lease is lost, as
// when someone cancels the run.
func (rn Runner) execute(parent context.Context, id string) {
	ctx, stop := context.WithCancel(parent)
	defer stop()
	store := NewStore(rn.db)
	go func() {
		ticker := time.NewTicker(renewEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if held, err := store.renew(ctx, id, lease); err == nil && !held {
					stop()
					return
				}
			}
		}
	}()

	if err := rn.carryOn(ctx, store, id); err != nil && !errors.Is(err, context.Canceled) {
		rn.logger.Errorf("Run %s stopped: %v", id, err)
		_ = store.finish(context.Background(), id, RunFailed, "The portal couldn't carry the run on: "+err.Error(), nil)
	}
	rn.Wake(context.Background(), id)
}

func (rn Runner) carryOn(ctx context.Context, store Store, id string) error {
	run, err := store.Get(ctx, id)
	if err != nil {
		return err
	}
	source, err := store.source(ctx, run.documentID, run.Version)
	if err != nil {
		return err
	}
	spec, issues := parseAny(source)
	if configdoc.HasErrors(issues) || len(spec.Steps) != len(run.Steps) {
		return fmt.Errorf("version %d of %s no longer reads as it did", run.Version, run.Action)
	}

	scope := rn.scope(run)
	if !spec.IsWorkflow() {
		current, err := entity.NewStore(rn.db).Get(ctx, run.Entity.Blueprint, run.Entity.Identifier)
		switch {
		case errors.Is(err, entity.ErrNotFound):
			return store.finish(ctx, id, RunFailed, fmt.Sprintf("%s %s is gone from the catalog.", run.Entity.Blueprint, run.Entity.Identifier), nil)
		case err != nil:
			return err
		}
		scope.Entity = entity.Document(current)
	}

	for i, step := range spec.Steps {
		state := run.Steps[i]
		switch state.Status {
		case StepDone:
			scope.setStep(step.ID, state.Output)
			continue
		case StepFailed:
			// Only a step that carries on when it fails is failed here: a run
			// that stopped at one starts it again.
			scope.setStep(step.ID, nil)
			continue
		case StepRunning:
			// The instance running this step stopped. Only steps safe to
			// repeat run again; a person decides about the rest.
			if !step.repeatable() {
				return store.interrupt(ctx, id, i)
			}
		}
		if step.Type == StepApproval {
			return rn.await(ctx, store, run, i, step, scope)
		}
		if step.Type == StepAction {
			return rn.fanOut(ctx, store, run, i, step, scope)
		}

		// Renewing first finds a run cancelled since the last step.
		if held, err := store.renew(ctx, id, lease); err != nil || !held {
			return cmp.Or(err, context.Canceled)
		}

		output, err := rn.runStep(ctx, store, run, i, step, scope)
		if ctx.Err() != nil {
			_ = store.finishStep(context.Background(), id, i, StepFailed, nil, "The run was cancelled during this step.", nil)
			return ctx.Err()
		}
		if err != nil {
			var failure stepError
			message, detail := err.Error(), any(nil)
			if errors.As(err, &failure) {
				message, detail = failure.message, failure.detail
			}
			if err := store.finishStep(ctx, id, i, StepFailed, nil, message, detail); err != nil {
				return err
			}
			if step.Continue {
				scope.setStep(step.ID, nil)
				continue
			}
			return store.finish(ctx, id, RunFailed, step.Title+": "+message, nil)
		}
		if err := store.finishStep(ctx, id, i, StepDone, output, "", nil); err != nil {
			return err
		}
		scope.setStep(step.ID, output)
	}

	outputs := map[string]any{}
	for _, output := range spec.Outputs {
		outputs[output.Name] = scope.Resolve(output.Value)
	}

	return store.finish(ctx, id, RunSucceeded, "", outputs)
}

// await pauses the run at an approval step, with what approvers review
// filled in.
func (rn Runner) await(ctx context.Context, store Store, run Run, position int, step Step, scope Scope) error {
	a := step.Approval
	review := make([]map[string]any, 0, len(a.Review))
	for _, r := range a.Review {
		item := map[string]any{}
		if r.Title != "" {
			item["title"] = r.Title
		}
		if r.Markdown != "" {
			item["markdown"] = scope.Text(r.Markdown)
		} else {
			item["json"] = scope.Resolve(r.JSON)
		}
		review = append(review, item)
	}
	detail := map[string]any{"require": a.Require, "message": scope.Text(a.Message), "review": review}

	return store.wait(ctx, run.ID, position, detail, time.Now().Add(time.Duration(a.Timeout)))
}

// repeatable reports whether a step can run again after an instance stopped
// partway through it: the portal's own steps can, and HTTP steps the author
// let retry, which says the other side takes repeats.
func (s Step) repeatable() bool {
	return s.Type != StepHTTP || s.Retry != nil
}

func (rn Runner) scope(run Run) Scope {
	groups := make([]any, len(run.Requester.Groups))
	for i, group := range run.Requester.Groups {
		groups[i] = group
	}

	return Scope{
		Inputs: run.Inputs,
		Me: map[string]any{
			"id": run.Requester.ID, "email": run.Requester.Email, "name": run.Requester.Name,
			"role": run.Requester.Role, "groups": groups,
		},
		Run:   map[string]any{"id": run.ID, "url": rn.baseURL + "/runs/" + run.ID},
		Steps: map[string]any{},
	}
}

func (s Scope) setStep(id string, output any) {
	if id != "" {
		s.Steps[id] = output
	}
}

// stepError is why a step failed, as the run page shows it.
type stepError struct {
	message string
	detail  any
}

func (e stepError) Error() string { return e.message }

func failed(detail any, format string, args ...any) error {
	return stepError{message: fmt.Sprintf(format, args...), detail: detail}
}
