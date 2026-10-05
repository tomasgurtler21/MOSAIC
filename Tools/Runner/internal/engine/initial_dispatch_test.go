package engine_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// ===== Initial dispatch (no prior invocations) =====

// TestNext_FirstCall_DispatchesFirstPreExecutionRow verifies that when the
// artifact has no execution history, Next dispatches the very first row of
// the workflow (a pre-execution row when the workflow starts before EXECUTION).
func TestNext_FirstCall_DispatchesFirstPreExecutionRow(t *testing.T) {
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
	// First row (index 0) is planner-tdd-soft in PLANNING phase.
	if step.RowIndex != 0 {
		t.Errorf("want RowIndex=0 (first row), got %d", step.RowIndex)
	}
	if agentName(step.Request.AgentInstanceID) != "planner-tdd-soft" {
		t.Errorf("want agent planner-tdd-soft, got %s", step.Request.AgentInstanceID)
	}
}

// TestNext_FirstCall_StagedOnly_DispatchesFirstExecutionRow verifies that a
// workflow with no pre-execution rows dispatches the first EXECUTION row of
// stage 1 on the initial call.
func TestNext_FirstCall_StagedOnly_DispatchesFirstExecutionRow(t *testing.T) {
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
	// implementation-only has no pre-execution rows; row 0 is EXECUTION.[StageNumber].
	if step.RowIndex != 0 {
		t.Errorf("want RowIndex=0 (first EXECUTION row), got %d", step.RowIndex)
	}
	if agentName(step.Request.AgentInstanceID) != "implementation-tdd" {
		t.Errorf("want agent implementation-tdd, got %s", step.Request.AgentInstanceID)
	}
	// Stage context must be the plain stage number for the first stage
	// (implementation-only declares no group).
	if step.Stage != "1" {
		t.Errorf("want Stage=1, got %q", step.Stage)
	}
}

// ===== Stage-set-absent diagnostic (domain.StageSource) =====
//
// When a staged row is reached with stages == nil, Next's stop message must
// name what was expected and where it was looked for, distinguishing "a
// stage table was supposed to be seeded here and is missing" from "this run
// never had one". stageSource is optional and variadic; omitting it (or
// passing the zero value) must still yield a non-empty stop reason.

// TestNext_NoStageSet_Omitted_StillProducesNonEmptyStopReason verifies that
// calling Next without a stageSource argument at all (the pre-existing call
// shape used everywhere else in this file) continues to produce a stop with
// a non-empty reason for a staged workflow with no stage set.
func TestNext_NoStageSet_Omitted_StillProducesNonEmptyStopReason(t *testing.T) {
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	agents := newTestAgents("implementation-tdd", "implementation-review", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if stop.Reason == "" {
		t.Error("want non-empty StopDecision.Reason when no stage set is available, got empty")
	}
}

// TestNext_NoStageSet_InitialDispatch_NeverSeeded_NamesPathLookedFor verifies
// that on the very first call (initial dispatch into a staged-only workflow),
// a stageSource whose Seeded field is false still names the path that was
// looked for in the stop reason.
func TestNext_NoStageSet_InitialDispatch_NeverSeeded_NamesPathLookedFor(t *testing.T) {
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	agents := newTestAgents("implementation-tdd", "implementation-review", "test-runner")
	src := domain.StageSource{Path: "/runs/example/Plan.md", Seeded: false}

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		StageSource:     src,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, src.Path) {
		t.Errorf("want StopDecision.Reason to name the looked-for path %q; got %q", src.Path, stop.Reason)
	}
}

// TestNext_NoStageSet_InitialDispatch_SeededButMissing_NamesPathLookedFor
// verifies that a stageSource whose Seeded field is true also names the path
// that was looked for -- the seeded-but-missing case must still identify
// where the table should have been.
func TestNext_NoStageSet_InitialDispatch_SeededButMissing_NamesPathLookedFor(t *testing.T) {
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	agents := newTestAgents("implementation-tdd", "implementation-review", "test-runner")
	src := domain.StageSource{Path: "/runs/example/Plan.md", Seeded: true}

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		StageSource:     src,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, src.Path) {
		t.Errorf("want StopDecision.Reason to name the looked-for path %q; got %q", src.Path, stop.Reason)
	}
}

// TestNext_NoStageSet_InitialDispatch_SeededVsNeverSeeded_DistinctMessages
// verifies that Seeded actually changes the rendered stop reason: the
// seeded-but-missing case and the never-seeded case, for the same Path, must
// be distinguishable from each other -- this is the entire point of carrying
// the Seeded field rather than just Path.
func TestNext_NoStageSet_InitialDispatch_SeededVsNeverSeeded_DistinctMessages(t *testing.T) {
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	agents := newTestAgents("implementation-tdd", "implementation-review", "test-runner")
	path := "/runs/example/Plan.md"

	neverSeeded := requireStop(t, engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		StageSource:     domain.StageSource{Path: path, Seeded: false},
		Mode:            domain.ExecutionModeAutoReview,
	}))
	seededMissing := requireStop(t, engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		StageSource:     domain.StageSource{Path: path, Seeded: true},
		Mode:            domain.ExecutionModeAutoReview,
	}))

	if neverSeeded.Reason == seededMissing.Reason {
		t.Errorf("want distinct stop reasons for Seeded=false vs Seeded=true (same Path); got identical reason %q for both",
			neverSeeded.Reason)
	}
}

// TestNext_NoStageSet_EnteringExecution_NeverSeeded_NamesPathLookedFor
// verifies that the second call site where a staged row can be reached with
// no stage set -- transitioning from a pre-EXECUTION row's SUCCESS into the
// staged EXECUTION phase -- also renders the stageSource path into the stop
// reason.
func TestNext_NoStageSet_EnteringExecution_NeverSeeded_NamesPathLookedFor(t *testing.T) {
	// quick-fix row 1: plan-review, OnSuccess="implementation-tdd" (an EXECUTION row).
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "plan-review#2", domain.StatusSUCCESS, 2)
	src := domain.StageSource{Path: "/runs/example/Plan.md", Seeded: false}

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           state,
		LastResponse:    successResponse("plan-review#2"),
		Agents:          agents,
		Seq:             2,
		Now:             fixedNow,
		StageSource:     src,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, src.Path) {
		t.Errorf("want StopDecision.Reason to name the looked-for path %q; got %q", src.Path, stop.Reason)
	}
}

// ===== Linear pre/post-execution advance =====

// TestNext_PreExecution_OnSuccess_AdvancesToNamedAgent verifies that a SUCCESS
// response from a pre-execution row routes to the agent named in On Success.
