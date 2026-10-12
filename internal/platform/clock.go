package platform

import (
	"sync"
	"time"
)

// Clock is the only source of "now" in the service. Production uses RealClock;
// the acceptance suite and the harness use FakeClock so time is deterministic.
type Clock interface {
	Now() time.Time
}

// RealClock reads the system clock, in UTC.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// FakeClock returns whatever time it was last set to. Safe for concurrent use.
type FakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func NewFakeClock(t time.Time) *FakeClock { return &FakeClock{t: t.UTC()} }

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t.UTC()
}

func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
