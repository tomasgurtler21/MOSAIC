package engine_test

import (
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

func TestNext_UnresolvableApproach_InitialDispatch_ReturnsStop(t *testing.T) {
	// brownfield-tdd has an approach table with TDD, Implementation-First,
	// Implementation-Only, Tests-Only. "UnknownApproach" is not in the table.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "UnknownApproach"},
	})
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	// Simulate being at contracts-review (row 6, last pre-execution row) → entering EXECUTION.
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

	stop := requireStop(t, dec)
	if stop.Reason == "" {
		t.Error("want non-empty StopDecision.Reason for unresolvable approach")
	}
}

// TestNext_UnresolvableApproach_StopReason_NamesApproachValue verifies that the
// StopDecision reason string contains the unresolvable approach token so the user
// can identify and fix the plan artifact.
//
// RED: current implementation produces no stop at all (falls back silently).
func TestNext_UnresolvableApproach_StopReason_NamesApproachValue(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "UnknownApproach"},
	})
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

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, "UnknownApproach") {
		t.Errorf("StopDecision.Reason must name the unresolvable approach %q; got %q",
			"UnknownApproach", stop.Reason)
	}
}

// TestNext_UnresolvableApproach_InterStageAdvance_ReturnsStop verifies that an
// unresolvable approach at stage N+1 produces a StopDecision when the engine tries
// to advance from the last row of stage N.
//
// RED: current implementation falls back to ApproachTDD for the missing approach.
func TestNext_UnresolvableApproach_InterStageAdvance_ReturnsStop(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	// Stage 1 uses TDD (valid), stage 2 uses "UnknownApproach" (not in table).
	stages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "TDD"},
		{Number: 2, HITL: false, Approach: "UnknownApproach"},
	})
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	// implementation-review (row 10) is the last row of the Implementation group in TDD stage 1.
	// After it completes, the engine should try to advance to stage 2 but find "UnknownApproach" → stop.
	state := stateAfter("EXECUTION.Implementation.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusSUCCESS, 11)

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

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, "UnknownApproach") {
		t.Errorf("StopDecision.Reason must name the unresolvable approach in stage 2; got %q", stop.Reason)
	}
}

// TestNext_UnresolvableApproach_NoFallbackDispatch verifies that a StopDecision is
// returned (not a DispatchDecision) when the approach is unresolvable. This proves
// there is no silent fallback to a default ordering.
//
// RED: current implementation dispatches normally (silently falls back to TDD).
func TestNext_UnresolvableApproach_NoFallbackDispatch(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "UnknownApproach"},
	})
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

	// Must be Stop, not Dispatch.
	if dec.Dispatch != nil {
		t.Errorf("unresolvable approach must not produce a DispatchDecision (got agent %s); want StopDecision",
			agentName(dec.Dispatch.Steps[0].Request.AgentInstanceID))
	}
	if dec.Stop == nil {
		t.Error("unresolvable approach must produce a StopDecision")
	}
}

// unresolvableApproachResumeState is the artifact state of a clean completion
// of the Test-group build-review (row 9 of the build-verified workflow), with
// the row recorded on every workflow entry.
func unresolvableApproachResumeState() domain.ArtifactState {
	return stateWithLog(9,
		execLogEntry(8, "test-writer-tdd#8", "EXECUTION", "Test.1", domain.StatusSUCCESS, bvRowTestWriter),
		execLogEntry(9, "build-review#9", "EXECUTION", "Test.1", domain.StatusSUCCESS, bvRowTestBuild),
	)
}

// TestResumePoint_UnresolvableApproach_ReturnsError verifies that ResumePoint
// returns a non-nil error (not a silent end-of-run or guessed row) when the
// stage's approach is unresolvable while working out where the run continues.
//
// build-review appears in both the Test and Implementation groups. Its row is
// identified from the recorded row; the advance from that row then needs the
// stage's ordered groups, which cannot be built for "UnknownApproach".
func TestResumePoint_UnresolvableApproach_ReturnsError(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.1")
	// "UnknownApproach" is not in brownfield-tdd-build-verified's Execution Groups table.
	stages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "UnknownApproach"},
	})

	_, err := engine.ResumePoint(aw, stages, unresolvableApproachResumeState(), nil)

	if err == nil {
		t.Fatal("ResumePoint must return an error when the approach is unresolvable")
	}
}

// TestResumePoint_UnresolvableApproach_ErrorContainsApproachValue verifies that the
// error returned by ResumePoint wraps or chains an error containing the unresolvable
// approach value for user diagnostics.
func TestResumePoint_UnresolvableApproach_ErrorContainsApproachValue(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.1")
	stages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "UnknownApproach"},
	})

	_, err := engine.ResumePoint(aw, stages, unresolvableApproachResumeState(), nil)
	if err == nil {
		t.Fatal("ResumePoint must return an error")
	}

	// The error chain must include *domain.UnresolvableApproachError.
	var uae *domain.UnresolvableApproachError
	if !errors.As(err, &uae) {
		t.Errorf("error must wrap *domain.UnresolvableApproachError; got %T: %v", err, err)
	}
	if uae != nil && !strings.Contains(uae.Error(), "UnknownApproach") {
		t.Errorf("UnresolvableApproachError must name the approach value; got %q", uae.Error())
	}
}

// TestNext_UnresolvableApproach_InitialDispatch_StopViaNext verifies that the
// unresolvable-approach failure surfaces through engine.Next as a StopDecision
// when the initial EXECUTION dispatch itself has an unresolvable approach
// (no pre-execution rows).
//
// RED: current engine falls back to TDD approach silently.
func TestNext_UnresolvableApproach_InitialDispatch_StagedOnly_ReturnsStop(t *testing.T) {
	// threeGroupContent has no pre-execution rows and its stage must have a
	// matching approach. Use "NoSuchApproach" which is absent from the table.
	aw := mustParseAndAdmit(t, threeGroupContent, "three-group", "1.0")
	stages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "NoSuchApproach"},
	})
	agents := newTestAgents("agent-alpha", "agent-beta", "agent-gamma")

	// Initial dispatch (no prior log) → engine enters EXECUTION immediately.
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

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, "NoSuchApproach") {
		t.Errorf("StopDecision.Reason must name %q; got %q", "NoSuchApproach", stop.Reason)
	}
}

// TestNext_UnresolvableApproach_WinsOverGenericRowStop verifies that an
// unresolvable approach surfaces as a StopDecision naming the approach value,
// not as the generic "could not determine current row from artifact state"
// message, when Next advances from a row of an agent that fills several
// EXECUTION rows.
//
// In brownfield-tdd-build-verified, build-review appears in both the Test and
// Implementation groups. The log records the row that ran (the Test-group
// build-review), so the row is identified without any approach-dependent
// arithmetic; the approach error comes from ordering the groups to find the
// next row.
func TestNext_UnresolvableApproach_WinsOverGenericRowStop(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.1")
	stages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "UnknownApproach"},
	})
	log := &runLog{}
	log.
		workflowStep("test-writer-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestWriter).
		workflowStep("build-review", "Test.1", domain.StatusSUCCESS, bvRowTestBuild)

	dec := nextAfterLog(aw, stages, buildVerifiedAgents(), log)

	stop := requireStop(t, dec)
	// The stop reason must name the unresolvable approach value. The generic
	// "could not determine current row" message is the wrong outcome here.
	if !strings.Contains(stop.Reason, "UnknownApproach") {
		t.Errorf("StopDecision.Reason must name the unresolvable approach %q (not emit the generic row-not-found message); got %q",
			"UnknownApproach", stop.Reason)
	}
}

// ===== T4.3: No-groups-declared path =====
//
// These tests verify that when a workflow declares no groups (bare EXECUTION rows,
// no approach table), any Approach value in the StageSet is silently ignored and
// all EXECUTION rows run in declaration order.

// TestNext_NoGroups_UnknownApproachValue_DoesNotStop verifies that when the workflow
// has no groups (GroupsDeclared=false), an approach value not present in any table
// does NOT cause a StopDecision. The approach is simply ignored.
func TestNext_NoGroups_UnknownApproachValue_DoesNotStop(t *testing.T) {
	// implOnlyContent has no groups and no approach table.
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	stages := newTestStageSet([]domain.StageEntry{
		// "CompletelyUnknownToken" is not a known approach, but with no groups
		// declared the engine must ignore it rather than stopping.
		{Number: 1, HITL: false, Approach: "CompletelyUnknownToken"},
	})
	agents := newTestAgents("implementation-tdd", "implementation-review", "test-runner")

	// Fresh start: initial dispatch.
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

	// Must be a dispatch (rows run in order), not a Stop.
	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "implementation-tdd" {
		t.Errorf("no-groups workflow: want implementation-tdd (first EXECUTION row), got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_NoGroups_AllExecutionRowsDispatchedInOrder verifies that all EXECUTION
// rows in a bare (no-groups) workflow run in declaration order when the StageSet
// carries an approach value.
func TestNext_NoGroups_AllExecutionRowsDispatchedInOrder(t *testing.T) {
	// implOnlyContent: EXECUTION rows are implementation-tdd (row 0), implementation-review (row 1).
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	stages := newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "SomeOpaqueValue"},
	})
	agents := newTestAgents("implementation-tdd", "implementation-review", "test-runner")

	// After implementation-tdd (row 0, seq 1) completes, next must be implementation-review (row 1).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-tdd#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("implementation-tdd#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "implementation-review" {
		t.Errorf("no-groups: want implementation-review (second EXECUTION row), got %s",
			step.Request.AgentInstanceID)
	}
}
