package entity

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"time"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

// Outcomes of a write.
const (
	Created   = "created"
	Updated   = "updated"
	Unchanged = "unchanged"
	// Older is a write whose source answered before the stored row's did,
	// so it would have taken the row backwards.
	Older = "older"
)

// Shared is the scope of a row written from more than one scope, which
// only a run that read every scope can judge.
const Shared = "*"

// Write is an entity an integration writes.
type Write struct {
	BlueprintID string
	Entity      blueprint.Entity
	Computed    map[string]any
	// Scope is the part of its system the entity was read from, which the
	// store doesn't interpret, or empty for none.
	Scope string
	// SourceAt is when its system answered with what the entity says, or
	// zero when the writer doesn't know, which never holds a write back.
	SourceAt time.Time
	// OccurredAt is the time its blueprint's retention keeps it by, or zero
	// for none, which retention never deletes.
	OccurredAt time.Time
}

// Written is what became of a Write. Owner names the integration the
// entity belongs to when it's another's, and nothing was written.
type Written struct {
	Outcome string
	Owner   string
}

// Key names an entity within the store.
type Key struct{ BlueprintID, Identifier string }

// WriteAll writes entities as an integration, writing only those that are
// new or changed. Of writes to one entity, the last counts and the rest are
// unchanged. Rows are written in key order, so concurrent writers take
// their locks in the same order. Every entity it was given is seen: one
// marked missed is unmarked, changed or not.
func (s Store) WriteAll(ctx context.Context, integrationID string, writes []Write) ([]Written, error) {
	out := make([]Written, len(writes))
	last := map[Key]int{}
	mixed := map[Key]bool{}
	for i, w := range writes {
		k := Key{w.BlueprintID, w.Entity.Identifier}
		if j, ok := last[k]; ok {
			out[j] = Written{Outcome: Unchanged}
			mixed[k] = mixed[k] || writes[j].Scope != w.Scope
		}
		last[k] = i
	}
	rows := make([]writeRow, 0, len(last))
	for k, i := range last {
		w := writes[i]
		r := writeRow{index: i, key: k, title: w.Entity.Title, scope: w.Scope, sourceAt: w.SourceAt, occurredAt: w.OccurredAt}
		if mixed[k] {
			r.scope = Shared
		}
		r.properties, _ = json.Marshal(w.Entity.Properties)
		r.relations, _ = json.Marshal(w.Entity.Relations)
		r.computed, _ = json.Marshal(w.Computed)
		rows = append(rows, r)
	}
	slices.SortFunc(rows, func(a, b writeRow) int {
		return cmp.Or(cmp.Compare(a.key.BlueprintID, b.key.BlueprintID), cmp.Compare(a.key.Identifier, b.key.Identifier))
	})

	var sighted []Key
	classify := func(r writeRow, st stored) bool {
		switch {
		case st.ownerID != integrationID:
			out[r.index] = Written{Owner: st.owner}
		case st.same && promote(st.scope, r.scope) == st.scope:
			out[r.index] = Written{Outcome: Unchanged}
		case olderThan(r.sourceAt, st.sourceAt):
			out[r.index] = Written{Outcome: Older}
		default:
			return false
		}
		if st.missed && st.ownerID == integrationID {
			sighted = append(sighted, r.key)
		}
		return true
	}

	found, err := s.compare(ctx, rows)
	if err != nil {
		return nil, err
	}
	var changed []writeRow
	for i, r := range rows {
		if st, ok := found[i]; !ok || !classify(r, st) {
			changed = append(changed, r)
		}
	}
	created, err := s.upsert(ctx, integrationID, changed)
	if err != nil {
		return nil, err
	}
	// A row the upsert left alone was written by someone else between the
	// read and the write: it's theirs, or holds a newer read, or the same.
	var raced []writeRow
	for _, r := range changed {
		switch isNew, ok := created[r.key]; {
		case ok && isNew:
			out[r.index] = Written{Outcome: Created}
		case ok:
			out[r.index] = Written{Outcome: Updated}
		default:
			raced = append(raced, r)
		}
	}
	if len(raced) > 0 {
		found, err := s.compare(ctx, raced)
		if err != nil {
			return nil, err
		}
		for i, r := range raced {
			if st, ok := found[i]; !ok || !classify(r, st) {
				out[r.index] = Written{Outcome: Unchanged}
			}
		}
	}

	return out, s.Sighted(ctx, integrationID, sighted)
}

// writeRow is a write as the statements take it.
type writeRow struct {
	index                           int
	key                             Key
	title, scope                    string
	properties, relations, computed []byte
	sourceAt, occurredAt            time.Time
}

// stored is what the store holds of an entity a write names.
type stored struct {
	ownerID, owner string
	// same is whether its content, and its time, are what the write
	// would set.
	same     bool
	scope    string
	sourceAt time.Time
	missed   bool
}

// promote is the scope a row stored with scope gets from a write from
// written: a row written from a second scope, or from none over one, is
// shared. One stored with none takes the writer's, as rows written before
// rows had scopes have none.
func promote(stored, written string) string {
	if stored == "" || stored == written {
		return written
	}

	return Shared
}

// olderThan reports whether a write whose source answered at written is
// older than what's stored, answered at stored. Either unknown is never
// older.
func olderThan(written, stored time.Time) bool {
	return !written.IsZero() && !stored.IsZero() && written.Before(stored)
}

// promoteSQL is promote, for the row being upserted.
const promoteSQL = `CASE WHEN e.scope IS NULL OR e.scope IS NOT DISTINCT FROM excluded.scope THEN excluded.scope ELSE '` + Shared + `' END`

// rowArrays are rows as the arrays the statements unnest.
func rowArrays(rows []writeRow) []any {
	var blueprints, identifiers, titles, properties, relations, computed, scopes, sourceAts, occurredAts []string
	for _, r := range rows {
		blueprints, identifiers = append(blueprints, r.key.BlueprintID), append(identifiers, r.key.Identifier)
		titles, scopes = append(titles, r.title), append(scopes, r.scope)
		properties, relations, computed = append(properties, string(r.properties)), append(relations, string(r.relations)), append(computed, string(r.computed))
		sourceAts, occurredAts = append(sourceAts, timeText(r.sourceAt)), append(occurredAts, timeText(r.occurredAt))
	}

	return []any{
		pq.Array(blueprints), pq.Array(identifiers), pq.Array(titles), pq.Array(properties), pq.Array(relations),
		pq.Array(computed), pq.Array(scopes), pq.Array(sourceAts), pq.Array(occurredAts),
	}
}

// timeText is a time as the statements take it, "" for none.
func timeText(at time.Time) string {
	if at.IsZero() {
		return ""
	}

	return at.UTC().Format(time.RFC3339Nano)
}

const unnestRows = `unnest($1::uuid[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[],
	$9::text[])`

// compare reads what's stored of each row, by its position in rows. Rows
// with nothing stored are left out. Content is compared in Postgres, as
// jsonb, since Go's JSON and Postgres's differ in form but not in value.
func (s Store) compare(ctx context.Context, rows []writeRow) (map[int]stored, error) {
	found := map[int]stored{}
	if len(rows) == 0 {
		return found, nil
	}
	result, err := s.db.QueryContext(ctx, `
		SELECT i.n - 1, e.integration_id, o.identifier,
			e.title = i.title AND e.properties = i.properties::jsonb AND e.relations = i.relations::jsonb
				AND e.computed = i.computed::jsonb
				AND e.occurred_at IS NOT DISTINCT FROM nullif(i.occurred_at, '')::timestamptz,
			coalesce(e.scope, ''), e.source_at, e.missed_at IS NOT NULL
		FROM `+unnestRows+` WITH ORDINALITY AS i (blueprint_id, identifier, title, properties, relations, computed, scope, source_at,
			occurred_at, n)
		JOIN entities e ON e.blueprint_id = i.blueprint_id AND e.identifier = i.identifier
		JOIN config_documents o ON o.id = e.integration_id`, rowArrays(rows)...)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	for result.Next() {
		var (
			n        int
			st       stored
			sourceAt sql.NullTime
		)
		if err := result.Scan(&n, &st.ownerID, &st.owner, &st.same, &st.scope, &sourceAt, &st.missed); err != nil {
			return nil, err
		}
		st.sourceAt = sourceAt.Time
		found[n] = st
	}

	return found, result.Err()
}

// upsert writes rows as the integration, unless one is another's, holds the
// same, or holds a newer read, and says which it wrote, and whether each
// was new.
func (s Store) upsert(ctx context.Context, integrationID string, rows []writeRow) (map[Key]bool, error) {
	written := map[Key]bool{}
	if len(rows) == 0 {
		return written, nil
	}
	result, err := s.db.QueryContext(ctx, `
		INSERT INTO entities AS e (blueprint_id, identifier, title, properties, relations, computed, integration_id, scope, source_at,
			occurred_at)
		SELECT i.blueprint_id, i.identifier, i.title, i.properties::jsonb, i.relations::jsonb, i.computed::jsonb, $10,
			nullif(i.scope, ''), nullif(i.source_at, '')::timestamptz, nullif(i.occurred_at, '')::timestamptz
		FROM `+unnestRows+` AS i (blueprint_id, identifier, title, properties, relations, computed, scope, source_at, occurred_at)
		ON CONFLICT (blueprint_id, identifier) DO UPDATE
			SET title = excluded.title, properties = excluded.properties, relations = excluded.relations,
				computed = excluded.computed, scope = `+promoteSQL+`, source_at = excluded.source_at,
				occurred_at = excluded.occurred_at, updated_at = now(), missed_at = NULL
			WHERE e.integration_id = excluded.integration_id
				AND ((e.title, e.properties, e.relations, e.computed, e.occurred_at)
						IS DISTINCT FROM (excluded.title, excluded.properties, excluded.relations, excluded.computed, excluded.occurred_at)
					OR e.scope IS DISTINCT FROM `+promoteSQL+`)
				AND (e.source_at IS NULL OR excluded.source_at IS NULL OR excluded.source_at >= e.source_at)
		RETURNING e.blueprint_id, e.identifier, xmax = 0`, append(rowArrays(rows), integrationID)...)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	for result.Next() {
		var (
			k     Key
			isNew bool
		)
		if err := result.Scan(&k.BlueprintID, &k.Identifier, &isNew); err != nil {
			return nil, err
		}
		written[k] = isNew
	}

	return written, result.Err()
}

// Upsert writes one entity as an integration, as WriteAll does.
func (s Store) Upsert(ctx context.Context, integrationID string, w Write) (string, error) {
	written, err := s.WriteAll(ctx, integrationID, []Write{w})
	if err != nil {
		return "", err
	}
	if written[0].Owner != "" {
		return "", OwnedError{Integration: written[0].Owner}
	}

	return written[0].Outcome, nil
}

// Sighted unmarks entities an integration wrote that were marked missed,
// for any sighting of them, even an item that couldn't be written: a
// complete run keeps what was seen, even when what was seen was wrong.
func (s Store) Sighted(ctx context.Context, integrationID string, keys []Key) error {
	if len(keys) == 0 {
		return nil
	}
	var blueprints, identifiers []string
	for _, k := range keys {
		blueprints, identifiers = append(blueprints, k.BlueprintID), append(identifiers, k.Identifier)
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE entities e SET missed_at = NULL
		FROM unnest($2::uuid[], $3::text[]) AS i (blueprint_id, identifier)
		WHERE e.blueprint_id = i.blueprint_id AND e.identifier = i.identifier AND e.integration_id = $1
			AND e.missed_at IS NOT NULL`, integrationID, pq.Array(blueprints), pq.Array(identifiers))

	return err
}

// Unit is the rows of one blueprint a complete run judges: those of the
// scopes it read in full, those with no scope or a shared one when it read
// everything they could have come from, and those of scopes it didn't list
// at all when every listing succeeded.
type Unit struct {
	BlueprintID string
	// Seen are the identifiers the run saw, from any scope.
	Seen     []string
	Scopes   []string
	Unscoped bool
	// Gone covers rows of scopes not in Listed.
	Gone   bool
	Listed []string
}

const unitRows = `integration_id = $1 AND blueprint_id = $2 AND NOT (identifier = ANY($3))
	AND (scope = ANY($4) OR ($5 AND (scope IS NULL OR scope = '` + Shared + `'))
		OR ($6 AND scope <> '` + Shared + `' AND NOT (scope = ANY($7))))`

// Reconcile deletes, after a complete run that started at before, the
// entities in units an integration wrote that neither that run nor the one
// before it that covered them saw. One missed for the first time is
// marked missed instead. A run that misses an entity may have missed it
// only because a list shifted under it, or a resource was briefly hidden;
// one that missed it after the run before it did, with nothing seeing it
// between, didn't.
// A mark is when it was made, as the run that made it finished, and counts
// only for a run that started after it: runs that overlap count once, while
// two that don't, such as a complete push sent again after the first
// finished, count as two. Any sighting since unmarks it, and one made
// since the run started, which the run can't have seen, isn't marked.
// Only runs since the integration was last published to read what it
// reconciles differently, at since, count, as others read with other
// mappings: marks made before since count for nothing, and a run that
// started before since, so read with the old mappings however late it
// finishes, neither deletes nor marks.
func (s Store) Reconcile(ctx context.Context, integrationID string, units []Unit, before, since time.Time) (int, error) {
	if before.Before(since) {
		return 0, nil
	}
	// A nil list is NULL to Postgres, which matches nothing, where an empty
	// one is the empty list it means.
	list := func(l []string) any { return pq.Array(append([]string{}, l...)) }
	deleted := 0
	for _, u := range units {
		args := []any{integrationID, u.BlueprintID, list(u.Seen), list(u.Scopes), u.Unscoped, u.Gone, list(u.Listed), before}
		result, err := s.db.ExecContext(ctx, `DELETE FROM entities WHERE `+unitRows+`
			AND missed_at < $8 AND missed_at >= $9`, append(args, since)...)
		if err != nil {
			return 0, err
		}
		n, _ := result.RowsAffected()
		deleted += int(n)
		if _, err := s.db.ExecContext(ctx, `UPDATE entities SET missed_at = clock_timestamp() WHERE `+unitRows+`
			AND created_at < $8`, args...); err != nil {
			return 0, err
		}
	}

	return deleted, nil
}
