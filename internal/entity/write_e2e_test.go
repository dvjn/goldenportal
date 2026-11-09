//go:build e2e

package entity

import (
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/pgtest"
)

// storeFixture is a database with one blueprint and two connections.
type storeFixture struct {
	t                       *testing.T
	db                      *gofrSQL.DB
	store                   Store
	blueprint, mine, theirs string
}

func newStoreFixture(t *testing.T) *storeFixture {
	t.Helper()
	db := pgtest.New(t)
	return &storeFixture{
		t: t, db: db, store: NewStore(db),
		blueprint: pgtest.Publish(t, db, "blueprint", "service", "kind: Blueprint"),
		mine:      pgtest.Publish(t, db, "connection", "mine", "kind: Connection"),
		theirs:    pgtest.Publish(t, db, "connection", "theirs", "kind: Connection"),
	}
}

func (f *storeFixture) write(connectionID string, writes ...Write) []string {
	f.t.Helper()
	for i := range writes {
		writes[i].BlueprintID = f.blueprint
	}
	written, err := f.store.WriteAll(f.t.Context(), connectionID, writes)
	if err != nil {
		f.t.Fatal(err)
	}
	outcomes := make([]string, len(written))
	for i, w := range written {
		outcomes[i] = w.Outcome
		if w.Owner != "" {
			outcomes[i] = "owned by " + w.Owner
		}
	}

	return outcomes
}

// row is what's stored of an entity, and the transaction that last wrote
// it, which shows whether a write touched it at all.
type row struct {
	title, scope string
	sourceAt     time.Time
	missed       bool
	xmin         string
}

func (f *storeFixture) row(identifier string) row {
	f.t.Helper()
	var (
		r        row
		scope    sql.NullString
		sourceAt sql.NullTime
	)
	err := f.db.QueryRowContext(f.t.Context(), `
		SELECT title, scope, source_at, missed_at IS NOT NULL, xmin::text FROM entities WHERE blueprint_id = $1 AND identifier = $2`,
		f.blueprint, identifier).Scan(&r.title, &scope, &sourceAt, &r.missed, &r.xmin)
	if err != nil {
		f.t.Fatalf("%s: %v", identifier, err)
	}
	r.scope, r.sourceAt = scope.String, sourceAt.Time

	return r
}

func (f *storeFixture) identifiers() string {
	f.t.Helper()
	rows, err := f.db.QueryContext(f.t.Context(), `SELECT identifier FROM entities ORDER BY identifier`)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, id)
	}

	return strings.Join(out, ",")
}

func (f *storeFixture) now() time.Time {
	f.t.Helper()
	var now time.Time
	if err := f.db.QueryRowContext(f.t.Context(), `SELECT clock_timestamp()`).Scan(&now); err != nil {
		f.t.Fatal(err)
	}

	return now
}

func entityWrite(identifier, title string, properties map[string]any) Write {
	return Write{Entity: blueprint.Entity{Identifier: identifier, Title: title, Properties: properties}, Computed: map[string]any{}}
}

func TestWriteAllWritesOnlyWhatChanged(t *testing.T) {
	f := newStoreFixture(t)
	writes := func(tier float64) []Write {
		return []Write{
			entityWrite("api", "API", map[string]any{"tier": 1.0, "tags": []any{"a"}}),
			entityWrite("web", "Web", map[string]any{"tier": tier}),
		}
	}
	if got := f.write(f.mine, writes(1)...); !slices.Equal(got, []string{Created, Created}) {
		t.Fatalf("first write = %v", got)
	}
	api, web := f.row("api"), f.row("web")

	if got := f.write(f.mine, writes(1)...); !slices.Equal(got, []string{Unchanged, Unchanged}) {
		t.Fatalf("same again = %v", got)
	}
	if f.row("api").xmin != api.xmin || f.row("web").xmin != web.xmin {
		t.Error("an unchanged entity was written")
	}

	if got := f.write(f.mine, writes(2)...); !slices.Equal(got, []string{Unchanged, Updated}) {
		t.Fatalf("one changed = %v", got)
	}
	if f.row("api").xmin != api.xmin || f.row("web").xmin == web.xmin {
		t.Error("want only the changed entity written")
	}
}

func TestWriteAllKeepsTheLastOfEachEntity(t *testing.T) {
	f := newStoreFixture(t)
	got := f.write(f.mine, entityWrite("api", "First", nil), entityWrite("web", "Web", nil), entityWrite("api", "Last", nil))
	if !slices.Equal(got, []string{Unchanged, Created, Created}) || f.row("api").title != "Last" {
		t.Fatalf("outcomes = %v, title = %q", got, f.row("api").title)
	}
}

func TestWriteAllNeverGoesBackwards(t *testing.T) {
	f := newStoreFixture(t)
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	read := func(title string, sourceAt time.Time) Write {
		w := entityWrite("api", title, nil)
		w.SourceAt = sourceAt
		return w
	}
	f.write(f.mine, read("Noon", at))

	if got := f.write(f.mine, read("Earlier", at.Add(-time.Minute))); !slices.Equal(got, []string{Older}) || f.row("api").title != "Noon" {
		t.Fatalf("an older read = %v, title %q", got, f.row("api").title)
	}
	if got := f.write(f.mine, read("Same second", at)); !slices.Equal(got, []string{Updated}) {
		t.Fatalf("a read as new = %v", got)
	}
	// A writer that doesn't know when its source answered is never held
	// back, and holds nothing back after it.
	if got := f.write(f.mine, read("Unknown", time.Time{})); !slices.Equal(got, []string{Updated}) || !f.row("api").sourceAt.IsZero() {
		t.Fatalf("a write without a time = %v, stored %v", got, f.row("api").sourceAt)
	}
	if got := f.write(f.mine, read("Earlier still", at.Add(-time.Hour))); !slices.Equal(got, []string{Updated}) {
		t.Fatalf("a read after one without a time = %v", got)
	}
}

func TestWriteAllLeavesOthersEntities(t *testing.T) {
	f := newStoreFixture(t)
	f.write(f.theirs, entityWrite("api", "Theirs", nil))
	if got := f.write(f.mine, entityWrite("api", "Mine", nil)); !slices.Equal(got, []string{"owned by theirs"}) || f.row("api").title != "Theirs" {
		t.Fatalf("outcomes = %v, title = %q", got, f.row("api").title)
	}
}

func TestWriteAllSharesWhatSeveralScopesWrite(t *testing.T) {
	f := newStoreFixture(t)
	from := func(scope string) Write {
		w := entityWrite("alice", "Alice", nil)
		w.Scope = scope
		return w
	}
	steps := []struct {
		scope, outcome, stored string
	}{
		// A row with no scope, as written before rows had one, takes the
		// first scope that writes it.
		{"", Created, ""},
		{"1", Updated, "1"},
		{"1", Unchanged, "1"},
		{"2", Updated, Shared},
		{"1", Unchanged, Shared},
		{"", Unchanged, Shared},
	}
	for _, step := range steps {
		if got := f.write(f.mine, from(step.scope)); got[0] != step.outcome || f.row("alice").scope != step.stored {
			t.Fatalf("from %q: %v, stored %q, want %s and %q", step.scope, got, f.row("alice").scope, step.outcome, step.stored)
		}
	}

	f.write(f.mine, func() Write { w := entityWrite("bob", "Bob", nil); w.Scope = "1"; return w }())
	if got := f.write(f.mine, from(""), func() Write { w := entityWrite("bob", "Bob", nil); return w }()); got[1] != Updated || f.row("bob").scope != Shared {
		t.Fatalf("a write with no scope over one = %v, stored %q", got, f.row("bob").scope)
	}
}

func TestWriteAllUnmarksWhatItSees(t *testing.T) {
	f := newStoreFixture(t)
	f.write(f.mine, entityWrite("api", "API", nil), entityWrite("web", "Web", nil), entityWrite("db", "DB", nil))
	if _, err := f.db.ExecContext(t.Context(), `UPDATE entities SET missed_at = now()`); err != nil {
		t.Fatal(err)
	}
	f.write(f.mine, entityWrite("api", "API", nil), entityWrite("web", "Web 2", nil))
	if err := f.store.Sighted(t.Context(), f.mine, []Key{{f.blueprint, "db"}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"api", "web", "db"} {
		if f.row(id).missed {
			t.Errorf("%s is still marked missed", id)
		}
	}
}

// TestReconcileJudgesWhatTheRunRead checks a complete run deletes what it
// and the run before it missed, of the scopes it read in full, and, as
// the unit says, of rows with no scope or a shared one and of scopes it
// didn't list, marking what it missed the first time.
func TestReconcileJudgesWhatTheRunRead(t *testing.T) {
	f := newStoreFixture(t)
	scoped := func(identifier, scope string) Write {
		w := entityWrite(identifier, identifier, nil)
		w.Scope = scope
		return w
	}
	f.write(f.mine, scoped("a1", "1"), scoped("a2", "1"), scoped("b1", "2"), scoped("none", ""),
		scoped("gone", "9"), scoped("shared", "1"), scoped("shared", "2"))
	f.write(f.mine, scoped("shared", "2"))
	f.write(f.theirs, scoped("theirs", "1"))
	published := f.now()

	reconcile := func(u Unit) int {
		t.Helper()
		u.BlueprintID = f.blueprint
		deleted, err := f.store.Reconcile(t.Context(), f.mine, []Unit{u}, f.now(), published)
		if err != nil {
			t.Fatal(err)
		}
		return deleted
	}
	everything := Unit{Seen: []string{"a1"}, Scopes: []string{"1", "2"}, Listed: []string{"1", "2"}}

	// Scope 2 failed, so only scope 1's rows are judged.
	onlyOne := everything
	onlyOne.Scopes = []string{"1"}
	if reconcile(onlyOne) != 0 || !f.row("a2").missed || f.row("b1").missed || f.row("none").missed || f.row("gone").missed {
		t.Fatal("want only scope 1's unseen row marked")
	}
	if reconcile(onlyOne) != 1 || f.identifiers() != "a1,b1,gone,none,shared,theirs" {
		t.Fatalf("after a second miss: %s", f.identifiers())
	}

	// Every scope read in full and listed: rows with no scope or a shared
	// one, and those of scopes gone, are judged too.
	whole := everything
	whole.Unscoped, whole.Gone = true, true
	reconcile(whole)
	if deleted := reconcile(whole); deleted != 4 || f.identifiers() != "a1,theirs" {
		t.Fatalf("deleted %d, left %s", deleted, f.identifiers())
	}

	// A run that started before the latest publish read with the old
	// sources, so it does nothing.
	if _, err := f.db.ExecContext(t.Context(), `UPDATE entities SET missed_at = now()`); err != nil {
		t.Fatal(err)
	}
	deleted, err := f.store.Reconcile(t.Context(), f.mine, []Unit{{BlueprintID: f.blueprint, Unscoped: true}}, published.Add(-time.Second), published)
	if err != nil || deleted != 0 {
		t.Fatalf("a run from before the publish deleted %d, %v", deleted, err)
	}
}

// TestReconcileSparesWhatWasSeenOrCameLater checks a row seen since its
// mark, by any writer, and one made after the run started, aren't deleted
// or marked.
func TestReconcileSparesWhatWasSeenOrCameLater(t *testing.T) {
	f := newStoreFixture(t)
	f.write(f.mine, entityWrite("api", "API", nil), entityWrite("web", "Web", nil))
	published := f.now()
	whole := []Unit{{BlueprintID: f.blueprint, Unscoped: true}}
	if _, err := f.store.Reconcile(t.Context(), f.mine, whole, f.now(), published); err != nil {
		t.Fatal(err)
	}
	f.write(f.mine, entityWrite("api", "API", nil))
	started := f.now()
	f.write(f.mine, entityWrite("new", "New", nil))
	deleted, err := f.store.Reconcile(t.Context(), f.mine, whole, started, published)
	if err != nil || deleted != 1 || f.identifiers() != "api,new" || f.row("new").missed || !f.row("api").missed {
		t.Fatalf("deleted %d, %v, left %s", deleted, err, f.identifiers())
	}
}
