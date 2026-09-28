package engine_test

import (
	"testing"

	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/workflow"
)

// ===== Free-form On Success hint → DeviationDecision (AC7.1, FR-27) =====

// TestNext_PreExecution_FreeFormOnSuccess_ReturnsDeviation verifies that when a
// pre-execution row's On Success column contains qualifying text (free-form value),
// a SUCCESS response yields a DeviationDecision rather than dispatching the named agent.
// This is distinct from the absent-column case: the column is present, but the value
// is not an unambiguous agent identifier.
func TestNext_PreExecution_FreeFormOnSuccess_ReturnsDeviation(t *testing.T) {
	// On Success = "agent-b (or other based on issue)" — presence of qualifying text
	// makes this a free-form / ambiguous hint that cannot be auto-routed.
	const freeFormOnSuccessContent = `## Free-Form On Success Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b (or other based on issue) | - | - | out.md |
| PLANNING | agent-b | FALSE | COMPLETE | - | out.md | final.md |
`
	info := domain.WorkflowInfo{ID: "free-form-on-success", Version: "1.0"}
	table, err := workflow.Parse([]byte(freeFormOnSuccessContent), info)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
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

// ===== Greenfield-TDD workflow fixture (T7.1 requirement) =====

// TestNext_GreenfieldTDD_ArchitecturePhaseRouting verifies that the greenfield-tdd
// workflow's ARCHITECTURE phase routes correctly: after system-design-review succeeds,
// the engine dispatches planner-tdd-soft as named in the On Success column.
// This exercises a routing path (ARCHITECTURE phase) absent in the other four workflow
// fixtures and confirms the engine handles the greenfield row structure correctly.
func TestNext_GreenfieldTDD_ArchitecturePhaseRouting(t *testing.T) {
	aw := mustParseAndAdmit(t, greenfieldTDDContent, "greenfield-tdd", "3.3")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"requirements-refinement", "requirements-review",
		"system-designer", "system-design-review",
		"planner-tdd-soft", "plan-review",
		"contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	// system-design-review is row 3 (0-indexed) in greenfield-tdd; On Success = "planner-tdd-soft".
	state := stateAfter("ARCHITECTURE", "", "system-design-review#4", domain.StatusSUCCESS, 4)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("system-design-review#4"),
		Agents:          agents,
		Seq:             4,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "planner-tdd-soft" {
		t.Errorf("greenfield-tdd: after system-design-review (ARCHITECTURE phase), want planner-tdd-soft, got %s",
			step.Request.AgentInstanceID)
	}
	// planner-tdd-soft is row 4 in greenfield-tdd.
	if step.RowIndex != 4 {
		t.Errorf("want RowIndex=4 (planner-tdd-soft), got %d", step.RowIndex)
	}
}

// ===== Tests-Only approach: final stage boundary (AC7.2) =====

// TestNext_Approach_TestsOnly_LastStage_AdvancesToPostExecution verifies that with
// Tests-Only approach, after the final test-group row in the last stage the engine
// advances to the first post-EXECUTION row (not to an implementation group that
// Tests-Only does not run). Completes the AC7.2 coverage for Tests-Only approach.
func TestNext_Approach_TestsOnly_LastStage_AdvancesToPostExecution(t *testing.T) {
	// brownfield-tdd, Tests-Only approach, single stage:
	// Test group = [test-writer-tdd (row 7), tests-review-tdd (row 8)].
	// After tests-review-tdd succeeds in Stage-1 (the only stage), there are no
	// more stages and no implementation group to run. Engine must dispatch
	// test-runner (row 11, the post-execution REVIEW row).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("Tests-Only")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusSUCCESS, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("tests-review-tdd#9"),
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-runner" {
		t.Errorf("Tests-Only last stage: after final test-group row, want post-execution test-runner, got %s",
			step.Request.AgentInstanceID)
	}
	// Post-execution row carries no Stage context.
	if step.Stage != "" {
		t.Errorf("want Stage=\"\" for post-execution row, got %q", step.Stage)
	}
}

// ===== Implementation-First approach: final stage boundary (AC7.2) =====

// TestNext_Approach_ImplementationFirst_LastStageTestGroupComplete_AdvancesToPostExecution
// verifies that with Implementation-First approach, after the test group (which runs
// second) finishes in the last stage, the engine advances to the first post-EXECUTION
// row. Completes the AC7.2 coverage for Implementation-First approach.
func TestNext_Approach_ImplementationFirst_LastStageTestGroupComplete_AdvancesToPostExecution(t *testing.T) {
	// brownfield-tdd, Implementation-First approach, single stage:
	// Group order: impl group (rows 9-10) runs first, then test group (rows 7-8).
	// After tests-review-tdd (row 8, last row of the test group which runs second)
	// in Stage-1 (the only stage), engine must advance to test-runner (post-execution).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("Implementation-First")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusSUCCESS, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("tests-review-tdd#9"),
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-runner" {
		t.Errorf("Implementation-First last-stage test-group completion: want post-execution test-runner, got %s",
			step.Request.AgentInstanceID)
	}
	// Post-execution row carries no Stage context.
	if step.Stage != "" {
		t.Errorf("want Stage=\"\" for post-execution row, got %q", step.Stage)
	}
}

// ===== HITL truth table: both row and stage HITL true (AC7.4) =====

// TestNext_HITL_RowTrue_StageTrue_EffectiveTrue verifies the final case in the HITL
// truth table: when both row HITL and stage HITL are true (and the dispatch is inside
// EXECUTION), effective HITL is true. Completes the four-combination truth table for AC7.4.
func TestNext_HITL_RowTrue_StageTrue_EffectiveTrue(t *testing.T) {
	// Use a minimal EXECUTION-only workflow with HITL=true on the first row.
	// Stage HITL=true as well → effective HITL must be true (OR of both).
	const bothHITLTrueContent = `## Both HITL True Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | agent-a | TRUE | agent-b | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/out.md |
| EXECUTION.[StageNumber] | agent-b | FALSE | COMPLETE | - | Stage-{StageNumber}/out.md | Stage-{StageNumber}/final.md |
`
	info := domain.WorkflowInfo{ID: "both-hitl-true", Version: "1.0"}
	table, err := workflow.Parse([]byte(bothHITLTrueContent), info)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	aw, err := compat.Admit(table, domain.ExecutionModeAuto)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	stagesHITL := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: true, Approach: "Implementation-Only"},
	})
	agents := newTestAgents("agent-a", "agent-b")

	// Initial dispatch: no prior log, dispatch agent-a (row 0, HITL=true) in Stage-1 (HITL=true).
	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stagesHITL,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "agent-a" {
		t.Fatalf("want agent-a as first dispatch, got %s", step.Request.AgentInstanceID)
	}
	if !step.EffectiveHITL {
		t.Error("want EffectiveHITL=true (row HITL=true AND stage HITL=true), got false")
	}
}
