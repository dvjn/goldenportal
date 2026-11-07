package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

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
	// TriggerAction is an action run writing an entity as the integration.
	TriggerAction = "action"
)

// SyncSchedule is how often the scheduler looks for integrations due a
// sync, as a cron expression.
const SyncSchedule = "* * * * *"

// pullTimeout bounds how long one sync may spend fetching from a provider.
const pullTimeout = 10 * time.Minute

// writeBatch is how many entries a sync writes per transaction, so a large
// sync neither holds one transaction open nor commits row by row.
const writeBatch = 500

// syncSlack keeps an interval from being skipped by jitter in the scheduler.
const syncSlack = 5 * time.Second

var (
	ErrSyncing  = errors.New("a sync of this integration is already running")
	ErrNotPull  = errors.New("only integrations that pull their data can sync; give a resource a url to fetch")
	ErrUnsynced = errors.New("publish the integration before syncing it")
)

// TxDB is a database that can start transactions.
type TxDB interface {
	configdoc.DB
	Begin() (*gofrSQL.Tx, error)
}

// Sync pulls a published integration's data and writes it, a batch at a
// time. mode is complete or incremental, or empty for the integration's
// usual one.
//
// A complete sync then deletes the entities the integration wrote before
// that neither it nor the complete run before it saw, of the blueprints
// whose resources it all fetched, so one that misses an entity by chance,
// as a list shifting under it, doesn't delete it. Items that fail validation still count as seen, so a bad value
// never deletes an entity, and an item without an identifier could be any
// of its blueprint's, so that blueprint deletes nothing. An incremental
// sync fetches what changed since the last sync that fetched everything,
// where the connector can, and deletes only what items say was deleted.
func Sync(ctx context.Context, db TxDB, identifier, trigger, mode string) (*Run, error) {
	return syncAs(ctx, db, identifier, Run{Trigger: trigger, Mode: mode}, nil)
}

// SyncForAction syncs for the action run actionRun, so a change an action
// made at the source shows without waiting for the schedule. resources
// narrows it to some of what the integration pulls; empty is all of it.
// It doesn't move the schedule, since it may have fetched only part.
func SyncForAction(ctx context.Context, db TxDB, identifier, actionRun, mode string, resources []string) (*Run, error) {
	return syncAs(ctx, db, identifier, Run{Trigger: TriggerAction, Mode: mode, ActionRun: actionRun}, resources)
}

func syncAs(ctx context.Context, db TxDB, identifier string, asked Run, only []string) (*Run, error) {
	s, err := startSync(ctx, db, identifier, asked, only)
	if err != nil {
		return nil, err
	}

	return s.finish(ctx)
}

// syncing is a sync that holds its integration's lock and is recorded as
// running.
type syncing struct {
	db         TxDB
	lock       *gofrSQL.Tx
	id         string
	identifier string
	spec       Spec
	puller     Puller
	mapping    mapper
	request    PullRequest
	run        *Run
}

// startSync takes the integration's lock and records the run as running,
// so a second sync fails fast and Sync Now can answer before the fetch.
func startSync(ctx context.Context, db TxDB, identifier string, asked Run, only []string) (_ *syncing, err error) {
	store := configdoc.NewStore(db, Name)
	record, err := store.Get(ctx, identifier)
	if err != nil {
		return nil, err
	}
	if !record.Published() {
		return nil, ErrUnsynced
	}
	spec, _ := Parse(record.PublishedSource)
	puller, ok := spec.config.(Puller)
	if !ok || !spec.Pulls {
		return nil, ErrNotPull
	}
	id, err := store.PublishedID(ctx, identifier)
	if err != nil {
		return nil, err
	}

	lock, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = lock.Rollback()
		}
	}()

	// Take the lock before fetching so overlapping syncs fail fast instead of
	// each spending a full provider fetch. It's held in a transaction of its
	// own, since what's fetched is written in batches.
	var locked bool
	if err := lock.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('integration-sync:' || $1))`,
		id).Scan(&locked); err != nil {
		return nil, err
	}
	if !locked {
		return nil, ErrSyncing
	}

	history, err := NewStore(db).syncHistory(ctx, id)
	if err != nil {
		return nil, err
	}
	mode := asked.Mode
	if mode == "" {
		mode = spec.usualMode(history, time.Now())
	}
	since := time.Unix(0, 0).UTC()
	if mode == ModeIncremental && !history.lastSucceeded.IsZero() {
		since = history.lastSucceeded.Add(-time.Minute)
	}
	started, err := dbNow(ctx, db)
	if err != nil {
		return nil, err
	}

	for _, name := range only {
		if problem := spec.SyncableResource(name); problem != "" {
			return nil, errors.New(problem)
		}
	}
	var resources []string
	for _, resource := range spec.mappedResources() {
		if slices.Contains(puller.Pulls(), resource) && (len(only) == 0 || slices.Contains(only, resource)) {
			resources = append(resources, resource)
		}
	}
	m, err := spec.mapper()
	if err != nil {
		return nil, err
	}

	run := &Run{StartedAt: started, Trigger: asked.Trigger, Mode: mode, ActionRun: asked.ActionRun, Errors: []RunError{}}
	if err := NewStore(db).startRun(ctx, id, run); err != nil {
		return nil, err
	}
	request := PullRequest{Resources: resources, Since: since}
	if len(only) == 0 {
		request.IngestURL, request.WebhookSecret = IngestURL(identifier), WebhookSecret(identifier)
	}

	return &syncing{
		db: db, lock: lock, id: id, identifier: identifier, spec: spec, puller: puller, mapping: m,
		request: request, run: run,
	}, nil
}

// finish fetches and writes what the sync started, then records how it
// ended. A sync that fails is recorded too, so the failure shows with the
// integration's runs and the scheduler waits out the interval instead of
// retrying, and burning provider rate limits, every minute.
func (s *syncing) finish(ctx context.Context) (*Run, error) {
	defer func() { _ = s.lock.Rollback() }()
	err := s.pull(ctx)
	if err != nil {
		s.run.Error = err.Error()
	}
	if finishErr := NewStore(s.db).finishRun(context.WithoutCancel(ctx), s.run); finishErr != nil {
		return nil, errors.Join(err, finishErr)
	}
	if err != nil {
		return nil, err
	}

	return s.run, s.lock.Commit()
}

func (s *syncing) pull(ctx context.Context) error {
	run := s.run
	unsafe := map[string]bool{}
	emit := func(_ string, items []Item) error {
		entries := s.mapping.Map(ctx, items)
		for start := 0; start < len(entries); start += writeBatch {
			if err := writeBatched(ctx, s.db, s.id, entries[start:min(start+writeBatch, len(entries))], run, unsafe); err != nil {
				return err
			}
		}
		return nil
	}

	// The lock holds a connection while fetching, so bound the fetch to keep
	// a slow provider from holding it indefinitely.
	pullCtx, cancel := context.WithTimeout(ctx, pullTimeout)
	var warnings []string
	request := s.request
	request.Warn = func(warning string) { warnings = append(warnings, warning) }
	failed, err := s.puller.Pull(pullCtx, s.db, request, emit)
	cancel()
	if err != nil {
		return err
	}
	run.Warning = strings.Join(warnings, " ")
	if len(failed) > 0 {
		names := slices.Sorted(maps.Keys(failed))
		reasons := make([]string, len(names))
		for i, name := range names {
			reasons[i] = failed[name].Error()
		}
		run.Error = strings.Join(reasons, "; ")
	}
	if len(failed) == len(request.Resources) {
		return fmt.Errorf("pull %s: %s", s.identifier, run.Error)
	}

	if run.Mode == ModeComplete {
		var covered []string
		for _, r := range request.Resources {
			if failed[r] == nil {
				covered = append(covered, r)
			}
		}
		if run.Deleted, err = reconcile(ctx, s.db, s.id, s.spec, covered, unsafe, run.StartedAt); err != nil {
			return err
		}
	}

	return nil
}

// writeBatched writes a batch, and the run's counts so far with it, so a
// running sync shows how far it got.
func writeBatched(ctx context.Context, db TxDB, integrationID string, entries []Entry, run *Run, unsafe map[string]bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	batchUnsafe, err := write(ctx, tx, integrationID, entries, run)
	if err != nil {
		return err
	}
	maps.Copy(unsafe, batchUnsafe)
	if err := NewStore(tx).progress(ctx, run); err != nil {
		return err
	}

	return tx.Commit()
}

// mappedResources are the resources the mappings read, each once, in the
// order they first come.
func (s Spec) mappedResources() []string {
	var resources []string
	for _, m := range s.Mappings {
		if !slices.Contains(resources, m.Resource) {
			resources = append(resources, m.Resource)
		}
	}

	return resources
}

// SyncableResource says why name can't be synced on its own, or nothing when
// it can: it must be a resource the integration pulls, and a mapping must
// read it.
func (s Spec) SyncableResource(name string) string {
	puller, ok := s.config.(Puller)
	if !ok || !slices.Contains(puller.Pulls(), name) {
		return s.Identifier + " doesn't pull a resource called " + name
	}
	if !slices.ContainsFunc(s.Mappings, func(m Mapping) bool { return m.Resource == name }) {
		return "no mapping of " + s.Identifier + " reads " + name + ", so syncing it writes nothing"
	}

	return ""
}

// usualMode is how a sync runs unless asked otherwise: complete without a
// complete interval, on the first sync, the first after a publish, and once
// the interval has passed; incremental between.
func (s Spec) usualMode(h syncHistory, now time.Time) string {
	if s.Sync == nil || s.Sync.CompleteEvery == 0 || h.lastComplete.IsZero() || h.lastComplete.Before(h.published) ||
		now.Sub(h.lastComplete) >= s.Sync.CompleteEvery-syncSlack {
		return ModeComplete
	}

	return ModeIncremental
}

// syncAll syncs the published integrations that pull whose interval has
// passed since their last sync. It runs every minute.
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
		if !spec.Pulls {
			continue
		}
		last, err := NewStore(ctx.SQL).LastSync(ctx, record.Identifier)
		if err != nil {
			ctx.Errorf("find the last sync of %s: %v", record.Identifier, err)
			continue
		}
		if !last.IsZero() && time.Since(last) < spec.SyncEvery()-syncSlack {
			continue
		}
		if _, err := Sync(ctx, ctx.SQL, record.Identifier, TriggerSchedule, ""); err != nil && !errors.Is(err, ErrSyncing) {
			ctx.Errorf("sync %s: %v", record.Identifier, err)
		}
	}
}

func syncNow(ctx *gofr.Context) (any, error) {
	return SyncNow(ctx, ctx.PathParam("identifier"), ctx.Param("mode"))
}

// SyncNow starts syncing one published integration that pulls straight
// away, completely or incrementally, or as its schedule would when mode is
// empty. It returns the running run once the sync holds its lock, so a sync
// already running is refused at once; the integration's runs say how it
// ended.
func SyncNow(ctx *gofr.Context, identifier, mode string) (*Run, error) {
	if _, err := auth.Require(ctx, auth.RoleBuilder); err != nil {
		return nil, err
	}
	if mode != "" && mode != ModeComplete && mode != ModeIncremental {
		return nil, httpx.BadRequest("mode must be complete or incremental")
	}
	if _, err := integrationID(ctx, identifier); err != nil {
		return nil, err
	}

	s, err := startSync(ctx, ctx.SQL, identifier, Run{Trigger: TriggerManual, Mode: mode}, nil)
	switch {
	case errors.Is(err, ErrSyncing), errors.Is(err, ErrNotPull), errors.Is(err, ErrUnsynced):
		return nil, httpx.Conflict(err.Error())
	case err != nil:
		return nil, err
	}

	started := *s.run
	// A sync can outlast the request, whose context is cancelled when the
	// client goes and mustn't be used once the handler returns, so it runs
	// on a context of its own, still bounded by pullTimeout.
	logger := ctx.Container.Logger
	background(func() {
		if _, err := s.finish(context.Background()); err != nil {
			logger.Errorf("sync %s: %v", identifier, err)
		}
	})

	return &started, nil
}

// background runs the rest of a sync Sync Now started; tests wait for it.
var background = func(finish func()) { go finish() }

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
