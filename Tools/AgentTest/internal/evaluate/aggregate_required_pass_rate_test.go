package evaluate_test

// Tests for evaluate.Aggregate populating RequiredPassRate on the returned
// AggregateResult. The required threshold is declared by the repetition policy
// and must be carried through unchanged so a rendering can state the threshold
// a test was judged against without re-deriving it from a policy no renderer
// can see.
//
// These are TDD RED phase tests: they specify the contract for RequiredPassRate
// propagation and fail until Aggregate is updated to populate the field.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// TestAggregate_RequiredPassRatePopulatedFromPolicy asserts that the returned
// AggregateResult carries RequiredPassRate equal to the policy's declared pass
// rate. The renderer reads this field — it cannot see the policy itself — so
// the value must survive the aggregation step exactly as declared.
func TestAggregate_RequiredPassRatePopulatedFromPolicy(t *testing.T) {
	results := []domain.TestResult{passResult(1), failResult(2), passResult(3)}
	policy := domain.RepetitionPolicy{Repetitions: 3, PassRate: 0.8}

	got := evaluate.Aggregate(results, policy)

	if got.RequiredPassRate != 0.8 {
		t.Errorf("RequiredPassRate = %v, want 0.8 — the required threshold must be "+
			"carried from the policy onto the aggregate so a renderer can state it "+
			"without re-deriving it from a policy no renderer can see", got.RequiredPassRate)
	}
}

// TestAggregate_RequiredPassRateWithHighThreshold_IsRecordedVerbatim asserts
// that a high required pass rate is carried through exactly, even when the test
// fails to meet it. The value is a record of the declared policy, not a derived
// or adjusted value.
func TestAggregate_RequiredPassRateWithHighThreshold_IsRecordedVerbatim(t *testing.T) {
	results := []domain.TestResult{passResult(1), failResult(2)}
	policy := domain.RepetitionPolicy{Repetitions: 2, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.RequiredPassRate != 1.0 {
		t.Errorf("RequiredPassRate = %v, want 1.0 — the declared threshold must "+
			"survive aggregation unchanged regardless of the achieved pass rate",
			got.RequiredPassRate)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL — 1/2 does not meet the declared pass "+
			"rate of 1.0", got.Verdict)
	}
}

// TestAggregate_RequiredPassRateWithExcludedRuns_IsStillFromPolicy asserts that
// excluded runs (state-integrity faults) do not affect the RequiredPassRate
// field. The field comes from the policy, not from a calculation over the runs.
func TestAggregate_RequiredPassRateWithExcludedRuns_IsStillFromPolicy(t *testing.T) {
	results := []domain.TestResult{stateIntegrityResult(1), passResult(2), passResult(3)}
	policy := domain.RepetitionPolicy{Repetitions: 3, PassRate: 0.75}

	got := evaluate.Aggregate(results, policy)

	if got.RequiredPassRate != 0.75 {
		t.Errorf("RequiredPassRate = %v, want 0.75 — excluded runs must not alter "+
			"the required threshold, which comes from the policy", got.RequiredPassRate)
	}
	if got.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1 — the state-integrity run must be excluded",
			got.Excluded)
	}
}

// TestAggregate_RequiredPassRateInfrastructureFailure_IsStillFromPolicy asserts
// that an infrastructure failure — which overrides the verdict — does not suppress
// or alter the RequiredPassRate field. The required threshold is always from the
// policy, regardless of how the aggregate was decided.
func TestAggregate_RequiredPassRateInfrastructureFailure_IsStillFromPolicy(t *testing.T) {
	results := []domain.TestResult{infrastructureResult(1)}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 0.9}

	got := evaluate.Aggregate(results, policy)

	if got.RequiredPassRate != 0.9 {
		t.Errorf("RequiredPassRate = %v, want 0.9 — an infrastructure failure "+
			"must not suppress or alter the required threshold", got.RequiredPassRate)
	}
	if !got.InfrastructureFailure {
		t.Error("InfrastructureFailure = false, want true — a single infrastructure result must set this flag")
	}
}

// TestAggregate_RequiredPassRateDistinctFromAchievedPassRate asserts that
// RequiredPassRate and PassRate are independent: RequiredPassRate comes from the
// policy and PassRate is computed from the counted results. A test that is above
// the required threshold carries both the achieved rate and the required threshold
// so a renderer can show both without choosing between them.
func TestAggregate_RequiredPassRateDistinctFromAchievedPassRate(t *testing.T) {
	// 4/5 pass, required 60% — achieved (80%) exceeds required (60%)
	results := []domain.TestResult{
		passResult(1), passResult(2), passResult(3), passResult(4), failResult(5),
	}
	policy := domain.RepetitionPolicy{Repetitions: 5, PassRate: 0.6}

	got := evaluate.Aggregate(results, policy)

	if got.RequiredPassRate != 0.6 {
		t.Errorf("RequiredPassRate = %v, want 0.6 — the required threshold from the policy",
			got.RequiredPassRate)
	}
	if got.PassRate != 0.8 {
		t.Errorf("PassRate = %v, want 0.8 — the achieved rate from the counted results",
			got.PassRate)
	}
	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS — 4/5 (0.8) meets the required threshold (0.6)",
			got.Verdict)
	}
}
