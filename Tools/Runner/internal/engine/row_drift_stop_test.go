package engine_test

// For an agent that fills several EXECUTION rows, a missing, unreadable or
// inconsistent recorded row makes Next stop and report. It never dispatches a
// guessed row.

import (
	"strconv"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// requireStopNaming asserts a Stop whose reason names the agent instance and,
// when row is non-zero, the recorded row.
func requireStopNaming(t *testing.T, dec domain.EngineDecision, agentInstance string, row int) {
	t.Helper()
	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, agentInstance) {
		t.Errorf("stop reason must name agent %q, got %q", agentInstance, stop.Reason)
	}
	// Strip the instance id first: its sequence number must not satisfy the row check.
	withoutAgent := strings.ReplaceAll(stop.Reason, agentInstance, "")
	if row != 0 && !strings.Contains(withoutAgent, strconv.Itoa(row)) {
		t.Errorf("stop reason must name recorded row %d, got %q", row, stop.Reason)
	}
}

// nextAfterBuildReviewEntry drives Next after a build-review step recorded with
// the given stage and row. build-review fills two EXECUTION rows (9 and 12).
func nextAfterBuildReviewEntry(t *testing.T, stage string, row int) domain.EngineDecision {
	t.Helper()
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := &runLog{}
	log.
		workflowStep("test-writer-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestWriter).
		workflowStep("build-review", stage, domain.StatusSUCCESS, row)
	return nextAfterLog(aw, singleStageSet("TDD"), buildVerifiedAgents(), atInvocationNine(log))
}

// atInvocationNine renumbers the last entry as invocation 9, the position the
// old invocation-count arithmetic maps to the Test-group build-review row.
// Stopping must come from the recorded row, not from that arithmetic failing.
func atInvocationNine(log *runLog) *runLog {
	last := &log.entries[len(log.entries)-1]
	last.Seq = 9
	last.Agent = "build-review#9"
	log.globalSeq = 9
	return log
}

func TestNext_MultiRowAgent_EntryWithoutRecordedRow_StopsNamingAgent(t *testing.T) {
	dec := nextAfterBuildReviewEntry(t, "Test.1", int(domain.NoWorkflowRow))

	requireStopNaming(t, dec, "build-review#9", 0)
}

func TestNext_MultiRowAgent_RecordedRowHoldsDifferentAgent_StopsNamingAgentAndRow(t *testing.T) {
	// Row 10 is tests-review-tdd, not build-review.
	dec := nextAfterBuildReviewEntry(t, "Test.1", bvRowTestsReview)

	requireStopNaming(t, dec, "build-review#9", bvRowTestsReview)
}

func TestNext_MultiRowAgent_RecordedRowInOtherGroupThanStage_StopsNamingAgentAndRow(t *testing.T) {
	// Row 9 is a Test-group row; the entry's stage names the Implementation group.
	dec := nextAfterBuildReviewEntry(t, "Implementation.1", bvRowTestBuild)

	requireStopNaming(t, dec, "build-review#9", bvRowTestBuild)
}

func TestNext_MultiRowAgent_UnreadableStage_StopsNamingAgentAndRow(t *testing.T) {
	for _, stage := range []string{"", "Stage-1", "garbage"} {
		t.Run("stage="+stage, func(t *testing.T) {
			dec := nextAfterBuildReviewEntry(t, stage, bvRowTestBuild)

			requireStopNaming(t, dec, "build-review#9", bvRowTestBuild)
		})
	}
}

func TestNext_MultiRowAgent_RecordedRowOutsideTable_StopsNamingAgentAndRow(t *testing.T) {
	// The build-verified workflow has 13 rows.
	for _, row := range []int{14, 99} {
		t.Run("row="+strconv.Itoa(row), func(t *testing.T) {
			dec := nextAfterBuildReviewEntry(t, "Test.1", row)

			requireStopNaming(t, dec, "build-review#9", row)
		})
	}
}

func TestNext_MultiRowAgent_NoLogEntryForLastAgent_StopsNamingAgent(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := &runLog{}
	log.workflowStep("test-writer-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestWriter)
	state := log.state()
	state.GlobalSequence = 9
	state.CurrentState.LastAgent = "build-review#9" // no log entry has this instance id

	dec := engineNextAuto(aw, singleStageSet("TDD"), state, successResponse("build-review#9"))

	requireStopNaming(t, dec, "build-review#9", 0)
}

func TestNext_MultiRowAgent_OnlyEarlierInstanceLogged_StopsInsteadOfUsingItsRow(t *testing.T) {
	// An earlier instance of the same agent (build-review#2) is logged with a
	// valid row, but the step that just ran (build-review#9) is not. The row of
	// a different instance must not be borrowed.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := &runLog{}
	log.
		workflowStep("test-writer-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestWriter).
		workflowStep("build-review", "Test.1", domain.StatusSUCCESS, bvRowTestBuild)
	state := log.state()
	state.GlobalSequence = 9
	state.CurrentState.LastAgent = "build-review#9"

	dec := engineNextAuto(aw, singleStageSet("TDD"), state, successResponse("build-review#9"))

	requireStopNaming(t, dec, "build-review#9", 0)
}
