package engine_test

import (
	"testing"

	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/workflow"
)

func TestNext_PreExecution_OnSuccess_AdvancesToNamedAgent(t *testing.T) {
	// quick-fix row 0: planner-tdd-soft, OnSuccess="plan-review"
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// On Success of row 0 = "plan-review" → dispatch row 1.
	if agentName(step.Request.AgentInstanceID) != "plan-review" {
		t.Errorf("want agent plan-review (from On Success hint), got %s", step.Request.AgentInstanceID)
	}
	if step.RowIndex != 1 {
		t.Errorf("want RowIndex=1 (plan-review), got %d", step.RowIndex)
	}
}

// TestNext_PostExecution_CompleteTarget_ReturnsComplete verifies that a SUCCESS
// response from the last post-execution row whose On Success is "COMPLETE"
// causes the engine to return a CompleteDecision.
func TestNext_PostExecution_CompleteTarget_ReturnsComplete(t *testing.T) {
	// quick-fix row 3: test-runner, OnSuccess="COMPLETE"
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("REVIEW", "", "test-runner#4", domain.StatusSUCCESS, 4)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("test-runner#4"),
		Agents:          agents,
		Seq:             4,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	requireComplete(t, dec)
}

// TestNext_PreExecution_AbsentOnSuccess_ReturnsDeviation verifies that a SUCCESS
// response from a pre-execution row with no On Success hint triggers a Deviation.
func TestNext_PreExecution_AbsentOnSuccess_ReturnsDeviation(t *testing.T) {
	// Build a minimal workflow where the first pre-execution row has no On Success.
	// brownfield-tdd row 0 (codebase-research) has OnSuccess="requirements-refinement",
	// so we construct a minimal table with a column-absent On Success.
	const noHintContent = `## No Hint Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a | FALSE | - | out.md |
| PLANNING | agent-b | FALSE | out.md | final.md |
`
	info := domain.WorkflowInfo{ID: "no-hint", Version: "1.0"}
	table, err := workflow.Parse([]byte(noHintContent), info)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Admit will fail for no Stage-*/Plan.md but this workflow has no EXECUTION rows,
	// so HasStagedPhase=false and compat allows it.
	aw, err := compat.Admit(table, domain.ExecutionModeAuto)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	agents := newTestAgents("agent-a", "agent-b")
	state := stateAfter("PLANNING", "", "agent-a#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           state,
		LastResponse:    successResponse("agent-a#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	requireDeviation(t, dec)
}

// ===== Agent instance ID assignment =====

// TestNext_AgentInstanceID_SeqIncrementedBeforeDispatch verifies that the
// dispatched AgentInstanceID uses seq+1, not seq (FR-11: increment before invocation).
// ===== On Findings hint-based auto-routing (AC7.10) =====

// TestNext_OnFindings_AutoReview_UnambiguousHint_CNA_ReturnsDispatch verifies
// that in auto-review mode a COMPLETED_NEEDS_ACTION response from a row with
// an unambiguous On Findings hint causes the engine to return a DispatchDecision
// targeting that agent (loop-back), not a Deviation.
func TestNext_OnFindings_AutoReview_UnambiguousHint_CNA_ReturnsDispatch(t *testing.T) {
	// brownfield-tdd-build-verified row 8: build-review (in test group), OnFindings="test-writer-tdd".
	// When build-review returns CNA in auto-review mode, engine dispatches test-writer-tdd.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	// build-review is at row 8 (0-indexed) in the test group of brownfield-tdd-build-verified.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("build-review#9"),
		Agents:       agents,
		Seq:          9,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-writer-tdd" {
		t.Errorf("auto-review+CNA+unambiguous OnFindings: want loop-back Dispatch to test-writer-tdd, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_OnFindings_Auto_UnambiguousHint_CNA_ReturnsDeviation verifies that
// in auto mode a COMPLETED_NEEDS_ACTION response from a row with an unambiguous
// On Findings hint returns a DeviationDecision, not a Dispatch. The On-Findings
// auto-route fires only in auto-review mode.
func TestNext_OnFindings_Auto_UnambiguousHint_CNA_ReturnsDeviation(t *testing.T) {
	// Same setup as the auto-review variant above; only the mode differs.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("build-review#9"),
		Agents:       agents,
		Seq:          9,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAuto,
	})

	requireDeviation(t, dec)
}

// TestNext_OnFindings_AutoReview_UnambiguousHint_CNA_InsideExecution_ReturnsDispatch
// verifies that in auto-review mode the On Findings auto-routing works inside
// EXECUTION even though On Success is ignored there.
func TestNext_OnFindings_AutoReview_UnambiguousHint_CNA_InsideExecution_ReturnsDispatch(t *testing.T) {
	// brownfield-tdd-build-verified row 11: second build-review (in impl group), OnFindings="implementation-tdd".
	// When it returns CNA in auto-review mode, engine dispatches implementation-tdd.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	// row 11 is build-review in the impl group; OnFindings="implementation-tdd".
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#12", domain.StatusCOMPLETED_NEEDS_ACTION, 12)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("build-review#12"),
		Agents:       agents,
		Seq:          12,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "implementation-tdd" {
		t.Errorf("auto-review+CNA+OnFindings inside EXECUTION: want loop-back to implementation-tdd, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_OnFindings_Auto_UnambiguousHint_CNA_InsideExecution_ReturnsDeviation
// verifies that in auto mode a CNA response with an unambiguous On Findings hint
// inside EXECUTION also returns Deviation, not Dispatch.
func TestNext_OnFindings_Auto_UnambiguousHint_CNA_InsideExecution_ReturnsDeviation(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#12", domain.StatusCOMPLETED_NEEDS_ACTION, 12)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("build-review#12"),
		Agents:       agents,
		Seq:          12,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAuto,
	})

	requireDeviation(t, dec)
}

// TestNext_OnFindings_AbsentColumn_CNA_ReturnsDeviation verifies that when the
// On Findings column is absent from the routing table, a CNA response yields
// a DeviationDecision.
func TestNext_OnFindings_AbsentColumn_CNA_ReturnsDeviation(t *testing.T) {
	// Construct a minimal workflow without an On Findings column at all.
	const noFindingsContent = `## No Findings Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| PLANNING | agent-a | FALSE | COMPLETE | - | out.md |
`
	info := domain.WorkflowInfo{ID: "no-findings", Version: "1.0"}
	table, err := workflow.Parse([]byte(noFindingsContent), info)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	aw, err := compat.Admit(table, domain.ExecutionModeAuto)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	agents := newTestAgents("agent-a")
	state := stateAfter("PLANNING", "", "agent-a#1", domain.StatusCOMPLETED_NEEDS_ACTION, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           state,
		LastResponse:    cnaResponse("agent-a#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	requireDeviation(t, dec)
}

// TestNext_OnFindings_EmptyValue_CNA_ReturnsDeviation verifies that a CNA
// response where On Findings is present but has no value (dash/empty) yields
// a DeviationDecision.
func TestNext_OnFindings_EmptyValue_CNA_ReturnsDeviation(t *testing.T) {
	// brownfield-tdd row 7: test-writer-tdd has OnFindings="-" (no hint value).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "test-writer-tdd#8", domain.StatusCOMPLETED_NEEDS_ACTION, 8)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    cnaResponse("test-writer-tdd#8"),
		Agents:          agents,
		Seq:             8,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	requireDeviation(t, dec)
}

// TestNext_OnFindings_FreeFormHint_CNA_ReturnsDeviation verifies that a CNA
// response where On Findings contains free-form qualification yields a
// DeviationDecision (ambiguous hint cannot be auto-routed).
func TestNext_OnFindings_FreeFormHint_CNA_ReturnsDeviation(t *testing.T) {
	// brownfield-tdd row 10: implementation-review has OnFindings=
	// "implementation-tdd (or other based on issue)" — free-form, not unambiguous.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	// implementation-review is row 10 (0-indexed) in brownfield-tdd.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusCOMPLETED_NEEDS_ACTION, 11)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    cnaResponse("implementation-review#11"),
		Agents:          agents,
		Seq:             11,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	requireDeviation(t, dec)
}

// TestNext_OnFindings_NonCNA_NonSuccess_ReturnsDeviation verifies that for
// non-CNA non-SUCCESS statuses, the engine always returns Deviation regardless
// of whether On Findings has a hint. On Findings auto-routing is only for CNA.
func TestNext_OnFindings_NonCNA_NonSuccess_ReturnsDeviation(t *testing.T) {
	// brownfield-tdd row 8: tests-review-tdd has OnFindings="test-writer-tdd".
	// If it returns PARTIALLY_DONE (not CNA), the engine must return Deviation,
	// even though On Findings names an agent.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusPARTIALLY_DONE, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    nonSuccessResponse("tests-review-tdd#9", domain.StatusPARTIALLY_DONE),
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	requireDeviation(t, dec)
}

// TestNext_Deviation_Kind_IsNonSuccess verifies the DeviationKind in the
// deviation info is set to DeviationNonSuccess for a non-SUCCESS response.
func TestNext_Deviation_Kind_IsNonSuccess(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusBLOCKED, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    nonSuccessResponse("tests-review-tdd#9", domain.StatusBLOCKED),
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	dev := requireDeviation(t, dec)
	if dev.Info.Kind != domain.DeviationNonSuccess {
		t.Errorf("want DeviationKind=%q, got %q", domain.DeviationNonSuccess, dev.Info.Kind)
	}
}

// TestNext_Deviation_IncludesCurrentRowAndPhase verifies that DeviationInfo
// carries the current row index and phase for the resolver's context.
func TestNext_Deviation_IncludesCurrentRowAndPhase(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	// tests-review-tdd is row 8 in brownfield-tdd.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusBLOCKED, 9)
	resp := nonSuccessResponse("tests-review-tdd#9", domain.StatusBLOCKED)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    resp,
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	dev := requireDeviation(t, dec)
	if dev.Info.CurrentRow != 8 {
		t.Errorf("want CurrentRow=8 (tests-review-tdd), got %d", dev.Info.CurrentRow)
	}
	if dev.Info.CurrentPhase != "EXECUTION.Test.[StageNumber]" {
		t.Errorf("want CurrentPhase=EXECUTION.Test.[StageNumber], got %q", dev.Info.CurrentPhase)
	}
	if dev.Info.CurrentStage != "Stage-1" {
		t.Errorf("want CurrentStage=Stage-1, got %q", dev.Info.CurrentStage)
	}
}

