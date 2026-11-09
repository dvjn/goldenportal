package connection

import (
	"context"

	"gofr.dev/pkg/gofr"
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"

	"github.com/dvjn/goldenportal/internal/telemetry"
)

const (
	// PruneSchedule is when, each day, old push runs go.
	PruneSchedule = "37 3 * * *"
	// PushRunDays is how long push runs are kept. Webhooks push a run for
	// every event, which would otherwise pile up for ever, while syncs are
	// few and kept. Each connection's latest push run stays, so a quiet
	// connection still shows when it last received.
	PushRunDays = 7
	// pruneBatch is how many push runs go per transaction, so a backlog
	// doesn't hold one long transaction.
	pruneBatch = 1000
)

const prunePushRunsSQL = `
	DELETE FROM ingestion_runs WHERE id IN (
		SELECT id FROM ingestion_runs
		WHERE trigger = 'push' AND started_at < now() - make_interval(days => $1)
			AND id NOT IN (SELECT max(id) FROM ingestion_runs WHERE trigger = 'push' GROUP BY connection_id)
		ORDER BY started_at LIMIT $2)`

// prunePushRuns deletes push runs older than PushRunDays, a batch per
// transaction, and returns how many went. Every replica runs every cron
// job, so each batch takes a lock in its transaction, and a replica that
// doesn't get it leaves the work to the one that did.
func prunePushRuns(ctx context.Context, db TxDB) (int64, error) {
	var deleted int64
	for {
		n, got, err := prunePushRunBatch(ctx, db)
		deleted += n
		if err != nil || !got || n < pruneBatch {
			return deleted, err
		}
	}
}

func prunePushRunBatch(ctx context.Context, db TxDB) (deleted int64, got bool, err error) {
	var tx *gofrSQL.Tx
	if tx, err = db.Begin(); err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('connection-prune-push-runs'))`).Scan(&got); err != nil || !got {
		return 0, false, err
	}
	result, err := tx.ExecContext(ctx, prunePushRunsSQL, PushRunDays, pruneBatch)
	if err != nil {
		return 0, true, err
	}
	if deleted, err = result.RowsAffected(); err != nil {
		return 0, true, err
	}

	return deleted, true, tx.Commit()
}

func prunePushRunsJob(ctx *gofr.Context) {
	deleted, err := prunePushRuns(ctx, ctx.SQL)
	if err != nil {
		telemetry.Error(ctx, "old push runs not deleted", err, telemetry.Deleted.Int64(deleted))
		return
	}
	telemetry.Info(ctx, "old push runs deleted", telemetry.Deleted.Int64(deleted))
}
