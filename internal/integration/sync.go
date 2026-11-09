package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gofr.dev/pkg/gofr"
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/filter"
	"github.com/dvjn/goldenportal/internal/httpx"
	"github.com/dvjn/goldenportal/internal/telemetry"
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

// pullHeadroom is how long before the next sync is due a sync stops
// fetching, so one that runs long, as a first sync reading far back may,
// ends in time for the next, which reads what it didn't from where it left
// off in each scope.
const pullHeadroom = 10 * time.Minute

// pullTimeout bounds how long one sync may spend fetching from a provider:
// until pullHeadroom before the next sync is due, or half the interval of
// one too short to leave that much.
func (s Spec) pullTimeout() time.Duration {
	every := s.SyncEvery()
	return max(every-pullHeadroom, every/2)
}

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
// its reconcile mappings write whose resources it all fetched, so one that
// misses an entity by chance, as a list shifting under it, doesn't delete
// it. Items that fail validation still count as seen, so a bad value never
// deletes an entity, and an item without an identifier could be any of its
// blueprint's, so that blueprint deletes nothing. What else a sync reads,
// it reads from where the last one left off, as syncCursors says, where
// the connector can, and deletes only what items say was deleted.
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
	timeout    time.Duration
	// traced carries the sync's span, which finish ends, on whatever
	// context it finishes on.
	traced  context.Context
	span    trace.Span
	started time.Time
}

// startSync takes the integration's lock and records the run as running,
// so a second sync fails fast and Sync Now can answer before the fetch.
func startSync(ctx context.Context, db TxDB, identifier string, asked Run, only []string) (_ *syncing, err error) {
	ctx, span := telemetry.Start(ctx, "sync", telemetry.Integration.String(identifier), telemetry.Trigger.String(asked.Trigger))
	defer func() {
		switch {
		case err == nil:
			return
		case errors.Is(err, ErrSyncing), errors.Is(err, ErrNotPull), errors.Is(err, ErrUnsynced):
			telemetry.Debug(ctx, "sync not started", attribute.String("gp.reason", err.Error()))
			span.End()
		default:
			telemetry.Error(ctx, "sync not started", err)
			telemetry.End(span, err)
		}
	}()
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
	stored, err := NewStore(db).cursors(ctx, id)
	if err != nil {
		return nil, err
	}
	blueprints, err := blueprint.Published(ctx, db)
	if err != nil {
		return nil, err
	}
	longer, err := NewStore(db).keptLonger(ctx, spec.writes())
	if err != nil {
		return nil, err
	}

	run := &Run{StartedAt: started, Trigger: asked.Trigger, Mode: mode, ActionRun: asked.ActionRun, Errors: []RunError{}}
	if err := NewStore(db).startRun(ctx, id, run); err != nil {
		return nil, err
	}
	request := PullRequest{
		Resources: resources, Cursors: spec.syncCursors(mode, resources, stored, blueprints, longer, history.changes, started), read: newReadLog(),
	}
	if len(only) == 0 {
		request.IngestURL, request.WebhookSecret = IngestURL(identifier), WebhookSecret(identifier)
	}

	span.SetAttributes(telemetry.RunID.Int64(run.ID), telemetry.Mode.String(mode))

	return &syncing{
		db: db, lock: lock, id: id, identifier: identifier, spec: spec, puller: puller, mapping: m,
		request: request, run: run, timeout: spec.pullTimeout(), traced: ctx, span: span, started: time.Now(),
	}, nil
}

// runAttributes are what spans and logs say of a run.
func runAttributes(run *Run) []attribute.KeyValue {
	return []attribute.KeyValue{
		telemetry.RunID.Int64(run.ID), telemetry.Trigger.String(run.Trigger), telemetry.Mode.String(run.Mode),
		telemetry.Received.Int(run.Received), telemetry.Created.Int(run.Created), telemetry.Updated.Int(run.Updated),
		telemetry.Unchanged.Int(run.Unchanged), telemetry.Deleted.Int(run.Deleted), telemetry.Failed.Int(run.Failed),
		telemetry.Written.Int(run.Created + run.Updated), telemetry.Older.Int(run.older), telemetry.Expired.Int(run.expired),
		telemetry.Retried.Int(run.retried),
	}
}

// finish fetches and writes what the sync started, then records how it
// ended. A sync that fails is recorded too, so the failure shows with the
// integration's runs and the scheduler waits out the interval instead of
// retrying, and burning provider rate limits, every minute.
func (s *syncing) finish(ctx context.Context) (_ *Run, err error) {
	ctx = telemetry.Resume(ctx, s.traced)
	defer func() {
		attrs := runAttributes(s.run)
		s.span.SetAttributes(attrs...)
		attrs = append(attrs, telemetry.Duration.Int64(time.Since(s.started).Milliseconds()))
		switch {
		case err != nil:
			telemetry.Error(ctx, "sync failed", err, attrs...)
		case s.run.Error != "":
			telemetry.Warn(ctx, "sync finished with failures", errors.New(s.run.Error), attrs...)
		default:
			telemetry.Info(ctx, "sync finished", attrs...)
		}
		telemetry.End(s.span, err)
	}()
	defer func() { _ = s.lock.Rollback() }()
	err = s.pull(ctx)
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
	saw := newSightings()
	// stuck are batches that kept deadlocking, which fail the scopes they
	// came from rather than the sync.
	var stuck []ScopeFailure
	emit := func(_ string, items []Item) error {
		entries := s.mapping.Map(ctx, items)
		saw.add(entries)
		for start := 0; start < len(entries); start += writeBatch {
			batch := entries[start:min(start+writeBatch, len(entries))]
			err := writeBatched(ctx, s.db, s.id, batch, run)
			var deadlock deadlockError
			if !errors.As(err, &deadlock) {
				if err != nil {
					return err
				}
				continue
			}
			stuck = append(stuck, s.failBatch(batch, deadlock)...)
		}
		return nil
	}

	// The lock holds a connection while fetching, so bound the fetch to keep
	// a slow provider from holding it indefinitely.
	pullCtx, cancel := context.WithTimeout(ctx, s.timeout)
	var warnings []string
	request := s.request
	request.Warn = func(warning string) { warnings = append(warnings, warning) }
	report, err := s.puller.Pull(pullCtx, s.db, request, emit)
	if errors.Is(pullCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		warnings = append(warnings, "The sync stopped reading after "+filter.FormatDuration(s.timeout)+
			", ahead of the next, which reads what this one didn't.")
	}
	cancel()
	run.ReadSince = request.read.readSince()
	if err != nil {
		return err
	}
	for _, f := range stuck {
		if f.Scope != "" {
			report.ScopeFailures = append(report.ScopeFailures, f)
		} else if report.Failed[f.Resource] == nil {
			if report.Failed == nil {
				report.Failed = map[string]error{}
			}
			report.Failed[f.Resource] = f.Err
		}
	}
	earlier, err := NewStore(s.db).earlierFailedScopes(ctx, s.id, run.ID, lastingRuns-1)
	if err != nil {
		return err
	}
	markLasting(&report, earlier)
	run.failedScopes = failedScopes(report)
	var lasting string
	run.Error, lasting = failureText(report)
	run.Warning = strings.Join(slices.DeleteFunc(append(warnings, lasting), func(w string) bool { return w == "" }), " ")
	if len(report.Failed) == len(request.Resources) {
		return fmt.Errorf("pull %s: %s", s.identifier, run.Error)
	}

	if run.Mode == ModeComplete {
		var covered []string
		for _, r := range request.Resources {
			if report.Failed[r] == nil {
				covered = append(covered, r)
			}
		}
		if run.Deleted, err = reconcile(ctx, s.db, s.id, s.spec, covered, report, saw, run.StartedAt); err != nil {
			return err
		}
	}

	return NewStore(s.db).advanceCursors(ctx, s.id, request.read.readWithout(report), run.StartedAt)
}

// failBatch counts a batch that couldn't be written as failed, and gives
// the scopes it came from, which deleted nothing.
func (s *syncing) failBatch(batch []Entry, deadlock deadlockError) []ScopeFailure {
	type part struct{ resource, scope string }
	parts := map[part]bool{}
	for _, e := range batch {
		index := s.run.Received
		s.run.Received++
		s.run.Failed++
		if len(s.run.Errors) < MaxRunErrors {
			s.run.Errors = append(s.run.Errors, RunError{
				Index: index, Blueprint: e.Blueprint, Identifier: e.Entity.Identifier, Problems: []string{deadlock.Error()},
			})
		}
		parts[part{e.Resource, e.Scope}] = true
	}
	var failures []ScopeFailure
	for p := range parts {
		failures = append(failures, ScopeFailure{Resource: p.resource, Scope: p.scope, Err: fmt.Errorf("%s: %w", p.resource, deadlock)})
	}

	return failures
}

// writes are the blueprints the mappings write, each once.
func (s Spec) writes() []string {
	var out []string
	for _, m := range s.Mappings {
		if !slices.Contains(out, m.Blueprint) {
			out = append(out, m.Blueprint)
		}
	}

	return out
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
// complete interval, on the first sync, the first after a publish that reads
// differently what a reconcile mapping reads, and once the interval has
// passed; incremental between.
func (s Spec) usualMode(h syncHistory, now time.Time) string {
	if s.Sync == nil || s.Sync.CompleteEvery == 0 || h.lastComplete.IsZero() || h.lastComplete.Before(h.changes.complete) ||
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
		telemetry.Error(ctx, "list integrations to sync", err)
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
			telemetry.Error(ctx, "find the last sync", err, telemetry.Integration.String(record.Identifier))
			continue
		}
		if !last.IsZero() && time.Since(last) < spec.SyncEvery()-syncSlack {
			continue
		}
		// Sync logs how it went.
		_, _ = Sync(ctx, ctx.SQL, record.Identifier, TriggerSchedule, "")
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
	// on a context of its own, still bounded by its pullTimeout. finish logs how
	// it went.
	background(func() { _, _ = s.finish(context.Background()) })

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
