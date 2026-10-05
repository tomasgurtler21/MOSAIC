package engine_test

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// ===== Staged EXECUTION — approach-driven group ordering =====

// TestNext_Approach_TDD_DispatchesTestGroupFirst verifies that TDD approach
// dispatches the test group (test-writer-tdd) before the implementation group.
func TestNext_Approach_TDD_DispatchesTestGroupFirst(t *testing.T) {
	// brownfield-tdd: after contracts-review (row 6, last pre-execution row),
	// with TDD approach, the first EXECUTION dispatch should be test-writer-tdd (row 7).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("contracts-review#7"),
		Agents:          agents,
		Seq:             7,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-writer-tdd" {
		t.Errorf("TDD approach: want first EXECUTION dispatch to test-writer-tdd, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_Approach_ImplementationFirst_DispatchesImplGroupFirst verifies that
// Implementation-First approach dispatches the implementation group first.
func TestNext_Approach_ImplementationFirst_DispatchesImplGroupFirst(t *testing.T) {
	// brownfield-tdd: after contracts-review (row 6), with Implementation-First
	// approach, the first EXECUTION dispatch should be implementation-tdd (row 9),
	// not test-writer-tdd.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("Implementation-First")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("contracts-review#7"),
		Agents:          agents,
		Seq:             7,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "implementation-tdd" {
		t.Errorf("Implementation-First approach: want first EXECUTION dispatch to implementation-tdd, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_Approach_ImplementationOnly_SkipsTestGroup verifies that
// Implementation-Only approach never dispatches test group agents.
// After contracts-review, the first dispatch must be implementation-tdd.
func TestNext_Approach_ImplementationOnly_SkipsTestGroup(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("contracts-review#7"),
		Agents:          agents,
		Seq:             7,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	name := agentName(step.Request.AgentInstanceID)
	if name == "test-writer-tdd" || name == "tests-review-tdd" {
		t.Errorf("Implementation-Only approach: must not dispatch test agents, got %s", name)
	}
	if name != "implementation-tdd" {
		t.Errorf("Implementation-Only approach: want implementation-tdd, got %s", name)
	}
}

// TestNext_Approach_TestsOnly_SkipsImplGroup verifies that Tests-Only approach
// never dispatches implementation group agents.
// After contracts-review, the first dispatch must be test-writer-tdd.
func TestNext_Approach_TestsOnly_SkipsImplGroup(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("Tests-Only")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("contracts-review#7"),
		Agents:          agents,
		Seq:             7,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	name := agentName(step.Request.AgentInstanceID)
	if name == "implementation-tdd" || name == "implementation-review" {
		t.Errorf("Tests-Only approach: must not dispatch implementation agents, got %s", name)
	}
	if name != "test-writer-tdd" {
		t.Errorf("Tests-Only approach: want test-writer-tdd, got %s", name)
	}
}

// ===== Staged EXECUTION — stage progression =====

// TestNext_Staged_TDD_AfterTestGroup_DispatchesImplGroup verifies that after
// the last row of the test group, the next dispatch is the first row of the
// implementation group (TDD approach).
func TestNext_Staged_TDD_AfterTestGroup_DispatchesImplGroup(t *testing.T) {
	// brownfield-tdd, TDD approach:
	// Test group: rows 7 (test-writer-tdd) and 8 (tests-review-tdd).
	// After tests-review-tdd succeeds (row 8), next must be implementation-tdd (row 9).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
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
	if agentName(step.Request.AgentInstanceID) != "implementation-tdd" {
		t.Errorf("after last test-group row, want implementation-tdd (impl group), got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_Staged_TDD_AfterImplGroupLastStage_AdvancesToNextStage verifies that
// after the last row of the implementation group in stage N (with more stages
// remaining), the engine starts stage N+1 with the test group (TDD approach).
func TestNext_Staged_TDD_AfterImplGroupLastStage_AdvancesToNextStage(t *testing.T) {
	// brownfield-tdd, TDD approach, two stages:
	// After implementation-review (row 10) in Stage-1, advance to Stage-2 test group.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := twoStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusSUCCESS, 11)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("implementation-review#11"),
		Agents:          agents,
		Seq:             11,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// Should start Stage-2, test group first (TDD) → test-writer-tdd.
	if agentName(step.Request.AgentInstanceID) != "test-writer-tdd" {
		t.Errorf("after Stage-1 last impl-group row, want test-writer-tdd in Stage-2, got %s",
			step.Request.AgentInstanceID)
	}
	if step.Stage != "Test.2" {
		t.Errorf("want Stage=Test.2, got %q", step.Stage)
	}
}

// TestNext_Staged_AfterLastStage_DispatchesPostExecutionRow verifies that after
// the last row of the last stage's last group, the engine dispatches the first
// post-execution row.
func TestNext_Staged_AfterLastStage_DispatchesPostExecutionRow(t *testing.T) {
	// brownfield-tdd, TDD approach, one stage:
	// After implementation-review (row 10, last impl-group row, only stage),
	// engine must dispatch test-runner (row 11, the post-execution REVIEW row).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusSUCCESS, 11)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("implementation-review#11"),
		Agents:          agents,
		Seq:             11,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-runner" {
		t.Errorf("after last stage last group, want post-execution test-runner, got %s",
			step.Request.AgentInstanceID)
	}
	// Post-execution row has no Stage context.
	if step.Stage != "" {
		t.Errorf("want Stage=\"\" for post-execution row, got %q", step.Stage)
	}
}

// TestNext_Staged_AfterLastStage_NoPostExecution_ReturnsComplete verifies that
// when a workflow has no post-execution rows, the engine returns CompleteDecision
// after the last row of the last stage.
func TestNext_Staged_AfterLastStage_NoPostExecution_ReturnsComplete(t *testing.T) {
	// brownfield-tdd-build-verified has no post-execution rows (table ends at row 12).
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	// implementation-review is row 12, the last EXECUTION row (and last row overall).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#13", domain.StatusSUCCESS, 13)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("implementation-review#13"),
		Agents:          agents,
		Seq:             13,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	requireComplete(t, dec)
}

// ===== On Success ignored inside EXECUTION (FR-12b) =====

// TestNext_Staged_OnSuccessIgnoredInsideExecution verifies that even when an
// EXECUTION row declares an On Success hint, the engine ignores it and routes
// by group/stage logic, not by the hint.
//
// Uses onSuccessDivergentContent where test-writer-tdd declares
// On Success="implementation-tdd" — which skips ahead to the implementation group.
// With TDD approach, group-order logic requires tests-review-tdd next.
// An engine that incorrectly consults On Success would dispatch implementation-tdd,
// making a correct vs incorrect implementation distinguishable.
func TestNext_Staged_OnSuccessIgnoredInsideExecution(t *testing.T) {
	aw := mustParseAndAdmit(t, onSuccessDivergentContent, "on-success-divergent", "1.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents("test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review")
	// test-writer-tdd is row 0; after it succeeds in Stage-1 (seq=1).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "test-writer-tdd#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("test-writer-tdd#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// Group-order logic (On Success ignored): within the test group, next is tests-review-tdd.
	// If On Success were consulted, implementation-tdd would be dispatched (wrong).
	if agentName(step.Request.AgentInstanceID) != "tests-review-tdd" {
		t.Errorf("On Success must be ignored inside EXECUTION: want tests-review-tdd (group-order next), got %s (suggests On Success was consulted)",
			step.Request.AgentInstanceID)
	}
	if step.Stage != "1" {
		t.Errorf("want 1, got %q", step.Stage)
	}
}

// ===== Templated artifact path resolution =====

// TestNext_Paths_StageNumber_SubstitutedInInput verifies that {StageNumber} in
// input artifact paths is replaced with the current stage number string.
