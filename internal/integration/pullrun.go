package integration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/dvjn/goldenportal/internal/telemetry"
)

// What reading a code host on each sync takes, whichever it is: what was
// asked for, and what failed.

// parentKey is the key items carry what they belong to under, such as their
// repository, since the objects a system gives don't all say. Underscored,
// it's unlikely to clash with a field of theirs, and a mapping reads it the
// same whatever the system.
const parentKey = "_gp"

// pullRun is one sync's reading of a system: what it was asked for, what it
// emitted, and what failed. Reads of different parts may run at once.
type pullRun struct {
	want map[string]bool
	mu   sync.Mutex
	emit func(resource string, items []Item) error
	// failures are each resource's failures as a whole, and scoped those in
	// one scope.
	failures map[string][]error
	scoped   []ScopeFailure
	// unlisted are the listings of scopes that failed.
	unlisted []error
}

func newPullRun(resources []string, emit func(resource string, items []Item) error) *pullRun {
	run := &pullRun{want: map[string]bool{}, emit: emit, failures: map[string][]error{}}
	for _, resource := range resources {
		run.want[resource] = true
	}

	return run
}

func (p *pullRun) wantsAny(resources []string) bool {
	return slices.ContainsFunc(resources, func(r string) bool { return p.want[r] })
}

// fail records why part of a resource couldn't be read: in scope, or, with
// no scope, as a whole, so a complete sync deletes none of what that part
// holds. where names the part for people. An emitError, a failure to write
// what was read, isn't recorded but returned, as it stops the sync.
func (p *pullRun) fail(ctx context.Context, resources []string, where, scope string, err error) error {
	if err == nil {
		return nil
	}
	var emitErr emitError
	if errors.As(err, &emitErr) {
		return err
	}
	if p.wantsAny(resources) {
		attrs := []attribute.KeyValue{telemetry.Scope.String(where), attribute.StringSlice("gp.resources", resources)}
		// The sync goes on without it, so its span notes the failure rather
		// than failing.
		trace.SpanFromContext(ctx).AddEvent("scope not read", trace.WithAttributes(
			append(attrs, attribute.String("exception.message", telemetry.Scrub(err.Error())))...))
		telemetry.Warn(ctx, "scope not read", err, attrs...)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, resource := range resources {
		if !p.want[resource] {
			continue
		}
		failure := fmt.Errorf("%s: %w", resource, err)
		if where != "" {
			failure = fmt.Errorf("%s of %s: %w", resource, where, err)
		}
		if scope == "" {
			p.failures[resource] = append(p.failures[resource], failure)
			continue
		}
		p.scoped = append(p.scoped, ScopeFailure{
			Resource: resource, Scope: scope, Err: failure, Refused: apiStatus(err) == http.StatusForbidden,
		})
	}

	return nil
}

// unlist records why a listing of scopes, what, such as a group's
// projects, failed, so no scope it would have listed counts as gone.
func (p *pullRun) unlist(ctx context.Context, what string, err error) error {
	if err == nil {
		return nil
	}
	var emitErr emitError
	if errors.As(err, &emitErr) {
		return err
	}
	telemetry.Warn(ctx, "scopes not listed", err, attribute.String("gp.listing", what))
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unlisted = append(p.unlisted, fmt.Errorf("%s: %w", what, err))

	return nil
}

// failAll fails every resource asked for, as when the connector can't sign
// in at all.
func (p *pullRun) failAll(err error) Report {
	failed := map[string]error{}
	for resource := range p.want {
		failed[resource] = err
	}

	return Report{Failed: failed}
}

// result is what Pull returns once reading the scopes listed ended with
// err: what was read in full, or the failure to write what was read, which
// stops the sync.
func (p *pullRun) result(scopes []string, err error) (Report, error) {
	var emitErr emitError
	if errors.As(err, &emitErr) {
		return Report{}, emitErr.err
	}
	if err != nil {
		return Report{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	failed := map[string]error{}
	for resource, errs := range p.failures {
		failed[resource] = errs[0]
		if len(errs) > 1 {
			failed[resource] = fmt.Errorf("%w, and %d more failed", errs[0], len(errs)-1)
		}
	}

	return Report{Failed: failed, ScopeFailures: p.scoped, Scopes: scopes, Unlisted: p.unlisted}, nil
}

// source is where objects were read from: the scope they're in, if any,
// and when the system answered with them.
type source struct {
	scope string
	at    time.Time
}

// emitObjects hands objects of a resource that was asked for to emit, with
// what they belong to and where they were read from, one batch at a time,
// since the sync writes them as they come.
func (p *pullRun) emitObjects(resource string, objects []map[string]any, parent map[string]any, from source) error {
	if !p.want[resource] || len(objects) == 0 {
		return nil
	}
	items := make([]Item, len(objects))
	for i, object := range objects {
		if parent != nil {
			object[parentKey] = parent
		}
		items[i] = Item{Resource: resource, Object: object, Scope: from.scope, SourceAt: from.at}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.emit(resource, items); err != nil {
		return emitError{err}
	}

	return nil
}

// listing is a list to read.
type listing struct {
	// key names the list in answers that wrap it.
	path, key string
	// keep leaves out the objects it's false for.
	keep func(map[string]any) bool
}

// pages hands page each page's objects that the listing keeps.
func (l listing) pages(page func([]map[string]any) error) func([]map[string]any) (bool, error) {
	return func(objects []map[string]any) (bool, error) {
		var out []map[string]any
		for _, object := range objects {
			if l.keep == nil || l.keep(object) {
				out = append(out, object)
			}
		}
		return true, page(out)
	}
}

// forEach runs fn on each item, limit at a time, so a sync of many reads
// them within its time, while the credential's own limit on requests in
// flight keeps the system from being flooded. Once one fails, it starts no
// more, and returns the first failure.
func forEach[T any](items []T, limit int, fn func(T) error) error {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	slots := make(chan struct{}, limit)
	for _, item := range items {
		slots <- struct{}{}
		mu.Lock()
		stopped := first != nil
		mu.Unlock()
		if stopped {
			break
		}
		wg.Go(func() {
			defer func() { <-slots }()
			if err := fn(item); err != nil {
				mu.Lock()
				if first == nil {
					first = err
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	return first
}
