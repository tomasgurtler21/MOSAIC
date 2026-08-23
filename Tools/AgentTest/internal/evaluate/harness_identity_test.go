package evaluate_test

// Tests for HarnessID being carried from RunEvidence through evaluate.Evaluate
// into TestResult (TDD RED phase).
//
// evaluate.Evaluate projects domain.RunEvidence into domain.TestResult. Like
// SubjectVersion and SubjectModel before it, HarnessID must pass through
// unchanged — neither the verdict engine nor any assertion class touches it,
// and the report layer reads it from TestResult.
//
// These tests start from baseEvidence (a clean, otherwise-passing run) and
// set HarnessID, then assert that the field appears unchanged on the returned
// TestResult. They cover all verdict paths — pass, fail, timeout, and the
// spawn_failed early-return path — matching the plan's requirement that
// HarnessID is present on every run regardless of outcome.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// TestEvaluate_HarnessIDCarriedThroughOnPassVerdict asserts that when
// RunEvidence.HarnessID is non-empty and the run passes all assertions,
// the same value appears unchanged on the returned TestResult.HarnessID.
// The verdict engine must not inspect or transform this field.
func TestEvaluate_HarnessIDCarriedThroughOnPassVerdict(t *testing.T) {
	// Arrange
	ev := baseEvidence()
	const wantID = "claude-code"
	ev.HarnessID = wantID

	// Act
	result := evaluate.Evaluate(ev)

	// Assert
	if result.Verdict != domain.VerdictPass {
		t.Logf("note: verdict is %q (expected pass for baseline evidence)", result.Verdict)
	}
	if result.HarnessID != wantID {
		t.Errorf("TestResult.HarnessID = %q, want %q on pass verdict; "+
			"evaluate.Evaluate must carry HarnessID from RunEvidence into "+
			"TestResult unchanged, exactly as SubjectVersion is carried",
			result.HarnessID, wantID)
	}
}

// TestEvaluate_HarnessIDCarriedThroughOnFailVerdict asserts that HarnessID is
// carried through unchanged when the run fails an assertion. The verdict
// engine reaching the normal return path with reasons must not drop the field.
func TestEvaluate_HarnessIDCarriedThroughOnFailVerdict(t *testing.T) {
	// Arrange: make an assertion fail (wrong orchestration phase)
	ev := baseEvidence()
	ev.Orchestration.Phase = "in_progress"
	ev.Definition.Assertions.FinalPhase = strp("complete")
	const wantID = "claude-code"
	ev.HarnessID = wantID

	// Act
	result := evaluate.Evaluate(ev)

	// Assert: verify the verdict is actually FAIL so the test is meaningful
	if result.Verdict != domain.VerdictFail {
		t.Fatalf("test setup error: expected VerdictFail, got %q — "+
			"adjust evidence so an assertion fails", result.Verdict)
	}
	if result.HarnessID != wantID {
		t.Errorf("TestResult.HarnessID = %q, want %q on fail verdict; "+
			"evaluate.Evaluate must carry HarnessID through the normal return "+
			"path regardless of the verdict",
			result.HarnessID, wantID)
	}
}

// TestEvaluate_HarnessIDCarriedThroughOnTimeoutVerdict asserts that HarnessID
// is carried through when the run times out. A timed-out run follows the same
// normal-path return as a failure, reaching the final return at the bottom of
// Evaluate, so this test confirms the field is copied there.
func TestEvaluate_HarnessIDCarriedThroughOnTimeoutVerdict(t *testing.T) {
	// Arrange
	ev := baseEvidence()
	ev.SubjectResult.Disposition = domain.DispositionTimedOut
	const wantID = "claude-code"
	ev.HarnessID = wantID

	// Act
	result := evaluate.Evaluate(ev)

	// Assert: verify the verdict is actually TIMEOUT so the test is meaningful
	if result.Verdict != domain.VerdictTimeout {
		t.Fatalf("test setup error: expected VerdictTimeout, got %q", result.Verdict)
	}
	if result.HarnessID != wantID {
		t.Errorf("TestResult.HarnessID = %q, want %q on timeout verdict; "+
			"evaluate.Evaluate must carry HarnessID through the normal return "+
			"path for timed-out runs",
			result.HarnessID, wantID)
	}
}

// TestEvaluate_HarnessIDCarriedThroughOnSpawnFailedPath asserts that HarnessID
// is carried through on the early-return path for a run whose subject refused
// to start (DispositionSpawnFailed). This path returns before the main verdict
// logic; the carry-through must still happen.
func TestEvaluate_HarnessIDCarriedThroughOnSpawnFailedPath(t *testing.T) {
	// Arrange
	ev := baseEvidence()
	ev.SubjectResult = domain.SubjectResult{
		Disposition: domain.DispositionSpawnFailed,
		ExitCode:    1,
		Stderr:      "harness: exec: exit status 1",
	}
	const wantID = "claude-code"
	ev.HarnessID = wantID

	// Act
	result := evaluate.Evaluate(ev)

	// Assert: verify the spawn-failed path was taken
	if result.Verdict != domain.VerdictFail {
		t.Fatalf("test setup error: expected VerdictFail on spawn-failed path, got %q", result.Verdict)
	}
	if !hasReason(result.Reasons, domain.ReasonInfrastructure) {
		t.Fatalf("test setup error: expected ReasonInfrastructure on spawn-failed path, got %+v", result.Reasons)
	}
	if result.HarnessID != wantID {
		t.Errorf("TestResult.HarnessID = %q, want %q on spawn_failed path; "+
			"evaluate.Evaluate must carry HarnessID through the spawn-failed "+
			"early-return path, exactly as SubjectVersion is carried there",
			result.HarnessID, wantID)
	}
}

// TestEvaluate_AbsentHarnessIDCarriedAsEmpty asserts that when
// RunEvidence.HarnessID is empty — meaning no harness identity was recorded —
// TestResult.HarnessID is also empty. The "" → "unknown" display mapping
// belongs to the report layer, not the verdict engine.
func TestEvaluate_AbsentHarnessIDCarriedAsEmpty(t *testing.T) {
	// Arrange
	ev := baseEvidence()
	ev.HarnessID = "" // no harness identity recorded — a legal state

	// Act
	result := evaluate.Evaluate(ev)

	// Assert
	if result.HarnessID != "" {
		t.Errorf("TestResult.HarnessID = %q, want empty string; "+
			"evaluate.Evaluate must not transform an absent harness identity — "+
			"the '' → 'unknown' mapping is the report layer's responsibility",
			result.HarnessID)
	}
}

// TestEvaluate_HarnessIDDoesNotAffectVerdict asserts that a non-empty
// HarnessID has no effect on the verdict. The harness identifier is
// informational only; it is never a pass/fail criterion.
func TestEvaluate_HarnessIDDoesNotAffectVerdict(t *testing.T) {
	// Arrange — otherwise-passing evidence with a harness ID set
	ev := baseEvidence()
	ev.HarnessID = "claude-code"

	// Act
	result := evaluate.Evaluate(ev)

	// Assert: verdict must remain PASS; a harness ID present is not a failure
	if result.Verdict != domain.VerdictPass {
		t.Errorf("Evaluate with HarnessID set produced verdict %q, want %q; "+
			"HarnessID is informational and must never influence the verdict",
			result.Verdict, domain.VerdictPass)
	}
}
