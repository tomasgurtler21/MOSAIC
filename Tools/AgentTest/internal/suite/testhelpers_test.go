package suite_test

// Shared test doubles for the suite package's tests.

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

// runSuite executes s.Run and converts a panic into a plain error, so a
// suite that is not yet implemented fails the calling test cleanly with a
// clear message instead of crashing the whole test binary — the same
// "compiles, then fails" RED phase every other test in this module gets.
func runSuite(t *testing.T, s *suite.Suite, ctx context.Context, p preflight.Plan) (result report.Result, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("suite.Run panicked: %v", r)
		}
	}()
	return s.Run(ctx, p)
}

// fakeClock is a manually advanced domain.Clock, so time-dependent
// assertions never depend on real sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

var _ domain.Clock = (*fakeClock)(nil)

// scriptedOutcome is one scripted response a scriptedRunner returns for one
// call belonging to a given test.
type scriptedOutcome struct {
	evidence domain.RunEvidence
	err      error
}

// repKey uniquely identifies one repetition of one test within a runner.
type repKey struct {
	testID    string
	runNumber int
}

// scriptedRunner is a suite.TestRunner double whose responses are scripted
// per test ID, consumed in order. To prevent concurrent goroutines from
// interleaving their script consumption — which causes spurious failures when
// MaxConcurrentRuns > 1 and retries are in play — each (testID, runNumber)
// pair pre-allocates its scripts atomically on first encounter. The window is
// bounded by (1 + suite.StateIntegrityRetries), the maximum number of
// runner.Run calls one repetition can ever make. Scripts beyond that window
// that are never consumed by retrying repetitions are simply unused.
//
// A test exhausted of scripted outcomes falls back to a single passing
// disposition, so a scheduling test that only cares about call count and
// order need not script every call.
type scriptedRunner struct {
	mu        sync.Mutex
	calls     []domain.RunKey
	script    map[string][]scriptedOutcome // global queue by testID
	repQueues map[repKey][]scriptedOutcome // per-(testID, runNumber) remaining scripts
	repSeen   map[repKey]bool              // tracks which (testID, runNumber) have been allocated
}

func newScriptedRunner() *scriptedRunner {
	return &scriptedRunner{
		script:    map[string][]scriptedOutcome{},
		repQueues: map[repKey][]scriptedOutcome{},
		repSeen:   map[repKey]bool{},
	}
}

// scriptFor queues outcomes to be returned, in order, for calls naming
// testID.
func (r *scriptedRunner) scriptFor(testID string, outcomes ...scriptedOutcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.script[testID] = outcomes
}

func (r *scriptedRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	r.mu.Lock()
	r.calls = append(r.calls, key)

	rk := repKey{testID: t.Definition.ID, runNumber: key.RunNumber}

	var next scriptedOutcome
	haveScripted := false

	if !r.repSeen[rk] {
		// First call for this (testID, runNumber): pre-allocate a bounded
		// window of scripts from the global queue. The window is bounded by
		// (1 + suite.StateIntegrityRetries) — the maximum number of
		// runner.Run calls one repetition can ever make. All allocation
		// happens inside this lock, so concurrent goroutines for different
		// repetitions cannot interleave their script consumption: each rep
		// gets its own contiguous slice of the global queue.
		r.repSeen[rk] = true
		maxPerRep := 1 + suite.StateIntegrityRetries

		globalQ := r.script[t.Definition.ID]
		var alloc []scriptedOutcome
		if len(globalQ) >= maxPerRep {
			alloc = globalQ[:maxPerRep]
			r.script[t.Definition.ID] = globalQ[maxPerRep:]
		} else {
			alloc = globalQ
			r.script[t.Definition.ID] = nil
		}

		if len(alloc) > 0 {
			next = alloc[0]
			haveScripted = true
			r.repQueues[rk] = alloc[1:]
		}
		// If alloc is empty, haveScripted stays false → fallback below.
	} else {
		// Subsequent call for this rep: use its pre-allocated queue.
		repQ := r.repQueues[rk]
		if len(repQ) > 0 {
			next = repQ[0]
			haveScripted = true
			r.repQueues[rk] = repQ[1:]
		}
	}

	r.mu.Unlock()

	if next.err != nil {
		// A scripted error means the attempt never started: eval is not called,
		// matching the TestRunner contract that says eval is not called when an
		// error is returned.
		return domain.TestResult{}, next.err
	}

	var ev domain.RunEvidence
	if haveScripted {
		ev = next.evidence
	} else {
		ev = domain.RunEvidence{
			SubjectResult: domain.SubjectResult{Disposition: domain.DispositionCompleted},
		}
	}
	ev.Key = key

	if eval != nil {
		return eval(ev), nil
	}
	// nil eval: the contract allows this from test doubles that do not exercise
	// evaluation. Return a zero-valued TestResult with the key stamped.
	return domain.TestResult{Key: key}, nil
}

// allCalls returns every RunKey Run was invoked with, in call order.
func (r *scriptedRunner) allCalls() []domain.RunKey {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.RunKey(nil), r.calls...)
}

// callsFor returns the RunKeys Run was invoked with for one test ID, in
// call order.
func (r *scriptedRunner) callsFor(testID string) []domain.RunKey {
	var out []domain.RunKey
	for _, k := range r.allCalls() {
		if k.TestID == testID {
			out = append(out, k)
		}
	}
	return out
}

// recordingSink is a domain.ProgressSink double that captures every emitted
// event, so a test can assert on the progress stream without a real
// frontend.
type recordingSink struct {
	mu     sync.Mutex
	events []domain.ProgressEvent
}

var _ domain.ProgressSink = (*recordingSink)(nil)

func (s *recordingSink) Emit(ev domain.ProgressEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *recordingSink) all() []domain.ProgressEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.ProgressEvent(nil), s.events...)
}

// blockingSink never returns from Emit until release is closed, so a test
// can prove a slow or blocking sink does not stall the suite.
type blockingSink struct {
	release chan struct{}

	mu    sync.Mutex
	count int
}

var _ domain.ProgressSink = (*blockingSink)(nil)

func newBlockingSink() *blockingSink {
	return &blockingSink{release: make(chan struct{})}
}

func (s *blockingSink) Emit(ev domain.ProgressEvent) {
	s.mu.Lock()
	s.count++
	s.mu.Unlock()
	<-s.release
}

func (s *blockingSink) emitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// failingSink panics on every Emit, so a test can prove a failing sink does
// not fail the run.
type failingSink struct{}

var _ domain.ProgressSink = failingSink{}

func (failingSink) Emit(ev domain.ProgressEvent) {
	panic("failingSink: deliberate failure")
}

// resolvedTest builds a minimal preflight.ResolvedTest for scheduling
// tests: an ID, a repetition count and a required pass rate. Layer is
// subagent throughout, since layer only affects protocol-violation
// evaluation, which these tests do not exercise.
func resolvedTest(id string, repetitions int, passRate float64) preflight.ResolvedTest {
	reps := repetitions
	rate := passRate
	return preflight.ResolvedTest{
		Definition: domain.TestDefinition{ID: id, Layer: domain.LayerSubagent},
		Settings:   domain.RunSettings{Repetitions: &reps, PassRate: &rate},
	}
}

// buildPlan wraps resolved tests into a preflight.Plan in the given order.
func buildPlan(tests ...preflight.ResolvedTest) preflight.Plan {
	return preflight.Plan{
		Suite: domain.TestSuite{ID: "suite-under-test"},
		Tests: tests,
	}
}

// passingEvidence is evidence that evaluate.Evaluate turns into VerdictPass
// with no reasons.
func passingEvidence() domain.RunEvidence {
	return domain.RunEvidence{
		SubjectResult: domain.SubjectResult{Disposition: domain.DispositionCompleted},
	}
}

// failingEvidence is evidence that evaluate.Evaluate turns into VerdictFail
// via a declared final-status assertion that does not hold.
func failingEvidence() domain.RunEvidence {
	want := "REJECTED"
	return domain.RunEvidence{
		Definition: domain.TestDefinition{
			Assertions: domain.Assertions{FinalStatus: &want},
		},
		Orchestration: domain.OrchestrationState{Present: true, LastStatus: "COMPLETED"},
		SubjectResult: domain.SubjectResult{Disposition: domain.DispositionCompleted},
	}
}

// stateIntegrityEvidence is evidence that evaluate.Evaluate turns into
// VerdictFail with ReasonStateIntegrity, via a recorded lock-reclaim event.
func stateIntegrityEvidence() domain.RunEvidence {
	return domain.RunEvidence{
		Records: []domain.LogRecord{
			{Kind: domain.RecordRun, Event: domain.RunEventLockReclaimed},
		},
		SubjectResult: domain.SubjectResult{Disposition: domain.DispositionCompleted},
	}
}
