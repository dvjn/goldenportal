package mcp

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/dvjn/goldenportal/internal/httpx"
)

// An agent works in bursts, reading several things before it acts, so a
// user gets a generous burst and a steady rate after it. The limit is per
// user, across their apps, and per portal instance.
const (
	requestsPerSecond = 5
	requestBurst      = 60
	limiterIdle       = 10 * time.Minute
)

// Limiter caps how fast each user calls the MCP server. Make one per
// portal and share it: the router builds middleware for every request.
type Limiter struct {
	mu    sync.Mutex
	users map[string]*userLimit
	now   func() time.Time
}

type userLimit struct {
	limit *rate.Limiter
	seen  time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{users: map[string]*userLimit{}, now: time.Now}
}

// allow takes a request from user's allowance, or says how long until
// there's one.
func (l *Limiter) allow(user string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	u, ok := l.users[user]
	if !ok {
		l.forgetIdle(now)
		u = &userLimit{limit: rate.NewLimiter(requestsPerSecond, requestBurst)}
		l.users[user] = u
	}
	u.seen = now
	r := u.limit.ReserveN(now, 1)
	if delay := r.DelayFrom(now); delay > 0 {
		r.CancelAt(now)
		return false, delay
	}
	return true, 0
}

// forgetIdle drops users whose allowance has refilled, so the map only
// holds who's active.
func (l *Limiter) forgetIdle(now time.Time) {
	for user, u := range l.users {
		if now.Sub(u.seen) > limiterIdle {
			delete(l.users, user)
		}
	}
}

func tooManyRequests(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	httpx.WriteError(w, http.StatusTooManyRequests, "too many requests; try again shortly")
}
