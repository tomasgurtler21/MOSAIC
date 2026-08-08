package evaluate_test

// Tests for repetition aggregation against a declared pass rate, including
// the rule that a run failed for state integrity is retried once and
// excluded from pass-rate aggregation — the aggregate must measure the
// subject, not the tool — and that a second such occurrence fails the test
// as an infrastructure failure.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

func passResult(n int) domain.TestResult {
	return domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestID: "example", RunNumber: n},
		Verdict: domain.VerdictPass,
	}
}

func failResult(n int) domain.TestResult {
	return domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestID: "example", RunNumber: n},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonAssertion},
	}
}

func stateIntegrityResult(n int) domain.TestResult {
	return domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestID: "example", RunNumber: n},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonStateIntegrity},
	}
}

func passResultWithCost(n int, usd float64) domain.TestResult {
	r := passResult(n)
	r.Cost = domain.CostReport{TotalUSD: usd, Attribution: domain.AttributionAttributed}
	return r
}

func TestAggregate_PassRateMet_YieldsPassVerdict(t *testing.T) {
	results := []domain.TestResult{passResult(1), passResult(2), failResult(3)}
	policy := domain.RepetitionPolicy{Repetitions: 3, PassRate: 0.6}

	got := evaluate.Aggregate(results, policy)

	if got.Counted != 3 {
		t.Errorf("Counted = %d, want 3", got.Counted)
	}
	if got.Passed != 2 {
		t.Errorf("Passed = %d, want 2", got.Passed)
	}
	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS — 2/3 (%.2f) meets the declared pass rate of 0.6", got.Verdict, got.PassRate)
	}
}

func TestAggregate_PassRateNotMet_YieldsFailVerdict(t *testing.T) {
	results := []domain.TestResult{passResult(1), failResult(2), failResult(3)}
	policy := domain.RepetitionPolicy{Repetitions: 3, PassRate: 0.6}

	got := evaluate.Aggregate(results, policy)

	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL — 1/3 does not meet the declared pass rate of 0.6", got.Verdict)
	}
}

// TestAggregate_PassRateExactlyMeetsThreshold_YieldsPassVerdict covers the
// boundary case: a pass rate exactly equal to the declared threshold must
// meet it, not fall short of it.
func TestAggregate_PassRateExactlyMeetsThreshold_YieldsPassVerdict(t *testing.T) {
	results := []domain.TestResult{passResult(1), failResult(2)}
	policy := domain.RepetitionPolicy{Repetitions: 2, PassRate: 0.5}

	got := evaluate.Aggregate(results, policy)

	if got.PassRate != 0.5 {
		t.Errorf("PassRate = %v, want 0.5", got.PassRate)
	}
	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS — a pass rate exactly equal to the declared threshold (0.5) meets it", got.Verdict)
	}
}

// TestAggregate_TotalCost_SumsAcrossCountedRuns confirms a dropped or
// double-counted cost total across repetitions would be caught.
func TestAggregate_TotalCost_SumsAcrossCountedRuns(t *testing.T) {
	results := []domain.TestResult{passResultWithCost(1, 0.10), passResultWithCost(2, 0.25)}
	policy := domain.RepetitionPolicy{Repetitions: 2, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.TotalCost.TotalUSD != 0.35 {
		t.Errorf("TotalCost.TotalUSD = %v, want 0.35 — the sum of both counted runs' costs", got.TotalCost.TotalUSD)
	}
}

// TestAggregate_SingleRunExcludedForStateIntegrity_ZeroCountedYieldsNonPassWithoutInfrastructureFailure
// covers the all-runs-excluded edge case: with a single repetition that
// fails for state integrity, the counted set is empty and the aggregate
// must not read that as a pass — but a lone occurrence, not yet recurred
// after its retry, is not yet an infrastructure failure either.
func TestAggregate_SingleRunExcludedForStateIntegrity_ZeroCountedYieldsNonPassWithoutInfrastructureFailure(t *testing.T) {
	results := []domain.TestResult{stateIntegrityResult(1)}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1 — the one repetition was excluded for state integrity", got.Excluded)
	}
	if got.Counted != 0 {
		t.Errorf("Counted = %d, want 0 — the only repetition was excluded for state integrity", got.Counted)
	}
	if got.Verdict == domain.VerdictPass {
		t.Error("Verdict = PASS, want anything but PASS — zero counted runs must not read as a passing aggregate")
	}
	if got.InfrastructureFailure {
		t.Error("InfrastructureFailure = true, want false — a single state-integrity occurrence awaiting its retry is not yet an infrastructure failure")
	}
}

// TestAggregate_StateIntegrityRun_ExcludedFromDenominator is the decisive
// AC8.8 property: the aggregate must measure the subject, not the tool.
func TestAggregate_StateIntegrityRun_ExcludedFromDenominator(t *testing.T) {
	results := []domain.TestResult{passResult(1), stateIntegrityResult(2), passResult(3)}
	policy := domain.RepetitionPolicy{Repetitions: 3, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1", got.Excluded)
	}
	if got.Counted != 2 {
		t.Errorf("Counted = %d, want 2 — the state-integrity run must not enter the denominator", got.Counted)
	}
	if got.Passed != 2 {
		t.Errorf("Passed = %d, want 2", got.Passed)
	}
	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS — both counted runs passed", got.Verdict)
	}
	if got.InfrastructureFailure {
		t.Error("InfrastructureFailure = true, want false — a single state-integrity occurrence is retried, not fatal")
	}
}

// TestAggregate_StateIntegrityRecursAfterRetry_IsInfrastructureFailure
// covers the second half of AC8.8: a state-integrity failure that recurs
// after its single retry is an infrastructure failure, not a subject
// regression.
func TestAggregate_StateIntegrityRecursAfterRetry_IsInfrastructureFailure(t *testing.T) {
	results := []domain.TestResult{stateIntegrityResult(1), stateIntegrityResult(2), passResult(3)}
	policy := domain.RepetitionPolicy{Repetitions: 3, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if !got.InfrastructureFailure {
		t.Error("InfrastructureFailure = false, want true — the state-integrity fault recurred after its retry")
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
	if !hasReason(got.Reasons, domain.ReasonInfrastructure) {
		t.Errorf("Reasons = %+v, want ReasonInfrastructure — this is a tool fault, not a subject regression", got.Reasons)
	}
}

func TestNeedsRetry_StateIntegrityResult_ReportsTrue(t *testing.T) {
	if !evaluate.NeedsRetry(stateIntegrityResult(1)) {
		t.Error("NeedsRetry(state-integrity result) = false, want true")
	}
}

func TestNeedsRetry_OrdinaryFailResult_ReportsFalse(t *testing.T) {
	if evaluate.NeedsRetry(failResult(1)) {
		t.Error("NeedsRetry(ordinary assertion failure) = true, want false — only an infrastructure fault should be retried")
	}
}

func TestNeedsRetry_PassResult_ReportsFalse(t *testing.T) {
	if evaluate.NeedsRetry(passResult(1)) {
		t.Error("NeedsRetry(pass result) = true, want false")
	}
}

func TestNeedsRetry_TimeoutResult_ReportsFalse(t *testing.T) {
	timeout := domain.TestResult{Verdict: domain.VerdictTimeout, Reasons: []domain.FailureReason{domain.ReasonTimeout}}
	if evaluate.NeedsRetry(timeout) {
		t.Error("NeedsRetry(timeout result) = true, want false — a timeout is the subject's own outcome, not an infrastructure fault to retry")
	}
}
