package engine_test

// Live routing identifies the row that ran from the row recorded in the
// Execution Log, not from the invocation count. These tests drive engine.Next
// through findings route-backs for agents that fill several EXECUTION rows.

import (
	"testing"

	"mosaic-run/internal/domain"
)

// requireStepAt asserts the dispatched step's zero-based row index and stage.
func requireStepAt(t *testing.T, step domain.DispatchStep, wantRowIndex int, wantStage string) {
	t.Helper()
	if step.RowIndex != wantRowIndex {
		t.Errorf("dispatched row index: want %d, got %d (agent %s)",
			wantRowIndex, step.RowIndex, step.Request.AgentInstanceID)
	}
	if step.Stage != wantStage {
		t.Errorf("dispatched stage: want %q, got %q", wantStage, step.Stage)
	}
}

// buildVerifiedRouteBackLog is the hand-built run: test-writer-tdd, build-review,
// tests-review-tdd (needs action), test-writer-tdd again, build-review again,
// all in the Test group of stage 1.
func buildVerifiedRouteBackLog() *runLog {
	log := &runLog{}
	return log.
		workflowStep("test-writer-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestWriter).
		workflowStep("build-review", "Test.1", domain.StatusSUCCESS, bvRowTestBuild).
		workflowStep("tests-review-tdd", "Test.1", domain.StatusCOMPLETED_NEEDS_ACTION, bvRowTestsReview).
		workflowStep("test-writer-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestWriter).
		workflowStep("build-review", "Test.1", domain.StatusSUCCESS, bvRowTestBuild)
}

func TestNext_BuildVerified_TestGroupBuildReviewAfterRouteBack_SuccessContinuesToTestsReview(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")

	step := requireNextStep(t, aw, singleStageSet("TDD"), buildVerifiedAgents(), buildVerifiedRouteBackLog())

	if got := agentName(step.Request.AgentInstanceID); got != "tests-review-tdd" {
		t.Errorf("after the Test-group build-review that follows a route-back, want tests-review-tdd, got %s", got)
	}
	requireStepAt(t, step, bvRowTestsReview-1, "Test.1")
}

func TestNext_BuildVerified_TestGroupBuildReviewAfterRouteBack_NeedsActionRoutesToTestWriter(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := buildVerifiedRouteBackLog()
	log.entries[len(log.entries)-1].Status = domain.StatusCOMPLETED_NEEDS_ACTION

	step := requireNextStep(t, aw, singleStageSet("TDD"), buildVerifiedAgents(), log)

	if got := agentName(step.Request.AgentInstanceID); got != "test-writer-tdd" {
		t.Errorf("findings from the Test-group build-review must route to test-writer-tdd, got %s", got)
	}
	requireStepAt(t, step, bvRowTestWriter-1, "Test.1")
}

func TestNext_BuildVerified_ContinuedRunAfterRouteBack_DispatchesImplementation(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := buildVerifiedRouteBackLog().
		workflowStep("tests-review-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestsReview)

	step := requireNextStep(t, aw, singleStageSet("TDD"), buildVerifiedAgents(), log)

	if got := agentName(step.Request.AgentInstanceID); got != "implementation-tdd" {
		t.Errorf("after tests-review-tdd succeeds, want implementation-tdd, got %s", got)
	}
	requireStepAt(t, step, bvRowTestsReview, "Implementation.1")
}

func TestNext_BuildVerified_ImplementationBuildReviewAfterRouteBack_ContinuesWithinImplementationGroup(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := buildVerifiedRouteBackLog().
		workflowStep("tests-review-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestsReview).
		workflowStep("implementation-tdd", "Implementation.1", domain.StatusSUCCESS, bvRowImplementation).
		workflowStep("build-review", "Implementation.1", domain.StatusSUCCESS, bvRowImplBuild)

	step := requireNextStep(t, aw, singleStageSet("TDD"), buildVerifiedAgents(), log)

	if got := agentName(step.Request.AgentInstanceID); got != "implementation-review" {
		t.Errorf("after the Implementation-group build-review, want implementation-review, got %s", got)
	}
	requireStepAt(t, step, bvRowImplReview-1, "Implementation.1")
}

// ===== Staged findings loop: one agent fills every EXECUTION row =====

// stagedFindingsLoopContent mirrors a workflow in which a single agent fills
// three Test-group rows and one Implementation-group row, and the gate row
// names that same agent as its On Findings target.
const stagedFindingsLoopContent = `## Staged Findings Loop Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.Test.[StageNumber] | loop-agent | FALSE | - | - | Script/writer.md | Stage-{StageNumber}/Written.md |
| EXECUTION.Test.[StageNumber] | loop-agent | FALSE | - | loop-agent | Script/gate.md | Stage-{StageNumber}/Gate.md |
| EXECUTION.Test.[StageNumber] | loop-agent | FALSE | - | - | Script/review.md | Stage-{StageNumber}/Review.md |
| EXECUTION.Implementation.[StageNumber] | loop-agent | FALSE | - | - | Script/impl.md | Stage-{StageNumber}/Impl.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
`

const (
	loopRowWriter = 1
	loopRowGate   = 2
	loopRowReview = 3
	loopRowImpl   = 4
)

func TestNext_StagedFindingsLoop_RepeatedRouteBackCycles_IdentifiesRowAfterEveryStep(t *testing.T) {
	aw := mustParseAndAdmit(t, stagedFindingsLoopContent, "staged-findings-loop", "1.0")
	stages := singleStageSet("TDD")
	agents := newTestAgents("loop-agent")

	// Each step is followed by the zero-based row and stage the engine must
	// dispatch next.
	steps := []struct {
		name      string
		stage     string
		status    domain.StatusCode
		row       int
		wantIndex int
		wantStage string
	}{
		{"writer", "Test.1", domain.StatusSUCCESS, loopRowWriter, loopRowGate - 1, "Test.1"},
		{"gate needs action", "Test.1", domain.StatusCOMPLETED_NEEDS_ACTION, loopRowGate, loopRowWriter - 1, "Test.1"},
		{"writer again", "Test.1", domain.StatusSUCCESS, loopRowWriter, loopRowGate - 1, "Test.1"},
		{"gate needs action again", "Test.1", domain.StatusCOMPLETED_NEEDS_ACTION, loopRowGate, loopRowWriter - 1, "Test.1"},
		{"writer a third time", "Test.1", domain.StatusSUCCESS, loopRowWriter, loopRowGate - 1, "Test.1"},
		{"gate succeeds", "Test.1", domain.StatusSUCCESS, loopRowGate, loopRowReview - 1, "Test.1"},
		{"review", "Test.1", domain.StatusSUCCESS, loopRowReview, loopRowImpl - 1, "Implementation.1"},
	}

	log := &runLog{}
	for _, s := range steps {
		log.workflowStep("loop-agent", s.stage, s.status, s.row)
		snapshot := &runLog{entries: append([]domain.ExecutionLogEntry(nil), log.entries...)}

		t.Run(s.name, func(t *testing.T) {
			step := requireNextStep(t, aw, stages, agents, snapshot)
			requireStepAt(t, step, s.wantIndex, s.wantStage)
		})
	}
}
