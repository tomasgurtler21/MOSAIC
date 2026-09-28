package engine_test

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

func TestNext_AgentInstanceID_SeqIncrementedBeforeDispatch(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	// seq=5: the next dispatch should be #6.
	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             5,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	wantID := "planner-tdd-soft#6"
	if step.Request.AgentInstanceID != wantID {
		t.Errorf("want AgentInstanceID=%q (seq incremented before dispatch), got %q",
			wantID, step.Request.AgentInstanceID)
	}
}

// TestNext_AgentInstanceID_ZeroSeqProducesOne verifies seq=0 → ID uses #1.
func TestNext_AgentInstanceID_ZeroSeqProducesOne(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	wantID := "planner-tdd-soft#1"
	if step.Request.AgentInstanceID != wantID {
		t.Errorf("want AgentInstanceID=%q, got %q", wantID, step.Request.AgentInstanceID)
	}
}

// ===== HITL computation =====

// TestNext_HITL_RowTrue_EffectiveTrue verifies that row-level HITL=true
// produces an effective HITL=true regardless of stage HITL.
func TestNext_HITL_RowTrue_EffectiveTrue(t *testing.T) {
	// quick-fix row 0: planner-tdd-soft has HITL=TRUE (true)
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if !step.EffectiveHITL {
		t.Error("want EffectiveHITL=true (row HITL=true), got false")
	}
}

// TestNext_HITL_BothFalse_EffectiveFalse verifies that when both row HITL and
// stage HITL are false, effective HITL is false.
func TestNext_HITL_BothFalse_EffectiveFalse(t *testing.T) {
	// quick-fix row 1: plan-review has HITL=FALSE (false), stage HITL=false
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stagesNoHITL := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "Implementation-Only"},
	})
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stagesNoHITL,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if step.EffectiveHITL {
		t.Error("want EffectiveHITL=false (row HITL=false, stage HITL=false), got true")
	}
}

// TestNext_HITL_StageTrue_InsideExecution_EffectiveTrue verifies that when stage
// HITL=true and the dispatch is inside the EXECUTION phase, effective HITL=true
// even when the row itself has HITL=false.
func TestNext_HITL_StageTrue_InsideExecution_EffectiveTrue(t *testing.T) {
	// quick-fix row 2: implementation-tdd has HITL=false, but stage HITL=true
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stagesHITL := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: true, Approach: "Implementation-Only"},
	})
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	// State: plan-review (row 1) just succeeded; next is EXECUTION row.
	state := stateAfter("PLANNING", "", "plan-review#2", domain.StatusSUCCESS, 2)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stagesHITL,
		State:           state,
		LastResponse:    successResponse("plan-review#2"),
		Agents:          agents,
		Seq:             2,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if !step.EffectiveHITL {
		t.Error("want EffectiveHITL=true (stage HITL=true inside EXECUTION), got false")
	}
}

// TestNext_HITL_StageTrue_OutsideExecution_EffectiveFalse verifies that stage
// HITL is NOT consulted for pre-execution or post-execution rows. When the row
// has HITL=false, effective HITL must be false even if stage HITL=true.
func TestNext_HITL_StageTrue_OutsideExecution_EffectiveFalse(t *testing.T) {
	// quick-fix row 1: plan-review is a PLANNING row (pre-execution), HITL=false.
	// Stage HITL=true should be ignored for this row.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stagesHITL := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: true, Approach: "Implementation-Only"},
	})
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stagesHITL,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if step.EffectiveHITL {
		t.Error("want EffectiveHITL=false (stage HITL not applied outside EXECUTION), got true")
	}
}

