package runstate_test

import (
	"sync"
	"time"
)

// fakeClock is a manually advanced domain.Clock, so acquisition-timeout and
// staleness-threshold tests never depend on real wall-clock sleeps for
// their pass/fail decision.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// startTicking fast-forwards the clock in the background so a real-time
// bounded test can exercise the 5s acquisition timeout and 30s staleness
// threshold without actually waiting that long. Call the returned func to
// stop it.
func (c *fakeClock) startTicking(step time.Duration) func() {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(2 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				c.Advance(step)
			}
		}
	}()
	return func() { close(stop) }
}
