//go:build e2e

package entity

import (
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/filter"
)

const timedServiceYAML = `kind: Blueprint
metadata: {identifier: service, title: Service}
spec:
  properties:
    environment: {type: string}
    startedAt: {type: string, format: date-time}
`

// startedWrite is a write of a row its blueprint's retention keeps by
// startedAt, or one without it when at is zero.
func startedWrite(identifier string, at time.Time, properties map[string]any) Write {
	if properties == nil {
		properties = map[string]any{}
	}
	w := entityWrite(identifier, identifier, properties)
	if !at.IsZero() {
		properties["startedAt"] = at.UTC().Format(time.RFC3339)
		w.OccurredAt = at
	}
	return w
}

func (f *storeFixture) occurredAt(identifier string) time.Time {
	f.t.Helper()
	var at sql.NullTime
	if err := f.db.QueryRowContext(f.t.Context(), `SELECT occurred_at FROM entities WHERE blueprint_id = $1 AND identifier = $2`,
		f.blueprint, identifier).Scan(&at); err != nil {
		f.t.Fatal(err)
	}

	return at.Time
}

func (f *storeFixture) publishBlueprint(source string) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.t.Context(), `UPDATE config_versions SET source = $1 WHERE document_id = $2`, source, f.blueprint); err != nil {
		f.t.Fatal(err)
	}
}

// TestWriteAllKeepsTheRetainedTime checks a row stores the time it's kept
// by, and a write that changes only that is a change.
func TestWriteAllKeepsTheRetainedTime(t *testing.T) {
	f := newStoreFixture(t)
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	untimed := startedWrite("p1", at, nil)
	untimed.OccurredAt = time.Time{}
	f.write(f.mine, untimed)
	if got := f.write(f.mine, startedWrite("p1", at, nil)); !slices.Equal(got, []string{Updated}) {
		t.Fatalf("filling the time = %v", got)
	}
	if stored := f.occurredAt("p1"); !stored.Equal(at) {
		t.Fatalf("stored %s", stored)
	}
	if got := f.write(f.mine, startedWrite("p1", at, nil)); !slices.Equal(got, []string{Unchanged}) {
		t.Fatalf("the same again = %v", got)
	}
}

// TestExpire checks retention deletes rows whose by is older than the
// blueprint keeps them, unless a rule keeps them longer, and never rows
// whose by is empty.
func TestExpire(t *testing.T) {
	f := newStoreFixture(t)
	f.publishBlueprint(timedServiceYAML + `  retention:
    by: startedAt
    keep: 7d
    rules:
      - filter: {property: environment, op: eq, value: production}
        keep: 30d
`)
	now := time.Now()
	daysAgo := func(n int) time.Time { return now.Add(-time.Duration(n) * 24 * time.Hour) }
	f.write(f.mine,
		startedWrite("new", daysAgo(1), map[string]any{"environment": "review"}),
		startedWrite("old", daysAgo(8), map[string]any{"environment": "review"}),
		startedWrite("old-production", daysAgo(8), map[string]any{"environment": "production"}),
		startedWrite("ancient-production", daysAgo(31), map[string]any{"environment": "production"}),
		startedWrite("not-started", time.Time{}, map[string]any{"environment": "review"}),
	)

	deleted, err := ExpireAll(t.Context(), f.db)
	if err != nil || deleted != 2 {
		t.Fatalf("deleted %d, %v", deleted, err)
	}
	if got := f.identifiers(); got != "new,not-started,old-production" {
		t.Fatalf("left %s", got)
	}
	if deleted, err := ExpireAll(t.Context(), f.db); err != nil || deleted != 0 {
		t.Fatalf("again deleted %d, %v", deleted, err)
	}
}

// TestExpireKeepsWithoutRetention checks a blueprint without retention
// keeps every row, however old.
func TestExpireKeepsWithoutRetention(t *testing.T) {
	f := newStoreFixture(t)
	f.publishBlueprint(timedServiceYAML)
	f.write(f.mine, startedWrite("ancient", time.Now().Add(-5*365*24*time.Hour), nil))
	if deleted, err := ExpireAll(t.Context(), f.db); err != nil || deleted != 0 {
		t.Fatalf("deleted %d, %v", deleted, err)
	}
}

// TestExpireInBatches checks a backlog larger than a batch all goes.
func TestExpireInBatches(t *testing.T) {
	f := newStoreFixture(t)
	f.publishBlueprint(timedServiceYAML + "  retention: {by: startedAt, keep: 30d}\n")
	if _, err := f.db.ExecContext(t.Context(), `
		INSERT INTO entities (blueprint_id, identifier, title, connection_id, occurred_at)
		SELECT $1, 'e' || n, 'e', $2, now() - interval '40 days' FROM generate_series(1, $3) n`,
		f.blueprint, f.mine, expireBatch+10); err != nil {
		t.Fatal(err)
	}
	if deleted, err := ExpireAll(t.Context(), f.db); err != nil || deleted != expireBatch+10 {
		t.Fatalf("deleted %d, %v", deleted, err)
	}
}

// TestRetainedPropertyReadsTheColumn checks filters and sorts on the
// property rows are kept by answer from occurred_at as they would from the
// property.
func TestRetainedPropertyReadsTheColumn(t *testing.T) {
	f := newStoreFixture(t)
	source := timedServiceYAML + "  retention: {by: startedAt, keep: 30d}\n"
	f.publishBlueprint(source)
	now := time.Now().Truncate(time.Second)
	f.write(f.mine,
		startedWrite("a", now.Add(-2*time.Hour), nil),
		startedWrite("b", now.Add(-26*time.Hour), nil),
		startedWrite("c", time.Time{}, nil),
	)
	spec, _ := blueprint.Parse(source)
	specs := map[string]blueprint.Spec{"service": spec}
	c := &compiler{spec: spec, specs: specs}
	where, err := c.compile(filter.Filter{Property: "startedAt", Op: filter.OpWithin, Value: "1d"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := "(e.occurred_at >= now() - $1::interval)"; where != want {
		t.Fatalf("within compiles to %s, want %s", where, want)
	}
	order, err := c.orderBy("-startedAt", nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := f.db.QueryContext(t.Context(), `SELECT e.identifier FROM entities e WHERE `+where+` ORDER BY `+order, c.args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	if !slices.Equal(got, []string{"a"}) {
		t.Fatalf("within 1d = %v, want a", got)
	}
}
