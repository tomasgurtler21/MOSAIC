package suite_test

// Tests for PauseControl unit behavior and suite-level pause/resume scheduling.
//
// PauseControl unit tests verify the concurrency-safe toggle:
//   - initial state is unpaused
//   - Pause/Resume transition IsPaused correctly
//   - Pause and Resume are idempotent (no deadlock on repeated calls)
//   - WaitIfPaused blocks when paused and returns nil on resume
//   - WaitIfPaused returns ctx.Err() when context is cancelled while paused
//
// Suite-level tests verify the worker loop's interaction with PauseControl:
//   - paused workers block before invoking runner.Run (they do not drain items)
//   - resuming allows all remaining work items to execute
//   - pause does not cancel the context: in-flight runs complete normally
//   - a run started before a Pause call completes and produces a result
//   - toggling pause/resume multiple times does not lose work items or deadlock

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
	"mosaic-agent-test/internal/suite"
)

// ---------------------------------------------------------------------------
// PauseControl unit tests
// ---------------------------------------------------------------------------

// TestPauseControl_StartsUnpaused verifies that a newly constructed
// PauseControl reports IsPaused() == false before any call to Pause.
func TestPauseControl_StartsUnpaused(t *testing.T) {
	pc := suite.NewPauseControl()
	if pc.IsPaused() {
		t.Error(
			"NewPauseControl: IsPaused() == true immediately after construction; " +
				"want false — the control must start in the unpaused state",
		)
	}
}

// TestPauseControl_PauseSetsState verifies that calling Pause transitions the
// control to the paused state and IsPaused reports it correctly.
func TestPauseControl_PauseSetsState(t *testing.T) {
	pc := suite.NewPauseControl()
	pc.Pause()
	if !pc.IsPaused() {
		t.Error(
			"IsPaused() == false after Pause(); want true — " +
				"Pause must set the paused state so callers can observe it",
		)
	}
}

// TestPauseControl_ResumeClearsState verifies that calling Resume after Pause
// transitions the control back to the unpaused state.
func TestPauseControl_ResumeClearsState(t *testing.T) {
	pc := suite.NewPauseControl()
	pc.Pause()
	pc.Resume()
	if pc.IsPaused() {
		t.Error(
			"IsPaused() == true after Pause() then Resume(); want false — " +
				"Resume must clear the paused state",
		)
	}
}

// TestPauseControl_WaitIfPaused_ReturnsNilImmediatelyWhenNotPaused verifies
// that WaitIfPaused returns nil without blocking when the control is not
// paused. Workers call WaitIfPaused on every iteration; it must be cheap when
// unpaused.
func TestPauseControl_WaitIfPaused_ReturnsNilImmediatelyWhenNotPaused(t *testing.T) {
	pc := suite.NewPauseControl()

	done := make(chan error, 1)
	go func() {
		done <- pc.WaitIfPaused(context.Background())
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf(
				"WaitIfPaused (not paused) returned %v; want nil — "+
					"when the control is not paused WaitIfPaused must return nil immediately",
				err,
			)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error(
			"WaitIfPaused blocked for 500ms when control is not paused; " +
				"it must return immediately when not paused",
		)
	}
}

// TestPauseControl_WaitIfPaused_BlocksUntilResumed verifies that WaitIfPaused
// blocks when the control is paused and returns nil once Resume is called.
// This is the core property that lets workers hold position without consuming
// CPU while the suite is paused.
func TestPauseControl_WaitIfPaused_BlocksUntilResumed(t *testing.T) {
	pc := suite.NewPauseControl()
	pc.Pause()

	done := make(chan error, 1)
	go func() {
		done <- pc.WaitIfPaused(context.Background())
	}()

	// Confirm it is still blocking after 50ms. If it returned already, the
	// implementation is not blocking when paused.
	select {
	case <-done:
		t.Fatal(
			"WaitIfPaused returned while control is paused; " +
				"it must block until Resume is called",
		)
	case <-time.After(50 * time.Millisecond):
		// still blocking as expected
	}

	pc.Resume()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf(
				"WaitIfPaused returned %v after Resume; want nil — "+
					"a normal resume (no context cancellation) must return nil",
				err,
			)
		}
	case <-time.After(2 * time.Second):
		t.Error("WaitIfPaused did not return within 2s after Resume")
	}
}

// TestPauseControl_WaitIfPaused_ReturnsContextErrWhenCancelledWhilePaused
// verifies that WaitIfPaused returns ctx.Err() when the context is cancelled
// while the control is paused. This lets the worker loop fall through to the
// existing cancel-drain path so the suite can shut down cleanly even when
// paused.
func TestPauseControl_WaitIfPaused_ReturnsContextErrWhenCancelledWhilePaused(t *testing.T) {
	pc := suite.NewPauseControl()
	pc.Pause()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- pc.WaitIfPaused(ctx)
	}()

	// Confirm it is blocking before we cancel.
	select {
	case <-done:
		t.Fatal(
			"WaitIfPaused returned while control is paused and context is alive; " +
				"it must block",
		)
	case <-time.After(50 * time.Millisecond):
		// still blocking as expected
	}

	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf(
				"WaitIfPaused returned %v after context cancellation; want context.Canceled — "+
					"cancelling the context while paused must unblock WaitIfPaused and "+
					"return ctx.Err() so the worker can fall through to the drain path",
				err,
			)
		}
	case <-time.After(2 * time.Second):
		t.Error("WaitIfPaused did not return within 2s after context cancellation")
	}
}

// TestPauseControl_Pause_IsIdempotent verifies that calling Pause more than
// once does not deadlock, panic, or produce an inconsistent state.
func TestPauseControl_Pause_IsIdempotent(t *testing.T) {
	pc := suite.NewPauseControl()
	pc.Pause()
	pc.Pause() // second call must not deadlock or panic
	if !pc.IsPaused() {
		t.Error(
			"IsPaused() == false after two consecutive Pause() calls; want true — " +
				"Pause must be idempotent",
		)
	}
}

// TestPauseControl_Resume_IsIdempotent verifies that calling Resume on an
// unpaused control does not panic or produce an inconsistent state.
func TestPauseControl_Resume_IsIdempotent(t *testing.T) {
	pc := suite.NewPauseControl()
	pc.Resume() // resume when not paused must not panic or deadlock
	pc.Resume()
	if pc.IsPaused() {
		t.Error(
			"IsPaused() == true after Resume() called on an already-unpaused control; " +
				"want false — Resume must be idempotent",
		)
	}
}

// ---------------------------------------------------------------------------
// Suite-level pause/resume runners
// ---------------------------------------------------------------------------

// immediateCountingRunner completes every Run call immediately with a passing
// result. It records the total number of Run invocations so tests can assert
// that workers were (or were not) dispatching work while the suite was paused.
type immediateCountingRunner struct {
	mu    sync.Mutex
	count int
}

func (r *immediateCountingRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	r.mu.Lock()
	r.count++
	r.mu.Unlock()

	ev := passingEvidence()
	ev.Key = key
	if eval != nil {
		return eval(ev), nil
	}
	return domain.TestResult{Key: key}, nil
}

func (r *immediateCountingRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

// gatedInFlightRunner is a suite.TestRunner double that:
//   - closes inFlight when the first Run call starts (so the test can observe
//     that at least one run is in progress before pausing)
//   - blocks every Run call until the gate channel is closed or ctx is done
//   - records ctx.Err() at the moment each Run call returns, so tests can
//     verify the run's context was not cancelled by pause
type gatedInFlightRunner struct {
	gate         chan struct{} // close to unblock all blocked Run calls
	inFlight     chan struct{} // closed when the first Run call enters

	mu               sync.Mutex
	inFlightOnce     sync.Once
	contextErrOnExit []error // ctx.Err() recorded when each Run returns
}

func newGatedInFlightRunner() *gatedInFlightRunner {
	return &gatedInFlightRunner{
		gate:     make(chan struct{}),
		inFlight: make(chan struct{}),
	}
}

func (r *gatedInFlightRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	r.inFlightOnce.Do(func() { close(r.inFlight) })

	select {
	case <-r.gate:
	case <-ctx.Done():
	}

	r.mu.Lock()
	r.contextErrOnExit = append(r.contextErrOnExit, ctx.Err())
	r.mu.Unlock()

	ev := passingEvidence()
	ev.Key = key
	if eval != nil {
		return eval(ev), nil
	}
	return domain.TestResult{Key: key}, nil
}

func (r *gatedInFlightRunner) contextErrorsAtExit() []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]error(nil), r.contextErrOnExit...)
}

// ---------------------------------------------------------------------------
// Suite-level pause/resume helper
// ---------------------------------------------------------------------------

// newPauseSuite builds a suite that uses the given PauseControl and runner,
// with MaxConcurrentRuns set to bound and a deterministic RunID function.
func newPauseSuite(runner suite.TestRunner, pc *suite.PauseControl, bound int) *suite.Suite {
	return suite.New(suite.Options{
		Runner:            runner,
		Progress:          &recordingSink{},
		Clock:             newFakeClock(),
		MaxConcurrentRuns: bound,
		RunID:             uniqueRunIDFunc(),
		Pause:             pc,
	})
}

// ---------------------------------------------------------------------------
// Suite-level pause/resume tests
// ---------------------------------------------------------------------------

// TestPause_WorkersBlockBeforeStartingRuns verifies that when the PauseControl
// is paused before the suite starts, workers block on WaitIfPaused and do not
// invoke runner.Run for any work item.
//
// This is the primary RED-phase suite test: without I3.2 (worker loop pause
// check), workers ignore the PauseControl and runner.Run is called immediately,
// so the 200ms quiescence assertion fails.
//
// The test also verifies the "block, not drain" contract: after Resume, all
// work items must be executed. If workers drained (discarded) items instead of
// blocking, the final call count would be less than numTests.
func TestPause_WorkersBlockBeforeStartingRuns(t *testing.T) {
	const (
		numTests = 3
		bound    = 1 // single worker: holds first item, two remain in channel
	)

	runner := &immediateCountingRunner{}
	pc := suite.NewPauseControl()
	pc.Pause() // pause before the suite starts

	s := newPauseSuite(runner, pc, bound)

	tests := make([]preflight.ResolvedTest, numTests)
	for i := range tests {
		tests[i] = resolvedTest(fmt.Sprintf("pause-block-test-%d", i), 1, 1.0)
	}
	plan := buildPlan(tests...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := runSuite(t, s, ctx, plan)
		done <- err
	}()

	// After 200ms the worker must be blocking in WaitIfPaused and must not
	// have called runner.Run at all. With the stub (Pause is a no-op and
	// WaitIfPaused returns immediately), the runner is called immediately and
	// this assertion fails, confirming the RED phase.
	time.Sleep(200 * time.Millisecond)
	if got := runner.callCount(); got != 0 {
		t.Errorf(
			"runner.Run called %d time(s) while suite was paused; want 0 — "+
				"paused workers must block on WaitIfPaused before invoking runner.Run",
			got,
		)
	}

	pc.Resume()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Suite.Run returned error after Resume: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal(
			"Suite.Run did not complete within 5s after Resume; " +
				"workers must unblock and process all remaining items",
		)
	}

	// All tests must have executed after Resume. A drain-on-pause implementation
	// would consume items without executing them, leaving count < numTests.
	if got := runner.callCount(); got != numTests {
		t.Errorf(
			"runner.Run called %d time(s) after Resume; want %d — "+
				"workers must block (not drain) so every item executes after resume",
			got, numTests,
		)
	}
}

// TestPause_ResumeAllowsRemainingRunsToExecute verifies that a pause followed
// by a resume produces a complete result containing every scheduled test.
// Guards against drain-on-pause implementations that silently discard items.
func TestPause_ResumeAllowsRemainingRunsToExecute(t *testing.T) {
	const (
		numTests = 5
		bound    = 2
	)

	runner := &immediateCountingRunner{}
	pc := suite.NewPauseControl()
	pc.Pause()

	s := newPauseSuite(runner, pc, bound)

	tests := make([]preflight.ResolvedTest, numTests)
	for i := range tests {
		tests[i] = resolvedTest(fmt.Sprintf("resume-test-%d", i), 1, 1.0)
	}
	plan := buildPlan(tests...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type suiteReturn struct {
		result report.Result
		err    error
	}
	returned := make(chan suiteReturn, 1)
	go func() {
		r, err := runSuite(t, s, ctx, plan)
		returned <- suiteReturn{r, err}
	}()

	time.Sleep(100 * time.Millisecond)
	pc.Resume()

	var sr suiteReturn
	select {
	case sr = <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Suite.Run did not complete within 5s after Resume")
	}
	if sr.err != nil {
		t.Fatalf("Suite.Run error: %v", sr.err)
	}

	// Every scheduled test must appear in the result.
	if got := len(sr.result.Tests); got != numTests {
		t.Errorf(
			"result contains %d test report(s); want %d — "+
				"all tests must execute and produce results after resume",
			got, numTests,
		)
	}
}

// TestPause_DoesNotCancelContextOfInFlightRuns verifies that calling Pause on
// a running suite does not cancel the context passed to runner.Run calls that
// are already in progress. Pause and context cancellation are independent
// mechanisms: Pause signals workers to stop scheduling; cancellation kills
// in-flight processes.
//
// The gatedInFlightRunner records ctx.Err() at return time. After the in-flight
// run completes and the suite exits, every ctx.Err() recorded must be nil,
// proving the context was not cancelled by the pause.
func TestPause_DoesNotCancelContextOfInFlightRuns(t *testing.T) {
	runner := newGatedInFlightRunner()
	pc := suite.NewPauseControl()

	s := newPauseSuite(runner, pc, 1)
	plan := buildPlan(resolvedTest("ctx-independence-test", 1, 1.0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := runSuite(t, s, ctx, plan)
		done <- err
	}()

	// Wait for the run to enter runner.Run.
	select {
	case <-runner.inFlight:
	case <-time.After(2 * time.Second):
		t.Fatal("runner.Run did not start within 2s")
	}

	// Pause while the run is in-flight. The run must continue unaffected.
	pc.Pause()

	// 50ms window: if Pause incorrectly cancelled the context, ctx.Done() would
	// fire for the in-flight run inside this window.
	time.Sleep(50 * time.Millisecond)

	// Release the gated run so it can complete.
	close(runner.gate)

	// Resume the suite so it can drain and exit.
	pc.Resume()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Suite.Run error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Suite.Run did not complete within 5s")
	}

	for i, ctxErr := range runner.contextErrorsAtExit() {
		if ctxErr != nil {
			t.Errorf(
				"run %d: ctx.Err() == %v when runner.Run returned while suite was paused; "+
					"want nil — Pause must not cancel the context of in-flight runs",
				i, ctxErr,
			)
		}
	}
}

// TestPause_InFlightRunsCompleteWhileSuiteIsPaused verifies that a run which
// started before Pause is called can finish and appear in the returned result.
// Pause blocks new scheduling but must not prevent already-executing runs from
// completing.
func TestPause_InFlightRunsCompleteWhileSuiteIsPaused(t *testing.T) {
	runner := newGatedInFlightRunner()
	pc := suite.NewPauseControl()

	// Two tests at bound 2: both may start before the pause.
	// The key assertion is that the suite returns a complete result after
	// resume regardless of how many runs were in-flight at pause time.
	s := newPauseSuite(runner, pc, 2)
	plan := buildPlan(
		resolvedTest("in-flight-a", 1, 1.0),
		resolvedTest("in-flight-b", 1, 1.0),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type suiteReturn struct {
		result report.Result
		err    error
	}
	returned := make(chan suiteReturn, 1)
	go func() {
		r, err := runSuite(t, s, ctx, plan)
		returned <- suiteReturn{r, err}
	}()

	// Wait for at least one run to be in-flight before pausing.
	select {
	case <-runner.inFlight:
	case <-time.After(2 * time.Second):
		t.Fatal("no run started within 2s")
	}

	// Pause while runs are in-flight; then release and resume.
	pc.Pause()
	close(runner.gate) // let in-flight runs complete
	pc.Resume()        // allow any remaining work items to be scheduled

	var sr suiteReturn
	select {
	case sr = <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Suite.Run did not complete within 5s after resume")
	}
	if sr.err != nil {
		t.Fatalf("Suite.Run error: %v", sr.err)
	}

	if got := len(sr.result.Tests); got != 2 {
		t.Errorf(
			"result contains %d test report(s); want 2 — "+
				"in-flight runs must complete and all tests must be represented in the result",
			got,
		)
	}
}

// TestPause_MultipleTogglesPauseResume_AllWorkCompletes verifies that toggling
// pause and resume repeatedly does not lose work items, corrupt scheduler
// state, or deadlock. Every test in the plan must produce a result after all
// toggles complete.
func TestPause_MultipleTogglesPauseResume_AllWorkCompletes(t *testing.T) {
	const numTests = 6

	runner := &immediateCountingRunner{}
	pc := suite.NewPauseControl()

	s := newPauseSuite(runner, pc, 2)

	tests := make([]preflight.ResolvedTest, numTests)
	for i := range tests {
		tests[i] = resolvedTest(fmt.Sprintf("toggle-test-%d", i), 1, 1.0)
	}
	plan := buildPlan(tests...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type suiteReturn struct {
		result report.Result
		err    error
	}
	returned := make(chan suiteReturn, 1)
	go func() {
		r, err := runSuite(t, s, ctx, plan)
		returned <- suiteReturn{r, err}
	}()

	// Toggle pause/resume four times at short intervals. Each pause must block
	// workers without losing items; each resume must allow progress. The suite
	// must complete with all tests present.
	for i := 0; i < 4; i++ {
		pc.Pause()
		time.Sleep(10 * time.Millisecond)
		pc.Resume()
		time.Sleep(10 * time.Millisecond)
	}

	var sr suiteReturn
	select {
	case sr = <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal(
			"Suite.Run did not complete within 10s after multiple pause/resume toggles; " +
				"suspect deadlock or lost work items",
		)
	}
	if sr.err != nil {
		t.Fatalf("Suite.Run error after multiple pause/resume toggles: %v", sr.err)
	}

	if got := len(sr.result.Tests); got != numTests {
		t.Errorf(
			"result contains %d test report(s); want %d — "+
				"all work items must survive multiple pause/resume cycles",
			got, numTests,
		)
	}
}
