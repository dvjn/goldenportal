package entity

import (
	"context"
	"maps"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"gofr.dev/pkg/gofr"
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"

	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/telemetry"
)

const (
	// RetentionScheduleEnv names the setting for when rows older than their
	// blueprint keeps them go, a cron expression, DefaultRetentionSchedule
	// unless set.
	RetentionScheduleEnv     = "RETENTION_SCHEDULE"
	DefaultRetentionSchedule = "17 4 * * *"
	// expireBatch is how many rows go per transaction, so a backlog
	// doesn't hold one long transaction.
	expireBatch = 5000
)

// TxDB is a database that can start a transaction.
type TxDB interface {
	configdoc.DB
	Begin() (*gofrSQL.Tx, error)
}

func expireJob(ctx *gofr.Context) {
	// Its outcome is logged and on its span; a failure waits for the next
	// scheduled run, as there's no one to answer.
	_, _ = ExpireAll(ctx, ctx.SQL)
}

// ExpireAll deletes, from every blueprint with retention, the rows kept by
// a time older than it keeps them, then vacuums what they held, and says
// how many it deleted. Every replica runs every cron job, so each batch
// takes a lock in its transaction, and a replica that doesn't get it
// leaves the work to the one that did.
func ExpireAll(ctx context.Context, db TxDB) (deleted int, err error) {
	ctx, span := telemetry.Start(ctx, "retention")
	started := time.Now()
	defer func() {
		span.SetAttributes(telemetry.Deleted.Int(deleted))
		attrs := []attribute.KeyValue{telemetry.Deleted.Int(deleted), telemetry.Duration.Int64(time.Since(started).Milliseconds())}
		if err != nil {
			telemetry.Error(ctx, "old rows not all deleted", err, attrs...)
		} else {
			telemetry.Info(ctx, "old rows deleted", attrs...)
		}
		telemetry.End(span, err)
	}()
	specs, err := blueprint.Published(ctx, db)
	if err != nil {
		return 0, err
	}
	for _, identifier := range slices.Sorted(maps.Keys(specs)) {
		if specs[identifier].Retention == nil {
			continue
		}
		n, got, err := expireBlueprint(ctx, db, specs, specs[identifier])
		deleted += n
		if err != nil || !got {
			return deleted, err
		}
	}
	if deleted == 0 {
		return 0, nil
	}
	vacuumCtx, vacuum := telemetry.Start(ctx, "vacuum entities")
	_, err = db.ExecContext(vacuumCtx, `VACUUM (ANALYZE) entities`)
	telemetry.End(vacuum, err)

	return deleted, err
}

// expireBlueprint deletes one blueprint's expired rows, a batch per
// transaction, and says whether it got the lock each time.
func expireBlueprint(ctx context.Context, db TxDB, specs map[string]blueprint.Spec, spec blueprint.Spec) (deleted int, got bool, err error) {
	ctx, span := telemetry.Start(ctx, "expire blueprint", telemetry.Blueprint.String(spec.Identifier))
	defer func() {
		span.SetAttributes(telemetry.Deleted.Int(deleted))
		telemetry.End(span, err)
	}()
	for {
		n, got, err := expireBatchOf(ctx, db, specs, spec)
		deleted += n
		if err != nil || !got || n < expireBatch {
			return deleted, got, err
		}
	}
}

func expireBatchOf(ctx context.Context, db TxDB, specs map[string]blueprint.Spec, spec blueprint.Spec) (deleted int, got bool, err error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('entity-retention'))`).Scan(&got); err != nil || !got {
		return 0, false, err
	}
	if deleted, err = NewStore(tx).Expire(ctx, specs, spec, expireBatch); err != nil {
		return 0, true, err
	}

	return deleted, true, tx.Commit()
}
