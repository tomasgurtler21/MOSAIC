package engine_test

// An EXECUTION log row that records a workflow row in a staged group but no
// stage is refused by both Next and ResumePoint with a typed cause that names
// the row and the repair. The opaque "stage 0 has no entry in stage set"
// failure is never reached.

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// stagedRowWithoutStageLog is a run whose last step is a build-review in the
// Implementation group, recorded with row 12 and an empty stage.
func stagedRowWithoutStageLog() *runLog {
	log := &runLog{}
	log.workflowStep("build-review", "", domain.StatusSUCCESS, bvRowImplBuild)
	return log
}

func requireNamesRowAndRepair(t *testing.T, msg string, row int) {
	t.Helper()
	if !strings.Contains(msg, strconv.Itoa(row)) {
		t.Errorf("message must name row %d, got %q", row, msg)
	}
	if !strings.Contains(msg, "Implementation.") {
		t.Errorf("message must give the repair in group form (Implementation.N), got %q", msg)
	}
	if strings.Contains(msg, "stage 0 has no entry") {
		t.Errorf("message must not be the opaque stage-set failure, got %q", msg)
	}
}

func TestNext_StagedRowRecordedWithoutStage_RefusesWithTypedCauseNamingRowAndRepair(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")

	dec := nextAfterLog(aw, singleStageSet("TDD"), buildVerifiedAgents(), stagedRowWithoutStageLog())

	perr := requirePositionCause(t, dec, domain.CauseStagedRowWithoutStage)
	requireNamesRowAndRepair(t, requireStop(t, dec).Reason, bvRowImplBuild)
	if perr.RecordedRow != domain.WorkflowRow(bvRowImplBuild) {
		t.Errorf("error must carry row %d, got %d", bvRowImplBuild, perr.RecordedRow)
	}
}

func TestNext_StagedRowRecordedWithoutStage_FrontmatterStageDoesNotHideIt(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := stagedRowWithoutStageLog()
	state := log.state()
	state.CurrentState.Stage = "Implementation.1"

	dec := engineNextAuto(aw, singleStageSet("TDD"), state, successResponse(log.last().Agent))

	requirePositionCause(t, dec, domain.CauseStagedRowWithoutStage)
}

func TestNext_StagedRowRecordedWithoutStage_StopReasonIsTheTypedError(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")

	dec := nextAfterLog(aw, singleStageSet("TDD"), buildVerifiedAgents(), stagedRowWithoutStageLog())

	stop := requireStop(t, dec)
	if stop.Err == nil || stop.Reason != stop.Err.Error() {
		t.Errorf("stop reason must equal the typed error text, reason %q, err %v", stop.Reason, stop.Err)
	}
}

func TestResumePoint_StagedRowRecordedWithoutStage_RefusesWithTypedCauseNamingRowAndRepair(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	for _, interrupted := range []bool{false, true} {
		name := "clean"
		if interrupted {
			name = "interrupted"
		}
		t.Run(name, func(t *testing.T) {
			log := newBuildVerifiedLog()
			log.workflowStep("implementation-tdd", "Implementation.1", domain.StatusSUCCESS, bvRowImplementation)
			log.workflowStep("build-review", "", domain.StatusSUCCESS, bvRowImplBuild)

			_, err := resumeLog(aw, singleStageSet("TDD"), log, interrupted)

			var perr *domain.PositionUnresolvedError
			if !errors.As(err, &perr) {
				t.Fatalf("want *PositionUnresolvedError, got %v", err)
			}
			if perr.Cause != domain.CauseStagedRowWithoutStage {
				t.Errorf("position cause: want CauseStagedRowWithoutStage, got %d (%v)", perr.Cause, perr)
			}
			requireNamesRowAndRepair(t, err.Error(), bvRowImplBuild)
		})
	}
}

func TestResumePoint_NonStagedRowRecordedWithoutStage_IsNotRefused(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	state := resumeState(newBuildVerifiedLog(), false)

	info, err := engine.ResumePoint(aw, singleStageSet("TDD"), state,
		domain.NewInfraAgentSet(declaredCheckpointInfraAgent()))

	if err != nil {
		t.Fatalf("a PLANNING/DESIGN row has no stage and must resume, got %v", err)
	}
	if info.RowIndex != bvRowTestWriter-1 {
		t.Errorf("resume row: want %d, got %d", bvRowTestWriter-1, info.RowIndex)
	}
}
