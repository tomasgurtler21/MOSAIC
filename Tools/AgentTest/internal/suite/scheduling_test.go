package suite_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
	"mosaic-agent-test/internal/suite"
)

func newSuite(runner *scriptedRunner, clock *fakeClock, progress domain.ProgressSink) *suite.Suite {
	return suite.New(suite.Options{
		Runner:   runner,
		Progress: progress,
		Clock:    clock,
		RunID:    func(testID string, runNumber, attempt int) string { return "run-" + testID },
	})
}

// TestSuiteRun_ExecutesTestsInDeclaredOrder asserts a suite's test reports
// are returned in the order the plan declares, not in some incidental order
// (e.g. map iteration or completion order under concurrent execution).
// The concurrent scheduler preserves declaration order in the assembled
// report regardless of which worker finishes first.
func TestSuiteRun_ExecutesTestsInDeclaredOrder(t *testing.T) {
	// Arrange
	runner := newScriptedRunner()
	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(
		resolvedTest("test-a", 1, 1),
		resolvedTest("test-b", 1, 1),
		resolvedTest("test-c", 1, 1),
	)

	// Act
	result, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	wantOrder := []string{"test-a", "test-b", "test-c"}
	if len(result.Tests) != len(wantOrder) {
		t.Fatalf("got %d test reports, want %d", len(result.Tests), len(wantOrder))
	}
	for i, want := range wantOrder {
		if result.Tests[i].TestID != want {
			t.Errorf("result.Tests[%d].TestID = %q, want %q", i, result.Tests[i].TestID, want)
		}
	}
}

// TestSuiteRun_ExecutesDeclaredRepetitions asserts each test runs exactly
// its declared number of repetitions, numbered from 1. Under concurrent
// execution workers may complete in any order, so this test checks that
// all expected run numbers are present in the call set rather than
// asserting that the i-th call carries run number i+1.
func TestSuiteRun_ExecutesDeclaredRepetitions(t *testing.T) {
	// Arrange
	runner := newScriptedRunner()
	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("test-a", 3, 1))

	// Act
	_, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	calls := runner.callsFor("test-a")
	if len(calls) != 3 {
		t.Fatalf("test-a was run %d times, want 3", len(calls))
	}
	seen := make(map[int]bool, len(calls))
	for _, c := range calls {
		seen[c.RunNumber] = true
	}
	for _, want := range []int{1, 2, 3} {
		if !seen[want] {
			t.Errorf("RunNumber %d not present in runner calls: calls recorded %v", want, runNumbersFrom(calls))
		}
	}
}

// runNumbersFrom extracts the RunNumber from each call for use in diagnostic
// messages.
func runNumbersFrom(calls []domain.RunKey) []int {
	nums := make([]int, len(calls))
	for i, c := range calls {
		nums[i] = c.RunNumber
	}
	return nums
}

// TestSuiteRun_AggregatesViaVerdictEngine asserts the reported aggregate for
// a test is exactly what evaluate.Aggregate computes from the same
// per-repetition results — the aggregation is delegated to the verdict
// engine, never recomputed independently at the scheduling level (AC14.2).
func TestSuiteRun_AggregatesViaVerdictEngine(t *testing.T) {
	// Arrange
	runner := newScriptedRunner()
	runner.scriptFor("test-a",
		scriptedOutcome{evidence: passingEvidence()},
		scriptedOutcome{evidence: passingEvidence()},
		scriptedOutcome{evidence: failingEvidence()},
	)
	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("test-a", 3, 1.0))

	// Act
	result, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	if len(result.Tests) != 1 {
		t.Fatalf("got %d test reports, want 1", len(result.Tests))
	}
	got := result.Tests[0].Aggregate

	var results []domain.TestResult
	for i, ev := range []domain.RunEvidence{passingEvidence(), passingEvidence(), failingEvidence()} {
		ev.Key = domain.RunKey{TestID: "test-a", RunNumber: i + 1}
		results = append(results, evaluate.Evaluate(ev))
	}
	want := evaluate.Aggregate(results, domain.RepetitionPolicy{Repetitions: 3, PassRate: 1.0})

	if got.Verdict != want.Verdict {
		t.Errorf("aggregate verdict = %v, want %v (from evaluate.Aggregate)", got.Verdict, want.Verdict)
	}
	if got.Counted != want.Counted || got.Passed != want.Passed || got.Excluded != want.Excluded {
		t.Errorf("aggregate counts = {Counted:%d Passed:%d Excluded:%d}, want {Counted:%d Passed:%d Excluded:%d} (from evaluate.Aggregate)",
			got.Counted, got.Passed, got.Excluded, want.Counted, want.Passed, want.Excluded)
	}
}

// TestSuiteRun_StateIntegrityRetriedOnceAndExcluded asserts a repetition
// that fails for state integrity is retried exactly once, and — once the
// retry passes — is excluded from the pass-rate denominator rather than
// counted as a subject failure (AC14.3).
func TestSuiteRun_StateIntegrityRetriedOnceAndExcluded(t *testing.T) {
	// Arrange
	runner := newScriptedRunner()
	runner.scriptFor("test-a",
		scriptedOutcome{evidence: stateIntegrityEvidence()},
		scriptedOutcome{evidence: passingEvidence()},
	)
	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	// Act
	result, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	calls := runner.callsFor("test-a")
	if len(calls) != 2 {
		t.Fatalf("test-a was run %d times, want 2 (the original attempt plus one retry)", len(calls))
	}

	if len(result.Tests) != 1 {
		t.Fatalf("got %d test reports, want 1", len(result.Tests))
	}
	agg := result.Tests[0].Aggregate
	if agg.Verdict != domain.VerdictPass {
		t.Errorf("aggregate verdict = %v, want PASS: a state-integrity run followed by a passing retry must not fail the test", agg.Verdict)
	}
	if agg.Excluded != 1 {
		t.Errorf("aggregate Excluded = %d, want 1: the state-integrity attempt must be excluded from the denominator", agg.Excluded)
	}
	if agg.Counted != 1 {
		t.Errorf("aggregate Counted = %d, want 1: only the retry counts toward the pass rate", agg.Counted)
	}
}

// TestSuiteRun_RepeatedStateIntegrityEndsAsInfrastructureFailure asserts a
// second state-integrity fault on the same repetition ends the test as an
// infrastructure failure, distinct from a subject regression (AC14.3).
func TestSuiteRun_RepeatedStateIntegrityEndsAsInfrastructureFailure(t *testing.T) {
	// Arrange
	runner := newScriptedRunner()
	runner.scriptFor("test-a",
		scriptedOutcome{evidence: stateIntegrityEvidence()},
		scriptedOutcome{evidence: stateIntegrityEvidence()},
	)
	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	// Act
	result, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	calls := runner.callsFor("test-a")
	if len(calls) != 2 {
		t.Fatalf("test-a was run %d times, want 2 (the original attempt plus its single retry, then stop)", len(calls))
	}

	if len(result.Tests) != 1 {
		t.Fatalf("got %d test reports, want 1", len(result.Tests))
	}
	agg := result.Tests[0].Aggregate
	if !agg.InfrastructureFailure {
		t.Errorf("aggregate InfrastructureFailure = false, want true after a second state-integrity fault")
	}
	if result.InfrastructureFailures != 1 {
		t.Errorf("Result.InfrastructureFailures = %d, want 1", result.InfrastructureFailures)
	}
}

// TestSuiteRun_ReturnsCompletedResultsOnCancellation asserts a cancelled
// suite returns the results it completed rather than discarding them, and
// does not report an error merely because the context was cancelled after
// useful work was done.
func TestSuiteRun_ReturnsCompletedResultsOnCancellation(t *testing.T) {
	// Arrange
	runner := newScriptedRunner()
	runner.scriptFor("test-a", scriptedOutcome{evidence: passingEvidence()})
	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("test-a", 1, 1.0), resolvedTest("test-b", 1, 1.0))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Act
	result, err := runSuite(t, s, ctx, plan)

	// Assert: a cancelled context is not itself reported as a suite
	// failure — the caller sees whatever results completed, not an error
	// merely because the run was cancelled.
	if err != nil {
		t.Errorf("Suite.Run returned an error for a cancelled context: %v", err)
	}
	// The completed test's report must actually be present in the returned
	// Result — an implementation that returns an empty report.Result{} on
	// cancellation would pass a "no error" check while discarding the work
	// that was already done.
	if len(result.Tests) < 1 {
		t.Fatalf("got %d test reports on a cancelled run, want at least 1: the already-completed test-a result must not be discarded", len(result.Tests))
	}
	if result.Tests[0].TestID != "test-a" {
		t.Errorf("result.Tests[0].TestID = %q, want %q: the completed test's own report must be returned, not a placeholder", result.Tests[0].TestID, "test-a")
	}
}

// TestSuiteRun_RunnerError_YieldsInfrastructureFailWithConditionCarryingErrorDetail
// asserts a runner error is never discarded: it must translate into a FAIL
// result carrying the infrastructure reason and a condition holding the
// error text, rather than being silently evaluated as zero-valued (and
// therefore passing) evidence (AC1.1).
func TestSuiteRun_RunnerError_YieldsInfrastructureFailWithConditionCarryingErrorDetail(t *testing.T) {
	// Arrange
	wantErrText := "spawn-plan failed: exec: \"claude\": executable file not found in $PATH"
	runner := newScriptedRunner()
	runner.scriptFor("test-a", scriptedOutcome{err: errors.New(wantErrText)})
	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	// Act
	result, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	if len(result.Tests) != 1 || len(result.Tests[0].Runs) != 1 {
		t.Fatalf("got %+v, want exactly one test with exactly one run", result.Tests)
	}
	run := result.Tests[0].Runs[0]

	if run.Verdict != domain.VerdictFail {
		t.Errorf("run Verdict = %q, want FAIL — a runner error must never evaluate as a pass", run.Verdict)
	}
	if !hasFailureReason(run.Reasons, domain.ReasonInfrastructure) {
		t.Errorf("run Reasons = %+v, want ReasonInfrastructure", run.Reasons)
	}
	cond, ok := findConditionKind(run.Conditions, domain.ConditionRunNotStarted)
	if !ok {
		t.Fatalf("run Conditions = %+v, want a ConditionRunNotStarted entry", run.Conditions)
	}
	if !strings.Contains(cond.Detail, wantErrText) {
		t.Errorf("condition Detail = %q, want it to contain the runner error text %q", cond.Detail, wantErrText)
	}
}

// TestSuiteRun_RunnerError_StopsRetryLoopForThatAttempt asserts a runner
// error ends the repetition's retry loop rather than retrying against the
// same fault — distinct from the state-integrity retry rule, which only
// fires on ReasonStateIntegrity (AC1.2).
func TestSuiteRun_RunnerError_StopsRetryLoopForThatAttempt(t *testing.T) {
	// Arrange
	runner := newScriptedRunner()
	runner.scriptFor("test-a",
		scriptedOutcome{err: errors.New("provisioning failed: workspace root is not writable")},
		scriptedOutcome{evidence: passingEvidence()},
	)
	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	// Act
	_, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	calls := runner.callsFor("test-a")
	if len(calls) != 1 {
		t.Fatalf("test-a was run %d times, want exactly 1 — a runner error must end the retry loop for that attempt, not consume the next scripted outcome", len(calls))
	}
}

// TestSuiteRun_RunnerError_AggregateReportsInfrastructureFailure confirms
// the runner-error path reaches the same aggregate and report-level
// infrastructure accounting a repeated state-integrity fault does (AC1.5).
func TestSuiteRun_RunnerError_AggregateReportsInfrastructureFailure(t *testing.T) {
	// Arrange
	runner := newScriptedRunner()
	runner.scriptFor("test-a", scriptedOutcome{err: errors.New("setup failed: panic recovered: nil pointer dereference")})
	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	// Act
	result, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	if len(result.Tests) != 1 {
		t.Fatalf("got %d test reports, want 1", len(result.Tests))
	}
	if !result.Tests[0].Aggregate.InfrastructureFailure {
		t.Error("Aggregate.InfrastructureFailure = false, want true — a runner error is an infrastructure fault")
	}
	if result.InfrastructureFailures != 1 {
		t.Errorf("Result.InfrastructureFailures = %d, want 1", result.InfrastructureFailures)
	}
}

func hasFailureReason(reasons []domain.FailureReason, want domain.FailureReason) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

func findConditionKind(conditions []domain.RunCondition, want domain.RunConditionKind) (domain.RunCondition, bool) {
	for _, c := range conditions {
		if c.Kind == want {
			return c, true
		}
	}
	return domain.RunCondition{}, false
}
