package evaluate_test

// Tests for negative inversion: a negative test passes when its underlying
// assertions fail as expected, and fails when they pass. Inversion must not
// invert echo fidelity or state integrity — a negative test cannot "expect"
// the infrastructure to be broken.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

func TestEvaluate_Negative_UnderlyingAssertionFailsAsExpected_OverallPasses(t *testing.T) {
	ev := baseEvidence()
	ev.Definition.Negative = true
	// The subject actually reached "complete", but the negative test
	// declares an outcome it expects the subject NOT to reach.
	ev.Orchestration.Phase = "complete"
	ev.Definition.Assertions.FinalPhase = strp("blocked")

	got := evaluate.Evaluate(ev)

	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS — the underlying assertion failed, which is what a negative test expects", got.Verdict)
	}
	if !got.NegativeApplied {
		t.Error("NegativeApplied = false, want true")
	}
}

func TestEvaluate_Negative_UnderlyingAssertionPasses_OverallFails(t *testing.T) {
	ev := baseEvidence()
	ev.Definition.Negative = true
	ev.Orchestration.Phase = "complete"
	ev.Definition.Assertions.FinalPhase = strp("complete")

	got := evaluate.Evaluate(ev)

	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL — the underlying assertion held, which a negative test must not accept", got.Verdict)
	}
	if !got.NegativeApplied {
		t.Error("NegativeApplied = false, want true")
	}
}

// TestEvaluate_Negative_NeverInvertsEchoFidelity is the exclusion T8.7
// calls out explicitly: a negative test cannot pass by virtue of a broken
// echo.
func TestEvaluate_Negative_NeverInvertsEchoFidelity(t *testing.T) {
	start := baseEvidence().Records[0].Timestamp
	ev := baseEvidence()
	ev.Definition.Negative = true
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1")),
		mismatchedEndRecord(1, 1, researcher(), start.Add(time.Second)),
	}
	// The declared assertion fails as this negative test expects, which
	// alone would invert to an overall pass — but the echo mismatch must
	// still decide the verdict.
	ev.Orchestration.Phase = "complete"
	ev.Definition.Assertions.FinalPhase = strp("blocked")

	got := evaluate.Evaluate(ev)

	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL — echo fidelity is never inverted", got.Verdict)
	}
	if !hasReason(got.Reasons, domain.ReasonEchoMismatch) {
		t.Errorf("Reasons = %+v, want ReasonEchoMismatch", got.Reasons)
	}
}

// TestEvaluate_Negative_NeverInvertsStateIntegrity mirrors the echo case
// for a reclaimed lock: infrastructure faults are never something a
// negative test can "expect".
func TestEvaluate_Negative_NeverInvertsStateIntegrity(t *testing.T) {
	ev := baseEvidence()
	ev.Definition.Negative = true
	ev.Records = append(ev.Records, domain.LogRecord{
		Kind:      domain.RecordRun,
		Event:     domain.RunEventLockReclaimed,
		Timestamp: ev.Records[0].Timestamp,
		Detail:    "lock reclaimed from a stale holder",
	})
	// Everything else about this run passes cleanly, and inversion would
	// have nothing else to flip to a fail — state integrity must supply it.
	ev.Orchestration.Phase = "complete"
	ev.Definition.Assertions.FinalPhase = strp("complete")

	got := evaluate.Evaluate(ev)

	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL — state integrity is never inverted", got.Verdict)
	}
	if !hasReason(got.Reasons, domain.ReasonStateIntegrity) {
		t.Errorf("Reasons = %+v, want ReasonStateIntegrity", got.Reasons)
	}
}
