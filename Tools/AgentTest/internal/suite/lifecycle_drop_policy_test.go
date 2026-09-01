package suite_test

// Explicit assertions for the AsyncSink's lifecycle-never-dropped contract.
//
// Invocation events are the only kind the async sink may drop under
// saturation; all four lifecycle kinds (suite-started, test-started,
// test-finished, suite-finished) must always be delivered. This contract
// is stated here as an explicit test so no consumer treats a per-run
// invocation count derived from the stream as authoritative: it can
// under-report, and that is deliberate and expected.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/suite"
)

// TestAsyncSink_AllLifecycleKindsDeliveredUnderSaturation verifies that
// every lifecycle event kind (suite-started, test-started, test-finished,
// suite-finished) is delivered even when the buffer is saturated with
// invocation events. This makes "lifecycle events are never dropped" an
// explicit test rather than a property assumed from the code's logic.
func TestAsyncSink_AllLifecycleKindsDeliveredUnderSaturation(t *testing.T) {
	runGuarded(t, func() {
		// Capacity 1: a single invocation event fills the buffer, so every
		// additional invocation emission displaces the previous one.
		inner := newGateSink()
		sink := suite.NewAsyncSink(inner, 1)

		// Build one event of each kind.
		suiteStarted := domain.ProgressEvent{
			Kind:       domain.ProgressSuiteStarted,
			SuiteID:    "suite-under-test",
			TotalTests: 1,
		}
		testStarted := domain.ProgressEvent{
			Kind:        domain.ProgressTestStarted,
			TestID:      "test-a",
			Repetition:  1,
			Repetitions: 1,
		}
		inv1 := domain.ProgressEvent{Kind: domain.ProgressInvocation, Seq: 1}
		inv2 := domain.ProgressEvent{Kind: domain.ProgressInvocation, Seq: 2}
		inv3 := domain.ProgressEvent{Kind: domain.ProgressInvocation, Seq: 3}
		testFinished := domain.ProgressEvent{
			Kind:        domain.ProgressTestFinished,
			TestID:      "test-a",
			Repetition:  1,
			Repetitions: 1,
		}
		suiteFinished := domain.ProgressEvent{
			Kind:   domain.ProgressSuiteFinished,
			Counts: map[domain.Verdict]int{},
		}

		// Prime the delivery goroutine: emit suiteStarted so it blocks on
		// inner.Emit (inner is a gateSink that blocks until released). All
		// subsequent Emits queue deterministically while the goroutine is stuck.
		sink.Emit(suiteStarted)
		select {
		case <-inner.started:
		case <-time.After(2 * time.Second):
			t.Fatal("delivery goroutine never started; cannot proceed with saturation test")
		}

		// Saturate: emit three invocations into capacity 1. inv1 is dropped
		// when inv2 arrives, inv2 is dropped when inv3 arrives. Then emit the
		// three remaining lifecycle events, which must never be dropped.
		sink.Emit(testStarted)
		sink.Emit(inv1)
		sink.Emit(inv2)
		sink.Emit(inv3)
		sink.Emit(testFinished)
		sink.Emit(suiteFinished)

		close(inner.release)
		sink.Close()

		got := inner.all()

		// All four lifecycle kinds must be present in delivered events.
		kindDelivered := map[domain.ProgressKind]bool{}
		for _, ev := range got {
			kindDelivered[ev.Kind] = true
		}
		for _, want := range []domain.ProgressKind{
			domain.ProgressSuiteStarted,
			domain.ProgressTestStarted,
			domain.ProgressTestFinished,
			domain.ProgressSuiteFinished,
		} {
			if !kindDelivered[want] {
				t.Errorf("lifecycle event kind %q was not delivered under saturation; lifecycle events must never be dropped", want)
			}
		}

		// At least two invocation events must have been dropped. We emitted
		// three into a buffer of capacity 1, so inv1 and inv2 were displaced.
		if sink.Dropped() < 2 {
			t.Errorf("Dropped() = %d, want >= 2; emitting 3 invocations into capacity 1 must drop at least 2", sink.Dropped())
		}
	})
}

// TestAsyncSink_InvocationCountIsApproximateUnderSaturation verifies that
// a consumer cannot rely on the number of invocation events delivered to
// be the number emitted. Invocation events may be dropped; the delivered
// count therefore under-reports and is not authoritative for totals.
func TestAsyncSink_InvocationCountIsApproximateUnderSaturation(t *testing.T) {
	runGuarded(t, func() {
		inner := newGateSink()
		const capacity = 2
		sink := suite.NewAsyncSink(inner, capacity)

		// Emit one lifecycle event to prime the gate, then overflow the buffer.
		testStarted := domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: "test-a"}
		sink.Emit(testStarted)
		select {
		case <-inner.started:
		case <-time.After(2 * time.Second):
			t.Fatal("delivery goroutine did not start")
		}

		const emitted = 5 // 5 invocations into capacity 2: 3 must be dropped
		for i := 0; i < emitted; i++ {
			sink.Emit(domain.ProgressEvent{Kind: domain.ProgressInvocation, Seq: i + 1})
		}

		close(inner.release)
		sink.Close()

		// Count delivered invocation events.
		var delivered int
		for _, ev := range inner.all() {
			if ev.Kind == domain.ProgressInvocation {
				delivered++
			}
		}

		// The delivered count must be less than emitted: saturation caused drops.
		if delivered >= emitted {
			t.Errorf("delivered %d invocation events, emitted %d; saturation should have caused drops (Dropped=%d)", delivered, emitted, sink.Dropped())
		}

		// Dropped() must equal the deficit.
		wantDropped := emitted - delivered
		if sink.Dropped() != wantDropped {
			t.Errorf("Dropped() = %d, want %d (emitted=%d delivered=%d)", sink.Dropped(), wantDropped, emitted, delivered)
		}
	})
}
