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
// Regression guards (pass with the current implementation; protect against
// future regressions):
//   - TestCancellation_ReachesEveryInFlightRun_NotOnlyMostRecent
//   - TestCancellation_GoroutinesJoined_BeforeSuiteReturns
//   - TestCancellation_AllStartedRuns_CompleteBeforeSuiteReturns
//   - TestCancellation_CompletedResultsReturned_SuiteIsNotAFailure
//   - TestCancellation_NoWorkOutlivesSuiteCall_AtBoundAboveOne
//   - TestCancellation_PanickingAttempt_SiblingAttemptsUnaffected
//
// RED-phase test (fails before I15.3, passes after):
//   - TestCancellation_PanickingAttempt_SchedulerContainsPanic

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
// cancellationPropagationRunner
// ---------------------------------------------------------------------------

// cancellationPropagationRunner blocks every Run call behind a "hold" gate
// that never opens from the runner side: calls unblock only when ctx is
// cancelled. A separate "allInFlight" channel closes when the expected number
// of calls are simultaneously in-flight, so the test can detect the right
// moment to cancel and then measure how many calls observed ctx.Done().
type cancellationPropagationRunner struct {
	expected int

	allInFlightOnce sync.Once
	allInFlight     chan struct{} // closed when expected calls are in-flight

	mu             sync.Mutex
	inFlight       int
	cancelledCount int // calls that unblocked via ctx.Done
}

func newCancellationPropagationRunner(expected int) *cancellationPropagationRunner {
	return &cancellationPropagationRunner{
		expected:    expected,
		allInFlight: make(chan struct{}),
	}
}

func (r *cancellationPropagationRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	r.mu.Lock()
	r.inFlight++
	if r.inFlight >= r.expected {
		r.allInFlightOnce.Do(func() { close(r.allInFlight) })
	}
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.inFlight--
		r.mu.Unlock()
	}()

	// Block until ctx is cancelled. There is no gate that the test
	// closes: the only way this run unblocks is via ctx.Done().
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

func (r *cancellationPropagationRunner) cancelledCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelledCount
}

// ---------------------------------------------------------------------------
// goroutineQuiescenceRunner
// ---------------------------------------------------------------------------

// goroutineQuiescenceRunner tracks the number of Run calls currently
// executing via an atomic counter. It blocks every call until ctx is
// cancelled, so the test can assert the counter is exactly zero after
// Suite.Run returns — proving every goroutine was joined.
type goroutineQuiescenceRunner struct {
	inFlight  atomic.Int64
	readyOnce sync.Once
	ready     chan struct{} // closed when expectedInFlight calls are executing
	expected  int
}

func newGoroutineQuiescenceRunner(expected int) *goroutineQuiescenceRunner {
	return &goroutineQuiescenceRunner{
		expected: expected,
		ready:    make(chan struct{}),
	}
}

func (r *goroutineQuiescenceRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	current := r.inFlight.Add(1)
	if current >= int64(r.expected) {
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

// currentInFlight is the number of Run calls executing right now.
// After Suite.Run returns, this must be zero.
func (r *goroutineQuiescenceRunner) currentInFlight() int64 {
	return r.inFlight.Load()
}

// ---------------------------------------------------------------------------
// completionTrackingRunner
// ---------------------------------------------------------------------------

// completionTrackingRunner records which runs were started and which
// returned ("completed their teardown"). Every started run must appear in
// the completed set after Suite.Run returns, because runner.Run runs
// teardown before returning and wg.Wait() joins every goroutine.
type completionTrackingRunner struct {
	mu        sync.Mutex
	started   []string // RunIDs entered
	completed []string // RunIDs returned

	// ready is closed when all expected runs are simultaneously blocking,
	// so the test can cancel at the right moment.
	expected  int
	readyOnce sync.Once
	ready     chan struct{}
	inFlight  int
}

func newCompletionTrackingRunner(expected int) *completionTrackingRunner {
	return &completionTrackingRunner{
		expected: expected,
		ready:    make(chan struct{}),
	}
}

func (r *completionTrackingRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	r.mu.Lock()
	r.started = append(r.started, key.RunID)
	r.inFlight++
	if r.inFlight >= r.expected {
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

func (r *completionTrackingRunner) startedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.started)
}

func (r *completionTrackingRunner) completedCount() int {
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
		fastID:        fastID,
		onFastComplete: onFastComplete,
		slowReady:     make(chan struct{}),
		slowExpected:  slowExpected,
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
// gatedSiblingRunner (panic simulation via error return, with gate)
// ---------------------------------------------------------------------------

// gatedSiblingRunner blocks all Run calls behind a gate that opens when
// releaseAt calls are simultaneously in-flight. After the gate opens:
//   - the test named panicTestID returns an error (simulating what
//     runner.Run returns after recovering an internal panic)
//   - all other tests return a passing result
//
// The gate ensures the test exercises genuinely concurrent execution rather
// than sequential scheduling: "while other attempts are in flight" is a
// fact, not an assumption, because all calls blocked together.
type gatedSiblingRunner struct {
	panicTestID string
	releaseAt   int

	mu          sync.Mutex
	inFlight    int
	peak        int
	gate        chan struct{}
	gateOnce    sync.Once
}

func newGatedSiblingRunner(panicTestID string, releaseAt int) *gatedSiblingRunner {
	return &gatedSiblingRunner{
		panicTestID: panicTestID,
		releaseAt:   releaseAt,
		gate:        make(chan struct{}),
	}
}

func (r *gatedSiblingRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
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

	// Block until the gate opens (all releaseAt calls are in-flight) or
	// the context is cancelled, whichever comes first.
	select {
	case <-r.gate:
	case <-ctx.Done():
	}

	r.mu.Lock()
	r.inFlight--
	r.mu.Unlock()

	if key.TestName == r.panicTestID {
		// Simulate what runner.Run returns after recovering a panic:
		// an error with no eval call. The suite must record an
		// infrastructure failure for this run.
		return domain.TestResult{}, fmt.Errorf("runner: recovered panic: deliberate panic in test double")
	}

	ev := passingEvidence()
	ev.Key = key
	if eval != nil {
		return eval(ev), nil
	}
	return domain.TestResult{Key: key}, nil
}

func (r *gatedSiblingRunner) peakConcurrent() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peak
}

// ---------------------------------------------------------------------------
// T15.1 — Cancellation reaches every in-flight run
// ---------------------------------------------------------------------------

// TestCancellation_ReachesEveryInFlightRun_NotOnlyMostRecent verifies that
// when N runs are simultaneously in-flight and the context is cancelled,
// cancellation reaches every one of them — not only the run whose goroutine
// happens to check ctx.Err() next.
//
// The cancellationPropagationRunner blocks every call until ctx is done and
// has no other release mechanism, so after cancellation every blocked call
// must unblock via ctx.Done(). The count of calls that did is the metric;
// it must equal the number of in-flight runs.
func TestCancellation_ReachesEveryInFlightRun_NotOnlyMostRecent(t *testing.T) {
	const (
		bound    = 3
		numTests = 3
	)

	runner := newCancellationPropagationRunner(numTests)
	tests := make([]preflight.ResolvedTest, numTests)
	for i := range tests {
		tests[i] = resolvedTest(fmt.Sprintf("test-%d", i), 1, 1.0)
	}
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(tests...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := runSuite(t, s, ctx, plan)
		done <- err
	}()

	// Wait until all numTests runs are simultaneously in-flight, then cancel.
	// Cancelling before all are in-flight would let some skip via the ctx.Err()
	// check in the worker loop, which would reduce the cancellation count and
	// make the assertion non-deterministic.
	select {
	case <-runner.allInFlight:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal(
			"all in-flight signal did not fire within 2s; the scheduler must " +
				"have all bound runs executing simultaneously before this test can " +
				"verify per-run cancellation propagation",
		)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Suite.Run did not return within 2s after context cancellation")
	}

	// Every run that was in-flight when we cancelled must have unblocked via
	// ctx.Done(), proving cancellation was not selective.
	if got := runner.cancelledCallCount(); got < numTests {
		t.Errorf(
			"cancellation reached %d of %d in-flight runs, want %d; "+
				"cancellation must propagate to every run simultaneously in flight, "+
				"not only the most recently started one",
			got, numTests, numTests,
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
// The goroutineQuiescenceRunner increments an atomic counter when Run starts
// and decrements it when Run returns. The counter must be zero immediately
// after Suite.Run returns, because wg.Wait() provides the happens-before edge
// between every goroutine's final write and the suite's return.
//
// This is the formal assertion that backs the "no orphaned harness process"
// guarantee: a process running inside runner.Run cannot outlive that call,
// and runner.Run cannot outlive Suite.Run.
func TestCancellation_GoroutinesJoined_BeforeSuiteReturns(t *testing.T) {
	const (
		bound    = 3
		numTests = 3
	)

	runner := newGoroutineQuiescenceRunner(numTests)
	tests := make([]preflight.ResolvedTest, numTests)
	for i := range tests {
		tests[i] = resolvedTest(fmt.Sprintf("test-%d", i), 1, 1.0)
	}
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(tests...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := runSuite(t, s, ctx, plan)
		done <- err
	}()

	// Wait until all runs are in-flight, then cancel.
	select {
	case <-runner.ready:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal(
			"not all runs reached in-flight status within 2s; " +
				"increase the timeout or check the scheduler dispatches bound goroutines",
		)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Suite.Run did not return within 2s after context cancellation")
	}

	// After Suite.Run returns, no Run call must still be executing.
	// wg.Wait() is the mechanism that makes this true: if it is absent
	// or the goroutines are not wg.Add(1)ed correctly, the counter
	// will be non-zero here.
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
// every run the scheduler started has its runner.Run call return before
// Suite.Run returns. Since runner.Run's contract specifies teardown runs on
// every exit path — including a cancelled context — every completed run
// implies its sandbox has been torn down.
//
// The completionTrackingRunner counts started and completed calls. After
// Suite.Run returns, started == completed must hold: no run was started but
// left mid-flight, which would mean its sandbox teardown had not run.
func TestCancellation_AllStartedRuns_CompleteBeforeSuiteReturns(t *testing.T) {
	const (
		bound    = 3
		numTests = 3
	)

	runner := newCompletionTrackingRunner(numTests)
	tests := make([]preflight.ResolvedTest, numTests)
	for i := range tests {
		tests[i] = resolvedTest(fmt.Sprintf("test-%d", i), 1, 1.0)
	}
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(tests...)

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
			"not all expected runs were in-flight within 2s; " +
				"scheduler must dispatch bound goroutines before this test is valid",
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
			"started %d runs but only %d completed before Suite.Run returned; "+
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
// assertion is the same as T15.2 but exercises a higher bound and more work
// items to stress the joining mechanism.
//
// A failing wg.Wait() (missing or incorrect Add/Done pairing) would leave
// goroutines running after Suite.Run returns. The in-flight counter would
// then be non-zero, revealing the flaw.
func TestCancellation_NoWorkOutlivesSuiteCall_AtBoundAboveOne(t *testing.T) {
	const (
		bound    = 4
		numTests = 6
		reps     = 2
	)

	runner := newGoroutineQuiescenceRunner(bound) // gate opens when bound are in-flight
	tests := make([]preflight.ResolvedTest, numTests)
	for i := range tests {
		tests[i] = resolvedTest(fmt.Sprintf("test-%d", i), reps, 1.0)
	}
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(tests...)

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
		t.Fatal("bound goroutines did not reach in-flight status within 2s")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal(
			"Suite.Run did not return within 2s after context cancellation; "+
				"at a bound above 1, the scheduler must join every goroutine "+
				"before returning — no work may outlive the call",
		)
	}

	if got := runner.currentInFlight(); got != 0 {
		t.Errorf(
			"in-flight count = %d after Suite.Run returned, want 0; "+
				"at bound=%d, Suite.Run must join all %d goroutines "+
				"before returning — missed wg.Done() or missing wg.Wait()",
			got, bound, bound,
		)
	}
}

// ---------------------------------------------------------------------------
// T15.6a — Panicking attempt's siblings are unaffected
// ---------------------------------------------------------------------------

// TestCancellation_PanickingAttempt_SiblingAttemptsUnaffected verifies the
// recovery behavior when one run fails with a simulated recovered panic while
// the sibling runs are genuinely in-flight at the same time.
//
// The gatedSiblingRunner blocks all three runs behind a gate that opens when
// all three are simultaneously in-flight. After the gate opens, the
// "panic-sim-test" returns an error (matching what runner.Run returns after
// recovering an internal panic — error, no eval call); the sibling tests
// complete normally. The suite must:
//   - Record an infrastructure failure for the panicking test
//   - Return the sibling runs' passing results
//   - Not itself fail
//
// This is a regression guard: it passes with the current implementation and
// pins the invariant that the scheduler's error handling does not abort sibling
// runs when one runner.Run call returns an error during concurrent execution.
func TestCancellation_PanickingAttempt_SiblingAttemptsUnaffected(t *testing.T) {
	const bound = 3

	runner := newGatedSiblingRunner("panic-sim-test", bound)
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(
		resolvedTest("sibling-a", 1, 1.0),
		resolvedTest("panic-sim-test", 1, 1.0),
		resolvedTest("sibling-b", 1, 1.0),
	)

	// Run with a timeout; the gate opens when all 3 are in-flight and the
	// suite returns immediately thereafter.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := runSuite(t, s, ctx, plan)
	if err != nil {
		t.Fatalf("Suite.Run returned unexpected error: %v", err)
	}

	// Verify concurrent execution: the peak in-flight count must be 3,
	// confirming all three runs were executing simultaneously when the error
	// occurred — this is what "while other attempts are in flight" means.
	if got := runner.peakConcurrent(); got < bound {
		t.Errorf(
			"peak concurrent calls = %d, want >= %d; the gated runner must "+
				"have all %d runs simultaneously in-flight so the test genuinely "+
				"exercises 'panicking attempt while siblings are in flight'",
			got, bound, bound,
		)
	}

	// All three tests must appear in the result.
	byID := make(map[string]report.TestReport)
	for _, tr := range result.Tests {
		byID[tr.TestName] = tr
	}

	for _, id := range []string{"sibling-a", "sibling-b", "panic-sim-test"} {
		if _, ok := byID[id]; !ok {
			t.Errorf(
				"test %q is missing from the result; a panicking sibling must "+
					"not suppress the results of tests that completed normally",
				id,
			)
		}
	}

	// The panicking test must carry an infrastructure failure result.
	if panicReport, ok := byID["panic-sim-test"]; ok {
		if panicReport.Aggregate.Verdict != domain.VerdictFail {
			t.Errorf(
				"panic-sim-test verdict = %v, want Fail; a run that returned an "+
					"error must be recorded as an infrastructure failure",
				panicReport.Aggregate.Verdict,
			)
		}
	}

	// Sibling tests must have completed normally (PASS).
	for _, id := range []string{"sibling-a", "sibling-b"} {
		if tr, ok := byID[id]; ok {
			if tr.Aggregate.Verdict != domain.VerdictPass {
				t.Errorf(
					"sibling test %q verdict = %v, want Pass; a panicking sibling "+
						"must not affect tests that ran normally",
					id, tr.Aggregate.Verdict,
				)
			}
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
// RED-phase failure: without I15.3, the scheduler goroutine has no
// defer/recover around its work loop. A panicking TestRunner causes a
// goroutine panic, which Go escalates to process termination. The subprocess
// exits with a non-zero code and the main test fails.
//
// GREEN-phase pass: after I15.3 adds a defer/recover inside each worker
// goroutine, the panic is caught, the attempt is recorded as an
// infrastructure failure, sibling goroutines continue, and the subprocess
// exits 0.
//
// The subprocess pattern is used to isolate the crash: a goroutine panic
// would otherwise abort the entire test binary, preventing subsequent tests
// in the package from running.
func TestCancellation_PanickingAttempt_SchedulerContainsPanic(t *testing.T) {
	if os.Getenv(envPanicSchedulerSubproc) == "1" {
		// We ARE the subprocess. Run the panicking scenario directly.
		// Without I15.3 the process will crash here (goroutine panic →
		// runtime.throw → process exit non-zero).
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
				"goroutine and crashes the binary. The fix (I15.3) adds a deferred "+
				"recover() inside each worker goroutine so the panic is contained, "+
				"the attempt is recorded as an infrastructure failure, and sibling "+
				"goroutines continue unaffected.",
			err, out,
		)
	}
}

// runPanicSchedulerScenario is the body of the subprocess for
// TestCancellation_PanickingAttempt_SchedulerContainsPanic. It sets up a
// suite with a literally-panicking TestRunner double and runs it. After I15.3,
// the panic is caught by the scheduler goroutine's defer/recover and Suite.Run
// returns normally; before I15.3, the goroutine panic crashes the process.
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

	// After I15.3: result is non-nil, panic-unit is an infrastructure failure,
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
