package suite_test

// Tests for AsyncSink's saturation/drop-oldest-invocation policy, MultiSink's
// fan-out, and DiscardSink's no-op behaviour (sink.go).

import (
	"sync"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/suite"
)

// gateSink is a domain.ProgressSink double whose first Emit call blocks
// until release is closed, signalling on started once it has begun
// blocking. This lets a test know precisely when AsyncSink's delivery
// goroutine has picked up the first event and is stuck delivering it,
// so every Emit the test issues afterward is deterministically queued
// (not racily also picked up) while the wrapper is saturated.
//
// Once release is closed, every call (including the blocked first one)
// proceeds immediately and records its event, preserving delivery order.
type gateSink struct {
	started     chan struct{}
	startedOnce sync.Once
	release     chan struct{}

	mu     sync.Mutex
	events []domain.ProgressEvent
}

var _ domain.ProgressSink = (*gateSink)(nil)

func newGateSink() *gateSink {
	return &gateSink{started: make(chan struct{}), release: make(chan struct{})}
}

func (g *gateSink) Emit(ev domain.ProgressEvent) {
	g.startedOnce.Do(func() { close(g.started) })
	<-g.release
	g.mu.Lock()
	g.events = append(g.events, ev)
	g.mu.Unlock()
}

func (g *gateSink) all() []domain.ProgressEvent {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]domain.ProgressEvent(nil), g.events...)
}

// runGuarded recovers a panic from fn into a test failure instead of
// letting it abort the whole test binary — needed because sink.go's stubs
// panic, following the same "compiles, then fails cleanly" RED convention
// runSuite already applies to Suite.Run.
func runGuarded(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	fn()
}

// TestAsyncSink_DropsOldestInvocationEventsUnderSaturation asserts that,
// once the wrapper's buffer is saturated, ProgressInvocation events are
// dropped oldest-first while every other kind is always delivered, and
// that Dropped() reports exactly how many invocation events were lost.
func TestAsyncSink_DropsOldestInvocationEventsUnderSaturation(t *testing.T) {
	runGuarded(t, func() {
		// Arrange
		inner := newGateSink()
		sink := suite.NewAsyncSink(inner, 2)

		started := domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: "test-a"}
		invocation1 := domain.ProgressEvent{Kind: domain.ProgressInvocation, Seq: 1}
		invocation2 := domain.ProgressEvent{Kind: domain.ProgressInvocation, Seq: 2}
		invocation3 := domain.ProgressEvent{Kind: domain.ProgressInvocation, Seq: 3}
		finished := domain.ProgressEvent{Kind: domain.ProgressTestFinished, TestID: "test-a"}

		// Act: prime the delivery goroutine so it is blocked delivering
		// `started`, then saturate the buffer (capacity 2) with three
		// invocation events — the oldest (invocation1) must be dropped —
		// and finally emit a non-invocation event, which must never be
		// dropped even though the buffer is already saturated.
		sink.Emit(started)
		select {
		case <-inner.started:
		case <-time.After(2 * time.Second):
			t.Fatal("AsyncSink's delivery goroutine never began delivering the first event")
		}

		sink.Emit(invocation1)
		sink.Emit(invocation2)
		sink.Emit(invocation3)
		sink.Emit(finished)

		close(inner.release)
		sink.Close()

		// Assert
		got := inner.all()
		want := []domain.ProgressEvent{started, invocation2, invocation3, finished}
		if len(got) != len(want) {
			t.Fatalf("delivered events = %+v, want %+v (invocation1 should have been dropped as the oldest invocation event)", got, want)
		}
		for i, w := range want {
			if got[i].Kind != w.Kind || got[i].Seq != w.Seq {
				t.Errorf("delivered event %d = {Kind:%v Seq:%d}, want {Kind:%v Seq:%d}", i, got[i].Kind, got[i].Seq, w.Kind, w.Seq)
			}
		}

		if sink.Dropped() != 1 {
			t.Errorf("Dropped() = %d, want 1 (only invocation1 should have been dropped)", sink.Dropped())
		}
	})
}

// TestMultiSink_FansOutToEveryInnerSink asserts a single Emit on a MultiSink
// reaches every sink it wraps.
func TestMultiSink_FansOutToEveryInnerSink(t *testing.T) {
	runGuarded(t, func() {
		// Arrange
		s1 := &recordingSink{}
		s2 := &recordingSink{}
		multi := suite.MultiSink(s1, s2)
		ev := domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: "test-a"}

		// Act
		multi.Emit(ev)

		// Assert
		for i, s := range []*recordingSink{s1, s2} {
			got := s.all()
			if len(got) != 1 {
				t.Fatalf("inner sink %d received %d events, want 1", i, len(got))
			}
			if got[0].Kind != ev.Kind || got[0].TestID != ev.TestID {
				t.Errorf("inner sink %d received %+v, want %+v", i, got[0], ev)
			}
		}
	})
}

// TestDiscardSink_IsANoOp asserts DiscardSink accepts any event without
// panicking or otherwise doing anything observable — it exists for a run
// whose progress nobody watches.
func TestDiscardSink_IsANoOp(t *testing.T) {
	runGuarded(t, func() {
		// Arrange
		sink := suite.DiscardSink()

		// Act & Assert: must not panic.
		sink.Emit(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 3})
		sink.Emit(domain.ProgressEvent{Kind: domain.ProgressSuiteFinished})
	})
}
