package suite

import (
	"context"
	"sync"
)

// PauseControl is a concurrency-safe toggle that lets callers pause and
// resume suite scheduling without cancelling in-flight runs.
//
// The zero value is not usable. Construct with NewPauseControl.
type PauseControl struct {
	mu     sync.Mutex
	cond   *sync.Cond
	paused bool
}

// NewPauseControl returns a ready-to-use PauseControl in the unpaused state.
func NewPauseControl() *PauseControl {
	p := &PauseControl{}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// Pause requests workers to stop picking up new work items. Workers that are
// mid-run are unaffected: they complete their current item normally. Calling
// Pause when already paused is a no-op.
func (p *PauseControl) Pause() {
	p.mu.Lock()
	p.paused = true
	p.mu.Unlock()
}

// Resume allows workers to pick up new work items again. Calling Resume when
// not paused is a no-op.
func (p *PauseControl) Resume() {
	p.mu.Lock()
	p.paused = false
	p.cond.Broadcast()
	p.mu.Unlock()
}

// IsPaused reports whether the control is currently in the paused state.
// Intended for display purposes (TUI status); the worker loop uses
// WaitIfPaused for correct synchronization.
func (p *PauseControl) IsPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paused
}

// WaitIfPaused blocks the calling goroutine until either (a) the control is
// not paused, or (b) ctx is cancelled. Returns ctx.Err() when the context was
// cancelled while waiting; returns nil when resumed normally.
//
// This is the method workers call at the scheduling point. It selects on both
// the pause state and ctx.Done() so that cancellation still works while
// paused.
func (p *PauseControl) WaitIfPaused(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for p.paused {
		// Check context cancellation before waiting.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Wait on the condition variable. We must interleave ctx.Done()
		// checking because sync.Cond does not support a select-style wait.
		// We use a channel-based approach: spawn a watcher goroutine that
		// broadcasts on context cancellation so the waiting goroutine wakes.
		waitDone := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				// Wake all waiters so they can check ctx.Err().
				p.cond.Broadcast()
			case <-waitDone:
				// Woken by Resume; nothing to do.
			}
		}()

		p.cond.Wait()
		close(waitDone)

		// Check again after waking: either resumed or cancelled.
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	return nil
}
