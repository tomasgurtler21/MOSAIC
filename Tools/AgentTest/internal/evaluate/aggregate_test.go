package evaluate_test

// Tests for repetition aggregation against a declared pass rate, including
// the rule that a run failed for state integrity is retried once and
// excluded from pass-rate aggregation — the aggregate must measure the
// subject, not the tool — and that a second such occurrence fails the test
// as an infrastructure failure.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

func passResult(n int) domain.TestResult {
	return domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: n},
		Verdict: domain.VerdictPass,
	}
}

func failResult(n int) domain.TestResult {
	return domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: n},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonAssertion},
	}
}

func stateIntegrityResult(n int) domain.TestResult {
	return domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: n},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonStateIntegrity},
	}
}

func passResultWithCost(n int, usd float64) domain.TestResult {
	r := passResult(n)
	r.Cost = domain.CostReport{TotalUSD: usd, Attribution: domain.AttributionAttributed}
	return r
}

// infrastructureResult is a TestResult as suite.runRepetition now builds it
// for a runner error: FAIL, ReasonInfrastructure, and a condition carrying
// the underlying error detail. A single occurrence is what Aggregate must act
// on: once infra runs are excluded by ExclusionOf, they are never retried a
// second time under the two-occurrence rule that applies to state-integrity.
func infrastructureResult(n int) domain.TestResult {
	return domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: n},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonInfrastructure},
		Conditions: []domain.RunCondition{
			{Kind: domain.ConditionRunNotStarted, Detail: "spawn-plan failed: exec: \"claude\": executable file not found in $PATH"},
		},
	}
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

// TestAggregate_SingleInfrastructureReasonResult_SetsInfrastructureFailure
// covers AC1.5: an infrastructure-reason result — the shape suite.runRepetition
// now builds for a runner error — must set InfrastructureFailure on a single
// occurrence, unlike the two-occurrence rule that applies to a recurring
// state-integrity fault.
func TestAggregate_SingleInfrastructureReasonResult_SetsInfrastructureFailure(t *testing.T) {
	results := []domain.TestResult{infrastructureResult(1)}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if !got.InfrastructureFailure {
		t.Error("InfrastructureFailure = false, want true — a single infrastructure-reason result must be enough, it is never retried like a state-integrity fault")
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
	if !hasReason(got.Reasons, domain.ReasonInfrastructure) {
		t.Errorf("Reasons = %+v, want ReasonInfrastructure", got.Reasons)
	}
}

// TestAggregate_InfrastructureReasonAmongOtherwisePassingRuns_StillFlagsInfrastructureFailure
// confirms an infrastructure fault on one repetition is never masked by other
// repetitions of the same test passing cleanly.
func TestAggregate_InfrastructureReasonAmongOtherwisePassingRuns_StillFlagsInfrastructureFailure(t *testing.T) {
	results := []domain.TestResult{passResult(1), infrastructureResult(2), passResult(3)}
	policy := domain.RepetitionPolicy{Repetitions: 3, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if !got.InfrastructureFailure {
		t.Error("InfrastructureFailure = false, want true — an infrastructure fault on one repetition must not be masked by the others passing")
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL — an infrastructure fault must never read as an overall pass", got.Verdict)
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

// echoMismatchResult builds an echo-mismatch-excluded TestResult analogous to
// stateIntegrityResult. The Assertions slice contains one ClassEchoFidelity
// fail entry for invocation 1, which is the minimal shape exclusionDetail
// needs to build the per-invocation detail string.
func echoMismatchResult(n int) domain.TestResult {
	return domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: n},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonEchoMismatch},
		Assertions: []domain.AssertionResult{
			{Class: domain.ClassEchoFidelity, Target: "1", Outcome: domain.AssertionFail},
		},
	}
}

// --- T1.3: Aggregate with echo-mismatch-excluded runs ---

// TestAggregate_EchoMismatchRun_ExcludedFromDenominator verifies that a run
// excluded for echo_mismatch does not enter the pass-rate denominator:
// Excluded increments and Counted does not.
func TestAggregate_EchoMismatchRun_ExcludedFromDenominator(t *testing.T) {
	results := []domain.TestResult{passResult(1), echoMismatchResult(2), passResult(3)}
	policy := domain.RepetitionPolicy{Repetitions: 3, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1 — the echo-mismatch run must be excluded", got.Excluded)
	}
	if got.Counted != 2 {
		t.Errorf("Counted = %d, want 2 — the echo-mismatch run must not enter the denominator", got.Counted)
	}
	if got.Passed != 2 {
		t.Errorf("Passed = %d, want 2", got.Passed)
	}
	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS — both counted runs passed", got.Verdict)
	}
}

// TestNeedsRetry_EchoMismatchResult_ReportsTrue verifies that NeedsRetry
// returns true for an echo-mismatch result. NeedsRetry delegates to
// ExclusionOf, so this also confirms ExclusionOf returns non-empty for
// ReasonEchoMismatch.
func TestNeedsRetry_EchoMismatchResult_ReportsTrue(t *testing.T) {
	if !evaluate.NeedsRetry(echoMismatchResult(1)) {
		t.Error("NeedsRetry(echo mismatch result) = false, want true — an echo-mismatch run must be retried like other excluded runs")
	}
}

// TestAggregate_TwoEchoMismatchRuns_SetsInfrastructureFailure verifies that
// two echo-mismatch exclusions in the same aggregate trigger InfrastructureFailure
// (same-reason recurrence rule, FR-5): the fault persisted after its single retry.
func TestAggregate_TwoEchoMismatchRuns_SetsInfrastructureFailure(t *testing.T) {
	results := []domain.TestResult{echoMismatchResult(1), echoMismatchResult(2), passResult(3)}
	policy := domain.RepetitionPolicy{Repetitions: 3, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if !got.InfrastructureFailure {
		t.Error("InfrastructureFailure = false, want true — two echo-mismatch exclusions indicate the fault recurred after its retry")
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

// TestAggregate_OneEchoMismatchAmongPassingRuns_NoInfrastructureFailure
// verifies that a single echo-mismatch exclusion among otherwise-passing runs
// does not set InfrastructureFailure: one occurrence awaiting its retry is not
// yet a confirmed infrastructure fault.
func TestAggregate_OneEchoMismatchAmongPassingRuns_NoInfrastructureFailure(t *testing.T) {
	results := []domain.TestResult{passResult(1), echoMismatchResult(2), passResult(3)}
	policy := domain.RepetitionPolicy{Repetitions: 3, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.InfrastructureFailure {
		t.Error("InfrastructureFailure = true, want false — a single echo-mismatch occurrence awaiting its retry is not yet an infrastructure failure")
	}
}

// --- T1.2: exclusionDetail for echo_mismatch (via Aggregate.Exclusions) ---

// TestAggregate_EchoMismatch_DetailListsSingleMismatchedInvocation verifies
// that when one invocation mismatched, the exclusion detail is
// "invocation N: echo mismatch" identifying the specific invocation number.
func TestAggregate_EchoMismatch_DetailListsSingleMismatchedInvocation(t *testing.T) {
	r := domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonEchoMismatch},
		Assertions: []domain.AssertionResult{
			{Class: domain.ClassEchoFidelity, Target: "2", Outcome: domain.AssertionFail},
		},
	}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0}

	got := evaluate.Aggregate([]domain.TestResult{r}, policy)

	if got.Excluded != 1 {
		t.Fatalf("Excluded = %d, want 1 — setup sanity", got.Excluded)
	}
	want := "invocation 2: echo mismatch"
	if got.Exclusions[0].Detail != want {
		t.Errorf("ExcludedRun.Detail = %q, want %q", got.Exclusions[0].Detail, want)
	}
}

// TestAggregate_EchoMismatch_DetailListsMultipleMismatchedInvocations verifies
// that when multiple invocations mismatched, the exclusion detail is a
// semicolon-separated list of per-invocation entries in the format
// "invocation 1: echo mismatch; invocation 3: echo mismatch" (FR-7).
func TestAggregate_EchoMismatch_DetailListsMultipleMismatchedInvocations(t *testing.T) {
	r := domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonEchoMismatch},
		Assertions: []domain.AssertionResult{
			{Class: domain.ClassEchoFidelity, Target: "1", Outcome: domain.AssertionFail},
			{Class: domain.ClassEchoFidelity, Target: "3", Outcome: domain.AssertionFail},
		},
	}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0}

	got := evaluate.Aggregate([]domain.TestResult{r}, policy)

	if got.Excluded != 1 {
		t.Fatalf("Excluded = %d, want 1 — setup sanity", got.Excluded)
	}
	want := "invocation 1: echo mismatch; invocation 3: echo mismatch"
	if got.Exclusions[0].Detail != want {
		t.Errorf("ExcludedRun.Detail = %q, want %q", got.Exclusions[0].Detail, want)
	}
}

// --- Infrastructure-exclusion aggregate tests ---

// TestAggregate_InfrastructureOnlyRun_ExcludedFromDenominator verifies that a
// run excluded for infrastructure does not enter the pass-rate denominator:
// Excluded increments and Counted does not.
func TestAggregate_InfrastructureOnlyRun_ExcludedFromDenominator(t *testing.T) {
	results := []domain.TestResult{infrastructureResult(1)}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1 — the infrastructure run must be excluded from the denominator", got.Excluded)
	}
	if got.Counted != 0 {
		t.Errorf("Counted = %d, want 0 — the infrastructure run must not enter the pass-rate denominator", got.Counted)
	}
}

// TestAggregate_InfrastructureOnlyRun_ExclusionReasonIsInfrastructure verifies
// that the exclusion entry for an infrastructure run carries ExclusionInfrastructure
// as its reason, so a reader knows why the run did not count without inspecting
// a sandbox.
func TestAggregate_InfrastructureOnlyRun_ExclusionReasonIsInfrastructure(t *testing.T) {
	results := []domain.TestResult{infrastructureResult(1)}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if len(got.Exclusions) != 1 {
		t.Fatalf("len(Exclusions) = %d, want 1 — setup sanity", len(got.Exclusions))
	}
	if got.Exclusions[0].Reason != domain.ExclusionInfrastructure {
		t.Errorf("Exclusions[0].Reason = %q, want %q — the exclusion entry must carry ExclusionInfrastructure so a reader knows why the run did not count", got.Exclusions[0].Reason, domain.ExclusionInfrastructure)
	}
}

// TestAggregate_InfrastructureOnlyRun_SetsInfrastructureFailure_DualMechanism
// verifies that InfrastructureFailure is set to true even when the infrastructure
// run is excluded (not counted). The dual-mechanism must be preserved: excluding
// infra runs from the denominator must not silently suppress the
// InfrastructureFailure flag that mechanism #2 (the direct hasReason check on
// counted results) used to set.
func TestAggregate_InfrastructureOnlyRun_SetsInfrastructureFailure_DualMechanism(t *testing.T) {
	results := []domain.TestResult{infrastructureResult(1)}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if !got.InfrastructureFailure {
		t.Error("InfrastructureFailure = false, want true — an infrastructure-excluded run must still set InfrastructureFailure; excluding the run from the denominator must not suppress the flag (dual-mechanism preservation)")
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL — an infrastructure failure must yield a FAIL verdict", got.Verdict)
	}
}

// TestAggregate_InfrastructureRun_ExclusionDetailContainsRunnerError verifies
// that the exclusion detail for an infrastructure run surfaces the runner error
// from the run_not_started condition, so a reader can understand what went wrong
// without inspecting the retained sandbox.
func TestAggregate_InfrastructureRun_ExclusionDetailContainsRunnerError(t *testing.T) {
	results := []domain.TestResult{infrastructureResult(1)}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if len(got.Exclusions) != 1 {
		t.Fatalf("len(Exclusions) = %d, want 1 — setup sanity", len(got.Exclusions))
	}
	detail := got.Exclusions[0].Detail
	if detail == "" {
		t.Fatal("Exclusions[0].Detail is empty, want a non-empty detail describing the runner error")
	}
	// The detail must surface the runner error text from the run_not_started condition.
	wantSubstr := "spawn-plan failed"
	if !strings.Contains(detail, wantSubstr) {
		t.Errorf("Exclusions[0].Detail = %q — want it to contain the runner error %q so a reader can diagnose the failure without sandbox inspection", detail, wantSubstr)
	}
}

// TestAggregate_InfrastructureRunAmongPassingRuns_OtherRunsStillCounted verifies
// that when an infrastructure run appears among otherwise-passing runs, the other
// runs are still counted normally and only the infrastructure run is excluded.
func TestAggregate_InfrastructureRunAmongPassingRuns_OtherRunsStillCounted(t *testing.T) {
	results := []domain.TestResult{passResult(1), infrastructureResult(2), passResult(3)}
	policy := domain.RepetitionPolicy{Repetitions: 3, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1 — only the infrastructure run must be excluded", got.Excluded)
	}
	if got.Counted != 2 {
		t.Errorf("Counted = %d, want 2 — the two passing runs must still be counted", got.Counted)
	}
	if got.Passed != 2 {
		t.Errorf("Passed = %d, want 2", got.Passed)
	}
}

// TestNeedsRetry_InfrastructureResult_ReportsTrue verifies that NeedsRetry
// returns true for an infrastructure result. NeedsRetry delegates to ExclusionOf,
// so this also confirms ExclusionOf returns non-empty for a run carrying only
// ReasonInfrastructure.
func TestNeedsRetry_InfrastructureResult_ReportsTrue(t *testing.T) {
	if !evaluate.NeedsRetry(infrastructureResult(1)) {
		t.Error("NeedsRetry(infrastructure result) = false, want true — an infrastructure-excluded run must be retried like other excluded runs")
	}
}

// TestAggregate_EchoMismatchWithAssertionFailure_DetailNotesAssertionFailures
// verifies that when ReasonAssertion is also present alongside ReasonEchoMismatch,
// the exclusion detail notes that assertion failures were also present (FR-2),
// so a reader understands the run had both infrastructure and subject faults.
func TestAggregate_EchoMismatchWithAssertionFailure_DetailNotesAssertionFailures(t *testing.T) {
	r := domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonEchoMismatch, domain.ReasonAssertion},
		Assertions: []domain.AssertionResult{
			{Class: domain.ClassEchoFidelity, Target: "1", Outcome: domain.AssertionFail},
		},
	}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0}

	got := evaluate.Aggregate([]domain.TestResult{r}, policy)

	if got.Excluded != 1 {
		t.Fatalf("Excluded = %d, want 1 — setup sanity", got.Excluded)
	}
	detail := got.Exclusions[0].Detail
	// The detail must describe the invocation mismatch.
	if detail == "" {
		t.Fatal("ExcludedRun.Detail is empty, want a non-empty detail describing the exclusion")
	}
	// The detail must also note that assertion failures were present alongside
	// the echo mismatch, so a reader is not misled into thinking the run
	// would have passed its assertions.
	if !strings.Contains(detail, "assertion") {
		t.Errorf("ExcludedRun.Detail = %q — want it to note assertion failures were also present (FR-2)", detail)
	}
}
