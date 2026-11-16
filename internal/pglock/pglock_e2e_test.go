//go:build e2e

package pglock

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"

	"github.com/dvjn/goldenportal/internal/pgtest"
)

const key = `hashtext('pglock-test:' || $1)`

// pooled is a database that keeps one connection, as a server's pool keeps
// idle ones, so what a released lock leaves on it shows on the next take.
func pooled(t *testing.T) *gofrSQL.DB {
	t.Helper()
	db := pgtest.New(t)
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)

	return db
}

func held(t *testing.T, db *gofrSQL.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(t.Context(), `
		SELECT count(*) FROM pg_locks
		WHERE locktype = 'advisory' AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&n); err != nil {
		t.Fatal(err)
	}

	return n
}

func TestTakeAndRelease(t *testing.T) {
	db := pooled(t)
	lock, err := Take(t.Context(), db, 0, key, "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Conn.ExecContext(t.Context(), `SET statement_timeout = 1234`); err != nil {
		t.Fatal(err)
	}
	if _, err := Take(t.Context(), db, 0, key, "a"); !errors.Is(err, ErrBusy) {
		t.Errorf("err = %v taking a held lock, want ErrBusy", err)
	}
	if other, err := Take(t.Context(), db, 0, key, "b"); err != nil {
		t.Errorf("err = %v taking another key, want it taken", err)
	} else {
		other.Release(t.Context())
	}
	lock.Release(t.Context())
	if n := held(t, db); n != 0 {
		t.Errorf("%d locks held after release, want none", n)
	}
	for range 2 {
		var timeout string
		if err := db.QueryRowContext(t.Context(), `SHOW statement_timeout`).Scan(&timeout); err != nil || timeout != "0" {
			t.Errorf("statement_timeout = %q (%v) on a pooled connection, want the setting gone with the lock", timeout, err)
		}
	}
}

func TestTakeWaits(t *testing.T) {
	db := pooled(t)
	holder, err := Take(t.Context(), db, 0, key, "a")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := Take(t.Context(), db, 200*time.Millisecond, key, "a"); !errors.Is(err, ErrBusy) || time.Since(started) < 200*time.Millisecond {
		t.Errorf("err = %v after %s, want ErrBusy once the wait ran out", err, time.Since(started))
	}

	taken := make(chan error, 1)
	go func() {
		lock, err := Take(context.Background(), db, 10*time.Second, key, "a")
		if err == nil {
			lock.Release(context.Background())
		}
		taken <- err
	}()
	time.Sleep(100 * time.Millisecond)
	holder.Release(t.Context())
	if err := <-taken; err != nil {
		t.Errorf("err = %v, want the lock once the holder let go", err)
	}
}

// TestPanicWhileTakingClosesConnection checks a panic once the lock may be
// taken closes the connection, ending the session and its lock, rather than
// pooling it.
func TestPanicWhileTakingClosesConnection(t *testing.T) {
	db := pooled(t)
	panicking := func(ctx context.Context, conn *sql.Conn) error {
		if err := try(ctx, conn, key, []any{"a"}); err != nil {
			return err
		}
		panic("boom")
	}
	func() {
		defer func() {
			if r := recover(); r == nil || !strings.Contains(r.(string), "boom") {
				t.Errorf("recovered %v, want the panic passed on", r)
			}
		}()
		_, _ = take(t.Context(), db, key, []any{"a"}, panicking)
	}()
	for deadline := time.Now().Add(5 * time.Second); held(t, db) != 0; {
		if time.Now().After(deadline) {
			t.Fatal("the lock is still held after a panic while taking it")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
