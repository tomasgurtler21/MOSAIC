package engine_test

// Live routing (Next after a step) and resume (ResumePoint on the state that
// step wrote) derive the same row and stage from the same recorded log entry.

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// requireNextAndResumeAgree runs Next for the last log entry and ResumePoint
// on the same state, and requires both to name wantRow (zero-based) and the
// stage of wantStage. Next reports the recorded stage form ("Test.2"), while
// ResumePoint reports its own stage text plus the stage number, so the stages
// are compared by number.
func requireNextAndResumeAgree(
	t *testing.T,
	aw domain.AdmittedWorkflow,
	stages *domain.StageSet,
	agents map[string]domain.AgentReference,
	state domain.ArtifactState,
	wantRow int,
	wantStage string,
) {
	t.Helper()
	last := state.ExecutionLog[len(state.ExecutionLog)-1]
	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: responseFor(last),
		Agents:       agents,
		Seq:          last.Seq,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})
	step := requireDispatch(t, dec)

	info, err := engine.ResumePoint(aw, stages, state, domain.NewInfraAgentSet(declaredCheckpointInfraAgent()))
	if err != nil {
		t.Fatalf("ResumePoint: %v", err)
	}

	requireStepAt(t, step, wantRow, wantStage)
	_, wantNumber, _ := domain.ParseStageValue(wantStage)
	if info.RowIndex != wantRow {
		t.Errorf("ResumePoint row: want %d, got %d", wantRow, info.RowIndex)
	}
	if info.StageNumber != wantNumber {
		t.Errorf("ResumePoint stage number: want %d, got %d", wantNumber, info.StageNumber)
	}
	_, stepNumber, _ := domain.ParseStageValue(step.Stage)
	if info.RowIndex != step.RowIndex || info.StageNumber != stepNumber {
		t.Errorf("Next (row %d, stage %d) and ResumePoint (row %d, stage %d) disagree",
			step.RowIndex, stepNumber, info.RowIndex, info.StageNumber)
	}
}

func TestNextAndResumePoint_FrontmatterStageDiffersFromLogStage_BothFollowTheLogEntry(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := &runLog{}
	log.workflowStep("test-writer-tdd", "Test.2", domain.StatusSUCCESS, bvRowTestWriter)
	state := log.state()
	state.CurrentState.Stage = "Test.1" // frontmatter lags behind the logged step

	requireNextAndResumeAgree(t, aw, twoStageSet("TDD"), buildVerifiedAgents(), state,
		bvRowTestBuild-1, "Test.2")
}

func TestNextAndResumePoint_FrontmatterStageEmptyButLogStageSet_BothFollowTheLogEntry(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := &runLog{}
	log.workflowStep("build-review", "Implementation.2", domain.StatusSUCCESS, bvRowImplBuild)
	state := log.state()
	state.CurrentState.Stage = ""

	requireNextAndResumeAgree(t, aw, twoStageSet("TDD"), buildVerifiedAgents(), state,
		bvRowImplReview-1, "Implementation.2")
}

func TestNextAndResumePoint_AgentFillingTwoRowsOfOnePhase_BothFollowTheRecordedRow(t *testing.T) {
	aw := mustParseAndAdmit(t, twoPlanningRowsContent, "two-planning-rows", "1.0")
	log := &runLog{}
	log.
		workflowStep("planner-tdd-soft", "", domain.StatusSUCCESS, tpRowDraft).
		workflowStep("plan-review", "", domain.StatusCOMPLETED_NEEDS_ACTION, tpRowReview).
		workflowStep("planner-tdd-soft", "", domain.StatusSUCCESS, tpRowFinalise).
		inPhase("PLANNING")

	requireNextAndResumeAgree(t, aw, singleStageSet("TDD"), twoPlanningRowsAgents(), log.state(),
		tpRowImplement-1, "1")
}

func TestNextAndResumePoint_RouteBackRunWithRecordedRows_AgreeAfterEveryStep(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	// steps is the number of buildVerifiedRun steps logged. The third step is
	// the tests review asking for changes, which is not a clean continue and
	// is covered by the route-back dispatch test.
	wantAfter := []struct {
		steps int
		row   int
		stage string
	}{
		{1, bvRowTestBuild - 1, "Test.1"},      // after the writer
		{2, bvRowTestsReview - 1, "Test.1"},    // after the Test-group build-review
		{4, bvRowTestBuild - 1, "Test.1"},      // writer again
		{5, bvRowTestsReview - 1, "Test.1"},    // build-review again
		{6, bvRowImplementation - 1, "Implementation.1"},
		{7, bvRowImplBuild - 1, "Implementation.1"},
		{8, bvRowImplReview - 1, "Implementation.1"},
	}
	for _, want := range wantAfter {
		step := buildVerifiedRun[want.steps-1]
		t.Run(step.agent+"@"+step.stage, func(t *testing.T) {
			log := newBuildVerifiedLog().play(buildVerifiedRun, want.steps)

			requireNextAndResumeAgree(t, aw, singleStageSet("TDD"), buildVerifiedAgents(),
				resumeState(log, false), want.row, want.stage)
		})
	}
}
