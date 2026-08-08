package evaluate_test

// Tests for echo fidelity (ClassEchoFidelity): it is evaluated on every run
// for every stubbed invocation, has no opt-out, and any mismatch
// unconditionally fails the test regardless of what other assertions say.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

func TestEvaluate_EchoFidelity_FaithfulEcho_Passes(t *testing.T) {
	ev := baseEvidence() // every end record in baseEvidence carries Echo.Match == true

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassEchoFidelity, "1")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — the stub was echoed faithfully", ar.Outcome)
	}
	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS", got.Verdict)
	}
}

// TestEvaluate_EchoFidelity_EvaluatedEvenWhenNoAssertionsDeclared is the
// core AC8.4 property: echo fidelity is never declared by a test author, so
// a definition with a completely empty Assertions struct must still
// produce an echo-fidelity result.
func TestEvaluate_EchoFidelity_EvaluatedEvenWhenNoAssertionsDeclared(t *testing.T) {
	ev := baseEvidence()
	ev.Definition.Assertions = domain.Assertions{}

	got := evaluate.Evaluate(ev)

	for _, ar := range got.Assertions {
		if ar.Class == domain.ClassEchoFidelity {
			return
		}
	}
	t.Errorf("no ClassEchoFidelity result found among %+v; echo fidelity must be evaluated with no opt-out", got.Assertions)
}

func TestEvaluate_EchoFidelity_Mismatch_FailsUnconditionally(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1")),
		mismatchedEndRecord(1, 1, researcher(), start.Add(time.Second)),
	}
	// Every other declared assertion is satisfied by this evidence, to
	// isolate that the echo mismatch alone decides the verdict.
	ev.Definition.Assertions.FinalPhase = strp("complete")
	ev.Definition.Assertions.FinalStatus = strp("SUCCESS")

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassEchoFidelity, "1")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail", ar.Outcome)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL — an echo mismatch fails the run even though every declared assertion passed", got.Verdict)
	}
	if !hasReason(got.Reasons, domain.ReasonEchoMismatch) {
		t.Errorf("Reasons = %+v, want ReasonEchoMismatch", got.Reasons)
	}
}

// TestEvaluate_EchoFidelity_EveryStubbedInvocationIsChecked_NotJustTheFirst
// covers "evaluated ... for every stubbed invocation": a mismatch on the
// second of two invocations must still be caught.
func TestEvaluate_EchoFidelity_EveryStubbedInvocationIsChecked_NotJustTheFirst(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1")),
		endRecord(1, 1, researcher(), start.Add(time.Second)),
		startRecord(2, 1, reviewer(), "", start.Add(2*time.Second), plainMessage("reviewer#1")),
		mismatchedEndRecord(2, 1, reviewer(), start.Add(3*time.Second)),
	}

	got := evaluate.Evaluate(ev)

	first := findAssertion(t, got.Assertions, domain.ClassEchoFidelity, "1")
	if first.Outcome != domain.AssertionPass {
		t.Errorf("invocation 1 Outcome = %q, want pass", first.Outcome)
	}
	second := findAssertion(t, got.Assertions, domain.ClassEchoFidelity, "2")
	if second.Outcome != domain.AssertionFail {
		t.Errorf("invocation 2 Outcome = %q, want fail", second.Outcome)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}
