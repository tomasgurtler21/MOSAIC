package evaluate_test

// Tests for the execution-log assertions (ClassExecutionLogAgentIDs,
// ClassExecutionLogAllStatus): the declared ordered list of agent instance
// IDs against the orchestration document's execution log, and the
// every-row-shares-this-status check.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

func agentIDsp(ids []string) *[]string { return &ids }

// executionLogRows is a minimal two-row execution log: two invocations,
// both recorded SUCCESS, in agent order researcher then reviewer.
func executionLogRows() []domain.ExecutionLogRow {
	return []domain.ExecutionLogRow{
		{Seq: 1, AgentInstance: "researcher#1", Status: "SUCCESS"},
		{Seq: 2, AgentInstance: "reviewer#1", Status: "SUCCESS"},
	}
}

func TestEvaluate_ExecutionLogAgentIDs_MatchesObservedOrder_Passes(t *testing.T) {
	ev := baseEvidence()
	ev.Orchestration.ExecutionLog = executionLogRows()
	ev.Definition.Assertions.ExecutionLogAgentIDs = agentIDsp([]string{"researcher#1", "reviewer#1"})

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassExecutionLogAgentIDs, "")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — declared agent-ID order matches the observed execution log", ar.Outcome)
	}
}

func TestEvaluate_ExecutionLogAgentIDs_OrderMismatch_Fails(t *testing.T) {
	ev := baseEvidence()
	ev.Orchestration.ExecutionLog = executionLogRows()
	ev.Definition.Assertions.ExecutionLogAgentIDs = agentIDsp([]string{"reviewer#1", "researcher#1"})

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassExecutionLogAgentIDs, "")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — the declared order is the reverse of the observed execution log", ar.Outcome)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

// TestEvaluate_ExecutionLogAllStatus_Pass_PopulatesActual verifies that Actual
// is non-empty on the pass path, not only on the fail path. When every row
// matches the declared status, Actual must reflect that observation (e.g. equal
// to Expected) rather than being left empty, so the evidence is always present
// regardless of outcome.
func TestEvaluate_ExecutionLogAllStatus_Pass_PopulatesActual(t *testing.T) {
	ev := baseEvidence()
	ev.Orchestration.ExecutionLog = executionLogRows() // all rows have status "SUCCESS"
	ev.Definition.Assertions.ExecutionLogAllStatus = strp("SUCCESS")

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassExecutionLogAllStatus, "")
	if ar.Outcome != domain.AssertionPass {
		t.Fatalf("Outcome = %q, want pass — every execution-log row's status is SUCCESS", ar.Outcome)
	}
	if ar.Actual == "" {
		t.Errorf("Actual = %q; want non-empty on pass — Actual must be populated unconditionally, not only on fail, for diagnostic context", ar.Actual)
	}
}

func TestEvaluate_ExecutionLogAllStatus_AllRowsMatch_Passes(t *testing.T) {
	ev := baseEvidence()
	ev.Orchestration.ExecutionLog = executionLogRows()
	ev.Definition.Assertions.ExecutionLogAllStatus = strp("SUCCESS")

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassExecutionLogAllStatus, "")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — every execution-log row's status is SUCCESS", ar.Outcome)
	}
}

func TestEvaluate_ExecutionLogAllStatus_OneRowDiffers_Fails(t *testing.T) {
	ev := baseEvidence()
	rows := executionLogRows()
	rows[1].Status = "BLOCKED"
	ev.Orchestration.ExecutionLog = rows
	ev.Definition.Assertions.ExecutionLogAllStatus = strp("SUCCESS")

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassExecutionLogAllStatus, "")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — one row's status (%q) differs from the declared uniform status %q", ar.Outcome, rows[1].Status, "SUCCESS")
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}
