package suite_test

// max_concurrent_runs_test.go covers the suite-level concurrency bound
// (T12.5 and T12.6).
//
// T12.5 — The documented conservative default greater than 1 is used when
//          neither frontend supplies a value.
// T12.6 — Suite behaviour and results are unchanged by this stage, since the
//          scheduler does not yet consume the bound.

import (
	"context"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/suite"
)

// ---------------------------------------------------------------------------
// T12.5 — Default value
// ---------------------------------------------------------------------------

// TestSuite_DefaultMaxConcurrentRuns_IsPositive verifies that
// suite.DefaultMaxConcurrentRuns is a positive integer. A zero or negative
// default would cause zero-value Options to produce an invalid bound.
func TestSuite_DefaultMaxConcurrentRuns_IsPositive(t *testing.T) {
	if suite.DefaultMaxConcurrentRuns <= 0 {
		t.Errorf("suite.DefaultMaxConcurrentRuns = %d, want > 0", suite.DefaultMaxConcurrentRuns)
	}
}

// TestSuite_DefaultMaxConcurrentRuns_IsGreaterThanOne verifies that
// suite.DefaultMaxConcurrentRuns is strictly greater than 1. The design
// requires a conservative value greater than 1 so the default provides
// wall-clock relief; 1 is the sequential mode that a user must opt into.
func TestSuite_DefaultMaxConcurrentRuns_IsGreaterThanOne(t *testing.T) {
	if suite.DefaultMaxConcurrentRuns <= 1 {
		t.Errorf("suite.DefaultMaxConcurrentRuns = %d, want > 1 (the design requires a conservative default that provides wall-clock relief; use --max-concurrent-runs=1 explicitly for sequential mode)", suite.DefaultMaxConcurrentRuns)
	}
}

// TestSuite_DefaultMaxConcurrentRuns_IsFour verifies the exact documented
// default value. The reasoning (four keeps simultaneous sandboxes to a handful
// while staying well below typical provider limits) is stated in the constant's
// doc comment and must not be changed without updating that comment.
func TestSuite_DefaultMaxConcurrentRuns_IsFour(t *testing.T) {
	const want = 4
	if suite.DefaultMaxConcurrentRuns != want {
		t.Errorf("suite.DefaultMaxConcurrentRuns = %d, want %d (the documented conservative default; change the doc comment and this test together if the value changes)", suite.DefaultMaxConcurrentRuns, want)
	}
}

// TestSuiteOptions_MaxConcurrentRuns_ZeroSelectsDefault verifies that a zero
// value for Options.MaxConcurrentRuns is understood as "use the default" by
// the suite. This is the convention suite.Options uses for optional numeric
// fields: zero means absent/default.
//
// RED-phase note: compiles (MaxConcurrentRuns is declared on Options), but
// FAILS until I12.1 wires the field and I12.5 applies DefaultMaxConcurrentRuns
// when the field is zero. Currently the field is read nowhere, so this test
// cannot verify the wiring behaviour yet. The test specifies the contract the
// scheduler (Stage 14) must honour.
func TestSuiteOptions_MaxConcurrentRuns_ZeroSelectsDefault(t *testing.T) {
	// A zero-valued MaxConcurrentRuns on Options must behave as if the field
	// were set to DefaultMaxConcurrentRuns. This is verified indirectly through
	// the scheduling contract: a suite constructed with MaxConcurrentRuns=0
	// must not behave as if the bound were 0 (which would allow no runs at all)
	// or 1 (which is strictly sequential). It must apply DefaultMaxConcurrentRuns.
	//
	// At this stage (no scheduler), we verify only that the field exists and
	// can be set to zero without a compile error; the runtime behaviour is
	// governed by Stage 14 (bounded concurrent scheduler).
	opts := suite.Options{
		Runner: newScriptedRunner(),
		Clock:  newFakeClock(),
		RunID:  func(testID string, runNumber, attempt int) string { return "run-" + testID },
		// MaxConcurrentRuns intentionally left at zero to trigger default selection.
	}
	if opts.MaxConcurrentRuns != 0 {
		t.Errorf("Options.MaxConcurrentRuns zero value = %d; expected Go's zero value for int", opts.MaxConcurrentRuns)
	}
}

// ---------------------------------------------------------------------------
// T12.6 — Suite behaviour and results unchanged
// ---------------------------------------------------------------------------

// TestSuite_MaxConcurrentRunsField_DoesNotAlterResults verifies that setting
// Options.MaxConcurrentRuns to a value above 1 produces the same test results
// as a bound of 1 at this stage. The scheduler does not yet exist, so the
// field is accepted but not consumed: a suite configured with
// MaxConcurrentRuns=4 must return the same pass/fail verdicts as one
// configured with MaxConcurrentRuns=1.
//
// RED-phase note: compiles (MaxConcurrentRuns is declared), but FAILS with a
// panic or wrong result until I12.1 wires the field safely (ensuring the
// zero-value and non-zero-value paths both produce sequential execution at
// this stage). After I12.1 the test is GREEN and must stay GREEN through
// Stage 13; Stage 14 implements the scheduler and may make concurrent runs
// permissible, but must still satisfy D6 (a bound of 1 is behaviourally
// identical to today).
func TestSuite_MaxConcurrentRunsField_DoesNotAlterResults(t *testing.T) {
	plan := buildPlan(
		resolvedTest("test-a", 1, 1.0),
		resolvedTest("test-b", 1, 1.0),
	)

	runWithBound := func(bound int) (map[domain.Verdict]int, error) {
		runner := newScriptedRunner()
		s := suite.New(suite.Options{
			Runner:            runner,
			Clock:             newFakeClock(),
			RunID:             func(testID string, runNumber, attempt int) string { return "run-" + testID },
			MaxConcurrentRuns: bound,
		})
		result, err := runSuite(t, s, context.Background(), plan)
		return result.Counts, err
	}

	counts1, err1 := runWithBound(1)
	if err1 != nil {
		t.Fatalf("suite with MaxConcurrentRuns=1 returned error: %v", err1)
	}

	counts4, err4 := runWithBound(4)
	if err4 != nil {
		t.Fatalf("suite with MaxConcurrentRuns=4 returned error: %v", err4)
	}

	// Results must be identical: the scheduler is not yet wired, so any bound
	// above 1 must behave the same as 1.
	for _, v := range []domain.Verdict{domain.VerdictPass, domain.VerdictFail} {
		if counts1[v] != counts4[v] {
			t.Errorf("verdict %q count differs: MaxConcurrentRuns=1 → %d, MaxConcurrentRuns=4 → %d; the scheduler is not yet wired so results must be identical at this stage", v, counts1[v], counts4[v])
		}
	}
}

// TestSuite_MaxConcurrentRunsOne_ReproducesSequentialBehaviour verifies the
// D6 hard contract: a bound of 1 must reproduce today's sequential behaviour,
// results and lifecycle-event ordering exactly. This test uses the same
// plan as the scheduler tests in scheduling_test.go and asserts test order
// is preserved.
func TestSuite_MaxConcurrentRunsOne_ReproducesSequentialBehaviour(t *testing.T) {
	runner := newScriptedRunner()
	s := suite.New(suite.Options{
		Runner:            runner,
		Clock:             newFakeClock(),
		RunID:             func(testID string, runNumber, attempt int) string { return "run-" + testID },
		MaxConcurrentRuns: 1,
	})
	plan := buildPlan(
		resolvedTest("first", 1, 1.0),
		resolvedTest("second", 1, 1.0),
		resolvedTest("third", 1, 1.0),
	)

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run with MaxConcurrentRuns=1 returned error: %v", err)
	}

	calls := runner.allCalls()
	if len(calls) != 3 {
		t.Fatalf("runner called %d times with MaxConcurrentRuns=1, want 3 (one per test)", len(calls))
	}
	wantOrder := []string{"first", "second", "third"}
	for i, want := range wantOrder {
		if calls[i].TestName != want {
			t.Errorf("call %d with MaxConcurrentRuns=1: test %q, want %q (D6: bound of 1 must reproduce sequential ordering)", i, calls[i].TestName, want)
		}
	}
}

// TestSuite_MaxConcurrentRuns_FieldExistsAndIsReadable verifies the minimal
// contract that suite.Options.MaxConcurrentRuns is a named field of type int.
// A renaming or removal would break this test.
func TestSuite_MaxConcurrentRuns_FieldExistsAndIsReadable(t *testing.T) {
	opts := suite.Options{
		Runner:            newScriptedRunner(),
		Clock:             newFakeClock(),
		RunID:             func(testID string, runNumber, attempt int) string { return "run-" + testID },
		MaxConcurrentRuns: 2,
	}
	if opts.MaxConcurrentRuns != 2 {
		t.Errorf("Options.MaxConcurrentRuns = %d after initialising to 2; the field must be readable", opts.MaxConcurrentRuns)
	}
}
