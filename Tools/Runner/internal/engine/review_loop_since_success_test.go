package engine_test

// Review loop limit: the count is the reviewer's COMPLETED_NEEDS_ACTION rows at
// the current phase and stage since the reviewer's last SUCCESS there. It is
// keyed by reviewer, phase and stage, not by workflow row.

import (
	"testing"

	"mosaic-run/internal/domain"
)

// loopWithSuccessInside is a reviewer that raised findings four times
// (#5, #7, #9, #11), passed (#13), and raised findings again (#72), with the
// fixer re-working between.
func loopWithSuccessInside() []domain.ExecutionLogEntry {
	return []domain.ExecutionLogEntry{
		rllRow(5, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(6, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(7, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(8, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(9, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(10, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(11, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(12, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(13, rllReviewer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(71, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(72, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
}

// The count at #72 is 1: the four earlier findings precede the reviewer's
// SUCCESS and no longer count.
func TestNext_ReviewLoopLimit_CountsOnlySinceReviewersLastSuccess(t *testing.T) {
	log := loopWithSuccessInside()

	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log), rllFixer)
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 5, log), rllFixer)
	requireLoopLimitDeviation(t, rllNext(t, domain.ExecutionModeAutoReview, 1, log))
}

// Without the reviewer's SUCCESS the same findings all count.
func TestNext_ReviewLoopLimit_WithoutSuccess_CountsEveryFinding(t *testing.T) {
	log := loopWithSuccessInside()
	log = append(log[:8:8], log[9:]...) // drop the reviewer's SUCCESS (#13)

	requireLoopLimitDeviation(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log))
}

// A SUCCESS by another agent does not reset the reviewer's count.
func TestNext_ReviewLoopLimit_OtherAgentsSuccess_DoesNotReset(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(2, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(3, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(4, "test-runner", rllPhase, "", domain.StatusSUCCESS),
		rllRow(5, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(6, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}

	requireLoopLimitDeviation(t, rllNext(t, domain.ExecutionModeAutoReview, 3, log))
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 4, log), rllFixer)
}

// A reviewer's SUCCESS in another phase does not reset the count here.
func TestNext_ReviewLoopLimit_SuccessInOtherPhase_DoesNotReset(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(2, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(3, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(4, rllReviewer, "REVIEW", "", domain.StatusSUCCESS),
		rllRow(5, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(6, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}

	requireLoopLimitDeviation(t, rllNext(t, domain.ExecutionModeAutoReview, 3, log))
}

// Decision: a reviewer SUCCESS that directly follows the reviewer's own
// COMPLETED_NEEDS_ACTION is a repair re-dispatch of the same iteration, not a
// pass. It does not reset the count; the earlier finding still counts once.
// (A SUCCESS after the fixer re-worked, as in loopWithSuccessInside, resets.)
func TestNext_ReviewLoopLimit_ReviewerSuccessDirectlyAfterOwnCNA_DoesNotReset(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(2, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(3, rllReviewer, rllPhase, "", domain.StatusSUCCESS),
	}

	requireLoopLimitDeviation(t, rllNext(t, domain.ExecutionModeAutoReview, 1, log))
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log), rllFixer)
}

// The repair SUCCESS does not hide earlier iterations: two iterations, the
// second repaired by a reviewer SUCCESS, count 2.
func TestNext_ReviewLoopLimit_RepairSuccessKeepsEarlierIterationsCounted(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(2, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(3, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(4, rllReviewer, rllPhase, "", domain.StatusSUCCESS),
	}

	requireLoopLimitDeviation(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log))
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 3, log), rllFixer)
}

// plan-review fills two PLANNING rows: a first review and a final review.
const reviewerTwoRowsContent = `## Reviewer Two Rows Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | - | Plan.md, Stage-*/Plan.md |
| PLANNING | plan-review | FALSE | planner-tdd-soft | planner-tdd-soft | Plan.md | plan-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Plan.md, plan-review.md | Plan.md |
| PLANNING | plan-review | FALSE | implementation-tdd | planner-tdd-soft | Plan.md | plan-review-final.md |
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | test-runner | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |
`

// A reviewer that fills two rows of a phase shares one count for that phase
// and stage: findings at both rows add up.
func TestNext_ReviewLoopLimit_ReviewerInTwoRows_SharesOneCount(t *testing.T) {
	aw := mustParseAndAdmit(t, reviewerTwoRowsContent, "reviewer-two-rows", "1.0")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	log := (&runLog{}).
		workflowStep("planner-tdd-soft", "", domain.StatusSUCCESS, 1).
		workflowStep("plan-review", "", domain.StatusCOMPLETED_NEEDS_ACTION, 2).
		workflowStep("planner-tdd-soft", "", domain.StatusSUCCESS, 3).
		workflowStep("plan-review", "", domain.StatusCOMPLETED_NEEDS_ACTION, 4).
		inPhase(rllPhase)
	run := func(limit int) domain.EngineDecision {
		state := log.state()
		state.ReviewLoopLimit = limit
		return nextAfterState(aw, singleStageSet("TDD"), agents, state, cnaResponse(log.last().Agent))
	}

	requireLoopLimitDeviation(t, run(2))
	requireRoutedTo(t, run(3), "planner-tdd-soft")
}

// A SUCCESS at one of the reviewer's rows resets the shared count for both.
func TestNext_ReviewLoopLimit_ReviewerInTwoRows_SuccessResetsSharedCount(t *testing.T) {
	aw := mustParseAndAdmit(t, reviewerTwoRowsContent, "reviewer-two-rows", "1.0")
	agents := newTestAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	log := (&runLog{}).
		workflowStep("plan-review", "", domain.StatusCOMPLETED_NEEDS_ACTION, 2).
		workflowStep("planner-tdd-soft", "", domain.StatusSUCCESS, 3).
		workflowStep("plan-review", "", domain.StatusCOMPLETED_NEEDS_ACTION, 2).
		workflowStep("planner-tdd-soft", "", domain.StatusSUCCESS, 3).
		workflowStep("plan-review", "", domain.StatusSUCCESS, 4).
		workflowStep("planner-tdd-soft", "", domain.StatusSUCCESS, 3).
		workflowStep("plan-review", "", domain.StatusCOMPLETED_NEEDS_ACTION, 4).
		inPhase(rllPhase)
	state := log.state()
	state.ReviewLoopLimit = 2

	dec := nextAfterState(aw, singleStageSet("TDD"), agents, state, cnaResponse(log.last().Agent))

	requireRoutedTo(t, dec, "planner-tdd-soft")
}
