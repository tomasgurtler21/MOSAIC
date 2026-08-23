package suite_test

// Tests for the bounded concurrent scheduler.
//
// The scheduling unit is one repetition of one test; it holds exactly one
// slot of the bound for its whole life and runs its attempts strictly in order
// inside that slot.
//
// Tests that are RED-phase (fail now, pass after the scheduler is implemented):
//   T14.1, T14.2, T14.3, T14.7 — require genuine concurrent execution that
//   the current sequential implementation cannot produce. They use a
//   holdingRunner whose release gate never opens when only one call is ever
//   in-flight, so the test context times out and the peak assertion fails.
//
// Tests that are regression guards (pass now, protect against scheduler bugs):
//   T14.4, T14.5, T14.6, T14.8 — properties already satisfied by sequential
//   execution that a concurrent scheduler must preserve.

import (
	"context"
	"fmt"
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
// T14.1 — Repetitions of one test execute concurrently up to the bound
// ---------------------------------------------------------------------------

// TestScheduler_RepetitionsOfOneTest_ExecuteConcurrentlyUpToBound verifies
// that when a test declares several repetitions, the scheduler sends at least
// bound calls to the runner simultaneously. The holdingRunner's gate only
// opens when releaseAt calls are in-flight at the same time; with sequential
// execution only one is ever in-flight, the gate never opens, the context
// times out, and the peak assertion fails.
func TestScheduler_RepetitionsOfOneTest_ExecuteConcurrentlyUpToBound(t *testing.T) {
	const (
		bound = 2
		reps  = 4
	)

	runner := newHoldingRunner(bound)
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(resolvedTest("test-a", reps, 1.0))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runSuite(t, s, ctx, plan) //nolint:errcheck — timeout and completion are both acceptable

	if got := runner.peakConcurrent(); got < bound {
		t.Errorf(
			"peak concurrent calls = %d, want >= %d; the scheduler must execute "+
				"repetitions of one test concurrently up to the configured bound",
			got, bound,
		)
	}
}

// ---------------------------------------------------------------------------
// T14.2 — Tests within a suite execute concurrently under the same bound
// ---------------------------------------------------------------------------

// TestScheduler_TestsWithinSuite_ExecuteConcurrentlyUnderBound verifies that
// distinct tests in the plan run concurrently with each other, not one after
// another, when the bound allows it.
func TestScheduler_TestsWithinSuite_ExecuteConcurrentlyUnderBound(t *testing.T) {
	const (
		bound    = 2
		numTests = 4
	)

	runner := newHoldingRunner(bound)
	tests := make([]preflight.ResolvedTest, numTests)
	for i := range tests {
		tests[i] = resolvedTest(fmt.Sprintf("test-%d", i), 1, 1.0)
	}
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(tests...)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runSuite(t, s, ctx, plan)

	if got := runner.peakConcurrent(); got < bound {
		t.Errorf(
			"peak concurrent calls across distinct tests = %d, want >= %d; "+
				"the scheduler must execute distinct tests concurrently",
			got, bound,
		)
	}
}

// ---------------------------------------------------------------------------
// T14.3 — One bound governs both nesting levels together
// ---------------------------------------------------------------------------

// TestScheduler_OneBoundGovernsTestsAndRepetitions_TotalInFlightNeverExceedsBound
// verifies two complementary properties:
//
//  1. The total in-flight count across ALL tests and ALL repetitions never
//     exceeds the configured bound — one bound over the entire
//     (test × repetition) matrix, not one bound per level.
//  2. The scheduler actually reaches peak ≥ 2 so a trivially sequential
//     fallback cannot satisfy the max-constraint alone.
//
// With sequential execution: the gate never opens, the context times out, and
// peak == 1. Assertion 2 fails.
func TestScheduler_OneBoundGovernsTestsAndRepetitions_TotalInFlightNeverExceedsBound(t *testing.T) {
	const (
		bound    = 2
		numTests = 3
		reps     = 3 // 9 total scheduling units
	)

	runner := newHoldingRunner(bound)
	tests := make([]preflight.ResolvedTest, numTests)
	for i := range tests {
		tests[i] = resolvedTest(fmt.Sprintf("test-%d", i), reps, 1.0)
	}
	s := newConcurrentSuite(runner, newFakeClock(), &recordingSink{}, bound)
	plan := buildPlan(tests...)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runSuite(t, s, ctx, plan)

	got := runner.peakConcurrent()

	if got > bound {
		t.Errorf(
			"peak concurrent runs = %d, exceeds bound %d; one bound must govern "+
				"both test and repetition concurrency — not two independent bounds",
			got, bound,
		)
	}
	if got < 2 {
		t.Errorf(
			"peak concurrent runs = %d, want >= 2; the scheduler must exploit the "+
				"bound and run multiple units concurrently",
			got,
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
	if result.Tests[0].TestID != "alpha" {
		t.Errorf("Tests[0].TestID = %q, want %q; test reports must follow plan order", result.Tests[0].TestID, "alpha")
	}
	if result.Tests[1].TestID != "beta" {
		t.Errorf("Tests[1].TestID = %q, want %q; test reports must follow plan order", result.Tests[1].TestID, "beta")
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
// T14.7 — Progress events emitted from concurrent evaluator closures
// ---------------------------------------------------------------------------

// TestScheduler_ConcurrentRuns_EmitAllLifecycleEvents verifies that every
// repetition in the plan produces a TestStarted and a TestFinished lifecycle
// event in the progress stream, with no event dropped, even when evaluator
// closures execute concurrently on many goroutines.
//
// The holdingRunner gates at bound: when bound calls are simultaneously
// in-flight the gate opens and all proceed. With sequential execution the gate
// never opens, the context times out, only a subset of events is emitted, and
// the expected-count assertion fails.
//
// It also verifies per-run attribution: each TestStarted and TestFinished
// event must carry a non-empty Run.RunID.
func TestScheduler_ConcurrentRuns_EmitAllLifecycleEvents(t *testing.T) {
	const (
		numTests = 3
		reps     = 2
		bound    = 3
	)

	runner := newHoldingRunner(bound)
	sink := &recordingSink{}
	s := newConcurrentSuite(runner, newFakeClock(), sink, bound)

	tests := make([]preflight.ResolvedTest, numTests)
	for i := range tests {
		tests[i] = resolvedTest(fmt.Sprintf("test-%d", i), reps, 1.0)
	}
	plan := buildPlan(tests...)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runSuite(t, s, ctx, plan)

	events := sink.all()
	totalReps := numTests * reps

	if got := countEventsOfKind(events, domain.ProgressTestStarted); got != totalReps {
		t.Errorf(
			"ProgressTestStarted events = %d, want %d; lifecycle events must never "+
				"be dropped even when evaluator closures execute concurrently",
			got, totalReps,
		)
	}

	if got := countEventsOfKind(events, domain.ProgressTestFinished); got != totalReps {
		t.Errorf(
			"ProgressTestFinished events = %d, want %d; lifecycle events must never "+
				"be dropped even when evaluator closures execute concurrently",
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
						"a non-empty run identity so they are attributable under concurrent "+
						"execution — a cross-run attribution error is undiagnosable",
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
