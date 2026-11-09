package connection

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// sourceClock keeps when a system answered, by its own clock, the earliest
// of the answers read under a context, which is when what was read from
// them was true. The system's clock, rather than the portal's, makes reads
// by different replicas compare alike.
type sourceClock struct {
	// parent is the clock of the context this one was made within, which
	// sees the same answers.
	parent *sourceClock
	mu     sync.Mutex
	at     time.Time
}

type sourceClockKey struct{}

// withSourceClock times the answers read under the context it returns.
func withSourceClock(ctx context.Context) (context.Context, *sourceClock) {
	c := &sourceClock{}
	c.parent, _ = ctx.Value(sourceClockKey{}).(*sourceClock)

	return context.WithValue(ctx, sourceClockKey{}, c), c
}

// answered notes an answer read under ctx, by its Date header.
func answered(ctx context.Context, header http.Header) {
	c, _ := ctx.Value(sourceClockKey{}).(*sourceClock)
	if c == nil {
		return
	}
	at, err := http.ParseTime(header.Get("Date"))
	if err != nil {
		return
	}
	answeredAt(ctx, at)
}

// answeredAt notes an answer read under ctx at a time, such as one read
// once for several objects.
func answeredAt(ctx context.Context, at time.Time) {
	if at.IsZero() {
		return
	}
	for c, _ := ctx.Value(sourceClockKey{}).(*sourceClock); c != nil; c = c.parent {
		c.mu.Lock()
		if c.at.IsZero() || at.Before(c.at) {
			c.at = at
		}
		c.mu.Unlock()
	}
}

// take gives the earliest answer since the clock was last taken, or zero
// for none, and starts it again.
func (c *sourceClock) take() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	at := c.at
	c.at = time.Time{}

	return at
}

// earliest is the earlier of two answers' times, either of which may be
// unknown.
func earliest(a, b time.Time) time.Time {
	if a.IsZero() || (!b.IsZero() && b.Before(a)) {
		return b
	}

	return a
}
