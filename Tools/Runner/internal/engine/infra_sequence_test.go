package engine_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)


// ===== Position immune to infrastructure activity =====
//
// Coverage:
//
//   Resume after infrastructure activity (T3.2):
//   - A run whose on-disk artifact ends with an infrastructure log entry resumes
//     at the workflow row after the last WORKFLOW step, not the row after the
//     infrastructure entry, and is not misdiagnosed as interrupted.
//   - A genuine mid-flight interruption occurring after infrastructure activity
//     is still detected and the interrupted workflow row is re-dispatched.
//
//   Sequence-based disambiguation with interleaved infrastructure invocations (T3.3):
//   - A repeated agent (occupying more than one EXECUTION row) is still
//     resolved to the correct row when an infrastructure step's own sequence
//     bump has shifted the raw sequence arithmetic.
//   - A single-agent-per-row workflow, where identification never depends on
//     sequence arithmetic, continues to resolve correctly across an interleaved
//     infrastructure invocation.
//
//   Unresolved-position diagnostic (T3.4):
//   - When the recorded agent is neither a workflow participant nor a declared
//     infrastructure agent, the stop names the agent and states that it is not
//     a workflow participant.

func declaredCheckpointInfraAgent() []domain.DeclaredInfraAgent {
	return []domain.DeclaredInfraAgent{
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
	}
}

// TestResumePoint_TrailingInfrastructureEntry_NotMisdiagnosedAsInterrupted verifies
// that when the execution log ends with an infrastructure entry and current_state
// correctly names the last WORKFLOW step (per the Apply fix), ResumePoint resumes
// at the row after that workflow step rather than treating the trailing
// infrastructure entry as an interruption.
func TestResumePoint_TrailingInfrastructureEntry_NotMisdiagnosedAsInterrupted(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	infra := domain.NewInfraAgentSet(declaredCheckpointInfraAgent())

	state := domain.ArtifactState{
		GlobalSequence: 2,
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			Stage:      "",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "planner-tdd-soft#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "planner-tdd-soft#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "checkpoint-manager-git#2", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state, infra)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if info.RerunLast {
		t.Error("want RerunLast=false (clean stop after infrastructure activity, not an interruption), got true")
	}
	if info.RowIndex != 1 {
		t.Errorf("want RowIndex=1 (plan-review, the row after the last workflow step), got %d", info.RowIndex)
	}
}

// TestResumePoint_InterruptionAfterInfrastructureActivity_RerunsWorkflowRow verifies
// that a genuine mid-flight interruption occurring after infrastructure activity is
// still detected: the workflow row that was dispatched but never recorded in
// current_state is re-run, and the interleaved infrastructure entry does not
// obscure the interruption.
func TestResumePoint_InterruptionAfterInfrastructureActivity_RerunsWorkflowRow(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	infra := domain.NewInfraAgentSet(declaredCheckpointInfraAgent())

	// planner-tdd-soft (row 0) completed and is recorded in current_state.
	// checkpoint-manager-git then ran (does not move current_state). plan-review
	// (row 1) was then dispatched and completed, but the runner was interrupted
	// before current_state could be updated to reflect it.
	state := domain.ArtifactState{
		GlobalSequence: 3,
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			Stage:      "",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "planner-tdd-soft#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "planner-tdd-soft#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "checkpoint-manager-git#2", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 3, Agent: "plan-review#3", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state, infra)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if !info.RerunLast {
		t.Error("want RerunLast=true (plan-review was dispatched but never recorded), got false")
	}
	if info.RowIndex != 1 {
		t.Errorf("want RowIndex=1 (plan-review, the interrupted row), got %d", info.RowIndex)
	}
}

// TestNext_Interleaved_Infrastructure_SequenceDisambiguation_RepeatedAgent verifies
// AC3.6 for a workflow where one agent (build-review) occupies more than one
// EXECUTION row: an infrastructure step's sequence bump, interleaved between
// two workflow rows, must not throw off sequence-based row disambiguation.
func TestNext_Interleaved_Infrastructure_SequenceDisambiguation_RepeatedAgent(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.1")
	stages := singleStageSet("TDD")
	agents := newTestAgents("test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review")

	// Stage-1, TDD approach, test group = [test-writer-tdd, build-review, tests-review-tdd].
	// 7 pre-execution rows consume seq 1-7. test-writer-tdd completes at seq 8.
	// checkpoint-manager-git then fires as an infrastructure step at seq 9 (not a
	// routing row). build-review then completes at seq 10 -- one ahead of where it
	// would land (seq 9) if only workflow rows consumed sequence numbers.
	state := domain.ArtifactState{
		GlobalSequence: 10,
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION.Test.[StageNumber]",
			Stage:      "Stage-1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "build-review#10",
		},
	}

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("build-review#10"),
		Agents:          agents,
		Seq:             10,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	got := agentName(step.Request.AgentInstanceID)
	if got != "tests-review-tdd" {
		t.Errorf("want dispatch of tests-review-tdd (the row after build-review in the test group), got %q "+
			"-- sequence-based disambiguation was thrown off by the infrastructure step's sequence bump", got)
	}
}

// TestNext_Interleaved_Infrastructure_SequenceDisambiguation_SingleAgentPerRow verifies
// AC3.6 for a workflow where every agent occupies exactly one row: identification is
// by agent+phase, not sequence arithmetic, so an inflated global_sequence from an
// interleaved infrastructure step must not affect resolution.
func TestNext_Interleaved_Infrastructure_SequenceDisambiguation_SingleAgentPerRow(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	// planner-tdd-soft completes at seq 1. checkpoint-manager-git then fires as an
	// infrastructure step at seq 2 (not a routing row). plan-review then completes
	// at seq 3 -- one ahead of where it would land (seq 2) if only workflow rows
	// consumed sequence numbers.
	state := domain.ArtifactState{
		GlobalSequence: 3,
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			Stage:      "",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "plan-review#3",
		},
	}

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("plan-review#3"),
		Agents:          agents,
		Seq:             3,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	got := agentName(step.Request.AgentInstanceID)
	if got != "implementation-tdd" {
		t.Errorf("want dispatch of implementation-tdd (On Success target of plan-review), got %q", got)
	}
}

// TestNext_UnresolvedPosition_AgentNotInWorkflow_StopNamesCause verifies AC3.7:
// when the recorded agent is not a workflow participant (and not recognisable as
// an infrastructure entry either), the stop names the agent and states that it is
// not a workflow participant, rather than a generic "could not determine" message.
func TestNext_UnresolvedPosition_AgentNotInWorkflow_StopNamesCause(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	state := domain.ArtifactState{
		GlobalSequence: 1,
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			Stage:      "",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "mystery-agent#1",
		},
	}

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("mystery-agent#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, "mystery-agent") {
		t.Errorf("Stop reason must name the unresolvable agent, got %q", stop.Reason)
	}
	lower := strings.ToLower(stop.Reason)
	if !strings.Contains(lower, "not a") || !strings.Contains(lower, "participant") {
		t.Errorf("Stop reason must state that the agent is not a workflow participant (AC3.7), got %q", stop.Reason)
	}
}
