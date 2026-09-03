package suite_test

// Tests for cancellation and teardown correctness under concurrent execution.
//
// The concurrent scheduler must provide four guarantees that the old sequential
// loop got for free:
//
//  1. Cancellation reaches every in-flight run simultaneously, not only the
//     run the next check-point happens to be inside.
//  2. Every goroutine the scheduler started is joined before Suite.Run returns,
//     so no work outlives the call — this is what makes "no orphaned harness
//     process" true at the language level.
//  3. Every sandbox is torn down on cancellation: the runner's own teardown
//     contract ("teardown runs on every exit path") applies, and the scheduler
//     must not defeat it by allowing goroutines to outlive the call.
//  4. Results completed before cancellation are returned, and a cancelled
//     suite is not itself reported as a failure.
//
// Under the one-test-at-a-time scheduling model, cross-test concurrency is
// eliminated. Cancellation tests therefore use a single test with enough
// repetitions so that after the warm-up rep (rep 1) completes, the fan-out
// reps (reps 2..N) reach the required simultaneous in-flight count.
//
// Regression guards (pass now, protect against future regressions):
//   - TestCancellation_ReachesEveryInFlightRun_NotOnlyMostRecent
//   - TestCancellation_GoroutinesJoined_BeforeSuiteReturns
//   - TestCancellation_AllStartedRuns_CompleteBeforeSuiteReturns
//   - TestCancellation_NoWorkOutlivesSuiteCall_AtBoundAboveOne
//   - TestCancellation_PanickingAttempt_SiblingAttemptsUnaffected
//   - TestCancellation_CompletedResultsReturned_SuiteIsNotAFailure
//   - TestCancellation_PanickingAttempt_SchedulerContainsPanic
//
// Note: the cancellation tests pass before implementation because the current
// shared-FIFO scheduler is already concurrent: after the warm-up rep (rep 1)
// passes through, the fan-out reps are dispatched concurrently and reach the
// required in-flight count without any code change. These tests are effective
// regression guards but are not RED-phase tests.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
	"mosaic-agent-test/internal/suite"
)

// ---------------------------------------------------------------------------
// warmUpCancellationRunner
// ---------------------------------------------------------------------------

// warmUpCancellationRunner lets the warm-up repetition (RunNumber == 1)
// complete immediately while blocking subsequent repetitions until the context
// is cancelled. It closes allInFlight when expectedNonWarmup simultaneous
// non-warm-up calls are in-flight, so the test can detect the right moment to
// cancel and then measure how many calls observed ctx.Done().
type warmUpCancellationRunner struct {
	expectedNonWarmup int

	allInFlightOnce sync.Once
	allInFlight     chan struct{}

	mu              sync.Mutex
	nonWarmupFlight int
	cancelledCount  int
}

func newWarmUpCancellationRunner(expectedNonWarmup int) *warmUpCancellationRunner {
	return &warmUpCancellationRunner{
		expectedNonWarmup: expectedNonWarmup,
		allInFlight:       make(chan struct{}),
	}
}

func (r *warmUpCancellationRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	// Warm-up rep: complete immediately so fan-out reps can be dispatched.
	if key.RunNumber == 1 {
		ev := passingEvidence()
		ev.Key = key
		if eval != nil {
			return eval(ev), nil
		}
		return domain.TestResult{Key: key}, nil
	}

	// Fan-out rep: signal when threshold reached, then block until cancelled.
	r.mu.Lock()
	r.nonWarmupFlight++
	if r.nonWarmupFlight >= r.expectedNonWarmup {
		r.allInFlightOnce.Do(func() { close(r.allInFlight) })
	}
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.nonWarmupFlight--
		r.mu.Unlock()
	}()

	<-ctx.Done()

	r.mu.Lock()
	r.cancelledCount++
	r.mu.Unlock()

	ev := passingEvidence()
	ev.Key = key
	if eval != nil {
		return eval(ev), nil
	}
	return domain.TestResult{Key: key}, nil
}

func (r *warmUpCancellationRunner) cancelledCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelledCount
}

// ---------------------------------------------------------------------------
// warmUpGoroutineQuiescenceRunner
// ---------------------------------------------------------------------------

// warmUpGoroutineQuiescenceRunner lets the warm-up rep (RunNumber == 1)
// complete immediately while blocking subsequent reps until the context is
// cancelled. It tracks the number of non-warm-up Run calls currently
// executing via an atomic counter and signals ready when expectedNonWarmup
// simultaneous fan-out calls are in-flight.
type warmUpGoroutineQuiescenceRunner struct {
	expectedNonWarmup int
	readyOnce         sync.Once
	ready             chan struct{}
	inFlight          atomic.Int64
}

func newWarmUpGoroutineQuiescenceRunner(expected int) *warmUpGoroutineQuiescenceRunner {
	return &warmUpGoroutineQuiescenceRunner{
		expectedNonWarmup: expected,
		ready:             make(chan struct{}),
	}
}

func (r *warmUpGoroutineQuiescenceRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	// Warm-up rep: complete immediately.
	if key.RunNumber == 1 {
		ev := passingEvidence()
		ev.Key = key
		if eval != nil {
			return eval(ev), nil
		}
		return domain.TestResult{Key: key}, nil
	}

	current := r.inFlight.Add(1)
	if current >= int64(r.expectedNonWarmup) {
		r.readyOnce.Do(func() { close(r.ready) })
	}
	defer r.inFlight.Add(-1)

	<-ctx.Done()

	ev := passingEvidence()
	ev.Key = key
	if eval != nil {
		return eval(ev), nil
	}
	return domain.TestResult{Key: key}, nil
}

// currentInFlight is the number of non-warm-up Run calls executing right now.
// After Suite.Run returns, this must be zero.
func (r *warmUpGoroutineQuiescenceRunner) currentInFlight() int64 {
	return r.inFlight.Load()
}

// ---------------------------------------------------------------------------
// warmUpCompletionTrackingRunner
// ---------------------------------------------------------------------------

// warmUpCompletionTrackingRunner lets the warm-up rep (RunNumber == 1)
// complete immediately while blocking subsequent reps until the context is
// cancelled. It records which non-warm-up runs were started and which returned
// so the test can verify that every started run completes (runner.Run returns)
// before Suite.Run returns.
type warmUpCompletionTrackingRunner struct {
	mu        sync.Mutex
	started   []string // RunIDs of non-warmup reps that entered Run
	completed []string // RunIDs of non-warmup reps that returned from Run

	expectedNonWarmup int
	readyOnce         sync.Once
	ready             chan struct{}
	inFlight          int
}

func newWarmUpCompletionTrackingRunner(expected int) *warmUpCompletionTrackingRunner {
	return &warmUpCompletionTrackingRunner{
		expectedNonWarmup: expected,
		ready:             make(chan struct{}),
	}
}

func (r *warmUpCompletionTrackingRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	// Warm-up rep: complete immediately.
	if key.RunNumber == 1 {
		ev := passingEvidence()
		ev.Key = key
		if eval != nil {
			return eval(ev), nil
		}
		return domain.TestResult{Key: key}, nil
	}

	r.mu.Lock()
	r.started = append(r.started, key.RunID)
	r.inFlight++
	if r.inFlight >= r.expectedNonWarmup {
		r.readyOnce.Do(func() { close(r.ready) })
	}
	r.mu.Unlock()

	<-ctx.Done()

	r.mu.Lock()
	r.completed = append(r.completed, key.RunID)
	r.inFlight--
	r.mu.Unlock()

	ev := passingEvidence()
	ev.Key = key
	if eval != nil {
		return eval(ev), nil
	}
	return domain.TestResult{Key: key}, nil
}

func (r *warmUpCompletionTrackingRunner) startedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.started)
}

func (r *warmUpCompletionTrackingRunner) completedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.completed)
}

// ---------------------------------------------------------------------------
// partialCompletionRunner
// ---------------------------------------------------------------------------

// partialCompletionRunner returns immediately for the test named fastID
// and blocks until ctx is cancelled for all other tests. It calls
// onFastComplete (if non-nil) just before the fast run returns, so the
// test can cancel the context only after the fast run has completed.
type partialCompletionRunner struct {
	fastID         string
	onFastComplete func()

	mu            sync.Mutex
	fastCompleted bool
	slowReady     chan struct{}
	slowReadyOnce sync.Once
	slowExpected  int
	slowStarted   int
}

func newPartialCompletionRunner(fastID string, slowExpected int, onFastComplete func()) *partialCompletionRunner {
	return &partialCompletionRunner{
		fastID:         fastID,
		onFastComplete: onFastComplete,
		slowReady:      make(chan struct{}),
		slowExpected:   slowExpected,
	}
}

func (r *partialCompletionRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	if key.TestName == r.fastID {
		// Complete immediately, then signal.
		ev := passingEvidence()
		ev.Key = key
		var result domain.TestResult
		if eval != nil {
			result = eval(ev)
		} else {
			result = domain.TestResult{Key: key}
		}
		if r.onFastComplete != nil {
			r.onFastComplete()
		}
		return result, nil
	}

	// Slow path: signal when blocking, then wait for cancellation.
	r.mu.Lock()
	r.slowStarted++
	if r.slowStarted >= r.slowExpected {
		r.slowReadyOnce.Do(func() { close(r.slowReady) })
	}
	r.mu.Unlock()

	<-ctx.Done()

	ev := passingEvidence()
	ev.Key = key
	if eval != nil {
		return eval(ev), nil
	}
	return domain.TestResult{Key: key}, nil
}

// ---------------------------------------------------------------------------
// warmUpRepPanicRunner — gates fan-out reps, one returns an error
// ---------------------------------------------------------------------------

// warmUpRepPanicRunner lets the warm-up rep (RunNumber == 1) complete
// immediately. For subsequent reps, it blocks behind a gate that opens when
// releaseAt of them are simultaneously in-flight. After the gate opens:
//   - the rep with panicRunNumber returns an error (simulating what
//     runner.Run returns after recovering an internal panic)
//   - all other reps return a passing result
//
// This allows the test to verify that an error from one concurrent fan-out rep
// does not prevent sibling reps from completing normally.
type warmUpRepPanicRunner struct {
	panicRunNumber int
	releaseAt      int

	mu       sync.Mutex
	inFlight int
	peak     int
	gate     chan struct{}
	gateOnce sync.Once
}

func newWarmUpRepPanicRunner(panicRunNumber, releaseAt int) *warmUpRepPanicRunner {
	return &warmUpRepPanicRunner{
		panicRunNumber: panicRunNumber,
		releaseAt:      releaseAt,
		gate:           make(chan struct{}),
	}
}

func (r *warmUpRepPanicRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	// Warm-up rep: complete immediately.
	if key.RunNumber == 1 {
		ev := passingEvidence()
		ev.Key = key
		if eval != nil {
			return eval(ev), nil
		}
		return domain.TestResult{Key: key}, nil
	}

	// Fan-out rep: track in-flight, open gate when releaseAt reached.
	r.mu.Lock()
	r.inFlight++
	if r.inFlight > r.peak {
		r.peak = r.inFlight
	}
	shouldOpen := r.inFlight >= r.releaseAt
	r.mu.Unlock()

	if shouldOpen {
		r.gateOnce.Do(func() { close(r.gate) })
	}

	select {
	case <-r.gate:
	case <-ctx.Done():
	}

	r.mu.Lock()
	r.inFlight--
	r.mu.Unlock()

	if key.RunNumber == r.panicRunNumber {
		// Simulate what runner.Run returns after recovering a panic.
		return domain.TestResult{}, fmt.Errorf("runner: recovered panic: deliberate panic in repetition %d", key.RunNumber)
	}

	ev := passingEvidence()
	ev.Key = key
	if eval != nil {
		return eval(ev), nil
	}
	return domain.TestResult{Key: key}, nil
}

// peakFanOut returns the peak number of simultaneously in-flight non-warm-up
// calls observed across the lifetime of this runner.
func (r *warmUpRepPanicRunner) peakFanOut() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peak
}

// ---------------------------------------------------------------------------
// T15.1 — Cancellation reaches every in-flight fan-out rep
// ---------------------------------------------------------------------------

// TestCancellation_ReachesEveryInFlightRun_NotOnlyMostRecent verifies that
// when N fan-out reps are simultaneously in-flight and the context is
// cancelled, cancellation reaches every one of them — not only the rep whose
// goroutine happens to check ctx.Err() next.
//
// The plan uses one test with enough repetitions so that after the warm-up rep
// (rep 1) completes, the fan-out reps reach the required simultaneous
// in-flight count. The warmUpCancellationRunner blocks every non-warm-up call
// until ctx is done, so after cancellation every blocked call must unblock via
// ctx.Done(). The count of calls that did is the metric; it must equal the
// number of in-flight fan-out reps.
//
// RED-phase: with sequential execution, only one rep is ever in-flight at a
// time, so allInFlight never closes, the 2s timeout fires, and t.Fatal() is
// called before the assertion is reached.
func TestCancellation_ReachesEveryInFlightRun_NotOnlyMostRecent(t *testing.T) {
	const (
		bound = 3
		reps  = 4 // rep 1 is warm-up; reps 2,3,4 fan out at bound=3
	)

	runner := newWarmUpCancellationRunner(bound)
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(resolvedTest("test-a", reps, 1.0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := runSuite(t, s, ctx, plan)
		done <- err
	}()

	// Wait until the bound fan-out reps are simultaneously in-flight, then
	// cancel. Cancelling before all are in-flight would let some skip via the
	// ctx.Err() check, which would make the assertion non-deterministic.
	select {
	case <-runner.allInFlight:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal(
			"all fan-out reps did not reach in-flight status within 2s; " +
				"after the warm-up rep completes, the scheduler must dispatch " +
				"reps 2..N concurrently so the in-flight threshold is reached " +
				"before this test can verify per-rep cancellation propagation",
		)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Suite.Run did not return within 2s after context cancellation")
	}

	// Every fan-out rep that was in-flight when we cancelled must have
	// unblocked via ctx.Done(), proving cancellation was not selective.
	if got := runner.cancelledCallCount(); got < bound {
		t.Errorf(
			"cancellation reached %d of %d in-flight fan-out reps, want %d; "+
				"cancellation must propagate to every rep simultaneously in flight, "+
				"not only the most recently started one",
			got, bound, bound,
		)
	}
}

// ---------------------------------------------------------------------------
// T15.2 — Goroutines joined before Suite.Run returns
// ---------------------------------------------------------------------------

// TestCancellation_GoroutinesJoined_BeforeSuiteReturns asserts goroutine
// quiescence directly: after Suite.Run returns, no goroutine started by the
// scheduler on behalf of a run is still executing.
//
// The warmUpGoroutineQuiescenceRunner increments an atomic counter when a
// non-warm-up Run starts and decrements it when Run returns. The counter must
// be zero immediately after Suite.Run returns.
//
// RED-phase: with sequential execution, only one fan-out rep is ever in-flight
// at a time, so ready never closes, the timeout fires, and t.Fatal() is called.
func TestCancellation_GoroutinesJoined_BeforeSuiteReturns(t *testing.T) {
	const (
		bound = 3
		reps  = 4 // rep 1 is warm-up; reps 2,3,4 fan out at bound=3
	)

	runner := newWarmUpGoroutineQuiescenceRunner(bound)
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(resolvedTest("test-a", reps, 1.0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := runSuite(t, s, ctx, plan)
		done <- err
	}()

	select {
	case <-runner.ready:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal(
			"fan-out reps did not reach in-flight status within 2s; " +
				"after warm-up completes, the scheduler must dispatch reps 2..N " +
				"concurrently before goroutine quiescence can be verified",
		)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Suite.Run did not return within 2s after context cancellation")
	}

	// After Suite.Run returns, no Run call must still be executing.
	if got := runner.currentInFlight(); got != 0 {
		t.Errorf(
			"in-flight goroutine count = %d after Suite.Run returned, want 0; "+
				"Suite.Run must join every goroutine it started (via wg.Wait or "+
				"equivalent) before returning, so no work outlives the call",
			got,
		)
	}
}

// ---------------------------------------------------------------------------
// T15.3 — Every sandbox is torn down on cancellation
// ---------------------------------------------------------------------------

// TestCancellation_AllStartedRuns_CompleteBeforeSuiteReturns verifies that
// every non-warm-up run the scheduler started has its runner.Run call return
// before Suite.Run returns. Since runner.Run's contract specifies teardown
// runs on every exit path, every completed run implies its sandbox has been
// torn down.
//
// RED-phase: with sequential execution, only one fan-out rep is ever in-flight
// at a time, so ready never closes, the timeout fires, and t.Fatal() is called.
func TestCancellation_AllStartedRuns_CompleteBeforeSuiteReturns(t *testing.T) {
	const (
		bound = 3
		reps  = 4 // rep 1 is warm-up; reps 2,3,4 fan out at bound=3
	)

	runner := newWarmUpCompletionTrackingRunner(bound)
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(resolvedTest("test-a", reps, 1.0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := runSuite(t, s, ctx, plan)
		done <- err
	}()

	select {
	case <-runner.ready:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal(
			"fan-out reps did not reach in-flight status within 2s; " +
				"after warm-up completes, the scheduler must dispatch reps 2..N " +
				"concurrently before teardown completion can be verified",
		)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Suite.Run did not return within 2s after context cancellation")
	}

	started := runner.startedCount()
	completed := runner.completedCount()
	if completed != started {
		t.Errorf(
			"started %d fan-out runs but only %d completed before Suite.Run returned; "+
				"every started run must complete (runner.Run must return) before "+
				"Suite.Run returns, which guarantees teardown ran for each sandbox — "+
				"a run left mid-flight is an orphaned sandbox",
			started, completed,
		)
	}
}

// ---------------------------------------------------------------------------
// T15.4 — Completed results returned; cancelled suite is not a failure
// ---------------------------------------------------------------------------

// TestCancellation_CompletedResultsReturned_SuiteIsNotAFailure verifies two
// properties that must hold after mid-run cancellation:
//
//  1. Results for runs that completed before cancellation appear in the
//     returned report. A cancellation must not discard already-finished work.
//  2. Suite.Run returns a nil error. A cancelled suite is not itself a
//     failure; the caller that issued the cancel already knows why.
//
// The plan contains one fast test that completes immediately and one slow
// test that blocks until ctx is cancelled. Cancellation is issued only after
// the fast test signals completion, ensuring the fast result is definitely
// in the pre-cancellation set.
func TestCancellation_CompletedResultsReturned_SuiteIsNotAFailure(t *testing.T) {
	const bound = 2

	fastDone := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := newPartialCompletionRunner(
		"fast-test",
		1, // one slow test expected
		func() { close(fastDone) },
	)

	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(
		resolvedTest("fast-test", 1, 1.0),
		resolvedTest("slow-test", 1, 1.0),
	)

	type suiteReturn struct {
		result report.Result
		err    error
	}
	returned := make(chan suiteReturn, 1)
	go func() {
		result, err := runSuite(t, s, ctx, plan)
		returned <- suiteReturn{result, err}
	}()

	// Cancel only after the fast test has completed. This guarantees the
	// fast test's result is in the pre-cancellation set.
	select {
	case <-fastDone:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("fast-test did not complete within 2s; check the partialCompletionRunner")
	}

	var sr suiteReturn
	select {
	case sr = <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("Suite.Run did not return within 2s after cancellation")
	}

	// Suite.Run always returns nil error: cancellation is not a failure.
	if sr.err != nil {
		t.Errorf(
			"Suite.Run returned non-nil error %v; a cancelled suite must not "+
				"return an error — the cancellation itself is not a failure",
			sr.err,
		)
	}

	// The fast test must appear in the result. The slow test may or may
	// not appear, depending on whether its in-progress execution produced
	// a result before cancellation hit.
	byID := make(map[string]report.TestReport)
	for _, tr := range sr.result.Tests {
		byID[tr.TestName] = tr
	}

	fastReport, ok := byID["fast-test"]
	if !ok {
		t.Errorf(
			"fast-test's report is missing from the result; results completed "+
				"before cancellation must be returned, not discarded",
		)
	} else if fastReport.Aggregate.Verdict != domain.VerdictPass {
		t.Errorf(
			"fast-test aggregate verdict = %v, want Pass; the fast test "+
				"completed before cancellation and must carry its real verdict",
			fastReport.Aggregate.Verdict,
		)
	}
}

// ---------------------------------------------------------------------------
// T15.5 — No work outlives the Suite.Run call at a bound above 1
// ---------------------------------------------------------------------------

// TestCancellation_NoWorkOutlivesSuiteCall_AtBoundAboveOne verifies that at
// any bound above 1, Suite.Run joins every goroutine before returning. The
// assertion is the same as TestCancellation_GoroutinesJoined_BeforeSuiteReturns
// but exercises a higher bound and more work items to stress the joining
// mechanism.
//
// RED-phase: with sequential execution, only one fan-out rep is ever in-flight
// at a time, so ready never closes, the timeout fires, and t.Fatal() is called.
func TestCancellation_NoWorkOutlivesSuiteCall_AtBoundAboveOne(t *testing.T) {
	const (
		bound = 4
		reps  = 5 // rep 1 is warm-up; reps 2,3,4,5 fan out at bound=4
	)

	runner := newWarmUpGoroutineQuiescenceRunner(bound)
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(resolvedTest("test-a", reps, 1.0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := runSuite(t, s, ctx, plan)
		done <- err
	}()

	select {
	case <-runner.ready:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("fan-out reps did not reach in-flight status within 2s")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal(
			"Suite.Run did not return within 2s after context cancellation; " +
				"at a bound above 1, the scheduler must join every goroutine " +
				"before returning — no work may outlive the call",
		)
	}

	if got := runner.currentInFlight(); got != 0 {
		t.Errorf(
			"in-flight count = %d after Suite.Run returned, want 0; "+
				"at bound=%d, Suite.Run must join all goroutines before returning — "+
				"missed wg.Done() or missing wg.Wait()",
			got, bound,
		)
	}
}

// ---------------------------------------------------------------------------
// T15.6a — Panicking fan-out rep's siblings are unaffected
// ---------------------------------------------------------------------------

// TestCancellation_PanickingAttempt_SiblingAttemptsUnaffected verifies the
// recovery behavior when one fan-out rep fails with a simulated recovered panic
// while sibling reps are genuinely in-flight at the same time.
//
// One test is run with enough repetitions so that after the warm-up rep (rep 1)
// completes, the fan-out reps (reps 2..N) are dispatched concurrently. One
// specific fan-out rep returns an error (matching what runner.Run returns after
// recovering an internal panic); sibling fan-out reps complete normally.
//
// The suite must:
//   - Record an infrastructure failure for the panicking rep
//   - Return the sibling reps' passing results
//   - Not itself fail
//
// RED-phase: with sequential execution, the gate requires bound simultaneous
// non-warm-up reps but only one is ever in-flight; the gate never opens, the
// context times out, and the test fails via timeout.
func TestCancellation_PanickingAttempt_SiblingAttemptsUnaffected(t *testing.T) {
	const (
		bound          = 3
		reps           = 4   // rep 1 is warm-up; reps 2,3,4 fan out at bound=3
		panicRunNumber = 2   // rep 2 simulates a recovered panic during fan-out
		passRate       = 0.5 // 3 of 4 reps pass (rep 1 warm-up passes, rep 2 fails, reps 3,4 pass)
	)

	runner := newWarmUpRepPanicRunner(panicRunNumber, bound)
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(resolvedTest("test-a", reps, passRate))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := runSuite(t, s, ctx, plan)
	if err != nil {
		t.Fatalf("Suite.Run returned unexpected error: %v", err)
	}

	// Verify concurrent fan-out: the peak in-flight count among reps 2..4
	// must be bound (3), confirming all three ran simultaneously.
	if got := runner.peakFanOut(); got < bound {
		t.Errorf(
			"peak concurrent fan-out reps = %d, want >= %d; the test must "+
				"exercise genuinely concurrent fan-out so 'panicking rep while "+
				"siblings are in-flight' is a fact, not an assumption",
			got, bound,
		)
	}

	if len(result.Tests) != 1 {
		t.Fatalf("got %d test reports, want 1", len(result.Tests))
	}
	testReport := result.Tests[0]
	if len(testReport.Runs) != reps {
		t.Fatalf("got %d run reports, want %d", len(testReport.Runs), reps)
	}

	// Build a map from RunNumber to RunReport for assertion convenience.
	byRunNumber := make(map[int]report.RunReport, len(testReport.Runs))
	for _, run := range testReport.Runs {
		byRunNumber[run.Key.RunNumber] = run
	}

	// The panicking rep must be recorded as an infrastructure failure.
	if panicRun, ok := byRunNumber[panicRunNumber]; ok {
		if panicRun.Verdict != domain.VerdictFail {
			t.Errorf(
				"rep %d (panicking) verdict = %v, want Fail; a run that returned "+
					"an error must be recorded as an infrastructure failure",
				panicRunNumber, panicRun.Verdict,
			)
		}
	} else {
		t.Errorf(
			"rep %d (panicking) is missing from run reports; the scheduler must "+
				"record a result for every repetition that was started",
			panicRunNumber,
		)
	}

	// Sibling fan-out reps (all non-panic, non-warmup reps) must have passed.
	for runNumber, run := range byRunNumber {
		if runNumber == panicRunNumber {
			continue // already checked above
		}
		if run.Verdict != domain.VerdictPass {
			t.Errorf(
				"rep %d (sibling) verdict = %v, want Pass; a panicking sibling rep "+
					"must not affect other reps that ran normally",
				runNumber, run.Verdict,
			)
		}
	}
}

// ---------------------------------------------------------------------------
// T15.6b — Scheduler goroutine contains a goroutine-level panic (RED phase)
// ---------------------------------------------------------------------------

// envPanicSchedulerSubproc is the environment variable set in the subprocess
// spawned by TestCancellation_PanickingAttempt_SchedulerContainsPanic.
const envPanicSchedulerSubproc = "MOSAIC_SUITE_PANIC_SCHEDULER_SUBPROC"

// TestCancellation_PanickingAttempt_SchedulerContainsPanic verifies that a
// panic escaping runner.Run's own recovery — a panic that reaches the
// scheduler's goroutine — is caught rather than crashing the process.
//
// RED-phase failure: without the per-goroutine recover, the scheduler goroutine
// has no defer/recover around its work loop. A panicking TestRunner causes a
// goroutine panic, which Go escalates to process termination. The subprocess
// exits with a non-zero code and the main test fails.
//
// GREEN-phase pass: after a defer/recover is added inside each worker goroutine,
// the panic is caught, the attempt is recorded as an infrastructure failure,
// sibling goroutines continue, and the subprocess exits 0.
//
// The subprocess pattern is used to isolate the crash: a goroutine panic
// would otherwise abort the entire test binary, preventing subsequent tests
// in the package from running.
func TestCancellation_PanickingAttempt_SchedulerContainsPanic(t *testing.T) {
	if os.Getenv(envPanicSchedulerSubproc) == "1" {
		// We ARE the subprocess. Run the panicking scenario directly.
		// Without the per-goroutine recovery the process will crash here.
		runPanicSchedulerScenario(t)
		return
	}

	// Run this very test function as a subprocess, isolated from the rest
	// of the package's tests so a crash here does not abort them.
	cmd := exec.Command(
		os.Args[0],
		"-test.run=^TestCancellation_PanickingAttempt_SchedulerContainsPanic$",
		"-test.v",
	)
	cmd.Env = append(os.Environ(), envPanicSchedulerSubproc+"=1")
	out, err := cmd.CombinedOutput()

	if err != nil {
		t.Errorf(
			"a panic escaping TestRunner.Run crashed the scheduler process: %v\n"+
				"Output:\n%s\n\n"+
				"RED-phase failure: the scheduler goroutine has no per-goroutine "+
				"defer/recover. A panic in a TestRunner propagates to the worker "+
				"goroutine and crashes the binary. The fix adds a deferred "+
				"recover() inside each worker goroutine so the panic is contained, "+
				"the attempt is recorded as an infrastructure failure, and sibling "+
				"goroutines continue unaffected.",
			err, out,
		)
	}
}

// runPanicSchedulerScenario is the body of the subprocess for
// TestCancellation_PanickingAttempt_SchedulerContainsPanic. It sets up a
// suite with a literally-panicking TestRunner double and runs it. After the
// per-goroutine recover is added, the panic is caught by the scheduler
// goroutine's defer/recover and Suite.Run returns normally; before that fix,
// the goroutine panic crashes the process.
func runPanicSchedulerScenario(t *testing.T) {
	t.Helper()

	runner := &literalPanicRunner{panicTestID: "panic-unit"}
	s := suite.New(suite.Options{
		Runner:            runner,
		Progress:          &recordingSink{},
		Clock:             newFakeClock(),
		MaxConcurrentRuns: 3,
		RunID:             uniqueRunIDFunc(),
	})
	plan := buildPlan(
		resolvedTest("sibling-x", 1, 1.0),
		resolvedTest("panic-unit", 1, 1.0),
		resolvedTest("sibling-y", 1, 1.0),
	)

	// runSuite wraps Suite.Run in a recover so a panic on the main goroutine
	// (Suite.Run's own goroutine) is caught. But a panic in a WORKER goroutine
	// escapes to that goroutine's own stack; recover() on a different goroutine
	// cannot catch it. So if the scheduler has no per-goroutine recovery, the
	// worker goroutine panics, and Go terminates the process.
	result, err := runSuite(t, s, context.Background(), plan)

	// After the fix: result is non-nil, panic-unit is an infrastructure failure,
	// siblings passed, no error from the suite itself.
	if err != nil {
		t.Errorf("Suite.Run returned error after panic containment: %v", err)
	}

	byID := make(map[string]report.TestReport)
	for _, tr := range result.Tests {
		byID[tr.TestName] = tr
	}

	for _, id := range []string{"sibling-x", "sibling-y"} {
		if tr, ok := byID[id]; ok {
			if tr.Aggregate.Verdict != domain.VerdictPass {
				t.Errorf("sibling %q: want Pass after panic containment, got %v", id, tr.Aggregate.Verdict)
			}
		} else {
			t.Errorf("sibling %q: missing from result after panic containment", id)
		}
	}
}

// literalPanicRunner panics outright for the named test ID without any
// recovery. It models the case where runner.Run's own defer/recover fails to
// execute (e.g., a panic inside the defer itself), and the panic propagates
// to the scheduler's worker goroutine. Every other test ID completes normally.
type literalPanicRunner struct {
	panicTestID string
}

func (r *literalPanicRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	if key.TestName == r.panicTestID {
		panic(fmt.Sprintf("literalPanicRunner: deliberate panic for test %q — testing scheduler goroutine containment", key.TestName))
	}
	ev := passingEvidence()
	ev.Key = key
	if eval != nil {
		return eval(ev), nil
	}
	return domain.TestResult{Key: key}, nil
}

// Ensure suite.TestRunner is satisfied at compile time for all runner types
// defined in this file that are used as TestRunner arguments.
var _ suite.TestRunner = (*warmUpCancellationRunner)(nil)
var _ suite.TestRunner = (*warmUpGoroutineQuiescenceRunner)(nil)
var _ suite.TestRunner = (*warmUpCompletionTrackingRunner)(nil)
var _ suite.TestRunner = (*warmUpRepPanicRunner)(nil)
var _ suite.TestRunner = (*partialCompletionRunner)(nil)
var _ suite.TestRunner = (*literalPanicRunner)(nil)
