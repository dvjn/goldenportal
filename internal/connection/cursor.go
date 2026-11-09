package connection

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// Where a sync reads each resource from. A complete sync reads in full what
// a reconcile source reads, since it deletes what it doesn't see. Anything
// else is read from where the last sync left off in each scope, such as a
// project: an incremental sync's reconcile sources, and upsert sources on
// every sync. Where no sync has read yet, upsert sources read as far back
// as the blueprints they write keep rows, and reconcile ones in full. A
// blueprint published to keep rows longer makes its resources read so
// again, as what its retention left unread before may now be kept. So
// does the connection published to read a resource differently, as
// readChanges says, as what its syncs left unwritten before may now be
// written; published to change anything else, such as its schedule, it
// reads on from where the last sync left off.

// cursorOverlap is how far before the last sync started the next one
// reads, so nothing that changed while it read, or that the system dated
// a little early, is missed.
const cursorOverlap = time.Minute

// syncCursors are where a sync in mode reads each of resources from, given
// where earlier syncs left off, stored, the published blueprints, when each
// was last published to keep rows longer, longer, and when the connection
// was last published to read each differently, changed.
func (s Spec) syncCursors(mode string, resources []string, stored map[string]map[string]time.Time,
	published map[string]blueprint.Spec, longer map[string]time.Time, changed readChanges, now time.Time,
) map[string]Cursor {
	cursors := map[string]Cursor{}
	for _, resource := range resources {
		reconciled := s.reconciles(resource)
		if mode == ModeComplete && reconciled {
			continue
		}
		since := changed.since(resource)
		for _, m := range s.sources {
			if m.Resource == resource && longer[m.Blueprint].After(since) {
				since = longer[m.Blueprint]
			}
		}
		c := Cursor{Scopes: map[string]time.Time{}}
		for scope, at := range stored[resource] {
			if !at.Before(since) {
				c.Scopes[scope] = at.Add(-cursorOverlap)
			}
		}
		if !reconciled {
			if keep := s.longestKeep(resource, published); keep > 0 {
				c.Floor = now.Add(-keep)
			}
		}
		cursors[resource] = c
	}

	return cursors
}

// longestKeep is the longest any blueprint the sources of resource write
// keeps rows, or zero when one keeps them for ever.
func (s Spec) longestKeep(resource string, published map[string]blueprint.Spec) time.Duration {
	var keep time.Duration
	for _, m := range s.sources {
		if m.Resource != resource {
			continue
		}
		longest := published[m.Blueprint].Longest()
		if longest == 0 {
			return 0
		}
		keep = max(keep, longest)
	}

	return keep
}

// readLog notes, as a pull asks From, which scopes of each resource it
// read and from when.
type readLog struct {
	mu     sync.Mutex
	scopes map[string]map[string]bool
	// since is the earliest time each resource was read from, and whole
	// those read in full somewhere.
	since map[string]time.Time
	whole map[string]bool
}

func newReadLog() *readLog {
	return &readLog{scopes: map[string]map[string]bool{}, since: map[string]time.Time{}, whole: map[string]bool{}}
}

func (l *readLog) note(resource, scope string, since time.Time) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.scopes[resource] == nil {
		l.scopes[resource] = map[string]bool{}
	}
	l.scopes[resource][scope] = true
	switch earliest, ok := l.since[resource]; {
	case since.IsZero():
		l.whole[resource] = true
	case !ok || since.Before(earliest):
		l.since[resource] = since
	}
}

// readSince is the earliest time each resource was read from, of those it
// read only from a time on everywhere.
func (l *readLog) readSince() map[string]time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]time.Time{}
	for resource, since := range l.since {
		if !l.whole[resource] {
			out[resource] = since
		}
	}

	return out
}

// cursorKey is a resource in a scope.
type cursorKey struct{ resource, scope string }

// readWithout lists what a pull read without failing, which the next sync
// reads from where this one started: each scope of each resource it asked
// about, but for resources that failed as a whole and scopes that failed.
func (l *readLog) readWithout(report Report) []cursorKey {
	l.mu.Lock()
	defer l.mu.Unlock()
	failed := map[cursorKey]bool{}
	for _, f := range report.ScopeFailures {
		failed[cursorKey{f.Resource, f.Scope}] = true
	}
	var keys []cursorKey
	for _, resource := range slices.Sorted(maps.Keys(l.scopes)) {
		if report.Failed[resource] != nil {
			continue
		}
		for _, scope := range slices.Sorted(maps.Keys(l.scopes[resource])) {
			if key := (cursorKey{resource, scope}); !failed[key] {
				keys = append(keys, key)
			}
		}
	}

	return keys
}

// cursors are where a connection's syncs left off in each scope of each
// resource.
func (s Store) cursors(ctx context.Context, connectionID string) (map[string]map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT resource, scope, read_at FROM sync_cursors WHERE connection_id = $1`, connectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]time.Time{}
	for rows.Next() {
		var (
			resource, scope string
			at              time.Time
		)
		if err := rows.Scan(&resource, &scope, &at); err != nil {
			return nil, err
		}
		if out[resource] == nil {
			out[resource] = map[string]time.Time{}
		}
		out[resource][scope] = at
	}

	return out, rows.Err()
}

// keptLonger is when each of blueprints was last published to keep rows
// longer than the version before, of those ever published so.
func (s Store) keptLonger(ctx context.Context, blueprints []string) (map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.identifier, v.source, v.published_at FROM config_versions v JOIN config_documents d ON d.id = v.document_id
		WHERE d.kind = $1 AND d.identifier = ANY($2) ORDER BY d.identifier, v.version`, blueprint.Name, pq.Array(blueprints))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	var previous struct {
		identifier string
		spec       blueprint.Spec
	}
	for rows.Next() {
		var (
			identifier, source string
			at                 time.Time
		)
		if err := rows.Scan(&identifier, &source, &at); err != nil {
			return nil, err
		}
		spec, _ := blueprint.Parse(source)
		if identifier == previous.identifier && blueprint.KeepsLonger(previous.spec, spec) {
			out[identifier] = at
		}
		previous.identifier, previous.spec = identifier, spec
	}

	return out, rows.Err()
}

// advanceCursors records that a sync that started at read keys without
// failing.
func (s Store) advanceCursors(ctx context.Context, connectionID string, keys []cursorKey, at time.Time) error {
	if len(keys) == 0 {
		return nil
	}
	resources, scopes := make([]string, len(keys)), make([]string, len(keys))
	for i, k := range keys {
		resources[i], scopes[i] = k.resource, k.scope
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_cursors (connection_id, resource, scope, read_at)
		SELECT $1, k.resource, k.scope, $4 FROM unnest($2::text[], $3::text[]) AS k (resource, scope)
		ON CONFLICT (connection_id, resource, scope) DO UPDATE SET read_at = greatest(sync_cursors.read_at, excluded.read_at)`,
		connectionID, pq.Array(resources), pq.Array(scopes), at)

	return err
}
