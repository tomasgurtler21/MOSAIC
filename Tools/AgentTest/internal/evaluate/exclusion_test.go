package evaluate_test

// Tests for ExclusionOf recognizing echo_mismatch as a third exclusion reason,
// and for the precedence ordering state_integrity > spawn_failed > echo_mismatch.
//
// These tests drive the TDD RED phase: all cases below fail until ExclusionOf
// grows an echo-mismatch branch in evaluate/exclusion.go.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// TestExclusionOf_EchoMismatch_ReturnsExclusionEchoMismatch verifies that a
// run carrying only ReasonEchoMismatch is assigned the echo_mismatch exclusion
// reason, removing it from the pass-rate denominator.
func TestExclusionOf_EchoMismatch_ReturnsExclusionEchoMismatch(t *testing.T) {
	r := domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonEchoMismatch},
	}

	got := evaluate.ExclusionOf(r)

	if got != domain.ExclusionEchoMismatch {
		t.Errorf("ExclusionOf(echo mismatch run) = %q, want %q", got, domain.ExclusionEchoMismatch)
	}
}

// TestExclusionOf_EchoMismatchWithAssertionFailure_ReturnsExclusionEchoMismatch
// verifies that when both ReasonEchoMismatch and ReasonAssertion are present,
// the run is still excluded for echo_mismatch (FR-2): a co-occurring assertion
// failure does not change the infrastructure nature of the echo fault.
func TestExclusionOf_EchoMismatchWithAssertionFailure_ReturnsExclusionEchoMismatch(t *testing.T) {
	r := domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonEchoMismatch, domain.ReasonAssertion},
	}

	got := evaluate.ExclusionOf(r)

	if got != domain.ExclusionEchoMismatch {
		t.Errorf("ExclusionOf(echo mismatch + assertion run) = %q, want %q — echo mismatch must remain the exclusion reason even when an assertion also failed", got, domain.ExclusionEchoMismatch)
	}
}

// TestExclusionOf_StateIntegrityPrecedesEchoMismatch verifies the precedence
// ordering state_integrity > echo_mismatch (FR-3): when both reasons are
// present, the run is excluded for state_integrity.
func TestExclusionOf_StateIntegrityPrecedesEchoMismatch(t *testing.T) {
	r := domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonStateIntegrity, domain.ReasonEchoMismatch},
	}

	got := evaluate.ExclusionOf(r)

	if got != domain.ExclusionStateIntegrity {
		t.Errorf("ExclusionOf(state_integrity + echo mismatch) = %q, want %q — state_integrity must precede echo_mismatch in the exclusion precedence chain", got, domain.ExclusionStateIntegrity)
	}
}

// TestExclusionOf_SpawnFailedPrecedesEchoMismatch verifies the precedence
// ordering spawn_failed > echo_mismatch (FR-3): when the run's disposition is
// spawn_failed and ReasonEchoMismatch is also present, the run is excluded for
// spawn_failed.
func TestExclusionOf_SpawnFailedPrecedesEchoMismatch(t *testing.T) {
	r := domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonEchoMismatch},
		SubjectResult: domain.SubjectResult{
			Disposition: domain.DispositionSpawnFailed,
		},
	}

	got := evaluate.ExclusionOf(r)

	if got != domain.ExclusionSpawnFailed {
		t.Errorf("ExclusionOf(spawn_failed disposition + echo mismatch reason) = %q, want %q — spawn_failed must precede echo_mismatch in the exclusion precedence chain", got, domain.ExclusionSpawnFailed)
	}
}

// TestExclusionOf_AdvisoryEchoMode_NoEchoMismatchReason_ReturnsEmpty verifies
// that ExclusionOf does not independently detect echo mismatch by any means
// other than the presence of ReasonEchoMismatch in r.Reasons. In advisory
// mode the upstream evaluator omits the reason, so a run without it must not
// be excluded for echo mismatch (FR-9/FR-10).
func TestExclusionOf_AdvisoryEchoMode_NoEchoMismatchReason_ReturnsEmpty(t *testing.T) {
	// Advisory mode upstream: ReasonEchoMismatch was not added.
	// The run has no failure reasons; it passed despite an advisory echo difference.
	r := domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1},
		Verdict: domain.VerdictPass,
		Reasons: nil,
	}

	got := evaluate.ExclusionOf(r)

	if got != "" {
		t.Errorf("ExclusionOf(no ReasonEchoMismatch in reasons) = %q, want empty — ExclusionOf must not detect echo mismatch independently of r.Reasons", got)
	}
}

// --- Infrastructure exclusion ---

// TestExclusionOf_InfrastructureOnly_ReturnsExclusionInfrastructure verifies that
// a run whose only failure reason is ReasonInfrastructure (the run_not_started
// condition -- a runner error or recovered panic before the subject ran) is assigned
// ExclusionInfrastructure, removing it from the pass-rate denominator. Evidence
// from a run that never started is evidence about the infrastructure, not the subject.
func TestExclusionOf_InfrastructureOnly_ReturnsExclusionInfrastructure(t *testing.T) {
	r := domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonInfrastructure},
		Conditions: []domain.RunCondition{
			{Kind: domain.ConditionRunNotStarted, Detail: "spawn-plan failed: exec: \"claude\": executable file not found in $PATH"},
		},
	}

	got := evaluate.ExclusionOf(r)

	if got != domain.ExclusionInfrastructure {
		t.Errorf("ExclusionOf(infrastructure-only run) = %q, want %q — a run that never started carries evidence about the infrastructure, not the subject, and must be excluded from the pass-rate denominator", got, domain.ExclusionInfrastructure)
	}
}

// TestExclusionOf_EchoMismatchPrecedesInfrastructure verifies the precedence
// ordering echo_mismatch > infrastructure: when a run carries both ReasonEchoMismatch
// and ReasonInfrastructure, the echo_mismatch exclusion takes effect so the run
// yields exactly one exclusion reason.
func TestExclusionOf_EchoMismatchPrecedesInfrastructure(t *testing.T) {
	r := domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonEchoMismatch, domain.ReasonInfrastructure},
	}

	got := evaluate.ExclusionOf(r)

	if got != domain.ExclusionEchoMismatch {
		t.Errorf("ExclusionOf(echo_mismatch + infrastructure) = %q, want %q — echo_mismatch must precede infrastructure in the exclusion precedence chain", got, domain.ExclusionEchoMismatch)
	}
}

// TestExclusionOf_StateIntegrityPrecedesInfrastructure verifies the precedence
// ordering state_integrity > infrastructure: when a run carries both
// ReasonStateIntegrity and ReasonInfrastructure, state_integrity exclusion takes
// effect.
func TestExclusionOf_StateIntegrityPrecedesInfrastructure(t *testing.T) {
	r := domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonStateIntegrity, domain.ReasonInfrastructure},
	}

	got := evaluate.ExclusionOf(r)

	if got != domain.ExclusionStateIntegrity {
		t.Errorf("ExclusionOf(state_integrity + infrastructure) = %q, want %q — state_integrity must precede infrastructure in the exclusion precedence chain", got, domain.ExclusionStateIntegrity)
	}
}

// TestExclusionOf_SpawnFailedPrecedesInfrastructure verifies the precedence
// ordering spawn_failed > infrastructure: when a run carries DispositionSpawnFailed
// alongside ReasonInfrastructure, the spawn_failed exclusion takes effect.
func TestExclusionOf_SpawnFailedPrecedesInfrastructure(t *testing.T) {
	r := domain.TestResult{
		Key:     domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonInfrastructure},
		SubjectResult: domain.SubjectResult{
			Disposition: domain.DispositionSpawnFailed,
		},
	}

	got := evaluate.ExclusionOf(r)

	if got != domain.ExclusionSpawnFailed {
		t.Errorf("ExclusionOf(spawn_failed disposition + infrastructure reason) = %q, want %q — spawn_failed must precede infrastructure in the exclusion precedence chain", got, domain.ExclusionSpawnFailed)
	}
}
