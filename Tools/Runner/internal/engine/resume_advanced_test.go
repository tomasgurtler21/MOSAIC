package engine_test

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

func TestResumePoint_TargetFormatState_RepeatedAgent_Test_ResolvesNextRow(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")

	state := domain.ArtifactState{
		GlobalSequence: 9,
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION",
			Stage:      "Test.1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "build-review#9",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 9, Agent: "build-review#9", Phase: "EXECUTION", Stage: "Test.1", WorkflowRow: 9, Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state, nil)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if info.RowIndex != 9 {
		t.Errorf("want RowIndex=9 (tests-review-tdd, next row in the Test group), got %d", info.RowIndex)
	}
	if info.RerunLast {
		t.Error("want RerunLast=false (clean completion), got true")
	}
}

// TestResumePoint_TargetFormatState_RepeatedAgent_Implementation_ResolvesNextRow
// mirrors the above for the Implementation-group row of the same repeated agent.
func TestResumePoint_TargetFormatState_RepeatedAgent_Implementation_ResolvesNextRow(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")

	state := domain.ArtifactState{
		GlobalSequence: 12,
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION",
			Stage:      "Implementation.1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "build-review#12",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 12, Agent: "build-review#12", Phase: "EXECUTION", Stage: "Implementation.1", WorkflowRow: 12, Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state, nil)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if info.RowIndex != 12 {
		t.Errorf("want RowIndex=12 (implementation-review, next row in the Implementation group), got %d", info.RowIndex)
	}
	if info.RerunLast {
		t.Error("want RerunLast=false (clean completion), got true")
	}
}

// TestResumePoint_LegacyArtifact_QualifiedPhaseAndStageN_StillResolvesAndResumes
// pins AC4.5: an artifact written by an earlier runner version, carrying the
// qualified phase and the "Stage-N" stage form on disk, must still resolve
// position correctly and be resumable after this stage's write-path change.
// Reading such an artifact is unaffected by what the runner now writes.
func TestResumePoint_LegacyArtifact_QualifiedPhaseAndStageN_StillResolvesAndResumes(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")

	state := domain.ArtifactState{
		GlobalSequence: 8,
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION.Test.[StageNumber]",
			Stage:      "Stage-1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "test-writer-tdd#8",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 8, Agent: "test-writer-tdd#8", Phase: "EXECUTION.Test.[StageNumber]", Stage: "Stage-1", Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state, nil)
	if err != nil {
		t.Fatalf("ResumePoint: legacy artifact must still resolve, got error: %v", err)
	}
	if info.RowIndex != 8 {
		t.Errorf("want RowIndex=8 (tests-review-tdd, next row after legacy test-writer-tdd entry), got %d", info.RowIndex)
	}
	if info.StageNumber != 1 {
		t.Errorf("want StageNumber=1 recovered from legacy %q, got %d", "Stage-1", info.StageNumber)
	}
	if info.RerunLast {
		t.Error("want RerunLast=false (clean completion), got true")
	}
}

// TestNext_LegacyArtifact_QualifiedPhaseAndStageN_ResolvesAndDispatches is the
// Next-level counterpart: a legacy-form CurrentState must still let Next
// determine the current row and route the following dispatch, exactly as it
// did before this stage's write-path change.
func TestNext_LegacyArtifact_QualifiedPhaseAndStageN_ResolvesAndDispatches(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.Test.[StageNumber]", "Stage-1", "test-writer-tdd#8", domain.StatusSUCCESS, 8)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("test-writer-tdd#8"),
		Agents:          agents,
		Seq:             8,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "tests-review-tdd" {
		t.Errorf("legacy artifact must still resolve position correctly: want next dispatch tests-review-tdd, got %s",
			step.Request.AgentInstanceID)
	}
}

// requireConsult asserts that the decision is a ConsultDecision.
