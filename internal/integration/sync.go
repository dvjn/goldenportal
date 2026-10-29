package integration

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/dvjn/goldport/internal/catalog"
	"github.com/dvjn/goldport/internal/connector"
	"github.com/dvjn/goldport/internal/definition"
	"github.com/dvjn/goldport/internal/httpx"
	"gofr.dev/pkg/gofr/container"
)

const syncTimeout = 30 * time.Minute

var (
	errSyncRunning = httpx.Error{
		Code:    http.StatusConflict,
		Message: "a sync of this integration is already running, wait for it to finish",
	}
	errNotDue = errors.New("the integration is not due to sync")
)

// Locker keeps a pull integration to one run at a time across every
// instance of the portal.
type Locker interface {
	// TryLock takes the integration's lock, or reports false when another
	// run holds it. The caller releases a lock it took.
	TryLock(ctx context.Context, integration string) (release func(), locked bool, err error)
}

// Syncer runs pull integrations: it fetches their records through their
// connector, maps them and writes the entities, sweeping what the source
// no longer has.
type Syncer struct {
	entities   entityWriter
	runs       runStore
	connectors map[definition.Connector]connector.Connector
	lookups    lookupReader
	locker     Locker
	getenv     func(string) string
	now        func() time.Time
	logger     logger

	mu      sync.Mutex
	running map[string]bool
}

type logger interface {
	Errorf(format string, args ...any)
}

// SyncerConfig is what a Syncer needs. Getenv reads the environment
// variables that hold the integrations' secrets.
type SyncerConfig struct {
	Entities   entityWriter
	Runs       runStore
	Connectors map[definition.Connector]connector.Connector
	Lookups    lookupReader
	Locker     Locker
	Getenv     func(string) string
	Now        func() time.Time
	Logger     logger
}

func NewSyncer(config SyncerConfig) *Syncer {
	return &Syncer{
		entities:   config.Entities,
		runs:       config.Runs,
		connectors: config.Connectors,
		lookups:    config.Lookups,
		locker:     config.Locker,
		getenv:     config.Getenv,
		now:        config.Now,
		logger:     config.Logger,
		running:    map[string]bool{},
	}
}

// syncStart asks for a run. Due decides, once the lock is held, whether the
// integration still needs it given when its last sync started.
type syncStart struct {
	set         definition.Set
	integration *definition.Integration
	trigger     Trigger
	actor       string
	due         func(lastPull time.Time) bool
}

func always(time.Time) bool { return true }

// Start takes the integration's lock and records a running run. The caller
// performs the run with the returned function, which releases the lock.
func (s *Syncer) Start(ctx context.Context, start syncStart) (Run, func(context.Context), error) {
	identifier := start.integration.Identifier
	if !s.claim(identifier) {
		return Run{}, nil, errSyncRunning
	}
	release, locked, err := s.locker.TryLock(ctx, identifier)
	if err != nil || !locked {
		s.unclaim(identifier)
		return Run{}, nil, cmp.Or(err, error(errSyncRunning))
	}
	done := func() {
		release()
		s.unclaim(identifier)
	}
	lastPull, err := s.runs.LastPull(ctx, identifier)
	if err == nil && !start.due(lastPull) {
		err = errNotDue
	}
	var run Run
	if err == nil {
		run, err = s.runs.Start(ctx, Run{Integration: identifier, Trigger: start.trigger, Actor: start.actor, StartedAt: s.now()})
	}
	if err != nil {
		done()
		return Run{}, nil, err
	}

	return run, func(ctx context.Context) {
		defer done()
		ctx, cancel := context.WithTimeout(ctx, syncTimeout)
		defer cancel()
		s.perform(ctx, start, &run)
	}, nil
}

func (s *Syncer) claim(integration string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[integration] {
		return false
	}
	s.running[integration] = true

	return true
}

func (s *Syncer) unclaim(integration string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, integration)
}

// perform fetches, maps and writes, then records how the run ended. A
// record that makes no entity is skipped with a warning.
func (s *Syncer) perform(ctx context.Context, start syncStart, run *Run) {
	err := s.sync(ctx, start, run)
	if failureText(err) == unexpectedFailure {
		s.logger.Errorf("sync of %s failed: %v", start.integration.Identifier, err)
	}
	run.finish(s.now(), failureText(err))
	if err := s.runs.Finish(context.WithoutCancel(ctx), *run); err != nil {
		s.logger.Errorf("record the sync of %s: %v", start.integration.Identifier, err)
	}
}

func (s *Syncer) sync(ctx context.Context, start syncStart, run *Run) error {
	integration := start.integration
	records, err := s.fetch(ctx, integration)
	if err != nil {
		return err
	}
	write := catalog.OwnedWrite{Integration: integration.Identifier, Sweep: true}
	groups := map[string]int{}
	mapped := map[string][]any{}
	for _, mapping := range integration.Mappings {
		mapper, err := NewMapper(start.set, mapping)
		if err != nil {
			return syncError{message: err.Error()}
		}
		bound, err := lookups(ctx, s.lookups, integration.Identifier, mapper, mapped)
		if err != nil {
			return err
		}
		entities, problems := mapper.Map(ctx, records[mapping.Kind], bound...)
		mapped[mapper.Blueprint()] = append(mapped[mapper.Blueprint()], LookupRecords(entities)...)
		run.Counts.Fetched += len(records[mapping.Kind])
		run.Counts.Skipped += skippedRecords(problems)
		for _, problem := range problems {
			run.Warnings = append(run.Warnings, problem.String())
		}
		index, seen := groups[mapper.Blueprint()]
		if !seen {
			index = len(write.Groups)
			groups[mapper.Blueprint()] = index
			write.Groups = append(write.Groups, catalog.OwnedGroup{Blueprint: mapper.Blueprint()})
		}
		write.Groups[index].Entities = append(write.Groups[index].Entities, entities...)
	}

	return applyWrite(ctx, s.entities, write, run, pullGuardWarning)
}

func skippedRecords(problems []Problem) int {
	indexes := map[int]bool{}
	for _, problem := range problems {
		indexes[problem.Index] = true
	}

	return len(indexes)
}

// fetch reads the records of every kind the integration maps, with the
// secrets it names read from the environment.
func (s *Syncer) fetch(ctx context.Context, integration *definition.Integration) (connector.Records, error) {
	source, ok := s.connectors[integration.Connector]
	if !ok {
		return nil, syncError{message: fmt.Sprintf("no connector reads %s", integration.Connector)}
	}
	secrets := map[string]string{}
	for name, secret := range integration.Secrets.All() {
		value := s.getenv(secret.SecretRef)
		if value == "" {
			return nil, syncError{message: fmt.Sprintf(
				"the %s secret reads the %s environment variable, which is not set on the server", name, secret.SecretRef)}
		}
		secrets[name] = value
	}
	var kinds []string
	for _, mapping := range integration.Mappings {
		kinds = append(kinds, mapping.Kind)
	}
	records, err := source.Fetch(ctx, connector.Request{
		Config:  integration.Config,
		Secrets: secrets,
		Kinds:   slices.Compact(slices.Sorted(slices.Values(kinds))),
	})
	if err != nil {
		return nil, syncError{message: err.Error()}
	}

	return records, nil
}

func pullGuardWarning(guarded catalog.GuardedSweep) string {
	return fmt.Sprintf("kept %d %s entities that the source no longer has, because the sync read %d of the %d the integration had, "+
		"fewer than half; the next sync deletes them once it reads at least half",
		guarded.Kept, guarded.Blueprint, guarded.Written, guarded.Existing)
}

// PostgresLocker locks an integration with a transaction-level advisory
// lock, which Postgres releases when the transaction ends, even when the
// instance that held it stops.
type PostgresLocker struct {
	db container.DB
}

func NewPostgresLocker(db container.DB) PostgresLocker {
	return PostgresLocker{db: db}
}

const queryTryLock = `SELECT pg_try_advisory_xact_lock(hashtext('goldport.sync'), hashtext($1))`

func (l PostgresLocker) TryLock(ctx context.Context, integration string) (func(), bool, error) {
	tx, err := l.db.Begin()
	if err != nil {
		return nil, false, err
	}
	release := func() { _ = tx.Rollback() }
	var locked bool
	if err := tx.QueryRowContext(ctx, queryTryLock, integration).Scan(&locked); err != nil || !locked {
		release()
		return nil, false, err
	}

	return release, true, nil
}
