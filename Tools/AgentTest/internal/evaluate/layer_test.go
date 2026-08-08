package evaluate_test

// Tests for the layer rule: `orchestrator` suppresses protocol-message
// validation, while `subagent` and `integration` apply it in full —
// including against the subject's own returned message.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

func TestEvaluate_Layer_Orchestrator_SuppressesProtocolViolationsEvenWhenDeclared(t *testing.T) {
	ev := baseEvidence()
	ev.Definition.Layer = domain.LayerOrchestrator
	ev.ProtocolViolations = map[domain.ViolationClassKey]int{"missing_required_field": 3}
	ev.Definition.Assertions.ProtocolViolations = intp(0)

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassProtocolViolations, "")
	if ar.Outcome != domain.AssertionNotEvaluated {
		t.Errorf("Outcome = %q, want not_evaluated — layer: orchestrator suppresses protocol-message validation even though the assertion was declared", ar.Outcome)
	}
	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS — a suppressed assertion cannot fail the run", got.Verdict)
	}
}

func TestEvaluate_Layer_Subagent_AppliesToSubjectsOwnMessage(t *testing.T) {
	ev := baseEvidence()
	ev.Definition.Layer = domain.LayerSubagent
	ev.ProtocolViolations = map[domain.ViolationClassKey]int{}
	// The violation exists only on the subject's own final message, not on
	// any collaborator invocation.
	ev.SubjectProtocolViolations = map[domain.ViolationClassKey]int{"not_bare_message": 1}
	ev.Definition.Assertions.ProtocolViolations = intp(0)

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassProtocolViolations, "")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — layer: subagent validates the subject's own final message too", ar.Outcome)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

func TestEvaluate_Layer_Integration_AppliesToSubjectsOwnMessage(t *testing.T) {
	ev := baseEvidence()
	ev.Definition.Layer = domain.LayerIntegration
	ev.ProtocolViolations = map[domain.ViolationClassKey]int{}
	ev.SubjectProtocolViolations = map[domain.ViolationClassKey]int{"not_bare_message": 1}
	ev.Definition.Assertions.ProtocolViolations = intp(0)

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassProtocolViolations, "")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — layer: integration validates the subject's own final message too", ar.Outcome)
	}
}

func TestEvaluate_Layer_Orchestrator_AlsoSuppressesSubjectsOwnMessageViolations(t *testing.T) {
	ev := baseEvidence()
	ev.Definition.Layer = domain.LayerOrchestrator
	ev.SubjectProtocolViolations = map[domain.ViolationClassKey]int{"not_bare_message": 1}
	ev.Definition.Assertions.ProtocolViolations = intp(0)

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassProtocolViolations, "")
	if ar.Outcome != domain.AssertionNotEvaluated {
		t.Errorf("Outcome = %q, want not_evaluated — the suppression covers the subject's own message too", ar.Outcome)
	}
	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS", got.Verdict)
	}
}
