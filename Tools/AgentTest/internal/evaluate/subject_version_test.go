package evaluate_test

// Tests for SubjectVersion being carried from RunEvidence through
// evaluate.Evaluate into TestResult (TDD RED phase).
//
// evaluate.Evaluate projects domain.RunEvidence into domain.TestResult. Like
// RetainedSandboxPath before it, SubjectVersion must pass through unchanged —
// neither the verdict engine nor any assertion class touches it, and the
// report layer reads it from TestResult.
//
// These tests start from baseEvidence (a clean, otherwise-passing run) and
// set SubjectVersion, then assert that the field appears unchanged on the
// returned TestResult. They say nothing about which assertions fired or what
// the verdict was — only the field carry-through is exercised here.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// TestEvaluate_SubjectVersionCarriedThroughToTestResult asserts that when
// RunEvidence.SubjectVersion is non-empty, the same value appears on the
// returned TestResult.SubjectVersion. The verdict engine must not inspect or
// transform this field.
func TestEvaluate_SubjectVersionCarriedThroughToTestResult(t *testing.T) {
	// Arrange
	ev := baseEvidence()
	const wantVersion = "v1.4.2"
	ev.SubjectVersion = wantVersion

	// Act
	result := evaluate.Evaluate(ev)

	// Assert
	if result.SubjectVersion != wantVersion {
		t.Errorf("TestResult.SubjectVersion = %q, want %q; "+
			"evaluate.Evaluate must carry SubjectVersion from RunEvidence into "+
			"TestResult unchanged, exactly as RetainedSandboxPath is carried",
			result.SubjectVersion, wantVersion)
	}
}

// TestEvaluate_AbsentSubjectVersionCarriedAsEmpty asserts that when
// RunEvidence.SubjectVersion is empty — meaning the source declared no version
// — TestResult.SubjectVersion is also empty. The "" → "unknown" display
// mapping belongs to the report layer, not the verdict engine.
func TestEvaluate_AbsentSubjectVersionCarriedAsEmpty(t *testing.T) {
	// Arrange
	ev := baseEvidence()
	ev.SubjectVersion = "" // source declared no version — a legal state

	// Act
	result := evaluate.Evaluate(ev)

	// Assert
	if result.SubjectVersion != "" {
		t.Errorf("TestResult.SubjectVersion = %q, want empty string; "+
			"evaluate.Evaluate must not transform an absent version — "+
			"that is the report layer's responsibility",
			result.SubjectVersion)
	}
}

// TestEvaluate_SubjectVersionDoesNotAffectVerdict asserts that a non-empty
// SubjectVersion has no effect on the verdict. The subject version is
// informational only; it is never a pass/fail criterion.
func TestEvaluate_SubjectVersionDoesNotAffectVerdict(t *testing.T) {
	// Arrange — otherwise-passing evidence with a version set
	ev := baseEvidence()
	ev.SubjectVersion = "v9.9.9"

	// Act
	result := evaluate.Evaluate(ev)

	// Assert: verdict must remain PASS; a version present is not a failure
	if result.Verdict != domain.VerdictPass {
		t.Errorf("Evaluate with SubjectVersion set produced verdict %q, want %q; "+
			"SubjectVersion is informational and must never influence the verdict",
			result.Verdict, domain.VerdictPass)
	}
}
