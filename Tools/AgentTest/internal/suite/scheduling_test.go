package suite_test

import (
	"context"
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
		RunID:    func(testID string, runNumber int) string { return "run-" + testID },
	})
}

// TestSuiteRun_ExecutesTestsInDeclaredOrder asserts a suite's tests are
// executed in the order the plan declares, not in some incidental order
// (e.g. map iteration).
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
	_, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	calls := runner.allCalls()
	if len(calls) != 3 {
		t.Fatalf("runner was called %d times, want 3", len(calls))
	}
	wantOrder := []string{"test-a", "test-b", "test-c"}
	for i, want := range wantOrder {
		if calls[i].TestID != want {
			t.Errorf("call %d: test %q, want %q", i, calls[i].TestID, want)
		}
	}
}

// TestSuiteRun_ExecutesDeclaredRepetitions asserts each test runs exactly
// its declared number of repetitions, numbered from 1.
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
	for i, c := range calls {
		if c.RunNumber != i+1 {
			t.Errorf("call %d: RunNumber = %d, want %d", i, c.RunNumber, i+1)
		}
	}
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
