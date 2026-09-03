package suite_test

// Tests for the bounded concurrent scheduler.
//
// The scheduling unit is one repetition of one test. Under the warm-up
// scheduling model:
//   - Each test's first repetition (rep 1) runs alone as a warm-up before any
//     of its remaining repetitions start.
//   - After rep 1 completes, remaining repetitions fan out concurrently,
//     bounded by MaxConcurrentRuns.
//   - Tests within a suite execute one at a time; no two distinct tests' runs
//     overlap.
//
// Tests that are RED-phase (fail now, pass after the scheduler is implemented):
//   T14.2 — asserts tests run one at a time with no cross-test overlap; the
//     current shared-FIFO concurrent scheduler dispatches distinct tests to the
//     worker pool simultaneously, triggering the overlap detector.
//   T14.3 — asserts the bound governs concurrent repetitions within one test
//     and no cross-test overlap occurs; fails for the same reason as T14.2.
//   Warm-up ordering — verifies rep 1 ends before reps 2..N start; blocks rep
//     1 long enough for a concurrent scheduler to provably start rep 2 while
//     rep 1 is still in-flight if the warm-up gate is absent.
//   Cancellation-during-warm-up — the current scheduler dispatches rep 2 before
//     rep 1 completes, so the fan-out rep starts during warm-up.
//
// Tests that are regression guards (pass now, protect against future bugs):
//   T14.1 — fan-out reps execute concurrently up to bound within a single test.
//   T14.4 — bound-of-1 determinism: sequential behavior must be preserved.
//   T14.5 — report ordering by plan order and repetition number.
//   T14.6 — attempt ordering within one repetition.
//   T14.7 — lifecycle events emitted for all repetitions.
//   T14.8 — distinct run identities for all runs.
//   Single-rep no-op — no warm-up gate overhead for a test with one rep.

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/suite"
)

// ---------------------------------------------------------------------------
// holdingRunner — blocks calls until releaseAt are simultaneously in-flight
// ---------------------------------------------------------------------------

// holdingRunner blocks every Run call behind a release gate. The gate opens
// automatically when releaseAt calls are simultaneously in-flight; all waiting
// calls then proceed immediately. Any call that arrives after the gate is open
// proceeds without waiting.
//
// If fewer than releaseAt calls are ever in-flight simultaneously (sequential
// execution with context cancellation) each blocked call returns early via
// ctx.Done, so the suite can drain without deadlocking.
type holdingRunner struct {
	releaseAt int

	mu               sync.Mutex
	inFlight         int
	peak             int
	calls            []domain.RunKey
	releaseTriggered bool
	gate             chan struct{} // closed once when releaseAt reached; never reopened
}

func newHoldingRunner(releaseAt int) *holdingRunner {
	return &holdingRunner{
		releaseAt: releaseAt,
		gate:      make(chan struct{}),
	}
}

func (r *holdingRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	r.mu.Lock()
	r.inFlight++
	if r.inFlight > r.peak {
		r.peak = r.inFlight
	}
	r.calls = append(r.calls, key)
	shouldOpen := r.inFlight >= r.releaseAt && !r.releaseTriggered
	if shouldOpen {
		r.releaseTriggered = true
	}
	r.mu.Unlock()

	if shouldOpen {
		close(r.gate)
	}

	select {
	case <-r.gate:
	case <-ctx.Done():
	}

	r.mu.Lock()
	r.inFlight--
	r.mu.Unlock()

	ev := passingEvidence()
	ev.Key = key
	if eval != nil {
		return eval(ev), nil
	}
	return domain.TestResult{Key: key}, nil
}

func (r *holdingRunner) peakConcurrent() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peak
}

func (r *holdingRunner) allRunKeys() []domain.RunKey {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.RunKey(nil), r.calls...)
}

// ---------------------------------------------------------------------------
// atomicTrackingRunner — wraps a runner to track peak in-flight via atomics
// ---------------------------------------------------------------------------

// atomicTrackingRunner wraps a TestRunner and records the peak number of
// simultaneously in-flight Run calls without blocking the calls themselves.
type atomicTrackingRunner struct {
	delegate     suite.TestRunner
	inFlight     *int64
	peakInFlight *int64
}

func (r *atomicTrackingRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	current := atomic.AddInt64(r.inFlight, 1)
	for {
		p := atomic.LoadInt64(r.peakInFlight)
		if current <= p {
			break
		}
		if atomic.CompareAndSwapInt64(r.peakInFlight, p, current) {
			break
		}
	}
	defer atomic.AddInt64(r.inFlight, -1)
	return r.delegate.Run(ctx, key, t, eval)
}

// ---------------------------------------------------------------------------
// warmUpSafeHoldingRunner — gates fan-out reps, passes warmup through
// ---------------------------------------------------------------------------

// warmUpSafeHoldingRunner is like holdingRunner but lets the warm-up
// repetition (RunNumber == 1) complete immediately without blocking. Subsequent
// repetitions are held behind a gate that opens when releaseAt of them are
// simultaneously in-flight, proving that the post-warm-up fan-out reaches the
// expected concurrency level.
//
// The gate never re-closes once opened: after it fires for the first test's
// fan-out, subsequent tests' fan-out reps also pass through immediately, which
// is correct because only one test's reps are ever in-flight at a time under
// the new scheduling model.
type warmUpSafeHoldingRunner struct {
	releaseAt int

	mu               sync.Mutex
	inFlight         int // counts non-warmup (RunNumber > 1) calls only
	peak             int // peak non-warmup concurrent calls
	releaseTriggered bool
	gate             chan struct{} // closed once when releaseAt reached
}

func newWarmUpSafeHoldingRunner(releaseAt int) *warmUpSafeHoldingRunner {
	return &warmUpSafeHoldingRunner{
		releaseAt: releaseAt,
		gate:      make(chan struct{}),
	}
}

func (r *warmUpSafeHoldingRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	// Warm-up rep: complete immediately without joining the gate.
	if key.RunNumber == 1 {
		ev := passingEvidence()
		ev.Key = key
		if eval != nil {
			return eval(ev), nil
		}
		return domain.TestResult{Key: key}, nil
	}

	// Fan-out rep: track in-flight count and open the gate when releaseAt reached.
	r.mu.Lock()
	r.inFlight++
	if r.inFlight > r.peak {
		r.peak = r.inFlight
	}
	shouldOpen := r.inFlight >= r.releaseAt && !r.releaseTriggered
	if shouldOpen {
		r.releaseTriggered = true
	}
	r.mu.Unlock()

	if shouldOpen {
		close(r.gate)
	}

	select {
	case <-r.gate:
	case <-ctx.Done():
	}

	r.mu.Lock()
	r.inFlight--
	r.mu.Unlock()

	ev := passingEvidence()
	ev.Key = key
	if eval != nil {
		return eval(ev), nil
	}
	return domain.TestResult{Key: key}, nil
}

// peakFanOut returns the peak number of simultaneously in-flight non-warm-up
// (RunNumber > 1) calls observed across the lifetime of this runner.
func (r *warmUpSafeHoldingRunner) peakFanOut() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peak
}

// ---------------------------------------------------------------------------
// testSerializationDetector — detects cross-test concurrent in-flight overlap
// ---------------------------------------------------------------------------

// testSerializationDetector wraps a delegate TestRunner and records whether
// two calls for distinct test names were ever simultaneously in-flight.
// It yields the goroutine after updating its in-flight map so that concurrent
// goroutines — if the scheduler dispatched them — have a chance to run and be
// detected.
type testSerializationDetector struct {
	delegate    suite.TestRunner
	mu          sync.Mutex
	inFlight    map[string]int // test name -> current in-flight count
	overlapSeen bool
}

func newTestSerializationDetector(delegate suite.TestRunner) *testSerializationDetector {
	return &testSerializationDetector{
		delegate: delegate,
		inFlight: make(map[string]int),
	}
}

func (d *testSerializationDetector) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	d.mu.Lock()
	for name, count := range d.inFlight {
		if name != t.Definition.Name && count > 0 {
			d.overlapSeen = true
		}
	}
	d.inFlight[t.Definition.Name]++
	d.mu.Unlock()

	// Yield to the runtime: if the scheduler dispatched concurrent tests,
	// their goroutines are now eligible to enter Run() and observe the
	// current test's entry in the in-flight map.
	runtime.Gosched()

	result, err := d.delegate.Run(ctx, key, t, eval)

	d.mu.Lock()
	d.inFlight[t.Definition.Name]--
	d.mu.Unlock()

	return result, err
}

// overlapDetected reports whether any two distinct test names were ever
// simultaneously in-flight during the suite run.
func (d *testSerializationDetector) overlapDetected() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.overlapSeen
}

// ---------------------------------------------------------------------------
// warmUpOrderingRunner — records start/end sequence per repetition
// ---------------------------------------------------------------------------

// warmUpOrderingRunner records the global event sequence number when each
// repetition starts and ends, proving temporal ordering: rep 1's end sequence
// must be strictly less than rep 2's start sequence.
//
// To make the ordering assertion reliable in the RED phase (before the warm-up
// gate is implemented), rep 1 blocks until either rep 2 attempts to start or a
// short timeout fires. This gives a concurrent scheduler without the warm-up
// gate a clear window to dispatch rep 2 while rep 1 is still in-flight;
// without the block, rep 1 finishes so fast that the race is rarely observable.
//
// With a correctly implemented warm-up gate, rep 2 never starts while rep 1 is
// blocked, so the timeout fires and the test proceeds normally.
type warmUpOrderingRunner struct {
	mu          sync.Mutex
	counter     int
	starts      map[int]int // runNumber -> global counter value at start
	ends        map[int]int // runNumber -> global counter value at end
	rep2Started chan struct{} // closed when rep 2 enters Run
	once        sync.Once
}

func newWarmUpOrderingRunner() *warmUpOrderingRunner {
	return &warmUpOrderingRunner{
		starts:      make(map[int]int),
		ends:        make(map[int]int),
		rep2Started: make(chan struct{}),
	}
}

func (r *warmUpOrderingRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	r.mu.Lock()
	r.counter++
	r.starts[key.RunNumber] = r.counter
	r.mu.Unlock()

	if key.RunNumber == 2 {
		// Signal before doing any work so rep 1 can observe this promptly.
		r.once.Do(func() { close(r.rep2Started) })
	}

	if key.RunNumber == 1 {
		// Hold rep 1 in-flight until rep 2 tries to start, or a timeout fires.
		// A scheduler without the warm-up gate will dispatch rep 2 concurrently;
		// with the gate, rep 2 is held back, the timeout fires, and the test passes.
		select {
		case <-r.rep2Started:
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
		}
	}

	ev := passingEvidence()
	ev.Key = key
	var result domain.TestResult
	if eval != nil {
		result = eval(ev)
	} else {
		result = domain.TestResult{Key: key}
	}

	r.mu.Lock()
	r.counter++
	r.ends[key.RunNumber] = r.counter
	r.mu.Unlock()

	return result, nil
}

// startOf returns the sequence counter value recorded when the given
// repetition number entered Run.
func (r *warmUpOrderingRunner) startOf(runNumber int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.starts[runNumber]
}

// endOf returns the sequence counter value recorded when the given
// repetition number exited Run.
func (r *warmUpOrderingRunner) endOf(runNumber int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ends[runNumber]
}

// ---------------------------------------------------------------------------
// warmUpBlockingRunner — blocks the warm-up rep until ctx is cancelled
// ---------------------------------------------------------------------------

// warmUpBlockingRunner blocks Run for RunNumber == 1 until the context is
// cancelled, and completes all other repetitions immediately. It tracks the
// total number of calls made so the test can verify that fan-out repetitions
// are never dispatched when cancellation occurs during warm-up.
type warmUpBlockingRunner struct {
	mu            sync.Mutex
	callCount     int
	warmUpStarted chan struct{}
	once          sync.Once
}

func newWarmUpBlockingRunner() *warmUpBlockingRunner {
	return &warmUpBlockingRunner{
		warmUpStarted: make(chan struct{}),
	}
}

func (r *warmUpBlockingRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	r.mu.Lock()
	r.callCount++
	r.mu.Unlock()

	if key.RunNumber == 1 {
		r.once.Do(func() { close(r.warmUpStarted) })
		<-ctx.Done()
	}

	ev := passingEvidence()
	ev.Key = key
	if eval != nil {
		return eval(ev), nil
	}
	return domain.TestResult{Key: key}, nil
}

func (r *warmUpBlockingRunner) totalCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.callCount
}

// ---------------------------------------------------------------------------
// Shared helpers for this file
// ---------------------------------------------------------------------------

// uniqueRunIDFunc returns a RunID function that produces a deterministic,
// unique identity per (testID, runNumber, attempt) tuple.
func uniqueRunIDFunc() func(string, int, int) string {
	return func(testID string, runNumber, attempt int) string {
		return fmt.Sprintf("%s-r%d-a%d", testID, runNumber, attempt)
	}
}

// newConcurrentSuite builds a suite with MaxConcurrentRuns set and a
// deterministic RunID function, so scheduling tests are reproducible.
func newConcurrentSuite(runner suite.TestRunner, clock *fakeClock, progress domain.ProgressSink, maxConcurrentRuns int) *suite.Suite {
	return suite.New(suite.Options{
		Runner:            runner,
		Progress:          progress,
		Clock:             clock,
		MaxConcurrentRuns: maxConcurrentRuns,
		RunID:             uniqueRunIDFunc(),
	})
}

// countEventsOfKind returns how many events in the slice have the given kind.
func countEventsOfKind(events []domain.ProgressEvent, kind domain.ProgressKind) int {
	n := 0
	for _, ev := range events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// eventKindSeq extracts the ProgressKind from each event, in order.
func eventKindSeq(events []domain.ProgressEvent) []domain.ProgressKind {
	kinds := make([]domain.ProgressKind, len(events))
	for i, ev := range events {
		kinds[i] = ev.Kind
	}
	return kinds
}

// ---------------------------------------------------------------------------
// T14.1 — Repetitions of one test fan out concurrently after warm-up
// ---------------------------------------------------------------------------

// TestScheduler_RepetitionsOfOneTest_ExecuteConcurrentlyUpToBound verifies
// that after the warm-up repetition (rep 1) completes, the remaining
// repetitions fan out concurrently up to the configured bound.
//
// The warmUpSafeHoldingRunner passes rep 1 through immediately, then gates
// reps 2..N until releaseAt of them are simultaneously in-flight. With
// sequential execution (current implementation), only one non-warmup rep is
// ever in-flight; the gate never opens, the context times out, and the
// peak assertion fails. With the new concurrent fan-out scheduler, reps 2..N
// dispatch simultaneously and the gate opens.
func TestScheduler_RepetitionsOfOneTest_ExecuteConcurrentlyUpToBound(t *testing.T) {
	const (
		bound = 2
		reps  = 4
	)

	runner := newWarmUpSafeHoldingRunner(bound)
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(resolvedTest("test-a", reps, 1.0))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runSuite(t, s, ctx, plan) //nolint:errcheck — timeout and completion are both acceptable

	if got := runner.peakFanOut(); got < bound {
		t.Errorf(
			"peak concurrent fan-out calls = %d, want >= %d; after the warm-up "+
				"repetition completes, the scheduler must dispatch reps 2..N "+
				"concurrently up to the configured bound",
			got, bound,
		)
	}
}

// ---------------------------------------------------------------------------
// T14.2 — Tests within a suite execute one at a time
// ---------------------------------------------------------------------------

// TestScheduler_TestsWithinSuite_ExecuteOneAtATime verifies that distinct
// tests in the plan run sequentially, with no temporal overlap between
// different tests' runs. Under the one-test-at-a-time scheduling model, the
// scheduler must complete all repetitions of one test before starting any
// repetition of the next.
//
// The testSerializationDetector wraps a scriptedRunner and records whether any
// two different test names were ever simultaneously in-flight. It yields the
// goroutine after updating its tracking state so that concurrent goroutines,
// if any were dispatched, have a chance to be observed. If tests are correctly
// serialized, no overlap is ever recorded.
func TestScheduler_TestsWithinSuite_ExecuteOneAtATime(t *testing.T) {
	const numTests = 4

	inner := newScriptedRunner()
	runner := newTestSerializationDetector(inner)
	tests := make([]preflight.ResolvedTest, numTests)
	for i := range tests {
		tests[i] = resolvedTest(fmt.Sprintf("test-%d", i), 1, 1.0)
	}
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, numTests)
	plan := buildPlan(tests...)

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned error: %v", err)
	}

	if runner.overlapDetected() {
		t.Errorf(
			"two distinct tests were simultaneously in-flight; tests within a " +
				"suite must execute one at a time with no temporal overlap — the " +
				"scheduler must not dispatch a new test until the previous test's " +
				"runs (all its repetitions) have completed",
		)
	}
}

// ---------------------------------------------------------------------------
// T14.3 — Bound governs concurrent repetitions within a single test
// ---------------------------------------------------------------------------

// TestScheduler_OneBoundGovernsTestsAndRepetitions_TotalInFlightNeverExceedsBound
// verifies two complementary properties under the one-test-at-a-time model:
//
//  1. No two distinct tests are ever simultaneously in-flight (tests are
//     serialized; one bound no longer spans the full test x repetition matrix).
//  2. Within one test's fan-out, the bound is exploited: peak concurrent
//     non-warmup reps >= 2, so a trivially sequential fallback is rejected.
//
// The warmUpSafeHoldingRunner gates non-warmup reps until bound are
// simultaneously in-flight. With the current shared-FIFO scheduler, the gate
// opens (cross-test reps reach the releaseAt threshold concurrently), but the
// testSerializationDetector fires because those reps belong to distinct tests.
// With the new scheduler, each test's reps 2..N fan out in isolation and the
// gate opens within a single test's fan-out, with no cross-test overlap.
func TestScheduler_OneBoundGovernsTestsAndRepetitions_TotalInFlightNeverExceedsBound(t *testing.T) {
	const (
		bound    = 2
		numTests = 3
		reps     = 3 // rep 1 of each test is warm-up; reps 2..3 fan out at bound
	)

	inner := newWarmUpSafeHoldingRunner(bound)
	runner := newTestSerializationDetector(inner)
	tests := make([]preflight.ResolvedTest, numTests)
	for i := range tests {
		tests[i] = resolvedTest(fmt.Sprintf("test-%d", i), reps, 1.0)
	}
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(tests...)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runSuite(t, s, ctx, plan) //nolint:errcheck

	got := inner.peakFanOut()

	if got > bound {
		t.Errorf(
			"peak concurrent fan-out reps = %d, exceeds bound %d; the bound must "+
				"cap concurrent repetitions within one test — the scheduler must not "+
				"start more than bound reps simultaneously during fan-out",
			got, bound,
		)
	}
	if got < 2 {
		t.Errorf(
			"peak concurrent fan-out reps = %d, want >= 2; after the warm-up rep "+
				"completes, the scheduler must exploit the bound and dispatch reps "+
				"2..N concurrently — a fully sequential fallback is not acceptable",
			got,
		)
	}

	if runner.overlapDetected() {
		t.Errorf(
			"two distinct tests were simultaneously in-flight; under the " +
				"one-test-at-a-time model the bound governs repetitions within one " +
				"test, not across tests — tests must be fully serialized",
		)
	}
}

// ---------------------------------------------------------------------------
// T14.4 — Bound of 1 produces identical results and event ordering
// ---------------------------------------------------------------------------

// TestScheduler_BoundOfOne_ProducesIdenticalResultsAndEventOrdering verifies
// the hard D6 contract: MaxConcurrentRuns=1 must reproduce the sequential
// implementation's behaviour, results and lifecycle-event ordering exactly. A
// user who does not opt into concurrency must be unaffected after the
// scheduler is introduced.
//
// This test runs the same plan twice under bound=1 and asserts identical
// verdicts and identical event-kind sequences. Both runs must agree because
// the runner has no side effects and the clock is deterministic.
func TestScheduler_BoundOfOne_ProducesIdenticalResultsAndEventOrdering(t *testing.T) {
	plan := buildPlan(
		resolvedTest("first", 2, 1.0),
		resolvedTest("second", 2, 1.0),
	)

	// First run under bound=1.
	runner1 := newScriptedRunner()
	sink1 := &recordingSink{}
	s1 := newConcurrentSuite(runner1, newFakeClock(), sink1, 1)
	result1, err := runSuite(t, s1, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run (run 1, bound=1) returned error: %v", err)
	}
	events1 := sink1.all()

	// Second identical run under bound=1.
	runner2 := newScriptedRunner()
	sink2 := &recordingSink{}
	s2 := newConcurrentSuite(runner2, newFakeClock(), sink2, 1)
	result2, err := runSuite(t, s2, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run (run 2, bound=1) returned error: %v", err)
	}
	events2 := sink2.all()

	// Verdicts must be identical.
	for _, v := range []domain.Verdict{domain.VerdictPass, domain.VerdictFail} {
		if result1.Counts[v] != result2.Counts[v] {
			t.Errorf(
				"Counts[%v]: run1=%d, run2=%d; bound=1 must produce deterministic verdicts",
				v, result1.Counts[v], result2.Counts[v],
			)
		}
	}

	// Lifecycle event kind sequences must be identical.
	kinds1, kinds2 := eventKindSeq(events1), eventKindSeq(events2)
	if len(kinds1) != len(kinds2) {
		t.Fatalf("event stream lengths differ: run1=%d, run2=%d; bound=1 must produce deterministic event ordering", len(kinds1), len(kinds2))
	}
	for i := range kinds1 {
		if kinds1[i] != kinds2[i] {
			t.Errorf("event[%d]: run1=%v, run2=%v; event-kind sequences must be identical under bound=1", i, kinds1[i], kinds2[i])
		}
	}
}

// ---------------------------------------------------------------------------
// T14.5 — Report ordering is by test order and repetition number
// ---------------------------------------------------------------------------

// TestScheduler_ReportOrdering_ByTestAndRepetitionNumber verifies that the
// result model orders test reports in plan order and run reports in repetition
// order. A concurrent scheduler that naively appends results on completion
// would violate this when completions arrive out of order.
//
// This test is a regression guard: the sequential implementation naturally
// appends in the correct order. It pins the contract so a concurrent
// implementation is forced to reserve slots by position before scheduling
// begins rather than appending on completion.
func TestScheduler_ReportOrdering_ByTestAndRepetitionNumber(t *testing.T) {
	runner := newScriptedRunner()
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, 4)
	plan := buildPlan(
		resolvedTest("alpha", 3, 1.0),
		resolvedTest("beta", 2, 1.0),
	)

	result, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned error: %v", err)
	}

	if len(result.Tests) != 2 {
		t.Fatalf("got %d test reports, want 2", len(result.Tests))
	}

	// Test reports must follow plan order.
	if result.Tests[0].TestName != "alpha" {
		t.Errorf("Tests[0].TestID = %q, want %q; test reports must follow plan order", result.Tests[0].TestName, "alpha")
	}
	if result.Tests[1].TestName != "beta" {
		t.Errorf("Tests[1].TestID = %q, want %q; test reports must follow plan order", result.Tests[1].TestName, "beta")
	}

	// alpha: 3 runs in repetition order 1, 2, 3.
	if len(result.Tests[0].Runs) != 3 {
		t.Fatalf("alpha: got %d run reports, want 3", len(result.Tests[0].Runs))
	}
	for i, run := range result.Tests[0].Runs {
		if run.Key.RunNumber != i+1 {
			t.Errorf("alpha.Runs[%d].Key.RunNumber = %d, want %d; run reports must be in repetition order", i, run.Key.RunNumber, i+1)
		}
	}

	// beta: 2 runs in repetition order 1, 2.
	if len(result.Tests[1].Runs) != 2 {
		t.Fatalf("beta: got %d run reports, want 2", len(result.Tests[1].Runs))
	}
	for i, run := range result.Tests[1].Runs {
		if run.Key.RunNumber != i+1 {
			t.Errorf("beta.Runs[%d].Key.RunNumber = %d, want %d; run reports must be in repetition order", i, run.Key.RunNumber, i+1)
		}
	}
}

// ---------------------------------------------------------------------------
// T14.6 — A repetition's raw attempts still run strictly in order
// ---------------------------------------------------------------------------

// TestScheduler_AttemptWithinRepetition_RunsStrictlyInOrder verifies that
// when a repetition fails for state integrity and is retried, the retry
// (attempt 1) runs only after the original attempt (attempt 0) returns. The
// scheduling unit is one whole repetition: it occupies one slot for its
// entire life, so both attempts are serialised inside that slot regardless
// of the suite's bound.
//
// The atomicTrackingRunner measures the peak number of simultaneously
// in-flight Run calls. Peak must be 1 for this single-repetition test: the
// two attempts of one repetition must never overlap.
func TestScheduler_AttemptWithinRepetition_RunsStrictlyInOrder(t *testing.T) {
	var inFlight, peakInFlight int64

	base := newScriptedRunner()
	base.scriptFor("test-a",
		scriptedOutcome{evidence: stateIntegrityEvidence()}, // attempt 0 → needs retry
		scriptedOutcome{evidence: passingEvidence()},         // attempt 1 → passes
	)

	runner := &atomicTrackingRunner{
		delegate:     base,
		inFlight:     &inFlight,
		peakInFlight: &peakInFlight,
	}

	// Use a high bound (4) so the scheduler is free to run things concurrently
	// if it mistakenly starts both attempts at once.
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, 4)
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned error: %v", err)
	}

	if got := atomic.LoadInt64(&peakInFlight); got > 1 {
		t.Errorf(
			"peak concurrent attempts for one repetition = %d, want 1; "+
				"a repetition's attempts must run strictly in order inside their slot, "+
				"never concurrently even at a bound above 1",
			got,
		)
	}
}

// ---------------------------------------------------------------------------
// T14.7 — Progress events emitted for all repetitions
// ---------------------------------------------------------------------------

// TestScheduler_ConcurrentRuns_EmitAllLifecycleEvents verifies that every
// repetition in the plan produces a TestStarted and a TestFinished lifecycle
// event in the progress stream, with no event dropped, regardless of
// scheduling model.
//
// This test uses a scriptedRunner (non-blocking) so it passes with both
// sequential and concurrent fan-out implementations. It is a regression guard
// that pins the event-count contract: every rep must produce exactly one
// TestStarted and one TestFinished event, and each must carry a non-empty
// RunID for attribution.
func TestScheduler_ConcurrentRuns_EmitAllLifecycleEvents(t *testing.T) {
	const (
		numTests = 3
		reps     = 2
		bound    = 3
	)

	runner := newScriptedRunner()
	sink := &recordingSink{}
	s := newConcurrentSuite(runner, newFakeClock(), sink, bound)

	tests := make([]preflight.ResolvedTest, numTests)
	for i := range tests {
		tests[i] = resolvedTest(fmt.Sprintf("test-%d", i), reps, 1.0)
	}
	plan := buildPlan(tests...)

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned error: %v", err)
	}

	events := sink.all()
	totalReps := numTests * reps

	if got := countEventsOfKind(events, domain.ProgressTestStarted); got != totalReps {
		t.Errorf(
			"ProgressTestStarted events = %d, want %d; lifecycle events must "+
				"never be dropped regardless of scheduling model",
			got, totalReps,
		)
	}

	if got := countEventsOfKind(events, domain.ProgressTestFinished); got != totalReps {
		t.Errorf(
			"ProgressTestFinished events = %d, want %d; lifecycle events must "+
				"never be dropped regardless of scheduling model",
			got, totalReps,
		)
	}

	// Every per-run event must carry a non-empty run identity for attribution.
	for _, ev := range events {
		switch ev.Kind {
		case domain.ProgressTestStarted, domain.ProgressTestFinished:
			if ev.Run.RunID == "" {
				t.Errorf(
					"%v event has empty Run.RunID; per-run lifecycle events must carry "+
						"a non-empty run identity so they are attributable — a cross-run "+
						"attribution error is undiagnosable",
					ev.Kind,
				)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// T14.8 — Concurrent runs receive distinct run identities
// ---------------------------------------------------------------------------

// TestScheduler_ConcurrentRuns_ReceiveDistinctRunIdentities verifies that
// every run receives a unique RunID in its RunKey, so each run's sandbox,
// run-state file and invocation log are at a distinct path and no two runs
// can observe or corrupt each other's evidence.
//
// This is a regression guard: the RunIDGenerator introduced in Stage 13
// provides collision-free identities by construction. This test pins that
// property so a future change to identity derivation fails here first.
func TestScheduler_ConcurrentRuns_ReceiveDistinctRunIdentities(t *testing.T) {
	runner := newScriptedRunner()
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, 4)
	plan := buildPlan(
		resolvedTest("test-a", 3, 1.0),
		resolvedTest("test-b", 3, 1.0),
	)

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned error: %v", err)
	}

	calls := runner.allCalls()
	seen := make(map[string]domain.RunKey, len(calls))
	for _, key := range calls {
		if prev, dup := seen[key.RunID]; dup {
			t.Errorf(
				"RunID %q assigned to more than one run: first %+v, then %+v; "+
					"every run must receive a distinct identity so its sandbox, "+
					"run-state file and invocation log are at distinct paths",
				key.RunID, prev, key,
			)
		}
		seen[key.RunID] = key
	}

	// Guard: the plan should produce at least 6 distinct run identities.
	if len(seen) < 6 {
		t.Errorf("only %d distinct run identities seen, want >= 6 (3 reps × 2 tests)", len(seen))
	}
}

// ---------------------------------------------------------------------------
// Warm-up gate test — rep 1 completes before reps 2..N start
// ---------------------------------------------------------------------------

// TestScheduler_WarmUpRep_CompletesBeforeFanOutBegins verifies that for a
// test with 2+ repetitions, repetition 1 (the warm-up) starts and completes
// before any of repetitions 2..N start.
//
// The warmUpOrderingRunner assigns a monotonically increasing sequence number
// to each start and end event and blocks rep 1 until rep 2 attempts to start
// (or a 200ms timeout). This hold gives a concurrent scheduler without the
// warm-up gate a clear window to dispatch rep 2 while rep 1 is still in-flight,
// making a scheduling violation observable and producing a reliable RED failure.
// With the gate implemented, rep 2 is withheld until rep 1 finishes, the
// timeout fires, and the sequence-number assertion passes.
//
// This is a RED-phase test: it fails with the current shared-FIFO scheduler
// because rep 2 is dispatched while rep 1 is still blocked.
func TestScheduler_WarmUpRep_CompletesBeforeFanOutBegins(t *testing.T) {
	const (
		bound = 2
		reps  = 3
	)

	tracker := newWarmUpOrderingRunner()
	s := newConcurrentSuite(tracker, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(resolvedTest("test-a", reps, 1.0))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned error: %v", err)
	}

	rep1End := tracker.endOf(1)
	rep2Start := tracker.startOf(2)

	if rep1End == 0 {
		t.Fatal("repetition 1 never completed; run end was not recorded")
	}
	if rep2Start == 0 {
		t.Fatal("repetition 2 never started; run start was not recorded")
	}

	if rep1End >= rep2Start {
		t.Errorf(
			"repetition 1 ended at sequence %d but repetition 2 started at "+
				"sequence %d; rep 1 (warm-up) must complete before any of reps 2..N "+
				"start — the scheduler must wait for the warm-up to finish before "+
				"dispatching fan-out repetitions",
			rep1End, rep2Start,
		)
	}
}

// ---------------------------------------------------------------------------
// Cancellation during warm-up test — fan-out never dispatched
// ---------------------------------------------------------------------------

// TestScheduler_CancellationDuringWarmUp_FanOutNeverDispatched verifies that
// if the context is cancelled while repetition 1 (warm-up) is still in-flight,
// repetitions 2..N are never dispatched.
//
// The warmUpBlockingRunner blocks rep 1 until ctx is cancelled and completes
// all other reps immediately. The test cancels the context after rep 1 has
// started, then verifies that runner.Run was called exactly once (only rep 1).
func TestScheduler_CancellationDuringWarmUp_FanOutNeverDispatched(t *testing.T) {
	const (
		bound = 3
		reps  = 4
	)

	runner := newWarmUpBlockingRunner()
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(resolvedTest("test-a", reps, 1.0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		runSuite(t, s, ctx, plan) //nolint:errcheck
	}()

	// Wait until rep 1 (warm-up) is in-flight, then cancel. Rep 1 unblocks
	// via ctx.Done(). Reps 2..4 must not be dispatched after cancellation.
	select {
	case <-runner.warmUpStarted:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("warm-up repetition did not start within 2s")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Suite.Run did not return within 2s after cancellation")
	}

	if got := runner.totalCalls(); got > 1 {
		t.Errorf(
			"runner.Run called %d times, want 1; when the context is cancelled "+
				"during the warm-up repetition, the scheduler must not dispatch any "+
				"of the fan-out repetitions (reps 2..N)",
			got,
		)
	}
}

// ---------------------------------------------------------------------------
// Single-repetition no-op test — no warm-up gate overhead
// ---------------------------------------------------------------------------

// TestScheduler_SingleRepetition_ExecutesWithoutWarmUpGate verifies that a
// test with exactly 1 repetition executes identically to today: no spurious
// wait, no gate, no changed result. This is a regression guard that protects
// against implementations that add warm-up overhead even when there are no
// fan-out repetitions to wait for.
func TestScheduler_SingleRepetition_ExecutesWithoutWarmUpGate(t *testing.T) {
	runner := newScriptedRunner()
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, 2)
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	result, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned error: %v", err)
	}

	if len(result.Tests) != 1 {
		t.Fatalf("got %d test reports, want 1", len(result.Tests))
	}
	if len(result.Tests[0].Runs) != 1 {
		t.Fatalf("got %d run reports for test-a, want 1; a single-repetition test "+
			"must produce exactly one run report with no extra gate or delay",
			len(result.Tests[0].Runs),
		)
	}
	if result.Tests[0].Runs[0].Key.RunNumber != 1 {
		t.Errorf(
			"run report RunNumber = %d, want 1; single-repetition test must "+
				"produce a run with RunNumber 1",
			result.Tests[0].Runs[0].Key.RunNumber,
		)
	}
}
