//go:build e2e

package scorecard

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/pglock"
)

// gradeServices publishes the readiness scorecard over six services in
// its scope and one out of it, and evaluates it.
func gradeServices(t *testing.T) *publishFixture {
	t.Helper()
	f := newPublishFixture(t)
	// Pooled as the server's are, so a connection given back holding a lock keeps it.
	f.db.SetMaxIdleConns(4)
	f.publish("blueprints", "team", teamYAML)
	f.publish("blueprints", "service", gradedYAML)
	f.addEntity("team", "core", `{}`)
	f.addEntity("team", "ops", `{"slack": "#ops"}`)
	now := time.Now().UTC().Format(time.RFC3339)
	starred := `{"stars": 500, "deployedAt": "` + now + `"}`
	f.addGraded("bare", `{}`, `{}`, `{"critical": false}`)
	f.addGraded("owned", `{"stars": 5}`, `{"team": "core"}`, `{"critical": false}`)
	f.addGraded("reachable", `{"stars": 5}`, `{"team": "ops"}`, `{"critical": false}`)
	f.addGraded("critical", strings.Replace(starred, "{", `{"tier": "critical", `, 1), `{"team": "ops"}`, `{"critical": true}`)
	f.addGraded("gold", starred, `{"team": "ops"}`, `{"critical": false}`)
	f.addGraded("unowned", starred, `{}`, `{"critical": false}`)
	f.addGraded("trial", `{"tier": "experimental"}`, `{"team": "ops"}`, `{"critical": false}`)
	f.publish("scorecards", "readiness", readinessFilters)
	f.evaluateQueued()

	return f
}

func withBatchSize(t *testing.T, n int) {
	t.Helper()
	previous := batchSize
	batchSize = n
	t.Cleanup(func() { batchSize = previous })
}

func (f *publishFixture) exec(statements ...string) {
	f.t.Helper()
	for _, statement := range statements {
		if _, err := f.db.ExecContext(f.t.Context(), statement); err != nil {
			f.t.Fatal(err)
		}
	}
}

// advisoryLocksHeld counts the advisory locks held in f's database, by any session.
func (f *publishFixture) advisoryLocksHeld() int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.t.Context(), `
		SELECT count(*) FROM pg_locks
		WHERE locktype = 'advisory' AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&n); err != nil {
		f.t.Fatal(err)
	}

	return n
}

// TestEvaluateInBatches checks evaluating a batch at a time, under the
// evaluation's lock throughout, stores what one batch of every entity does,
// whether or not the batch size divides the entities in scope.
func TestEvaluateInBatches(t *testing.T) {
	f := gradeServices(t)
	whole := f.results("readiness")
	if len(whole) != 6 {
		t.Fatalf("scored %d services in one batch, want 6", len(whole))
	}
	f.exec(`CREATE FUNCTION locked() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND pid = pg_backend_pid() AND granted) THEN
				RAISE EXCEPTION 'a result was written without the evaluation lock';
			END IF;
			RETURN NEW;
		END $$`,
		`CREATE TRIGGER locked BEFORE INSERT OR UPDATE ON scorecard_results FOR EACH ROW EXECUTE FUNCTION locked()`)

	for _, size := range []int{1, 2, 4, 6} {
		withBatchSize(t, size)
		f.exec(`DELETE FROM scorecard_results`)
		scored, err := Evaluate(t.Context(), f.db, "readiness")
		if err != nil || scored != 6 {
			t.Fatalf("batches of %d scored %d (%v), want 6", size, scored, err)
		}
		if got := f.results("readiness"); !reflect.DeepEqual(got, whole) {
			t.Errorf("batches of %d stored %v, want %v as in one batch", size, got, whole)
		}
		if n := f.advisoryLocksHeld(); n != 0 {
			t.Errorf("%d locks held after the run, want none", n)
		}
	}
}

// TestEvaluationRemovesOutOfScope checks a run drops the results of
// entities that left the scope, whichever batch they'd have been in, and
// all of them once the scorecard grades another blueprint.
func TestEvaluationRemovesOutOfScope(t *testing.T) {
	f := gradeServices(t)
	withBatchSize(t, 2)
	f.addGraded("bare", `{"tier": "experimental"}`, `{}`, `{"critical": false}`)
	f.addGraded("gold", `{"tier": "experimental"}`, `{}`, `{"critical": false}`)
	if _, err := Evaluate(t.Context(), f.db, "readiness"); err != nil {
		t.Fatal(err)
	}
	got := f.results("readiness")
	_, bare := got["bare"]
	_, gold := got["gold"]
	if bare || gold || len(got) != 4 {
		t.Errorf("results %v, want bare and gold dropped and the other four kept", got)
	}

	f.publish("scorecards", "readiness", `kind: Scorecard
metadata: {identifier: readiness, title: Readiness}
spec:
  blueprint: team
  tiers:
    - name: bronze
      criteria:
        owned: {check: {property: slack, op: exists}}
`)
	f.evaluateQueued()
	if got := f.results("readiness"); len(got) != 2 || got["ops"].tier != "bronze" || got["core"].tier != "" {
		t.Errorf("results %v, want only the teams', ops bronze", got)
	}
}

// TestFailedRunLeavesVersionUnmarked checks a run that fails partway keeps
// the batches it wrote but leaves the scorecard Evaluating, until a run
// succeeds.
func TestFailedRunLeavesVersionUnmarked(t *testing.T) {
	f := gradeServices(t)
	withBatchSize(t, 2)
	var last string
	if err := f.db.QueryRowContext(t.Context(), `
		SELECT r.entity_id FROM scorecard_results r ORDER BY r.entity_id DESC LIMIT 1`).Scan(&last); err != nil {
		t.Fatal(err)
	}
	f.exec(`DELETE FROM scorecard_results`,
		`CREATE FUNCTION refuse() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.entity_id = '`+last+`' THEN
				RAISE EXCEPTION 'refused';
			END IF;
			RETURN NEW;
		END $$`,
		`CREATE TRIGGER refuse BEFORE INSERT ON scorecard_results FOR EACH ROW EXECUTE FUNCTION refuse()`)
	f.publish("scorecards", "readiness", strings.Replace(readinessFilters, "title: Readiness", "title: Ready", 1))
	f.evaluateQueued()

	if got := f.results("readiness"); len(got) != 4 {
		t.Errorf("results %v, want the first two batches' four", got)
	}
	if !f.evaluating("readiness") {
		t.Error("a failed run marked the version evaluated")
	}

	f.exec(`DROP TRIGGER refuse ON scorecard_results`)
	if _, err := Evaluate(t.Context(), f.db, "readiness"); err != nil {
		t.Fatal(err)
	}
	if got := f.results("readiness"); len(got) != 6 {
		t.Errorf("results %v, want all six", got)
	}
	if f.evaluating("readiness") {
		t.Error("the summary says Evaluating after a run succeeded")
	}
}

// TestEvaluationLockReleased checks a run that ends early gives up its
// lock: one cancelled once it holds it, and one that panics, which comes
// back as an error so the worker carries on.
func TestEvaluationLockReleased(t *testing.T) {
	f := gradeServices(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancelling := func(ctx context.Context, db pglock.Lender, name string) (*pglock.Lock, error) {
		defer cancel()
		return tryLock(ctx, db, name)
	}
	_, err := runEvaluation(ctx, f.db, "readiness", cancelling)
	if pqErr, ok := errors.AsType[*pq.Error](err); !errors.Is(err, context.Canceled) && (!ok || pqErr.Code != "57014") {
		t.Errorf("err = %v, want the cancel", err)
	}
	if n := f.advisoryLocksHeld(); n != 0 {
		t.Errorf("%d locks held after a cancelled run, want none", n)
	}

	panicking := func(context.Context, pglock.Lender, string) (*pglock.Lock, error) { panic("boom") }
	if _, err := runEvaluation(t.Context(), f.db, "readiness", panicking); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want the panic as an error", err)
	}
	if _, err := Evaluate(t.Context(), f.db, "readiness"); err != nil {
		t.Errorf("err = %v after runs that ended early, want an evaluation", err)
	}
}

// TestScheduledRunSkipsBusyScorecard checks the scheduled run passes over
// a scorecard another run holds, at once, while an evaluation in turn
// waits for it and then runs.
func TestScheduledRunSkipsBusyScorecard(t *testing.T) {
	f := gradeServices(t)
	f.exec(`DELETE FROM scorecard_results`)

	holder, err := tryLock(t.Context(), f.db, "readiness")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	evaluateAll(t.Context(), f.db)
	if waited := time.Since(started); waited > lockWait/2 {
		t.Errorf("the scheduled run waited %s for a busy scorecard, want it to skip", waited)
	}
	if got := f.results("readiness"); len(got) != 0 {
		t.Errorf("results %v, want the busy scorecard skipped", got)
	}

	done := make(chan error, 1)
	go func() {
		_, err := evaluateInTurn(context.Background(), f.db, "readiness")
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("evaluation in turn finished (%v) while another run held the scorecard", err)
	case <-time.After(300 * time.Millisecond):
	}
	holder.Release(t.Context())
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := f.results("readiness"); len(got) != 6 {
		t.Errorf("results %v, want all six once the other run let go", got)
	}
}

// TestUnchangedBatchesWriteNothing checks a rerun over several batches
// writes no result that hasn't changed, and a result's tier keeps the time
// it was reached while only its score changes.
func TestUnchangedBatchesWriteNothing(t *testing.T) {
	f := gradeServices(t)
	withBatchSize(t, 2)
	changedAt := func() map[string]time.Time {
		t.Helper()
		rows, err := f.db.QueryContext(t.Context(), `
			SELECT e.identifier, r.changed_at FROM scorecard_results r JOIN entities e ON e.id = r.entity_id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		at := map[string]time.Time{}
		for rows.Next() {
			var (
				identifier string
				changed    time.Time
			)
			if err := rows.Scan(&identifier, &changed); err != nil {
				t.Fatal(err)
			}
			at[identifier] = changed
		}
		return at
	}
	before := changedAt()
	spec := mustParse(t, readinessFilters)
	if counts, err := scoreVersion(t.Context(), f.db, spec, 1); err != nil || counts.scored != 6 || counts.written != 0 {
		t.Fatalf("rerun scored %d and wrote %d (%v), want 6 and none", counts.scored, counts.written, err)
	}

	f.addGraded("gold", `{"stars": 50, "deployedAt": "`+time.Now().UTC().Format(time.RFC3339)+`"}`, `{"team": "ops"}`, `{"critical": false}`)
	f.addGraded("owned", `{"stars": 500}`, `{"team": "core"}`, `{"critical": false}`)
	if counts, err := scoreVersion(t.Context(), f.db, spec, 1); err != nil || counts.scored != 6 || counts.written != 2 {
		t.Fatalf("after two changes scored %d and wrote %d (%v), want 6 and 2", counts.scored, counts.written, err)
	}
	after := changedAt()
	for identifier, at := range after {
		if moved := !at.Equal(before[identifier]); moved != (identifier == "gold") {
			t.Errorf("%s changed_at moved %t, want it moved only for gold, whose tier changed", identifier, moved)
		}
	}
	if got := f.results("readiness"); got["gold"].tier != "silver" || got["owned"].tier != "bronze" || *got["owned"].score != 50 {
		t.Errorf("results %v, want gold down to silver and owned bronze at 50", got)
	}
}
