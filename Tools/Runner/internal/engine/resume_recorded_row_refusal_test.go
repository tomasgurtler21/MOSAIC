package engine_test

// ResumePoint refuses a recorded workflow row that is not in the table or
// that holds another agent, for non-EXECUTION and single-row EXECUTION agents
// as well as for agents that fill several EXECUTION rows.

import (
	"errors"
	"strconv"
	"testing"

	"mosaic-run/internal/domain"
)

func requireResumeRefusesRecordedRow(t *testing.T, log *runLog, wantRow int) {
	t.Helper()
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	for _, interrupted := range []bool{false, true} {
		name := "clean"
		if interrupted {
			name = "interrupted"
		}
		t.Run(name, func(t *testing.T) {
			_, err := resumeLog(aw, singleStageSet("TDD"), log, interrupted)

			var perr *domain.PositionUnresolvedError
			if !errors.As(err, &perr) {
				t.Fatalf("want *PositionUnresolvedError, got %v", err)
			}
			if perr.Cause != domain.CauseRecordedRowInvalid {
				t.Errorf("position cause: want CauseRecordedRowInvalid, got %d (%v)", perr.Cause, perr)
			}
			if perr.RecordedRow != domain.WorkflowRow(wantRow) {
				t.Errorf("error must carry recorded row %d, got %d", wantRow, perr.RecordedRow)
			}
		})
	}
}

func TestResumePoint_NonExecutionAgentRecordedRowHoldsDifferentAgent_Refuses(t *testing.T) {
	// Row 3 is requirements-review, not codebase-research.
	log := newBuildVerifiedLog()
	log.workflowStep("codebase-research", "", domain.StatusSUCCESS, 3).lastInPhase("RESEARCH")

	requireResumeRefusesRecordedRow(t, log, 3)
}

func TestResumePoint_NonExecutionAgentRecordedRowOutsideTable_Refuses(t *testing.T) {
	for _, row := range []int{14, 99} {
		t.Run("row="+strconv.Itoa(row), func(t *testing.T) {
			log := newBuildVerifiedLog()
			log.workflowStep("codebase-research", "", domain.StatusSUCCESS, row).lastInPhase("RESEARCH")

			requireResumeRefusesRecordedRow(t, log, row)
		})
	}
}

func TestResumePoint_SingleRowExecutionAgentRecordedRowHoldsDifferentAgent_Refuses(t *testing.T) {
	log := newBuildVerifiedLog()
	log.workflowStep("test-writer-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestsReview)

	requireResumeRefusesRecordedRow(t, log, bvRowTestsReview)
}

func TestResumePoint_SingleRowExecutionAgentRecordedRowOutsideTable_Refuses(t *testing.T) {
	log := newBuildVerifiedLog()
	log.workflowStep("test-writer-tdd", "Test.1", domain.StatusSUCCESS, 99)

	requireResumeRefusesRecordedRow(t, log, 99)
}
