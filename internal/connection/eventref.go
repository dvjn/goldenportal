package connection

import (
	"context"
	"fmt"
	"time"
)

// eventRef is an object a webhook event is about, to read again with a C,
// a system's client.
type eventRef[C any] struct {
	resource string
	// scope is the part of the system it's in, as a sync gives it.
	scope  string
	parent map[string]any
	// fetch reads the object, shaped as a sync gives it, or nil when it
	// shouldn't be written.
	fetch func(ctx context.Context, client C) (map[string]any, error)
	// fetchAll, in place of fetch, reads several objects, such as each
	// attempt of a pipeline's jobs, shaped as a sync gives them.
	fetchAll func(ctx context.Context, client C) ([]map[string]any, error)
	// gone is what the event says of an object it says was deleted, which
	// is deleted if the system no longer has it.
	gone map[string]any
	// readAt is when the system answered with what else the object is made
	// from, if anything, such as its project read before it.
	readAt time.Time
}

// resolveRefs reads each object again, so sources see what a sync would.
// One the event says was deleted is deleted only once the system answers
// that it's gone, so an event that comes late or again can't delete what's
// there. Otherwise one that's gone gives no item: a deletion event or the
// next sync says which. The first that can't be read fails them all, for
// the run to record.
func resolveRefs[C any](ctx context.Context, client C, system, event string, refs []eventRef[C]) ([]Item, error) {
	var items []Item
	for _, ref := range refs {
		fetchCtx, clock := withSourceClock(ctx)
		var (
			found []map[string]any
			err   error
		)
		if ref.fetchAll != nil {
			found, err = ref.fetchAll(fetchCtx, client)
		} else {
			var object map[string]any
			if object, err = ref.fetch(fetchCtx, client); object != nil {
				found = []map[string]any{object}
			}
		}
		gone := apiGone(err)
		deleted := false
		switch {
		case gone && ref.gone != nil:
			found, deleted = []map[string]any{ref.gone}, true
		case gone:
			continue
		case err != nil:
			return nil, FetchError{fmt.Errorf("%s didn't answer when asked again for the %s of a webhook event, %s, so nothing was written, "+
				"and the next sync catches up: %w", system, ref.resource, event, err)}
		}
		at := earliest(ref.readAt, clock.take())
		for _, object := range found {
			if ref.parent != nil {
				object[parentKey] = ref.parent
			}
			items = append(items, Item{Resource: ref.resource, Object: object, Deleted: deleted, Scope: ref.scope, SourceAt: at})
		}
	}

	return items, nil
}
