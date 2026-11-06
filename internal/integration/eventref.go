package integration

import (
	"context"
	"fmt"
)

// eventRef is an object a webhook event is about, to read again with a C,
// a system's client.
type eventRef[C any] struct {
	resource string
	parent   map[string]any
	// fetch reads the object, shaped as a sync gives it, or nil when it
	// shouldn't be written.
	fetch func(ctx context.Context, client C) (map[string]any, error)
	// gone is what the event says of an object it says was deleted, which
	// is deleted if the system no longer has it.
	gone map[string]any
}

// resolveRefs reads each object again, so mappings see what a sync would.
// One the event says was deleted is deleted only once the system answers
// that it's gone, so an event that comes late or again can't delete what's
// there. Otherwise one that's gone gives no item: a deletion event or the
// next sync says which. keep says whether a sync would read an object. The
// first that can't be read fails them all, for the run to record.
func resolveRefs[C any](ctx context.Context, client C, system, event string, refs []eventRef[C],
	keep func(resource string, object map[string]any) bool,
) ([]Item, error) {
	var items []Item
	for _, ref := range refs {
		object, err := ref.fetch(ctx, client)
		gone := apiGone(err)
		deleted := false
		switch {
		case gone && ref.gone != nil:
			object, deleted = ref.gone, true
		case gone:
			continue
		case err != nil:
			return nil, FetchError{fmt.Errorf("%s didn't answer when asked again for the %s of a webhook event, %s, so nothing was written, "+
				"and the next sync catches up: %w", system, ref.resource, event, err)}
		case object == nil || !keep(ref.resource, object):
			continue
		}
		if ref.parent != nil {
			object[parentKey] = ref.parent
		}
		items = append(items, Item{Resource: ref.resource, Object: object, Deleted: deleted})
	}

	return items, nil
}
