package session

import "sync"

// StopSignal is a concurrency-safe request/observe flag for graceful stop.
// The TUI's main goroutine calls Request (on confirmed stop) and Reset
// (before a resumed run restarts); the session's dispatch-loop goroutine
// calls Requested. Safe for concurrent use by multiple goroutines.
//
// The zero value is not usable; construct with NewStopSignal.
type StopSignal struct {
	mu        sync.Mutex
	requested bool
}

// NewStopSignal returns a disarmed StopSignal.
func NewStopSignal() *StopSignal {
	return &StopSignal{}
}

// Request arms the signal. Idempotent: safe to call on every tick after a
// confirmed stop (mirroring the prior ctxCancel()-every-tick pattern), and
// safe to call from a different goroutine than Requested's caller.
func (s *StopSignal) Request() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requested = true
}

// Reset disarms the signal. Called before a resumed run restarts so a prior
// run's confirmed stop does not immediately re-trigger the new run.
func (s *StopSignal) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requested = false
}

// Requested reports whether Request has been called since the last Reset (or
// since construction). Suitable as Deps.StopRequested via method value:
// Deps{StopRequested: signal.Requested}.
func (s *StopSignal) Requested() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requested
}
