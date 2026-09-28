package engine_test

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)


// ===== Canonical phase and stage recording =====
//
// The runner and the LLM orchestrator write the same Orchestration.md: the
// bare phase name in the phase field, and the group-and-stage value in the
// stage field ("Test.1" for a grouped EXECUTION row, "1" for an ungrouped
// one, absent for a non-staged phase). The tests below cover what a
// dispatch step records (AC4.2/AC4.3), that position resolution works
// against that recorded form including disambiguation by group (AC4.4),
// and that a legacy on-disk artifact (qualified phase, "Stage-N" stage)
// remains resumable (AC4.5).

// TestNext_StagedExecution_GroupedRow_RecordsBarePhaseAndGroupQualifiedStage
// verifies that a dispatch into a grouped EXECUTION row (the common case)
// records the bare phase name and the group-qualified stage value, not the
// routing table's qualified phase string or a bare "Stage-N" form.
func TestNext_StagedExecution_GroupedRow_RecordsBarePhaseAndGroupQualifiedStage(t *testing.T) {
	// brownfield-tdd: after contracts-review (row 6), TDD approach dispatches
	// test-writer-tdd (row 7, group "Test") for stage 1.
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
	if step.Phase != "EXECUTION" {
		t.Errorf("DispatchStep.Phase: want bare %q, got %q (the routing table's qualified string must not reach the recorded field)",
			"EXECUTION", step.Phase)
	}
	if step.Stage != "Test.1" {
		t.Errorf("DispatchStep.Stage: want group-qualified %q, got %q", "Test.1", step.Stage)
	}
}

// TestNext_StagedExecution_UngroupedRow_RecordsBarePhaseAndPlainStageNumber
// verifies that a dispatch into an ungrouped staged row records the bare
// phase name and the plain stage number, with no group segment.
func TestNext_StagedExecution_UngroupedRow_RecordsBarePhaseAndPlainStageNumber(t *testing.T) {
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("implementation-tdd", "implementation-review", "test-runner")

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
	if step.Phase != "EXECUTION" {
		t.Errorf("DispatchStep.Phase: want bare %q, got %q", "EXECUTION", step.Phase)
	}
	if step.Stage != "1" {
		t.Errorf("DispatchStep.Stage: want plain stage number %q (no group declared), got %q", "1", step.Stage)
	}
}

// TestNext_NonStagedRow_RecordsEmptyStage verifies that a dispatch into a
// non-staged row records an empty stage value.
func TestNext_NonStagedRow_RecordsEmptyStage(t *testing.T) {
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
	if step.Phase != "PLANNING" {
		t.Errorf("DispatchStep.Phase: want %q, got %q", "PLANNING", step.Phase)
	}
	if step.Stage != "" {
		t.Errorf("DispatchStep.Stage: want empty (non-staged phase), got %q", step.Stage)
	}
}

// TestNext_TargetFormatState_RepeatedAgent_Test_ResolvesToTestGroupRow verifies
// that position resolution works against the target recorded format
// (bare phase, group-qualified stage) for an agent that occupies more than
// one EXECUTION row, and that the Test-group row is picked when the
// recorded stage names the Test group.
func TestNext_TargetFormatState_RepeatedAgent_Test_ResolvesToTestGroupRow(t *testing.T) {
	// brownfield-tdd-build-verified: build-review appears at row 8 (Test
	// group) and row 11 (Implementation group). Recorded state names the
	// Test-group row (agent instance "build-review#9", stage "Test.1");
	// the next dispatch must continue within the Test group
	// (tests-review-tdd, row 9), not jump to the Implementation group.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION", "Test.1", "build-review#9", domain.StatusSUCCESS, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("build-review#9"),
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "tests-review-tdd" {
		t.Errorf("recorded stage %q must resolve build-review to the Test-group row (row 8); want next dispatch tests-review-tdd, got %s",
			"Test.1", step.Request.AgentInstanceID)
	}
}

// TestNext_TargetFormatState_RepeatedAgent_Implementation_ResolvesToImplementationGroupRow
// is the mirror of the above: the same repeated agent, but the recorded
// stage names the Implementation group, and resolution must pick the
// Implementation-group row instead -- proving the group segment is not
// discarded during position resolution.
func TestNext_TargetFormatState_RepeatedAgent_Implementation_ResolvesToImplementationGroupRow(t *testing.T) {
	// build-review at row 11 (Implementation group); recorded stage
	// "Implementation.1" must resolve there, continuing to
	// implementation-review (row 12), not back into the Test group.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION", "Implementation.1", "build-review#12", domain.StatusSUCCESS, 12)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("build-review#12"),
		Agents:          agents,
		Seq:             12,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "implementation-review" {
		t.Errorf("recorded stage %q must resolve build-review to the Implementation-group row (row 11); want next dispatch implementation-review, got %s",
			"Implementation.1", step.Request.AgentInstanceID)
	}
}

// TestResumePoint_TargetFormatState_RepeatedAgent_Test_ResolvesNextRow is the
// ResumePoint counterpart of the Next-level disambiguation tests above,
// exercising findRowForLogEntry against a target-format execution log entry.
