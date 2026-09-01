package evaluate_test

// Tests for spawn-failure retry and exclusion behaviour (TDD RED phase).
//
// Behaviour specified:
//  - A run whose harness exited non-zero (DispositionSpawnFailed) must be
//    retried exactly once, using the same retry-and-exclude shape the
//    state-integrity rule already establishes.
//  - A spawn failure surviving its retry is excluded from the pass-rate
//    denominator and reported as an infrastructure failure, never as an
//    assertion or routing failure against the subject.
//  - A spawn failure whose retry succeeds leaves the repetition's final
//    verdict determined by the successful attempt, with the failed attempt
//    still counted as an exclusion.
//  - A subject that genuinely ran and failed its assertions is still scored
//    against the subject — the new rule must not mask real failures.
//  - The report shows how many runs were counted, how many excluded, and the
//    disposition and reason for each excluded run, readable from the
//    AggregateResult without sandbox inspection.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// spawnFailedResult builds a TestResult as evaluate.Evaluate produces for a
// run whose harness exited non-zero: VerdictFail with ReasonInfrastructure
// and DispositionSpawnFailed in SubjectResult. It mirrors the shape
// evaluate.Evaluate returns for that disposition — confirmed by reading
// evaluate.go — without calling Evaluate itself, so these tests are pure
// predicate and aggregation tests that do not depend on the full evaluation
// path.
func spawnFailedResult(n int) domain.TestResult {
	return domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: n},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonInfrastructure},
		SubjectResult: domain.SubjectResult{
			Disposition: domain.DispositionSpawnFailed,
			ExitCode:    1,
			Stderr:      "harness: exec: exit status 1",
		},
		TerminationReason: string(domain.DispositionSpawnFailed),
	}
}

// ---------------------------------------------------------------------------
// T16.1 — NeedsRetry for spawn-failed
// ---------------------------------------------------------------------------

// TestNeedsRetry_SpawnFailedResult_ReportsTrue verifies that a result whose
// subject's harness exited non-zero (DispositionSpawnFailed) is flagged for
// retry. A non-zero harness exit is evidence about the tool's environment,
// not about the subject, so it must be retried rather than counted.
func TestNeedsRetry_SpawnFailedResult_ReportsTrue(t *testing.T) {
	if !evaluate.NeedsRetry(spawnFailedResult(1)) {
		t.Error("NeedsRetry(spawn-failed result) = false, want true — a non-zero harness exit is an environment fault, not a subject outcome, and must be retried exactly once")
	}
}

// ---------------------------------------------------------------------------
// T16.1 — ExclusionOf
// ---------------------------------------------------------------------------

// TestExclusionOf_SpawnFailedResult_ReturnsSpawnFailedReason verifies that
// ExclusionOf reports the spawn-failed exclusion reason for a result carrying
// DispositionSpawnFailed.
func TestExclusionOf_SpawnFailedResult_ReturnsSpawnFailedReason(t *testing.T) {
	got := evaluate.ExclusionOf(spawnFailedResult(1))
	if got != domain.ExclusionSpawnFailed {
		t.Errorf("ExclusionOf(spawn-failed result) = %q, want %q", got, domain.ExclusionSpawnFailed)
	}
}

// TestExclusionOf_StateIntegrityResult_ReturnsStateIntegrityReason verifies
// that ExclusionOf still reports state-integrity for a state-integrity result.
func TestExclusionOf_StateIntegrityResult_ReturnsStateIntegrityReason(t *testing.T) {
	got := evaluate.ExclusionOf(stateIntegrityResult(1))
	if got != domain.ExclusionStateIntegrity {
		t.Errorf("ExclusionOf(state-integrity result) = %q, want %q", got, domain.ExclusionStateIntegrity)
	}
}

// TestExclusionOf_PassResult_ReturnsEmptyReason verifies that a passing run
// has no exclusion reason: it is counted, not excluded.
func TestExclusionOf_PassResult_ReturnsEmptyReason(t *testing.T) {
	got := evaluate.ExclusionOf(passResult(1))
	if got != "" {
		t.Errorf("ExclusionOf(pass result) = %q, want empty — a passing run is counted, not excluded", got)
	}
}

// TestExclusionOf_AssertionFailResult_ReturnsEmptyReason verifies that an
// ordinary assertion failure has no exclusion reason: the subject ran and
// must be scored against it.
func TestExclusionOf_AssertionFailResult_ReturnsEmptyReason(t *testing.T) {
	got := evaluate.ExclusionOf(failResult(1))
	if got != "" {
		t.Errorf("ExclusionOf(assertion failure) = %q, want empty — an assertion failure is evidence about the subject and must be counted, not excluded", got)
	}
}

// TestExclusionOf_StateIntegrityTakesPrecedenceOverSpawnFailed verifies the
// precedence rule: when a result carries both state-integrity and spawn-failed
// markers, state integrity wins so the run yields exactly one exclusion reason
// and the two are never double-counted.
func TestExclusionOf_StateIntegrityTakesPrecedenceOverSpawnFailed(t *testing.T) {
	combined := domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonStateIntegrity, domain.ReasonInfrastructure},
		SubjectResult: domain.SubjectResult{
			Disposition: domain.DispositionSpawnFailed,
			ExitCode:    1,
		},
		TerminationReason: string(domain.DispositionSpawnFailed),
	}
	got := evaluate.ExclusionOf(combined)
	if got != domain.ExclusionStateIntegrity {
		t.Errorf("ExclusionOf(state-integrity + spawn-failed result) = %q, want %q — state integrity takes precedence so the result yields exactly one exclusion reason", got, domain.ExclusionStateIntegrity)
	}
}

// TestExclusionOf_InfrastructureRunnerError_ReturnsExclusionInfrastructure
// verifies that a result from a runner error carrying ReasonInfrastructure
// (the run_not_started condition -- a runner or deploy error before the subject
// ran) is assigned ExclusionInfrastructure and excluded from the pass-rate
// denominator. Evidence from a run that never started is evidence about the
// infrastructure, not the subject.
func TestExclusionOf_InfrastructureRunnerError_ReturnsExclusionInfrastructure(t *testing.T) {
	got := evaluate.ExclusionOf(infrastructureResult(1))
	if got != domain.ExclusionInfrastructure {
		t.Errorf("ExclusionOf(infrastructure runner-error result) = %q, want %q — a run that never started must be excluded from the pass-rate denominator via ExclusionInfrastructure", got, domain.ExclusionInfrastructure)
	}
}

// TestNeedsRetry_ExclusionOfSymmetry_NeedsRetryAndExclusionOfAgree verifies
// the declared contract: NeedsRetry(r) is true exactly when ExclusionOf(r)
// is non-empty.
func TestNeedsRetry_ExclusionOfSymmetry_NeedsRetryAndExclusionOfAgree(t *testing.T) {
	cases := []struct {
		name   string
		result domain.TestResult
	}{
		{"pass", passResult(1)},
		{"assertion fail", failResult(1)},
		{"state integrity", stateIntegrityResult(1)},
		{"spawn failed", spawnFailedResult(1)},
		{"infrastructure", infrastructureResult(1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			needsRetry := evaluate.NeedsRetry(tc.result)
			hasExclusion := evaluate.ExclusionOf(tc.result) != ""
			if needsRetry != hasExclusion {
				t.Errorf("NeedsRetry = %v but ExclusionOf non-empty = %v for %q: NeedsRetry must be true exactly when ExclusionOf is non-empty", needsRetry, hasExclusion, tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T16.2 — Spawn-failed excluded from denominator; InfrastructureFailure
// ---------------------------------------------------------------------------

// TestAggregate_SpawnFailedRun_ExcludedFromDenominator verifies that a run
// whose harness exited non-zero is excluded from the pass-rate denominator:
// the aggregate must measure the subject, not the tool.
func TestAggregate_SpawnFailedRun_ExcludedFromDenominator(t *testing.T) {
	results := []domain.TestResult{passResult(1), spawnFailedResult(2), passResult(3)}
	policy := domain.RepetitionPolicy{Repetitions: 3, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1 — the spawn-failed run must not enter the denominator", got.Excluded)
	}
	if got.Counted != 2 {
		t.Errorf("Counted = %d, want 2 — only the two non-excluded runs count", got.Counted)
	}
	if got.Passed != 2 {
		t.Errorf("Passed = %d, want 2", got.Passed)
	}
	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS — both counted runs passed", got.Verdict)
	}
	if got.InfrastructureFailure {
		t.Error("InfrastructureFailure = true, want false — a single spawn-failure occurrence is retried, not yet a recurring fault")
	}
}

// TestAggregate_SpawnFailedRecursAfterRetry_IsInfrastructureFailure verifies
// that a spawn failure that recurs after its single retry is reported as an
// infrastructure failure: the same reason recurring twice signals the tool's
// environment is broken.
func TestAggregate_SpawnFailedRecursAfterRetry_IsInfrastructureFailure(t *testing.T) {
	results := []domain.TestResult{spawnFailedResult(1), spawnFailedResult(2)}
	policy := domain.RepetitionPolicy{Repetitions: 2, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.Excluded != 2 {
		t.Errorf("Excluded = %d, want 2 — both attempts failed with spawn-failed and must be excluded", got.Excluded)
	}
	if got.Counted != 0 {
		t.Errorf("Counted = %d, want 0 — no run was successfully evaluated", got.Counted)
	}
	if !got.InfrastructureFailure {
		t.Error("InfrastructureFailure = false, want true — a spawn-failure that recurs after its retry is evidence the tool's environment is broken")
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
	if !hasReason(got.Reasons, domain.ReasonInfrastructure) {
		t.Errorf("Reasons = %+v, want ReasonInfrastructure — a recurring spawn-failure is a tool fault, not a subject regression", got.Reasons)
	}
}

// TestAggregate_SpawnFailedPlusStateIntegrity_DifferentReasons_NoInfrastructureFailure
// verifies the same-reason recurrence rule: one spawn-failure plus one
// state-integrity fault are different reasons. Neither recurred after its
// own retry, so InfrastructureFailure must not be set.
func TestAggregate_SpawnFailedPlusStateIntegrity_DifferentReasons_NoInfrastructureFailure(t *testing.T) {
	results := []domain.TestResult{spawnFailedResult(1), stateIntegrityResult(2)}
	policy := domain.RepetitionPolicy{Repetitions: 2, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.Excluded != 2 {
		t.Errorf("Excluded = %d, want 2 — both runs were excluded (different reasons)", got.Excluded)
	}
	if got.InfrastructureFailure {
		t.Error("InfrastructureFailure = true, want false — one spawn-failure plus one state-integrity fault are different exclusion reasons; neither recurred after its retry")
	}
}

// ---------------------------------------------------------------------------
// T16.3 — Spawn-failed retry succeeds
// ---------------------------------------------------------------------------

// TestAggregate_SpawnFailedThenSuccessfulRetry_ExcludesFailedCountsPass
// verifies that when a spawn failure's retry succeeds, the repetition's
// verdict is determined by the successful attempt: one excluded, one counted,
// aggregate PASS.
func TestAggregate_SpawnFailedThenSuccessfulRetry_ExcludesFailedCountsPass(t *testing.T) {
	results := []domain.TestResult{spawnFailedResult(1), passResult(2)}
	policy := domain.RepetitionPolicy{Repetitions: 2, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1 — the first spawn-failed attempt is excluded", got.Excluded)
	}
	if got.Counted != 1 {
		t.Errorf("Counted = %d, want 1 — the successful retry is counted toward the pass rate", got.Counted)
	}
	if got.Passed != 1 {
		t.Errorf("Passed = %d, want 1 — the retry passed", got.Passed)
	}
	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS — the retry's clean run determines the final verdict", got.Verdict)
	}
	if got.InfrastructureFailure {
		t.Error("InfrastructureFailure = true, want false — a spawn-failure followed by a successful retry is a recovery, not an infrastructure failure")
	}
}

// ---------------------------------------------------------------------------
// T16.4 — Non-masking of genuine subject failures
// ---------------------------------------------------------------------------

// TestEvaluate_GenuineAssertionFailure_NotExcluded verifies that a subject
// that ran and failed its assertions is still scored against the subject:
// ExclusionOf must return empty for an ordinary assertion failure. The new
// spawn-failure rule must not mask real failures.
func TestEvaluate_GenuineAssertionFailure_NotExcluded(t *testing.T) {
	got := evaluate.ExclusionOf(failResult(1))
	if got != "" {
		t.Errorf("ExclusionOf(assertion failure) = %q, want empty — a subject that ran and failed its assertions must be scored against the subject, not excluded as infrastructure", got)
	}
}

// TestEvaluate_GenuineAssertionFailure_NotMasked verifies that evaluating
// evidence from a run that completed and failed its assertions yields
// ReasonAssertion, not ReasonInfrastructure, so the new spawn-failure rule
// does not interfere with ordinary failure scoring.
func TestEvaluate_GenuineAssertionFailure_NotMasked(t *testing.T) {
	ev := baseEvidence()
	want := "REJECTED"
	ev.Definition.Assertions.FinalStatus = &want
	ev.Orchestration = domain.OrchestrationState{Present: true, LastStatus: "COMPLETED"}
	ev.SubjectResult = domain.SubjectResult{Disposition: domain.DispositionCompleted}

	result := evaluate.Evaluate(ev)

	if !hasReason(result.Reasons, domain.ReasonAssertion) {
		t.Errorf("Reasons = %+v, want ReasonAssertion — a subject that ran and failed must be scored with an assertion reason", result.Reasons)
	}
	if hasReason(result.Reasons, domain.ReasonInfrastructure) {
		t.Errorf("Reasons = %+v, want no ReasonInfrastructure — an assertion failure must not be reported as an infrastructure exclusion", result.Reasons)
	}
	exclusion := evaluate.ExclusionOf(result)
	if exclusion != "" {
		t.Errorf("ExclusionOf(genuine assertion failure result) = %q, want empty — the result must be counted, not excluded", exclusion)
	}
}

// TestEvaluate_SpawnFailedDisposition_ReportsInfrastructureNotAssertion
// verifies that a run whose subject's harness exited non-zero carries
// ReasonInfrastructure and no assertion or routing reason: the spawn-failure
// rule must never produce a false assertion failure.
func TestEvaluate_SpawnFailedDisposition_ReportsInfrastructureNotAssertion(t *testing.T) {
	ev := baseEvidence()
	ev.SubjectResult = domain.SubjectResult{
		Disposition: domain.DispositionSpawnFailed,
		ExitCode:    1,
		Stderr:      "harness: failed to start: exec error",
	}

	got := evaluate.Evaluate(ev)

	if hasReason(got.Reasons, domain.ReasonAssertion) {
		t.Errorf("Reasons = %+v, want no ReasonAssertion — a spawn failure must not produce a false assertion failure", got.Reasons)
	}
	if !hasReason(got.Reasons, domain.ReasonInfrastructure) {
		t.Errorf("Reasons = %+v, want ReasonInfrastructure — a spawn failure is an infrastructure fault", got.Reasons)
	}
}

// ---------------------------------------------------------------------------
// T16.5 — Excluded-run visibility: Exclusions populated in AggregateResult
// ---------------------------------------------------------------------------

// TestAggregate_Exclusions_EmptySliceNotNull_WhenNoExclusions verifies that
// when no runs were excluded, Exclusions is an initialized empty slice rather
// than nil. This is the additive-only wire invariant: every collection renders
// as [] rather than null.
func TestAggregate_Exclusions_EmptySliceNotNull_WhenNoExclusions(t *testing.T) {
	results := []domain.TestResult{passResult(1)}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.Exclusions == nil {
		t.Error("Exclusions is nil, want an initialized empty slice — the wire must render [] rather than null")
	}
	if len(got.Exclusions) != 0 {
		t.Errorf("len(Exclusions) = %d, want 0 — no runs were excluded", len(got.Exclusions))
	}
}

// TestAggregate_Exclusions_LengthAlwaysEqualsExcluded verifies the invariant
// len(Exclusions) == Excluded so a reader can verify the count by inspecting
// the slice.
func TestAggregate_Exclusions_LengthAlwaysEqualsExcluded(t *testing.T) {
	cases := []struct {
		name    string
		results []domain.TestResult
	}{
		{"no exclusions", []domain.TestResult{passResult(1)}},
		{"one spawn-failed", []domain.TestResult{spawnFailedResult(1), passResult(2)}},
		{"one state-integrity", []domain.TestResult{stateIntegrityResult(1)}},
		{"spawn-failed + state-integrity", []domain.TestResult{spawnFailedResult(1), stateIntegrityResult(2)}},
		{"two spawn-failed", []domain.TestResult{spawnFailedResult(1), spawnFailedResult(2)}},
	}
	policy := domain.RepetitionPolicy{Repetitions: 2, PassRate: 0.5}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluate.Aggregate(tc.results, policy)
			if len(got.Exclusions) != got.Excluded {
				t.Errorf("len(Exclusions) = %d, Excluded = %d: the invariant len(Exclusions) == Excluded must hold", len(got.Exclusions), got.Excluded)
			}
		})
	}
}

// TestAggregate_Exclusions_SpawnFailedCarriesReason verifies that an excluded
// spawn-failed run's ExcludedRun carries ExclusionSpawnFailed as its reason,
// so a reader knows why the run did not count without inspecting a sandbox.
func TestAggregate_Exclusions_SpawnFailedCarriesReason(t *testing.T) {
	results := []domain.TestResult{spawnFailedResult(1), passResult(2)}
	policy := domain.RepetitionPolicy{Repetitions: 2, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if len(got.Exclusions) != 1 {
		t.Fatalf("len(Exclusions) = %d, want 1", len(got.Exclusions))
	}
	if got.Exclusions[0].Reason != domain.ExclusionSpawnFailed {
		t.Errorf("Exclusions[0].Reason = %q, want %q", got.Exclusions[0].Reason, domain.ExclusionSpawnFailed)
	}
}

// TestAggregate_Exclusions_SpawnFailedCarriesTerminationReason verifies that
// an excluded run's TerminationReason carries the subject disposition so the
// report can surface it without requiring a sandbox inspection.
func TestAggregate_Exclusions_SpawnFailedCarriesTerminationReason(t *testing.T) {
	results := []domain.TestResult{spawnFailedResult(1)}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if len(got.Exclusions) != 1 {
		t.Fatalf("len(Exclusions) = %d, want 1", len(got.Exclusions))
	}
	if got.Exclusions[0].TerminationReason != string(domain.DispositionSpawnFailed) {
		t.Errorf("Exclusions[0].TerminationReason = %q, want %q — the disposition must be carried for readable exclusion reports", got.Exclusions[0].TerminationReason, string(domain.DispositionSpawnFailed))
	}
}

// TestAggregate_Exclusions_SpawnFailedDetailNonEmpty verifies that an excluded
// run's Detail is never empty: a reader must be able to understand why a run
// was excluded from the report alone, without inspecting a retained sandbox.
func TestAggregate_Exclusions_SpawnFailedDetailNonEmpty(t *testing.T) {
	results := []domain.TestResult{spawnFailedResult(1)}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if len(got.Exclusions) != 1 {
		t.Fatalf("len(Exclusions) = %d, want 1", len(got.Exclusions))
	}
	if got.Exclusions[0].Detail == "" {
		t.Error("Exclusions[0].Detail is empty, want non-empty — the detail must explain the exclusion without requiring sandbox inspection")
	}
}

// TestAggregate_Exclusions_StateIntegrityCarriesReason verifies that the
// existing state-integrity exclusion also populates the Exclusions slice with
// ExclusionStateIntegrity, not only the Excluded counter.
func TestAggregate_Exclusions_StateIntegrityCarriesReason(t *testing.T) {
	results := []domain.TestResult{stateIntegrityResult(1)}
	policy := domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if len(got.Exclusions) != 1 {
		t.Fatalf("len(Exclusions) = %d, want 1", len(got.Exclusions))
	}
	if got.Exclusions[0].Reason != domain.ExclusionStateIntegrity {
		t.Errorf("Exclusions[0].Reason = %q, want %q", got.Exclusions[0].Reason, domain.ExclusionStateIntegrity)
	}
}

// TestAggregate_Exclusions_PreservesRunKeyForIdentification verifies that the
// excluded run's Key is carried through, so the report can identify which run
// was excluded without requiring a sandbox inspection.
func TestAggregate_Exclusions_PreservesRunKeyForIdentification(t *testing.T) {
	key := domain.RunKey{RunID: "20260807T120000Z-0001", TestName: "my-test", RunNumber: 2}
	sf := spawnFailedResult(2)
	sf.Key = key

	got := evaluate.Aggregate([]domain.TestResult{sf}, domain.RepetitionPolicy{Repetitions: 1, PassRate: 1.0})

	if len(got.Exclusions) != 1 {
		t.Fatalf("len(Exclusions) = %d, want 1", len(got.Exclusions))
	}
	if got.Exclusions[0].Key != key {
		t.Errorf("Exclusions[0].Key = %+v, want %+v — the run key must be carried through for identification", got.Exclusions[0].Key, key)
	}
}

// TestAggregate_Exclusions_OrderedByAttempt verifies that Exclusions is
// ordered in the same order the runs were attempted: the first excluded run
// appears first in the slice.
func TestAggregate_Exclusions_OrderedByAttempt(t *testing.T) {
	first := spawnFailedResult(1)
	first.Key = domain.RunKey{RunID: "run-first", TestName: "example", RunNumber: 1}
	second := stateIntegrityResult(2)
	second.Key = domain.RunKey{RunID: "run-second", TestName: "example", RunNumber: 2}

	got := evaluate.Aggregate([]domain.TestResult{first, second}, domain.RepetitionPolicy{Repetitions: 2, PassRate: 0.5})

	if len(got.Exclusions) != 2 {
		t.Fatalf("len(Exclusions) = %d, want 2", len(got.Exclusions))
	}
	if got.Exclusions[0].Key.RunID != "run-first" {
		t.Errorf("Exclusions[0].Key.RunID = %q, want %q — exclusions must be ordered by attempt order", got.Exclusions[0].Key.RunID, "run-first")
	}
	if got.Exclusions[1].Key.RunID != "run-second" {
		t.Errorf("Exclusions[1].Key.RunID = %q, want %q", got.Exclusions[1].Key.RunID, "run-second")
	}
}
