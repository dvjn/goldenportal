package integration

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// What reading a code host whole on each sync takes, whichever it is:
// what was asked for, what failed, and what piles up within its history.

// parentKey is the key items carry what they belong to under, such as their
// repository, since the objects a system gives don't all say. Underscored,
// it's unlikely to clash with a field of theirs, and a mapping reads it the
// same whatever the system.
const parentKey = "_gp"

// pullRun is one sync's reading of a system: what it was asked for, what it
// emitted, and what failed. Reads of different parts may run at once.
type pullRun struct {
	want map[string]bool
	// cutoff is when what piles up must be newer than to be read.
	cutoff   time.Time
	mu       sync.Mutex
	emit     func(resource string, items []Item) error
	failures map[string][]error
}

func newPullRun(resources []string, cutoff time.Time, emit func(resource string, items []Item) error) *pullRun {
	run := &pullRun{want: map[string]bool{}, cutoff: cutoff, emit: emit, failures: map[string][]error{}}
	for _, resource := range resources {
		run.want[resource] = true
	}

	return run
}

func (p *pullRun) wantsAny(resources []string) bool {
	return slices.ContainsFunc(resources, func(r string) bool { return p.want[r] })
}

// fail records why part of a resource couldn't be read, so a complete sync
// deletes none of it, unless err is a failure to write what was read, an
// emitError, which stops the sync and is returned.
func (p *pullRun) fail(resources []string, scope string, err error) error {
	if err == nil {
		return nil
	}
	var emitErr emitError
	if errors.As(err, &emitErr) {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, resource := range resources {
		if !p.want[resource] {
			continue
		}
		failure := fmt.Errorf("%s: %w", resource, err)
		if scope != "" {
			failure = fmt.Errorf("%s of %s: %w", resource, scope, err)
		}
		p.failures[resource] = append(p.failures[resource], failure)
	}

	return nil
}

// failAll fails every resource asked for, as when the connector can't sign
// in at all.
func (p *pullRun) failAll(err error) map[string]error {
	failed := map[string]error{}
	for resource := range p.want {
		failed[resource] = err
	}

	return failed
}

// result is what Pull returns once reading ended with err: what failed, or
// the failure to write what was read, which stops the sync.
func (p *pullRun) result(err error) (map[string]error, error) {
	var emitErr emitError
	if errors.As(err, &emitErr) {
		return nil, emitErr.err
	}
	if err != nil {
		return nil, err
	}

	return p.failed(), nil
}

// failed is each failed resource's first failure, and how many more.
func (p *pullRun) failed() map[string]error {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]error{}
	for resource, errs := range p.failures {
		out[resource] = errs[0]
		if len(errs) > 1 {
			out[resource] = fmt.Errorf("%w, and %d more failed", errs[0], len(errs)-1)
		}
	}

	return out
}

// emitObjects hands objects of a resource that was asked for to emit, with
// what they belong to, one batch at a time, since the sync writes them as
// they come.
func (p *pullRun) emitObjects(resource string, objects []map[string]any, parent map[string]any) error {
	if !p.want[resource] || len(objects) == 0 {
		return nil
	}
	items := make([]Item, len(objects))
	for i, object := range objects {
		if parent != nil {
			object[parentKey] = parent
		}
		items[i] = Item{Resource: resource, Object: object}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.emit(resource, items); err != nil {
		return emitError{err}
	}

	return nil
}

// olderThan is a listing's stop for lists sorted newest first by field.
func (p *pullRun) olderThan(field string) func(map[string]any) bool {
	return func(object map[string]any) bool {
		at := timeOf(object, field)
		return !at.IsZero() && at.Before(p.cutoff)
	}
}

// listing is a list to read.
type listing struct {
	// key names the list in answers that wrap it.
	path, key string
	// keep leaves out the objects it's false for.
	keep func(map[string]any) bool
	// stop is true for the first object older than wanted, in a list
	// sorted newest first.
	stop func(map[string]any) bool
}

// pages hands page each page's objects that the listing keeps, until one
// it stops at.
func (l listing) pages(page func([]map[string]any) error) func([]map[string]any) (bool, error) {
	return func(objects []map[string]any) (bool, error) {
		var out []map[string]any
		for _, object := range objects {
			if l.stop != nil && l.stop(object) {
				return false, page(out)
			}
			if l.keep == nil || l.keep(object) {
				out = append(out, object)
			}
		}
		return true, page(out)
	}
}

// current reports whether an object a sync reads within its history is
// one it reads: in one of the open states, or ended, as the first of fields
// that's set says, since cutoff. Each field is a time that doesn't change
// once set, so an object never drifts in and out.
func current(object map[string]any, cutoff time.Time, open []string, fields ...string) bool {
	if state, _ := object["state"].(string); slices.Contains(open, state) {
		return true
	}
	for _, field := range fields {
		if at := timeOf(object, field); !at.IsZero() {
			return !at.Before(cutoff)
		}
	}

	return false
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
