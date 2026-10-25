package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/lib/pq"
	"gofr.dev/pkg/gofr"
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/httpx"
)

// What started a run.
const (
	TriggerPush     = "push"
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

// SyncSchedule is how often pull integrations sync, as a cron expression.
const SyncSchedule = "* * * * *"

var (
	ErrSyncing  = errors.New("a sync of this integration is already running")
	ErrNotPull  = errors.New("only integrations that pull their data can sync")
	ErrUnsynced = errors.New("publish the integration before syncing it")
)

// TxDB is a database that can start transactions.
type TxDB interface {
	configdoc.DB
	Begin() (*gofrSQL.Tx, error)
}

// Sync pulls a published pull integration's data and writes it as a full
// snapshot: entities the integration wrote before that aren't in the data
// any more are deleted. Items that fail validation still count as present,
// so a bad value never deletes an entity.
func Sync(ctx context.Context, db TxDB, identifier, trigger string) (*Run, error) {
	started := time.Now().UTC()
	store := configdoc.NewStore(db, Name)
	record, err := store.Get(ctx, identifier)
	if err != nil {
		return nil, err
	}
	if !record.Published() {
		return nil, ErrUnsynced
	}
	spec, _ := Parse(record.PublishedSource)
	c, _ := connector(spec.Connector)
	pull := c.pull()
	if pull == nil {
		return nil, ErrNotPull
	}
	id, err := store.PublishedID(ctx, identifier)
	if err != nil {
		return nil, err
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var locked bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('integration-sync:' || $1))`,
		id).Scan(&locked); err != nil {
		return nil, err
	}
	if !locked {
		return nil, ErrSyncing
	}

	data, err := pull(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("pull %s: %w", identifier, err)
	}
	items, err := spec.Map(data)
	if err != nil {
		return nil, err
	}

	run := &Run{StartedAt: started, Trigger: trigger, Received: len(items), Errors: []RunError{}}
	seen, err := write(ctx, tx, id, items, run)
	if err != nil {
		return nil, err
	}

	var keep []string
	for bp, identifiers := range seen {
		for _, identifier := range identifiers {
			keep = append(keep, bp+"/"+identifier)
		}
	}
	result, err := tx.ExecContext(ctx, `
		DELETE FROM entities e USING config_documents b
		WHERE b.id = e.blueprint_id AND e.integration_id = $1
			AND NOT ((b.identifier || '/' || e.identifier) = ANY($2))`, id, pq.Array(keep))
	if err != nil {
		return nil, err
	}
	deleted, _ := result.RowsAffected()
	run.Deleted = int(deleted)

	if err := NewStore(tx).RecordRun(ctx, id, "", run); err != nil {
		return nil, err
	}

	return run, tx.Commit()
}

// syncAll syncs every published pull integration, on the schedule.
func syncAll(ctx *gofr.Context) {
	records, err := configdoc.NewStore(ctx.SQL, Name).List(ctx)
	if err != nil {
		ctx.Errorf("list integrations to sync: %v", err)
		return
	}
	for _, record := range records {
		if !record.Published() {
			continue
		}
		spec, _ := Parse(record.PublishedSource)
		if c, _ := connector(spec.Connector); c.pull() == nil {
			continue
		}
		if _, err := Sync(ctx, ctx.SQL, record.Identifier, TriggerSchedule); err != nil && !errors.Is(err, ErrSyncing) {
			ctx.Errorf("sync %s: %v", record.Identifier, err)
		}
	}
}

// syncNow syncs one integration straight away.
func syncNow(ctx *gofr.Context) (any, error) {
	if _, err := auth.Require(ctx, auth.RoleBuilder); err != nil {
		return nil, err
	}
	_, identifier, err := integrationID(ctx)
	if err != nil {
		return nil, err
	}

	run, err := Sync(ctx, ctx.SQL, identifier, TriggerManual)
	switch {
	case errors.Is(err, ErrSyncing), errors.Is(err, ErrNotPull), errors.Is(err, ErrUnsynced):
		return nil, httpx.Error{Code: http.StatusConflict, Message: err.Error()}
	case err != nil:
		return nil, err
	}

	return run, nil
}

// toJSONValue turns a struct into the maps and slices jq works on.
func toJSONValue(v any) (any, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	err = json.Unmarshal(encoded, &out)

	return out, err
}
