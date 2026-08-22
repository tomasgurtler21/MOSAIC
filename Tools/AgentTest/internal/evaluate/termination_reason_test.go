package evaluate_test

// Tests for TerminationReason being plumbed from RunEvidence.SubjectResult.Disposition
// through evaluate.Evaluate into TestResult.
//
// The evaluate layer must carry the disposition string through to
// TestResult.TerminationReason unchanged, without interpreting it, so the
// report layer can surface why a run ended without re-reading the evidence.
// This follows the same carry-through pattern as SubjectVersion,
// RetainedSandboxPath and SubjectModel.
//
// These tests start from baseEvidence and set SubjectResult.Disposition to a
// known value, then assert TestResult.TerminationReason matches the string
// form of that disposition. The tests say nothing about assertions, verdicts
// or conditions — only the field plumbing is exercised here.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// TestEvaluate_TerminationReasonCarriedFromDisposition asserts that when
// RunEvidence.SubjectResult.Disposition is set to a known value, the returned
// TestResult.TerminationReason contains the same string. The evaluate layer
// must not interpret, translate, or drop the disposition.
func TestEvaluate_TerminationReasonCarriedFromDisposition(t *testing.T) {
	for _, d := range []domain.RunDisposition{
		domain.DispositionCompleted,
		domain.DispositionEarlyExit,
		domain.DispositionTimedOut,
		domain.DispositionTurnLimit,
		domain.DispositionSpawnFailed,
	} {
		d := d
		t.Run(string(d), func(t *testing.T) {
			ev := baseEvidence()
			ev.SubjectResult.Disposition = d

			result := evaluate.Evaluate(ev)

			if result.TerminationReason != string(d) {
				t.Errorf("TestResult.TerminationReason = %q, want %q; "+
					"evaluate.Evaluate must carry SubjectResult.Disposition as a string "+
					"into TestResult.TerminationReason unchanged",
					result.TerminationReason, string(d))
			}
		})
	}
}

// TestEvaluate_TerminationReasonEmptyWhenDispositionEmpty asserts that when
// RunEvidence.SubjectResult.Disposition is the zero value (empty string),
// TestResult.TerminationReason is also empty. The "" → "unknown" display
// mapping belongs to the report layer, not the evaluate layer.
func TestEvaluate_TerminationReasonEmptyWhenDispositionEmpty(t *testing.T) {
	ev := baseEvidence()
	ev.SubjectResult.Disposition = "" // zero value — disposition not recorded

	result := evaluate.Evaluate(ev)

	if result.TerminationReason != "" {
		t.Errorf("TestResult.TerminationReason = %q, want empty string; "+
			"evaluate.Evaluate must not map an absent disposition to a display value — "+
			"that is the report layer's responsibility",
			result.TerminationReason)
	}
}

// TestEvaluate_TerminationReasonDoesNotAffectVerdict asserts that setting a
// non-empty Disposition on SubjectResult (other than DispositionSpawnFailed,
// which has its own verdict path) does not change the verdict of an otherwise
// passing run. TerminationReason is informational; it is not a pass/fail
// criterion.
func TestEvaluate_TerminationReasonDoesNotAffectVerdict(t *testing.T) {
	for _, d := range []domain.RunDisposition{
		domain.DispositionCompleted,
		domain.DispositionEarlyExit,
	} {
		d := d
		t.Run(string(d), func(t *testing.T) {
			ev := baseEvidence()
			ev.SubjectResult.Disposition = d

			result := evaluate.Evaluate(ev)

			if result.Verdict != domain.VerdictPass {
				t.Errorf("Evaluate with Disposition %q produced verdict %q, want %q; "+
					"TerminationReason is informational and must never influence the verdict",
					d, result.Verdict, domain.VerdictPass)
			}
		})
	}
}
