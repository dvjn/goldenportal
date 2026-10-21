package catalog

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/dvjn/goldport/internal/definition"
)

const planLifetime = 15 * time.Minute

// Plan is a proposed set of definitions with what applying it changes. Base
// is the revision it was made on top of; applying it on top of any other
// revision is refused.
type Plan struct {
	ID         string
	ExpiresAt  time.Time
	Base       int
	RollbackOf int
	Desired    definition.Set
	Changes    []definition.Change
	Effects    definition.Effects
	Impact     Impact
}

// plans holds plans in memory until they are applied or expire. A plan is
// not a draft: it is never stored and a restart forgets it.
type plans struct {
	mu   sync.Mutex
	byID map[string]Plan
	now  func() time.Time
}

func newPlans(now func() time.Time) *plans {
	return &plans{byID: map[string]Plan{}, now: now}
}

func (p *plans) add(plan Plan) Plan {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.prune()
	plan.ID = rand.Text()
	plan.ExpiresAt = p.now().Add(planLifetime)
	p.byID[plan.ID] = plan

	return plan
}

// take removes the plan and returns it, unless it expired.
func (p *plans) take(id string) (Plan, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.prune()
	plan, ok := p.byID[id]
	delete(p.byID, id)

	return plan, ok
}

func (p *plans) prune() {
	for id, plan := range p.byID {
		if !p.now().Before(plan.ExpiresAt) {
			delete(p.byID, id)
		}
	}
}
