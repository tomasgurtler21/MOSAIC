package evaluate_test

// Tests for Stage 8 (T8.3): assertion evaluation must remain independent of
// the evidence fields LogsProduced, LogRoot, and FallbackBucketPresent.
//
// This is AC8.3: "No assertion class reads the evidence fields this stage
// touches; assertion evaluation remains independent of which bucket a run's
// logs land in."
//
// The test is a pin, not a new-behaviour test: it asserts an invariant that
// must hold both before and after Stage 8's implementation changes. If
// evaluateAssertions were ever changed to consult bucket-layout fields, these
// tests would catch the violation.
//
// The approach is behavioral: two evidence values that differ ONLY in bucket-
// layout fields (LogsProduced, FallbackBucketPresent, LogRoot) are evaluated
// and their assertion outcomes are compared. Conditions may legitimately
// differ (conditions.go explicitly uses these fields); assertions must not.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// TestEvaluate_AssertionOutcomes_AreIndependentOfLogBucketFields asserts that
// assertion class outcomes do not change when the bucket-layout evidence
// fields change. Two evidence values that are identical except for
// LogsProduced, LogRoot, and FallbackBucketPresent must yield identical
// assertion outcomes.
//
// This is the general pin: all assertion classes, not just the ones that
// happen to be declared in the definition, must be unaffected by bucket
// layout.
func TestEvaluate_AssertionOutcomes_AreIndependentOfLogBucketFields(t *testing.T) {
	// base evidence with an artifact assertion to produce a non-trivial result.
	base := baseEvidence()
	base.Definition.Assertions.ArtifactCreated = []string{"Research.md"}
	base.SnapshotFiles = []string{"Research.md"}

	// Evidence A: logs produced in the per-run folder, no fallback bucket.
	evA := base
	evA.LogsProduced = true
	evA.FallbackBucketPresent = false
	evA.LogRoot = "/sandbox/OrchestrationLogs/run-1"

	// Evidence B: identical assertions but bucket layout differs entirely —
	// logs exist only in the fallback, and the fallback bucket is present.
	evB := base
	evB.LogsProduced = false
	evB.FallbackBucketPresent = true
	evB.LogRoot = "/sandbox/OrchestrationLogs/run-2"

	resultA := evaluate.Evaluate(evA)
	resultB := evaluate.Evaluate(evB)

	if len(resultA.Assertions) != len(resultB.Assertions) {
		t.Fatalf(
			"len(Assertions): evA=%d, evB=%d — the number of assertion results must not depend on bucket-layout fields",
			len(resultA.Assertions), len(resultB.Assertions),
		)
	}
	for i, a := range resultA.Assertions {
		b := resultB.Assertions[i]
		if a.Class != b.Class {
			t.Errorf("Assertions[%d].Class: evA=%q, evB=%q — class identity must not depend on bucket layout", i, a.Class, b.Class)
		}
		if a.Outcome != b.Outcome {
			t.Errorf(
				"Assertions[%d] (class=%q) Outcome: evA=%q, evB=%q — "+
					"assertion outcome changed when only LogsProduced/FallbackBucketPresent/LogRoot changed; "+
					"evaluateAssertions must not read these fields",
				i, a.Class, a.Outcome, b.Outcome,
			)
		}
	}
}

// TestEvaluate_VerdictFromAssertions_IsIndependentOfLogBucketFields asserts
// that the verdict — as determined solely by assertion outcomes — does not
// change when bucket-layout fields change. It is the verdict-level complement
// to TestEvaluate_AssertionOutcomes_AreIndependentOfLogBucketFields.
//
// Conditions (which CAN change) may add to the conditions slice, but they
// must never flip the verdict from PASS to FAIL or vice versa.
func TestEvaluate_VerdictFromAssertions_IsIndependentOfLogBucketFields(t *testing.T) {
	base := baseEvidence()
	base.Definition.Assertions.ArtifactCreated = []string{"Research.md"}
	base.SnapshotFiles = []string{"Research.md"}

	evA := base
	evA.LogsProduced = true
	evA.FallbackBucketPresent = false

	evB := base
	evB.LogsProduced = false
	evB.FallbackBucketPresent = true

	resultA := evaluate.Evaluate(evA)
	resultB := evaluate.Evaluate(evB)

	if resultA.Verdict != resultB.Verdict {
		t.Errorf(
			"Verdict: evA=%q, evB=%q — the verdict changed when only bucket-layout fields changed; "+
				"assertion evaluation must be independent of LogsProduced and FallbackBucketPresent",
			resultA.Verdict, resultB.Verdict,
		)
	}
}

// TestEvaluate_InvocationSequenceAssertion_IsIndependentOfLogBucketFields
// pins specifically that the invocation-sequence assertion does not consult
// bucket-layout fields. The sequence assertion is the assertion class most
// likely to be tempted to use LogsProduced (since "no logs" implies no
// invocations); it must not.
func TestEvaluate_InvocationSequenceAssertion_IsIndependentOfLogBucketFields(t *testing.T) {
	base := baseEvidence()
	// Declare a sequence assertion that matches the invocations in baseEvidence
	// (one researcher invocation).
	r := researcher()
	base.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{
			{Identity: &r},
		},
	}

	evA := base
	evA.LogsProduced = true
	evA.FallbackBucketPresent = false

	evB := base
	evB.LogsProduced = false
	evB.FallbackBucketPresent = true

	resultA := evaluate.Evaluate(evA)
	resultB := evaluate.Evaluate(evB)

	seqA := findAssertionOrNil(resultA.Assertions, domain.ClassInvocationSequence, "")
	seqB := findAssertionOrNil(resultB.Assertions, domain.ClassInvocationSequence, "")

	if seqA == nil || seqB == nil {
		t.Fatal("InvocationSequence assertion was not evaluated in one or both results — need it to be present for this pin")
	}
	if seqA.Outcome != seqB.Outcome {
		t.Errorf(
			"InvocationSequence assertion outcome: evA=%q, evB=%q — "+
				"the sequence assertion changed when only bucket-layout fields changed; "+
				"it must not consult LogsProduced or FallbackBucketPresent",
			seqA.Outcome, seqB.Outcome,
		)
	}
}

// findAssertionOrNil returns the first AssertionResult for the given class
// and target, or nil when none is present. Unlike findAssertion (which fails
// the test on absence), this is used when a "not present" result is itself
// meaningful.
func findAssertionOrNil(results []domain.AssertionResult, class domain.AssertionClass, target string) *domain.AssertionResult {
	for i, ar := range results {
		if ar.Class == class && ar.Target == target {
			return &results[i]
		}
	}
	return nil
}
