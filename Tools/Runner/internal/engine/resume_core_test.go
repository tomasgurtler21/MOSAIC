package engine_test

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

func TestResumePoint_NoLogEntries_ReturnsFirstRow(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")

	info, err := engine.ResumePoint(aw, stages, emptyState(), nil)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if info.RowIndex != 0 {
		t.Errorf("want RowIndex=0, got %d", info.RowIndex)
	}
	if info.RerunLast {
		t.Error("want RerunLast=false for fresh start, got true")
	}
	if info.Seq != 0 {
		t.Errorf("want Seq=0 (no prior invocations), got %d", info.Seq)
	}
}

// TestResumePoint_LastEntrySuccess_AdvancesToNextRow verifies that when the
// last log entry matches CurrentState (clean completion), the resume point is
// the row after the last completed one.
func TestResumePoint_LastEntrySuccess_AdvancesToNextRow(t *testing.T) {
	// After planner-tdd-soft (row 0) completed with SUCCESS.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	info, err := engine.ResumePoint(aw, stages, state, nil)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if info.RowIndex != 1 {
		t.Errorf("want RowIndex=1 (row after planner-tdd-soft), got %d", info.RowIndex)
	}
	if info.RerunLast {
		t.Error("want RerunLast=false (clean completion), got true")
	}
	if info.Seq != 1 {
		t.Errorf("want Seq=1 (GlobalSequence from artifact), got %d", info.Seq)
	}
}

// TestResumePoint_Interruption_RerunsLastRow verifies that when the last log
// entry does not match CurrentState (detected mismatch), RerunLast=true and
// RowIndex points to the interrupted row (not the next one).
func TestResumePoint_Interruption_RerunsLastRow(t *testing.T) {
	// Simulate an interruption: the log says plan-review#2 ran, but the runner
	// was interrupted before CurrentState was updated to reflect it. CurrentState
	// still shows planner-tdd-soft from the prior step.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")

	// CurrentState reflects planner-tdd-soft (row 0), but the log has a second
	// entry for plan-review (row 1) — meaning plan-review started but the artifact
	// was not updated to reflect its completion.
	state := domain.ArtifactState{
		GlobalSequence: 1,
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			Stage:      "",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "planner-tdd-soft#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "planner-tdd-soft#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "plan-review#2", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state, nil)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if !info.RerunLast {
		t.Error("want RerunLast=true (interruption detected), got false")
	}
	// RowIndex should point to the interrupted row (plan-review = row 1).
	if info.RowIndex != 1 {
		t.Errorf("want RowIndex=1 (plan-review, the interrupted row), got %d", info.RowIndex)
	}
}

// TestResumePoint_StagedWorkflow_DerivesGroupAndStage verifies that ResumePoint
// correctly derives GroupIndex and StageNumber for a staged workflow position.
func TestResumePoint_StagedWorkflow_DerivesGroupAndStage(t *testing.T) {
	// After test-writer-tdd (row 7) in Stage-1 of brownfield-tdd.
	// GroupIndex should be 0 (test group), StageNumber=1.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "test-writer-tdd#8", domain.StatusSUCCESS, 8)

	info, err := engine.ResumePoint(aw, stages, state, nil)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	// Next row should be tests-review-tdd (row 8).
	if info.RowIndex != 8 {
		t.Errorf("want RowIndex=8 (tests-review-tdd), got %d", info.RowIndex)
	}
	// GroupIndex=0 (test group, first group in admitted workflow Groups slice).
	if info.GroupIndex != 0 {
		t.Errorf("want GroupIndex=0 (test group), got %d", info.GroupIndex)
	}
	if info.StageNumber != 1 {
		t.Errorf("want StageNumber=1, got %d", info.StageNumber)
	}
	if info.Stage != "Stage-1" {
		t.Errorf("want Stage=Stage-1, got %q", info.Stage)
	}
}

// TestResumePoint_StagedWorkflow_NonExecutionRow_GroupIndexNegativeOne verifies
// that a resume point outside the staged phase has GroupIndex=-1.
func TestResumePoint_StagedWorkflow_NonExecutionRow_GroupIndexNegativeOne(t *testing.T) {
	// After codebase-research (row 0) in brownfield-tdd (RESEARCH phase, pre-execution).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	state := stateAfter("RESEARCH", "", "codebase-research#1", domain.StatusSUCCESS, 1)

	info, err := engine.ResumePoint(aw, stages, state, nil)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if info.GroupIndex != -1 {
		t.Errorf("want GroupIndex=-1 (not in staged phase), got %d", info.GroupIndex)
	}
	if info.StageNumber != 0 {
		t.Errorf("want StageNumber=0 (not in staged phase), got %d", info.StageNumber)
	}
}

// TestResumePoint_MidStageInterruption_DerivesCorrectRow verifies FR-33:
// when the runner was interrupted while executing an EXECUTION row, ResumePoint
// returns RerunLast=true so the session re-dispatches the interrupted row.
func TestResumePoint_MidStageInterruption_DerivesCorrectRow(t *testing.T) {
	// In brownfield-tdd (TDD approach), Stage-1:
	// Log says tests-review-tdd ran (row 8), but CurrentState only shows test-writer-tdd.
	// This means tests-review-tdd was dispatched but the artifact update was lost.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")

	state := domain.ArtifactState{
		GlobalSequence: 8,
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION.[StageNumber]",
			Stage:      "Stage-1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "test-writer-tdd#8",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 8, Agent: "test-writer-tdd#8", Phase: "EXECUTION.[StageNumber]", Stage: "Stage-1", Status: domain.StatusSUCCESS},
			{Seq: 9, Agent: "tests-review-tdd#9", Phase: "EXECUTION.[StageNumber]", Stage: "Stage-1", Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state, nil)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if !info.RerunLast {
		t.Error("want RerunLast=true (mid-stage interruption), got false")
	}
	// tests-review-tdd is row 8 (0-indexed) in brownfield-tdd.
	if info.RowIndex != 8 {
		t.Errorf("want RowIndex=8 (tests-review-tdd, the interrupted row), got %d", info.RowIndex)
	}
}

