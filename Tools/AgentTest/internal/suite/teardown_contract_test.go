package suite_test

// Tests over the revised TestRunner port contract: the suite supplies an
// evaluator closure on every call to Runner.Run, the evaluator is invoked
// exactly once per attempt that produced evidence, and it is not invoked when
// the runner returns an error (no evidence exists to evaluate). These
// properties hold across the pass path, the assertion-failure path, the
// state-integrity retry path and the runner-error path.
//
// Coverage note — eval CALL COUNT vs eval ORDERING:
// These tests verify that the suite supplies an evaluator and the runner calls
// it the correct number of times. They do NOT verify that eval is called
// BEFORE teardown, because the scriptedRunner double does not perform teardown
// and therefore has no observable ordering between eval and teardown. That
// ordering contract ("eval is invoked before teardown, so the retention
// decision can see the verdict") is verified indirectly at the runner level by
// TestRetention_OnFailure_AssertionFailure_SandboxIsKept in retention_pin_test.go:
// if teardown executed before eval, the retention decision would use Failed=false
// (no launch error, no verdict yet) and would remove the sandbox; the test
// passing implies eval ran first and that its VerdictFail was visible to the
// teardown decision. No direct call-order assertion on the recorder exists; the
// indirect evidence-from-retention-outcome test is the current coverage for
// the eval→teardown sequence.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/suite"
)

// evalCountingRunner is a TestRunner double that records how many times the
// eval closure it received was actually called. The runner must call eval on
// every path where evidence was gathered, and must not call it when returning
// an error. evalCountingRunner wraps the scriptedRunner to count invocations.
type evalCountingRunner struct {
	// evalCount is incremented each time eval is invoked inside Run, so a test
	// can assert that the suite's evaluator closure was actually called by the
	// runner and not silently dropped.
	evalCount atomic.Int64

	inner *scriptedRunner
}

func newEvalCountingRunner() *evalCountingRunner {
	return &evalCountingRunner{inner: newScriptedRunner()}
}

var _ suite.TestRunner = (*evalCountingRunner)(nil)

func (r *evalCountingRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	// Wrap eval to count invocations before delegating to the scripted inner runner.
	var wrappedEval domain.AttemptEvaluator
	if eval != nil {
		wrappedEval = func(ev domain.RunEvidence) domain.TestResult {
			r.evalCount.Add(1)
			return eval(ev)
		}
	}
	return r.inner.Run(ctx, key, t, wrappedEval)
}

func newSuiteWithEvalCounting(r *evalCountingRunner, clock *fakeClock) *suite.Suite {
	return suite.New(suite.Options{
		Runner:   r,
		Progress: &recordingSink{},
		Clock:    clock,
		RunID:    func(testID string, runNumber, attempt int) string { return "run-" + testID },
	})
}

// TestRunnerContract_EvalCalledOncePerSuccessfulAttempt verifies that the
// suite's evaluator closure is passed to Runner.Run and called exactly once
// per attempt that produces evidence on the pass path.
func TestRunnerContract_EvalCalledOncePerSuccessfulAttempt(t *testing.T) {
	r := newEvalCountingRunner()
	s := newSuiteWithEvalCounting(r, newFakeClock())
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	if got := r.evalCount.Load(); got != 1 {
		t.Errorf("eval was called %d times for one attempt, want exactly 1 — the evaluator must be invoked once per attempt by the runner", got)
	}
}

// TestRunnerContract_EvalCalledOncePerRepetition verifies the evaluator is
// called exactly once per declared repetition across the full suite run.
func TestRunnerContract_EvalCalledOncePerRepetition(t *testing.T) {
	const repetitions = 3
	r := newEvalCountingRunner()
	s := newSuiteWithEvalCounting(r, newFakeClock())
	plan := buildPlan(resolvedTest("test-a", repetitions, 0.5))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	if got := r.evalCount.Load(); got != int64(repetitions) {
		t.Errorf("eval was called %d times for %d repetitions, want exactly %d — one eval per repetition, no more", got, repetitions, repetitions)
	}
}

// TestRunnerContract_EvalNotCalledOnRunnerError verifies the evaluator is
// NOT called when Runner.Run returns an error: no evidence was gathered, so
// no evaluation should occur. The suite synthesises the infrastructure-fail
// result from the error alone.
func TestRunnerContract_EvalNotCalledOnRunnerError(t *testing.T) {
	r := newEvalCountingRunner()
	r.inner.scriptFor("test-a",
		scriptedOutcome{err: errors.New("setup failed: sandbox root not writable")},
	)
	s := newSuiteWithEvalCounting(r, newFakeClock())
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	if got := r.evalCount.Load(); got != 0 {
		t.Errorf("eval was called %d times on a runner-error path, want 0 — the runner must not invoke eval when no evidence was gathered", got)
	}
}

// TestRunnerContract_EvalCalledForEachAttemptOnStateIntegrityRetry verifies
// that during a state-integrity retry, eval is called for both the original
// attempt and the retry — one eval per attempt, no leaks, no
// double-evaluation of a single attempt's evidence.
func TestRunnerContract_EvalCalledForEachAttemptOnStateIntegrityRetry(t *testing.T) {
	r := newEvalCountingRunner()
	r.inner.scriptFor("test-a",
		scriptedOutcome{evidence: stateIntegrityEvidence()},
		scriptedOutcome{evidence: passingEvidence()},
	)
	s := newSuiteWithEvalCounting(r, newFakeClock())
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	// The original attempt (state-integrity fault) and the retry (passing) each
	// produce evidence, so eval must be called once for each: 2 total.
	if got := r.evalCount.Load(); got != 2 {
		t.Errorf("eval was called %d times across a state-integrity attempt and its retry, want exactly 2 — one per attempt that produced evidence, no more", got)
	}
}

// TestRunnerContract_EvalCalledOnAssertionFailurePath verifies the evaluator
// is called even when the resulting verdict is FAIL due to an assertion
// failure, not only on passing runs.
func TestRunnerContract_EvalCalledOnAssertionFailurePath(t *testing.T) {
	r := newEvalCountingRunner()
	r.inner.scriptFor("test-a",
		scriptedOutcome{evidence: failingEvidence()},
	)
	s := newSuiteWithEvalCounting(r, newFakeClock())
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	if got := r.evalCount.Load(); got != 1 {
		t.Errorf("eval was called %d times on an assertion-failure path, want exactly 1 — eval must fire on every path where evidence was gathered, including those that produce VerdictFail", got)
	}
}

// TestRunnerContract_EvalCalledOncePerAttemptAcrossMultipleTests verifies the
// per-test evaluator count is correct when the plan contains multiple tests,
// so no test steals or shares another test's evaluator invocations.
func TestRunnerContract_EvalCalledOncePerAttemptAcrossMultipleTests(t *testing.T) {
	r := newEvalCountingRunner()
	s := newSuiteWithEvalCounting(r, newFakeClock())
	plan := buildPlan(
		resolvedTest("test-a", 2, 1.0),
		resolvedTest("test-b", 3, 1.0),
	)

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	// 2 repetitions for test-a and 3 for test-b → 5 eval calls total.
	if got := r.evalCount.Load(); got != 5 {
		t.Errorf("eval was called %d times for 2+3 repetitions across two tests, want exactly 5", got)
	}
}

// TestRunnerContract_NoSandboxLeakOnRepeatedStateIntegrityFault verifies
// that when a repetition ends as an infrastructure failure due to a second
// consecutive state-integrity fault, both attempts still received their eval
// call — no attempt leaked without evaluation (no orphaned sandboxes from
// the suite's perspective).
func TestRunnerContract_NoSandboxLeakOnRepeatedStateIntegrityFault(t *testing.T) {
	r := newEvalCountingRunner()
	r.inner.scriptFor("test-a",
		scriptedOutcome{evidence: stateIntegrityEvidence()},
		scriptedOutcome{evidence: stateIntegrityEvidence()},
	)
	s := newSuiteWithEvalCounting(r, newFakeClock())
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	// Both state-integrity attempts produced evidence and must be evaluated.
	if got := r.evalCount.Load(); got != 2 {
		t.Errorf("eval was called %d times for two consecutive state-integrity attempts, want exactly 2 — neither attempt must be skipped or double-counted", got)
	}
}
