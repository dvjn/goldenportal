package integration

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/dvjn/goldport/internal/audit"
	"github.com/dvjn/goldport/internal/definition"
)

const (
	schedulerTick  = 15 * time.Second
	maxStartJitter = time.Minute
)

// Scheduler syncs each pull integration on its interval. It reads the
// definitions on every tick, so an applied change takes effect without a
// restart, and spreads the first syncs after a start over a minute.
type Scheduler struct {
	definitions definitionReader
	runs        runStore
	syncer      *Syncer
	audit       auditLog
	now         func() time.Time
	logger      logger
	jitter      func() time.Duration
	started     time.Time

	mu       sync.Mutex
	firstDue map[string]time.Time
}

func NewScheduler(definitions definitionReader, runs runStore, syncer *Syncer, log auditLog, now func() time.Time, logger logger) *Scheduler {
	return &Scheduler{
		definitions: definitions,
		runs:        runs,
		syncer:      syncer,
		audit:       log,
		now:         now,
		logger:      logger,
		jitter:      func() time.Duration { return rand.N(maxStartJitter) },
		started:     now(),
		firstDue:    map[string]time.Time{},
	}
}

// Next returns when the integration syncs next on its schedule: an
// interval after its last sync started, and not before its first chance
// after the portal started. It reports false for an integration without a
// schedule.
func (s *Scheduler) Next(integration *definition.Integration, lastPull time.Time) (time.Time, bool) {
	interval, err := time.ParseDuration(integration.Interval)
	if !integration.Pull() || err != nil {
		return time.Time{}, false
	}
	first := s.first(integration.Identifier)
	if next := lastPull.Add(interval); next.After(first) {
		return next, true
	}

	return first, true
}

func (s *Scheduler) first(integration string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.firstDue[integration]; !ok {
		s.firstDue[integration] = s.started.Add(s.jitter())
	}

	return s.firstDue[integration]
}

func (s *Scheduler) due(integration *definition.Integration) func(lastPull time.Time) bool {
	return func(lastPull time.Time) bool {
		next, scheduled := s.Next(integration, lastPull)
		return scheduled && !next.After(s.now())
	}
}

// Run syncs the integrations that are due until the context ends.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(schedulerTick)
	defer ticker.Stop()
	for {
		s.syncDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// syncDue starts a sync of every pull integration that is due. Another
// instance may hold an integration's lock or have synced it since, and then
// this one skips it.
func (s *Scheduler) syncDue(ctx context.Context) {
	set, _, err := s.definitions.Definitions(ctx)
	if err != nil {
		s.logger.Errorf("read the definitions to schedule syncs: %v", err)
		return
	}
	for _, integration := range set.Integrations() {
		due := s.due(integration)
		lastPull, err := s.runs.LastPull(ctx, integration.Identifier)
		if err != nil {
			s.logger.Errorf("read the last sync of %s: %v", integration.Identifier, err)
			continue
		}
		if !due(lastPull) {
			continue
		}
		run, perform, err := s.syncer.Start(ctx, syncStart{
			set: set, integration: integration, trigger: TriggerSchedule, actor: audit.Scheduler.Label(), due: due,
		})
		switch {
		case errors.Is(err, errSyncRunning) || errors.Is(err, errNotDue):
		case err != nil:
			s.logger.Errorf("start the sync of %s: %v", integration.Identifier, err)
		default:
			go perform(ctx)
			if err := s.audit.Record(ctx, runEntry(audit.Scheduler, audit.IntegrationSynced, integration, run)); err != nil {
				s.logger.Errorf("record the sync of %s in the audit log: %v", integration.Identifier, err)
			}
		}
	}
}
