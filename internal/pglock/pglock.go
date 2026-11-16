// Package pglock holds Postgres session-level advisory locks, each on a
// connection of its own for as long as the work it guards, which runs on
// that connection, takes.
package pglock

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/lib/pq"

	"github.com/dvjn/goldenportal/internal/telemetry"
)

// ErrBusy says another session holds the lock.
var ErrBusy = errors.New("the lock is held by another session")

// releaseTimeout bounds giving a lock up, which runs after the work's
// context may have ended.
const releaseTimeout = 10 * time.Second

// Lender is a database that can lend one of its connections.
type Lender interface {
	Conn(ctx context.Context) (*sql.Conn, error)
}

// Lock is a session-level advisory lock held on Conn.
type Lock struct {
	Conn *sql.Conn
	key  string
	args []any
}

// Take holds the lock key names, SQL for a bigint over args, on one of
// db's connections. It waits up to wait for another session to give the
// lock up, or with no wait gives up at once, with ErrBusy.
func Take(ctx context.Context, db Lender, wait time.Duration, key string, args ...any) (*Lock, error) {
	acquire := func(ctx context.Context, conn *sql.Conn) error { return try(ctx, conn, key, args) }
	if wait > 0 {
		acquire = func(ctx context.Context, conn *sql.Conn) error { return waitFor(ctx, conn, wait, key, args) }
	}

	return take(ctx, db, key, args, acquire)
}

func take(ctx context.Context, db Lender, key string, args []any, acquire func(context.Context, *sql.Conn) error) (lock *Lock, err error) {
	// A lent connection bypasses gofr's query logs and metrics; its spans remain.
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	settled := false
	defer func() {
		if settled {
			return
		}
		// A cancel or panic partway may have left the lock taken.
		r := recover()
		cause := err
		if r != nil {
			cause = fmt.Errorf("panic: %v", r)
		}
		discard(ctx, conn, cause)
		if r != nil {
			panic(r)
		}
	}()
	err = acquire(ctx, conn)
	if errors.Is(err, ErrBusy) {
		settled = true
		_ = conn.Close()
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	settled = true

	return &Lock{Conn: conn, key: key, args: args}, nil
}

func try(ctx context.Context, conn *sql.Conn, key string, args []any) error {
	var locked bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(`+key+`)`, args...).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return ErrBusy
	}

	return nil
}

func waitFor(ctx context.Context, conn *sql.Conn, wait time.Duration, key string, args []any) error {
	// The wait's bound is local to this transaction; the session's lock
	// outlasts it.
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('lock_timeout', $1, true)`, strconv.FormatInt(wait.Milliseconds(), 10)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `SELECT pg_advisory_lock(`+key+`)`, args...)
	if pqErr, ok := errors.AsType[*pq.Error](err); ok && pqErr.Code == "55P03" {
		return ErrBusy
	}
	if err != nil {
		return err
	}

	return tx.Commit()
}

// Release gives up the lock and any session settings the work made, then
// the connection to its pool, or else closes the connection, which mustn't
// go back to the pool holding either.
func (l *Lock) Release(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()
	var unlocked bool
	err := l.Conn.QueryRowContext(ctx, `SELECT pg_advisory_unlock(`+l.key+`)`, l.args...).Scan(&unlocked)
	if err == nil {
		_, err = l.Conn.ExecContext(ctx, `RESET ALL`)
	}
	if err != nil || !unlocked {
		discard(ctx, l.Conn, err)
		return
	}
	_ = l.Conn.Close()
}

// discard closes conn rather than return it to the pool, which ends its
// session and with it any lock or setting the session holds.
func discard(ctx context.Context, conn *sql.Conn, cause error) {
	telemetry.Warn(ctx, "a connection that held a lock is closed rather than reused", cause)
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}
