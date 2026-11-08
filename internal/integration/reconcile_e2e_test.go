//go:build e2e

package integration

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/pgtest"
)

// scopedFixture is an integration that writes pipelines from projects'
// pipelines, and users both from projects' pipelines and from groups'
// members.
type scopedFixture struct {
	t    *testing.T
	db   *gofrSQL.DB
	id   string
	spec Spec
}

func newScopedFixture(t *testing.T) *scopedFixture {
	t.Helper()
	db := pgtest.New(t)
	for _, bp := range []string{"pipeline", "user"} {
		pgtest.Publish(t, db, "blueprint", bp, "kind: Blueprint\nmetadata: {identifier: "+bp+", title: "+bp+"}\nspec:\n  properties:\n    status: {type: string}\n")
	}
	spec := Spec{Mappings: []Mapping{
		{Resource: "pipelines", Blueprint: "pipeline"},
		{Resource: "pipelines", Blueprint: "user"},
		{Resource: "members", Blueprint: "user"},
	}}

	return &scopedFixture{t: t, db: db, id: pgtest.Publish(t, db, Name, "code", "kind: Integration"), spec: spec}
}

func entry(bp, identifier, resource, scope string) Entry {
	return Entry{Blueprint: bp, Resource: resource, Scope: scope, Entity: blueprint.Entity{Identifier: identifier, Properties: map[string]any{}}}
}

// sync writes entries as one complete run and reconciles what report says
// was read in full, as a sync does, giving how many it deleted.
func (f *scopedFixture) sync(report Report, entries ...Entry) int {
	f.t.Helper()
	started, err := dbNow(f.t.Context(), f.db)
	if err != nil {
		f.t.Fatal(err)
	}
	run := &Run{Errors: []RunError{}}
	if err := writeBatched(f.t.Context(), f.db, f.id, entries, run); err != nil {
		f.t.Fatal(err)
	}
	deleted, err := reconcile(f.t.Context(), f.db, f.id, f.spec, []string{"pipelines", "members"}, report, sightingsOf(entries), started)
	if err != nil {
		f.t.Fatal(err)
	}

	return deleted
}

func (f *scopedFixture) stored() string {
	f.t.Helper()
	rows, err := f.db.QueryContext(f.t.Context(), `
		SELECT e.identifier || coalesce('@' || e.scope, '') FROM entities e ORDER BY e.identifier`)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, s)
	}

	return strings.Join(out, " ")
}

// TestReconcileByScope checks one project that fails holds back deleting
// only what was read from it, while a row more than one project wrote goes
// only once none of them, nor anything else, emits it.
func TestReconcileByScope(t *testing.T) {
	f := newScopedFixture(t)
	read := Report{Scopes: []string{"1", "2"}}
	f.sync(read,
		entry("pipeline", "p1", "pipelines", "1"), entry("pipeline", "p2", "pipelines", "1"), entry("pipeline", "p3", "pipelines", "2"),
		entry("user", "alice", "pipelines", "1"), entry("user", "bob", "pipelines", "1"), entry("user", "carol", "members", ""),
	)
	f.sync(read, entry("user", "alice", "pipelines", "2"))
	if got := f.stored(); got != "alice@* bob@1 carol p1@1 p2@1 p3@2" {
		t.Fatalf("stored %s", got)
	}

	// Project 2's pipelines fail: its pipeline and the shared and unscoped
	// users it might have written stay; project 1's missed rows go on the
	// second miss.
	failing := Report{Scopes: []string{"1", "2"}, ScopeFailures: []ScopeFailure{{Resource: "pipelines", Scope: "2", Err: errors.New("refused")}}}
	for range 2 {
		f.sync(failing, entry("pipeline", "p1", "pipelines", "1"))
	}
	if got := f.stored(); got != "alice@* carol p1@1 p3@2" {
		t.Fatalf("with project 2 failing, stored %s", got)
	}

	// Read in full, with project 2 gone from the listing: its rows, and the
	// shared and unscoped rows no one emitted, go on the second miss.
	whole := Report{Scopes: []string{"1"}}
	for range 2 {
		f.sync(whole, entry("pipeline", "p1", "pipelines", "1"), entry("user", "carol", "members", ""))
	}
	if got := f.stored(); got != "carol p1@1" {
		t.Fatalf("read in full, stored %s", got)
	}

	// A listing that failed means a scope missing from it may still be
	// there, so its rows stay.
	f.sync(read, entry("pipeline", "p4", "pipelines", "2"))
	unlisted := Report{Scopes: []string{"1"}, Unlisted: []error{errors.New("the projects of acme: 500")}}
	for range 2 {
		f.sync(unlisted, entry("pipeline", "p1", "pipelines", "1"))
	}
	if got := f.stored(); got != "carol p1@1 p4@2" {
		t.Fatalf("with a failed listing, stored %s", got)
	}
}

// TestReconcileWithoutAnIdentifier checks an entry without an identifier
// holds back the deletes of its scope, or of its blueprint when it came
// from no scope, since it could have been any of their entities.
func TestReconcileWithoutAnIdentifier(t *testing.T) {
	f := newScopedFixture(t)
	read := Report{Scopes: []string{"1", "2"}}
	f.sync(read, entry("pipeline", "p1", "pipelines", "1"), entry("pipeline", "p2", "pipelines", "2"), entry("user", "carol", "members", ""))
	for range 2 {
		f.sync(read, entry("pipeline", "", "pipelines", "2"), entry("user", "", "members", ""))
	}
	if got := f.stored(); got != "carol p2@2" {
		t.Fatalf("stored %s", got)
	}
}

// TestWriteRetriesADeadlock checks a batch a concurrent writer deadlocks
// with is written again, rather than failing.
func TestWriteRetriesADeadlock(t *testing.T) {
	f := newScopedFixture(t)
	f.sync(Report{}, entry("pipeline", "a", "pipelines", ""), entry("pipeline", "b", "pipelines", ""))
	ctx := t.Context()

	// The other writer takes b, waits for the batch to take a and wait for
	// b, then asks for a, which closes the cycle.
	other, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Rollback() }()
	if _, err := other.ExecContext(ctx, `UPDATE entities SET title = 'other' WHERE identifier = 'b'`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for waiting := 0; waiting == 0; time.Sleep(10 * time.Millisecond) {
			if err := f.db.QueryRowContext(context.Background(), `SELECT count(*) FROM pg_locks WHERE NOT granted`).Scan(&waiting); err != nil {
				t.Error(err)
				return
			}
		}
		if _, err := other.ExecContext(context.Background(), `UPDATE entities SET title = 'other' WHERE identifier = 'a'`); err != nil {
			t.Error(err)
		}
		_ = other.Commit()
	})

	changed := []Entry{entry("pipeline", "a", "pipelines", ""), entry("pipeline", "b", "pipelines", "")}
	for i := range changed {
		changed[i].Entity.Title = "batch"
	}
	run := &Run{Errors: []RunError{}}
	err = writeBatched(ctx, f.db, f.id, changed, run)
	wg.Wait()
	if err != nil || run.retried == 0 || run.Updated != 2 {
		t.Fatalf("err = %v, retried %d, updated %d", err, run.retried, run.Updated)
	}
	var titles []string
	rows, err := f.db.QueryContext(ctx, `SELECT title FROM entities ORDER BY identifier`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var title string
		_ = rows.Scan(&title)
		titles = append(titles, title)
	}
	if !slices.Equal(titles, []string{"batch", "batch"}) {
		t.Errorf("titles = %v", titles)
	}
}
