package pastesafe

import (
	"sync"
	"time"
)

var (
	clockMu     sync.RWMutex
	processWide Clock = time.Now
)

func currentProcessClock() Clock {
	clockMu.RLock()
	defer clockMu.RUnlock()
	return processWide
}

// SetClock sets the process-wide clock used by every Field without a
// per-instance clock and returns a function restoring the previous one. The
// restore function is idempotent. Tests that call SetClock must not run in
// parallel with other tests that depend on the clock.
func SetClock(c Clock) (restore func()) {
	if c == nil {
		c = time.Now
	}
	clockMu.Lock()
	prev := processWide
	processWide = c
	clockMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			clockMu.Lock()
			processWide = prev
			clockMu.Unlock()
		})
	}
}

// SeparateKeysClock returns a clock that advances by more than PasteWindow on
// every read, so every key event is seen as typed on its own and a
// typed-then-Enter sequence submits. Each returned clock starts fresh.
func SeparateKeysClock() Clock {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := 10 * PasteWindow
	var mu sync.Mutex
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		at = at.Add(step)
		return at
	}
}
