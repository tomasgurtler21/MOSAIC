package suite_test

// T10.1: when Options.RunID is left nil, the suite falls back to its own
// default run-identity generator, which must produce an id in the logger
// bundle's required shape, deterministically under an injected clock, and
// distinctly across tests and across repetitions of the same test. The
// generator itself is unexported (defaultRunID); these tests observe it
// indirectly through the RunKey a scripted runner is actually called with,
// exactly as production observes it.

import (
	"context"
	"fmt"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/suite"
)

// newSuiteWithDefaultRunID builds a Suite with Options.RunID left nil, so
// runID falls back to the package's own default generator.
func newSuiteWithDefaultRunID(runner *scriptedRunner, clock *fakeClock) *suite.Suite {
	return suite.New(suite.Options{
		Runner:   runner,
		Progress: &recordingSink{},
		Clock:    clock,
		// RunID deliberately left nil.
	})
}

func TestSuiteRun_DefaultRunID_ProducesTheLoggerBundlesRequiredShape(t *testing.T) {
	runner := newScriptedRunner()
	s := newSuiteWithDefaultRunID(runner, newFakeClock())
	plan := buildPlan(resolvedTest("shape-check", 1, 1.0))

	if _, err := runSuite(t, s, context.Background(), plan); err != nil {
		t.Fatalf("Suite.Run returned unexpected error: %v", err)
	}

	calls := runner.allCalls()
	if len(calls) != 1 {
		t.Fatalf("runner was called %d times, want 1", len(calls))
	}
	if !domain.ValidRunID(calls[0].RunID) {
		t.Errorf("default run id %q is not domain.ValidRunID-shaped — the logger bundle would silently bucket it under unknown-run", calls[0].RunID)
	}
}

func TestSuiteRun_DefaultRunID_IsDeterministicUnderAnInjectedClock(t *testing.T) {
	plan := buildPlan(resolvedTest("determinism-check", 1, 1.0))

	runnerA := newScriptedRunner()
	sA := newSuiteWithDefaultRunID(runnerA, newFakeClock())
	if _, err := runSuite(t, sA, context.Background(), plan); err != nil {
		t.Fatalf("first Suite.Run returned unexpected error: %v", err)
	}

	runnerB := newScriptedRunner()
	sB := newSuiteWithDefaultRunID(runnerB, newFakeClock())
	if _, err := runSuite(t, sB, context.Background(), plan); err != nil {
		t.Fatalf("second Suite.Run returned unexpected error: %v", err)
	}

	gotA := runnerA.allCalls()[0].RunID
	gotB := runnerB.allCalls()[0].RunID
	if gotA != gotB {
		t.Errorf("default run id = %q then %q for the same (clock, testID, runNumber), want identical — generation must not read ambient time or randomness", gotA, gotB)
	}
}

func TestSuiteRun_DefaultRunID_DiffersAcrossTestsInTheSameRun(t *testing.T) {
	runner := newScriptedRunner()
	s := newSuiteWithDefaultRunID(runner, newFakeClock())
	plan := buildPlan(
		resolvedTest("test-alpha", 1, 1.0),
		resolvedTest("test-beta", 1, 1.0),
	)

	if _, err := runSuite(t, s, context.Background(), plan); err != nil {
		t.Fatalf("Suite.Run returned unexpected error: %v", err)
	}

	calls := runner.allCalls()
	if len(calls) != 2 {
		t.Fatalf("runner was called %d times, want 2", len(calls))
	}
	if calls[0].RunID == calls[1].RunID {
		t.Errorf("two different tests under the same non-advancing clock produced the same run id %q, want distinct ids", calls[0].RunID)
	}
}

// TestSuiteRun_DefaultRunID_DiffersAcrossRepetitionsOfTheSameTestUnderANonAdvancingClock
// guards the case a clock-only-derived suffix would silently collide: two
// repetitions of the same test, driven by a clock that never advances
// between them, must still produce distinct run ids, since two attempts
// with the same run id would clash on sandbox naming and log/cost
// attribution.
func TestSuiteRun_DefaultRunID_DiffersAcrossRepetitionsOfTheSameTestUnderANonAdvancingClock(t *testing.T) {
	runner := newScriptedRunner()
	s := newSuiteWithDefaultRunID(runner, newFakeClock())
	plan := buildPlan(resolvedTest("repeat-me", 2, 1.0))

	if _, err := runSuite(t, s, context.Background(), plan); err != nil {
		t.Fatalf("Suite.Run returned unexpected error: %v", err)
	}

	calls := runner.allCalls()
	if len(calls) != 2 {
		t.Fatalf("runner was called %d times, want 2 (two repetitions)", len(calls))
	}
	if calls[0].RunID == calls[1].RunID {
		t.Errorf("two repetitions of the same test under a non-advancing clock produced the same run id %q, want distinct ids", calls[0].RunID)
	}
}

// TestSuiteRun_DefaultRunID_TwoAttemptsOfOneRepetitionGetDistinctRunIDs
// guards the per-attempt run-identity property: when a state-integrity fault
// triggers the retry-and-exclude rule, the retry must receive a run id that
// differs from the original attempt's. Sharing a run id means the retry's
// workspace.Manager.Create call fails with ErrSandboxExists — the sandbox
// was already created for the first attempt — and the subject is never
// actually re-run.
//
// This is a RED test: it fails until I14.1 threads the attempt index into
// defaultRunID's suffix derivation, because the current generator hashes
// only (testID, runNumber) and produces the same id for both attempts of one
// repetition.
func TestSuiteRun_DefaultRunID_TwoAttemptsOfOneRepetitionGetDistinctRunIDs(t *testing.T) {
	runner := newScriptedRunner()
	runner.scriptFor("id-per-attempt",
		scriptedOutcome{evidence: stateIntegrityEvidence()},
		scriptedOutcome{evidence: passingEvidence()},
	)
	s := newSuiteWithDefaultRunID(runner, newFakeClock())
	plan := buildPlan(resolvedTest("id-per-attempt", 1, 1.0))

	if _, err := runSuite(t, s, context.Background(), plan); err != nil {
		t.Fatalf("Suite.Run returned unexpected error: %v", err)
	}

	calls := runner.callsFor("id-per-attempt")
	if len(calls) != 2 {
		t.Fatalf("runner was called %d times for id-per-attempt, want 2 (the original attempt plus the state-integrity retry)", len(calls))
	}
	if calls[0].RunID == calls[1].RunID {
		t.Errorf("both attempts of one repetition were driven with the same run id %q — the retry's workspace.Manager.Create call will hit ErrSandboxExists and the subject is never actually re-run",
			calls[0].RunID)
	}
}

// TestSuiteRun_InjectedRunID_ReceivesAttemptIndex verifies that the seam
// change threads the zero-based attempt index to the injected RunID function,
// so an injected generator can produce per-attempt distinct ids without
// reading ambient state. The injected function records every attempt index
// it receives; the assertion checks that the two calls for one repetition
// carry different attempt values.
func TestSuiteRun_InjectedRunID_ReceivesAttemptIndex(t *testing.T) {
	var receivedAttempts []int

	runner := newScriptedRunner()
	runner.scriptFor("attempt-idx",
		scriptedOutcome{evidence: stateIntegrityEvidence()},
		scriptedOutcome{evidence: passingEvidence()},
	)
	s := suite.New(suite.Options{
		Runner:   runner,
		Progress: &recordingSink{},
		Clock:    newFakeClock(),
		RunID: func(testID string, runNumber, attempt int) string {
			receivedAttempts = append(receivedAttempts, attempt)
			// Return a ValidRunID-shaped string that varies by attempt
			// so the two calls produce distinct, usable run ids.
			return fmt.Sprintf("20260807T120000Z-%04x", attempt)
		},
	})
	plan := buildPlan(resolvedTest("attempt-idx", 1, 1.0))

	if _, err := runSuite(t, s, context.Background(), plan); err != nil {
		t.Fatalf("Suite.Run returned unexpected error: %v", err)
	}

	if len(receivedAttempts) != 2 {
		t.Fatalf("injected RunID was called %d times, want 2 (original attempt and state-integrity retry)", len(receivedAttempts))
	}
	if receivedAttempts[0] == receivedAttempts[1] {
		t.Errorf("injected RunID received attempt=%d on both calls, want distinct attempt indices so the generator can produce different run ids for the two attempts of one repetition",
			receivedAttempts[0])
	}
}
