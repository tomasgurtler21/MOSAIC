package evaluate_test

// Tests for the echo_fidelity mode field on RunSettings: whether an echo
// mismatch blocks the verdict ("required") or is only advisory ("advisory").
//
// Echo fidelity is always evaluated and always appears in assertion results
// regardless of the mode — only the verdict impact differs.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// evidenceWithEchoMismatch returns otherwise-passing evidence containing one
// mismatched echo, with all declared assertions satisfied, isolating the
// echo-fidelity mode as the only variable.
func evidenceWithEchoMismatch() domain.RunEvidence {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1")),
		mismatchedEndRecord(1, 1, researcher(), start.Add(time.Second)),
	}
	// Satisfy all declared assertions so the only failure source is the echo.
	ev.Definition.Assertions.FinalPhase = strp("complete")
	ev.Definition.Assertions.FinalStatus = strp("SUCCESS")
	return ev
}

// TestEvaluate_EchoFidelityMode_RequiredExplicit_MismatchFails verifies that
// explicitly setting echo_fidelity to "required" preserves the current
// behavior: a mismatch causes FAIL with ReasonEchoMismatch.
func TestEvaluate_EchoFidelityMode_RequiredExplicit_MismatchFails(t *testing.T) {
	ev := evidenceWithEchoMismatch()
	mode := domain.EchoFidelityRequired
	ev.Definition.Settings.EchoFidelity = &mode

	got := evaluate.Evaluate(ev)

	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL when echo_fidelity=required and echo mismatches", got.Verdict)
	}
	if !hasReason(got.Reasons, domain.ReasonEchoMismatch) {
		t.Errorf("Reasons = %+v, want ReasonEchoMismatch when echo_fidelity=required", got.Reasons)
	}
}

// TestEvaluate_EchoFidelityMode_DefaultNil_MismatchFails verifies that when
// echo_fidelity is absent (nil), the behavior is identical to "required".
func TestEvaluate_EchoFidelityMode_DefaultNil_MismatchFails(t *testing.T) {
	ev := evidenceWithEchoMismatch()
	// EchoFidelity is nil by default — no explicit assignment.
	if ev.Definition.Settings.EchoFidelity != nil {
		t.Fatal("test setup error: expected EchoFidelity to be nil by default")
	}

	got := evaluate.Evaluate(ev)

	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL when echo_fidelity is absent and echo mismatches", got.Verdict)
	}
	if !hasReason(got.Reasons, domain.ReasonEchoMismatch) {
		t.Errorf("Reasons = %+v, want ReasonEchoMismatch when echo_fidelity is absent", got.Reasons)
	}
}

// TestEvaluate_EchoFidelityMode_Advisory_MismatchDoesNotBlockVerdict verifies
// that advisory mode allows PASS even when there is an echo mismatch.
func TestEvaluate_EchoFidelityMode_Advisory_MismatchDoesNotBlockVerdict(t *testing.T) {
	ev := evidenceWithEchoMismatch()
	mode := domain.EchoFidelityAdvisory
	ev.Definition.Settings.EchoFidelity = &mode

	got := evaluate.Evaluate(ev)

	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS when echo_fidelity=advisory and only echo mismatches", got.Verdict)
	}
	if hasReason(got.Reasons, domain.ReasonEchoMismatch) {
		t.Errorf("Reasons = %+v, must not contain ReasonEchoMismatch when echo_fidelity=advisory", got.Reasons)
	}
}

// TestEvaluate_EchoFidelityMode_Advisory_EchoStillEvaluatedAndReported verifies
// that advisory mode does not suppress echo fidelity from assertion results —
// the result appears with its actual outcome, only the verdict is unaffected.
func TestEvaluate_EchoFidelityMode_Advisory_EchoStillEvaluatedAndReported(t *testing.T) {
	ev := evidenceWithEchoMismatch()
	mode := domain.EchoFidelityAdvisory
	ev.Definition.Settings.EchoFidelity = &mode

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassEchoFidelity, "1")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("echo assertion Outcome = %q, want Fail — advisory mode must not suppress the echo mismatch result", ar.Outcome)
	}
}

// TestEvaluate_EchoFidelityMode_Advisory_AdditionalAssertionFailureStillFails
// verifies that advisory mode only softens the echo fidelity impact. If other
// assertions fail, the verdict is still FAIL.
func TestEvaluate_EchoFidelityMode_Advisory_AdditionalAssertionFailureStillFails(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1")),
		mismatchedEndRecord(1, 1, researcher(), start.Add(time.Second)),
	}
	// Declare a non-echo assertion that will not be satisfied.
	ev.Definition.Assertions.FinalPhase = strp("complete")
	ev.Orchestration.Phase = "in_progress" // phase does not match
	mode := domain.EchoFidelityAdvisory
	ev.Definition.Settings.EchoFidelity = &mode

	got := evaluate.Evaluate(ev)

	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL — advisory echo mode does not absorb other assertion failures", got.Verdict)
	}
	if !hasReason(got.Reasons, domain.ReasonAssertion) {
		t.Errorf("Reasons = %+v, want ReasonAssertion from the non-echo assertion failure", got.Reasons)
	}
	if hasReason(got.Reasons, domain.ReasonEchoMismatch) {
		t.Errorf("Reasons = %+v, must not contain ReasonEchoMismatch when echo_fidelity=advisory", got.Reasons)
	}
}

// TestEvaluate_EchoFidelityMode_NegativeTrue_Required_EchoNotInverted verifies
// that negative:true does not invert the echo fidelity assertion in required mode.
// A mismatch stays a mismatch and still causes FAIL regardless of negative.
func TestEvaluate_EchoFidelityMode_NegativeTrue_Required_EchoNotInverted(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1")),
		mismatchedEndRecord(1, 1, researcher(), start.Add(time.Second)),
	}
	ev.Definition.Negative = true
	mode := domain.EchoFidelityRequired
	ev.Definition.Settings.EchoFidelity = &mode

	got := evaluate.Evaluate(ev)

	// The echo assertion must remain Fail (not inverted to Pass).
	ar := findAssertion(t, got.Assertions, domain.ClassEchoFidelity, "1")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("echo assertion Outcome = %q after negative inversion, want Fail — echo must not be inverted by negative:true", ar.Outcome)
	}
	// ReasonEchoMismatch must still appear.
	if !hasReason(got.Reasons, domain.ReasonEchoMismatch) {
		t.Errorf("Reasons = %+v, want ReasonEchoMismatch even when negative:true (echo is not inverted)", got.Reasons)
	}
}

// TestEvaluate_EchoFidelityMode_NegativeTrue_Advisory_EchoNotInverted verifies
// that negative:true does not invert the echo fidelity assertion in advisory mode
// either. The mismatch stays Fail in the results and the verdict is PASS (from
// advisory softening), not because it was inverted.
func TestEvaluate_EchoFidelityMode_NegativeTrue_Advisory_EchoNotInverted(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1")),
		mismatchedEndRecord(1, 1, researcher(), start.Add(time.Second)),
	}
	ev.Definition.Negative = true
	ev.Definition.Assertions.FinalPhase = strp("complete")
	ev.Definition.Assertions.FinalStatus = strp("SUCCESS")
	mode := domain.EchoFidelityAdvisory
	ev.Definition.Settings.EchoFidelity = &mode

	got := evaluate.Evaluate(ev)

	// Echo assertion stays Fail (not flipped by negative inversion).
	ar := findAssertion(t, got.Assertions, domain.ClassEchoFidelity, "1")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("echo assertion Outcome = %q, want Fail — echo must not be inverted by negative:true in advisory mode", ar.Outcome)
	}
	// ReasonEchoMismatch must not appear (advisory mode).
	if hasReason(got.Reasons, domain.ReasonEchoMismatch) {
		t.Errorf("Reasons = %+v, must not contain ReasonEchoMismatch in advisory mode", got.Reasons)
	}
}

// TestEffectiveEchoFidelity_Nil_DefaultsToRequired verifies the helper
// function returns EchoFidelityRequired when EchoFidelity is nil.
func TestEffectiveEchoFidelity_Nil_DefaultsToRequired(t *testing.T) {
	s := domain.RunSettings{} // EchoFidelity is nil
	got := domain.EffectiveEchoFidelity(s)
	if got != domain.EchoFidelityRequired {
		t.Errorf("EffectiveEchoFidelity(nil) = %q, want %q", got, domain.EchoFidelityRequired)
	}
}

// TestEffectiveEchoFidelity_Required_ReturnsRequired verifies the helper
// passes through EchoFidelityRequired unchanged.
func TestEffectiveEchoFidelity_Required_ReturnsRequired(t *testing.T) {
	mode := domain.EchoFidelityRequired
	s := domain.RunSettings{EchoFidelity: &mode}
	got := domain.EffectiveEchoFidelity(s)
	if got != domain.EchoFidelityRequired {
		t.Errorf("EffectiveEchoFidelity(required) = %q, want %q", got, domain.EchoFidelityRequired)
	}
}

// TestEffectiveEchoFidelity_Advisory_ReturnsAdvisory verifies the helper
// passes through EchoFidelityAdvisory unchanged.
func TestEffectiveEchoFidelity_Advisory_ReturnsAdvisory(t *testing.T) {
	mode := domain.EchoFidelityAdvisory
	s := domain.RunSettings{EchoFidelity: &mode}
	got := domain.EffectiveEchoFidelity(s)
	if got != domain.EchoFidelityAdvisory {
		t.Errorf("EffectiveEchoFidelity(advisory) = %q, want %q", got, domain.EchoFidelityAdvisory)
	}
}
