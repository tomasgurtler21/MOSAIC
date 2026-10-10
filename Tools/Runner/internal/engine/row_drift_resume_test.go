package engine_test

// Resume identifies the row of the last logged step from the row recorded in
// the Execution Log, not from the invocation count. These tests drive
// engine.ResumePoint with hand-built logs that carry a recorded row on every
// workflow entry, for runs that contain findings route-backs and
// infrastructure steps.

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// Zero-based group positions in the admitted workflow's Groups.
const (
	groupTest = 0
	groupImpl = 1
)

// runStep is one workflow step of a hand-built run.
type runStep struct {
	agent  string
	stage  string
	status domain.StatusCode
	row    int
}

// play appends the first n steps to the log as workflow steps.
func (l *runLog) play(steps []runStep, n int) *runLog {
	for _, s := range steps[:n] {
		l.workflowStep(s.agent, s.stage, s.status, s.row)
	}
	return l
}

// preExecutionEntries returns the seven pre-execution entries of the
// build-verified workflow, each with its recorded row.
func preExecutionEntries() []domain.ExecutionLogEntry {
	rows := []struct{ agent, phase string }{
		{"codebase-research", "RESEARCH"}, {"requirements-refinement", "RESEARCH"},
		{"requirements-review", "RESEARCH"}, {"planner-tdd-soft", "PLANNING"},
		{"plan-review", "PLANNING"}, {"contracts-designer", "DESIGN"},
		{"contracts-review", "DESIGN"},
	}
	entries := make([]domain.ExecutionLogEntry, 0, len(rows))
	for i, r := range rows {
		seq := i + 1
		entries = append(entries, execLogEntry(seq, r.agent+"#"+strconv.Itoa(seq), r.phase, "", domain.StatusSUCCESS, seq))
	}
	return entries
}

// newBuildVerifiedLog starts a log with the pre-execution entries, so the
// invocation count runs ahead of the EXECUTION row positions as in a real run.
func newBuildVerifiedLog() *runLog {
	return &runLog{entries: preExecutionEntries()}
}

// lastWorkflowIndex returns the index of the latest entry at or before from
// that is not an infrastructure entry, or -1.
func lastWorkflowIndex(entries []domain.ExecutionLogEntry, from int) int {
	for i := from; i >= 0; i-- {
		if !strings.HasPrefix(entries[i].Agent, checkpointAgent+"#") {
			return i
		}
	}
	return -1
}

// resumeState returns the artifact state of the log. When the run ended
// cleanly, CurrentState names the last workflow step. When it was interrupted,
// the last workflow step was logged but CurrentState still names the one before.
func resumeState(log *runLog, interrupted bool) domain.ArtifactState {
	cur := lastWorkflowIndex(log.entries, len(log.entries)-1)
	if interrupted {
		cur = lastWorkflowIndex(log.entries, cur-1)
	}
	state := domain.ArtifactState{GlobalSequence: len(log.entries), ExecutionLog: log.entries}
	if cur >= 0 {
		e := log.entries[cur]
		state.CurrentState = domain.CurrentState{
			Phase: e.Phase, Stage: e.Stage, LastStatus: e.Status, LastAgent: e.Agent,
		}
	}
	return state
}

// resumeLog calls ResumePoint with the checkpoint infrastructure agent declared.
func resumeLog(
	aw domain.AdmittedWorkflow,
	stages *domain.StageSet,
	log *runLog,
	interrupted bool,
) (domain.ResumeInfo, error) {
	infra := domain.NewInfraAgentSet(declaredCheckpointInfraAgent())
	return engine.ResumePoint(aw, stages, resumeState(log, interrupted), infra)
}

// requireResumeAt asserts a successful resume at the given zero-based row.
func requireResumeAt(
	t *testing.T,
	info domain.ResumeInfo,
	err error,
	wantRow int,
	wantRerun bool,
	wantStage domain.StageNumber,
	wantGroup int,
) {
	t.Helper()
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if info.RowIndex != wantRow {
		t.Errorf("RowIndex: want %d, got %d", wantRow, info.RowIndex)
	}
	if info.RerunLast != wantRerun {
		t.Errorf("RerunLast: want %v, got %v", wantRerun, info.RerunLast)
	}
	if info.StageNumber != wantStage {
		t.Errorf("StageNumber: want %d, got %d", wantStage, info.StageNumber)
	}
	if info.GroupIndex != wantGroup {
		t.Errorf("GroupIndex: want %d, got %d", wantGroup, info.GroupIndex)
	}
}

// ===== Build-verified shape =====

// buildVerifiedRun is one stage of the build-verified workflow with a
// tests-review-tdd findings route-back after the Test group's build-review.
var buildVerifiedRun = []runStep{
	{"test-writer-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestWriter},
	{"build-review", "Test.1", domain.StatusSUCCESS, bvRowTestBuild},
	{"tests-review-tdd", "Test.1", domain.StatusCOMPLETED_NEEDS_ACTION, bvRowTestsReview},
	{"test-writer-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestWriter},
	{"build-review", "Test.1", domain.StatusSUCCESS, bvRowTestBuild},
	{"tests-review-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestsReview},
	{"implementation-tdd", "Implementation.1", domain.StatusSUCCESS, bvRowImplementation},
	{"build-review", "Implementation.1", domain.StatusSUCCESS, bvRowImplBuild},
	{"implementation-review", "Implementation.1", domain.StatusSUCCESS, bvRowImplReview},
}

// Each case resumes after the first n steps of buildVerifiedRun. A clean
// resume continues at the row after the last step (cleanRow, zero-based); an
// interrupted resume re-runs the last step's row (the row recorded for it).
var buildVerifiedResumeCases = []struct {
	name       string
	n          int
	cleanRow   int
	cleanGroup int
	rerunGroup int
}{
	{"writer re-run after route-back", 4, bvRowTestBuild - 1, groupTest, groupTest},
	{"test-group build-review after route-back", 5, bvRowTestsReview - 1, groupTest, groupTest},
	{"tests review passes", 6, bvRowImplementation - 1, groupImpl, groupTest},
	{"implementation", 7, bvRowImplBuild - 1, groupImpl, groupImpl},
	{"implementation-group build-review", 8, bvRowImplReview - 1, groupImpl, groupImpl},
	{"last row", 9, bvRowImplReview, -1, groupImpl},
}

func TestResumePoint_BuildVerifiedAfterRouteBack_CleanCompletionContinuesFromRecordedRow(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	for _, tc := range buildVerifiedResumeCases {
		t.Run(tc.name, func(t *testing.T) {
			log := newBuildVerifiedLog().play(buildVerifiedRun, tc.n)

			info, err := resumeLog(aw, singleStageSet("TDD"), log, false)

			wantStage := domain.StageNumber(1)
			if tc.cleanGroup < 0 { // run complete
				wantStage = 0
			}
			requireResumeAt(t, info, err, tc.cleanRow, false, wantStage, tc.cleanGroup)
		})
	}
}

func TestResumePoint_BuildVerifiedAfterRouteBack_InterruptedStepRerunsRecordedRow(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	for _, tc := range buildVerifiedResumeCases {
		t.Run(tc.name, func(t *testing.T) {
			log := newBuildVerifiedLog().play(buildVerifiedRun, tc.n)
			wantRow := buildVerifiedRun[tc.n-1].row - 1

			info, err := resumeLog(aw, singleStageSet("TDD"), log, true)

			requireResumeAt(t, info, err, wantRow, true, 1, tc.rerunGroup)
		})
	}
}

// ===== Staged findings loop shape =====

// stagedLoopRun repeats the route-back cycle twice: the gate asks for findings
// to be fixed twice before it passes.
var stagedLoopRun = []runStep{
	{"loop-agent", "Test.1", domain.StatusSUCCESS, loopRowWriter},
	{"loop-agent", "Test.1", domain.StatusCOMPLETED_NEEDS_ACTION, loopRowGate},
	{"loop-agent", "Test.1", domain.StatusSUCCESS, loopRowWriter},
	{"loop-agent", "Test.1", domain.StatusCOMPLETED_NEEDS_ACTION, loopRowGate},
	{"loop-agent", "Test.1", domain.StatusSUCCESS, loopRowWriter},
	{"loop-agent", "Test.1", domain.StatusSUCCESS, loopRowGate},
	{"loop-agent", "Test.1", domain.StatusSUCCESS, loopRowReview},
	{"loop-agent", "Implementation.1", domain.StatusSUCCESS, loopRowImpl},
}

func TestResumePoint_StagedFindingsLoop_CleanCompletionContinuesFromRecordedRow(t *testing.T) {
	aw := mustParseAndAdmit(t, stagedFindingsLoopContent, "staged-findings-loop", "1.0")
	// n is the number of steps logged; steps ending in a findings status are
	// not resumed cleanly.
	cases := []struct {
		n, wantRow, wantGroup int
	}{
		{1, loopRowGate - 1, groupTest},
		{3, loopRowGate - 1, groupTest},
		{5, loopRowGate - 1, groupTest},
		{6, loopRowReview - 1, groupTest},
		{7, loopRowImpl - 1, groupImpl},
		{8, len(aw.Table.Rows), -1},
	}
	for _, tc := range cases {
		t.Run("after step "+strconv.Itoa(tc.n), func(t *testing.T) {
			log := (&runLog{}).play(stagedLoopRun, tc.n)

			info, err := resumeLog(aw, singleStageSet("TDD"), log, false)

			wantStage := domain.StageNumber(1)
			if tc.wantGroup < 0 { // run complete
				wantStage = 0
			}
			requireResumeAt(t, info, err, tc.wantRow, false, wantStage, tc.wantGroup)
		})
	}
}

func TestResumePoint_StagedFindingsLoop_InterruptedStepRerunsRecordedRow(t *testing.T) {
	aw := mustParseAndAdmit(t, stagedFindingsLoopContent, "staged-findings-loop", "1.0")
	for n := 1; n <= len(stagedLoopRun); n++ {
		t.Run("step "+strconv.Itoa(n), func(t *testing.T) {
			log := (&runLog{}).play(stagedLoopRun, n)
			row := stagedLoopRun[n-1].row
			wantGroup := groupTest
			if row == loopRowImpl {
				wantGroup = groupImpl
			}

			info, err := resumeLog(aw, singleStageSet("TDD"), log, true)

			requireResumeAt(t, info, err, row-1, true, 1, wantGroup)
		})
	}
}

// ===== Infrastructure entries =====

const checkpointAgent = "checkpoint-manager-git"

func TestResumePoint_BuildVerified_TrailingInfrastructureEntry_ResumesFromRecordedRow(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := newBuildVerifiedLog().play(buildVerifiedRun, 5).infraStep(checkpointAgent, "Test.1")

	clean, cleanErr := resumeLog(aw, singleStageSet("TDD"), log, false)
	rerun, rerunErr := resumeLog(aw, singleStageSet("TDD"), log, true)

	requireResumeAt(t, clean, cleanErr, bvRowTestsReview-1, false, 1, groupTest)
	requireResumeAt(t, rerun, rerunErr, bvRowTestBuild-1, true, 1, groupTest)
}

func TestResumePoint_BuildVerified_InterleavedInfrastructureEntries_ResumeFromRecordedRow(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := newBuildVerifiedLog().play(buildVerifiedRun, 2).
		infraStep(checkpointAgent, "Test.1").
		play(buildVerifiedRun[2:], 2).
		infraStep(checkpointAgent, "Test.1").
		play(buildVerifiedRun[4:], 1)

	clean, cleanErr := resumeLog(aw, singleStageSet("TDD"), log, false)
	rerun, rerunErr := resumeLog(aw, singleStageSet("TDD"), log, true)

	requireResumeAt(t, clean, cleanErr, bvRowTestsReview-1, false, 1, groupTest)
	requireResumeAt(t, rerun, rerunErr, bvRowTestBuild-1, true, 1, groupTest)
}

func TestResumePoint_StagedFindingsLoop_InfrastructureAfterRouteBack_ResumesFromRecordedRow(t *testing.T) {
	aw := mustParseAndAdmit(t, stagedFindingsLoopContent, "staged-findings-loop", "1.0")
	log := (&runLog{}).play(stagedLoopRun, 2).
		infraStep(checkpointAgent, "Test.1").
		play(stagedLoopRun[2:], 1).
		infraStep(checkpointAgent, "Test.1").
		play(stagedLoopRun[5:], 1)

	clean, cleanErr := resumeLog(aw, singleStageSet("TDD"), log, false)
	rerun, rerunErr := resumeLog(aw, singleStageSet("TDD"), log, true)

	// The log ends with the gate passing after two route-backs.
	requireResumeAt(t, clean, cleanErr, loopRowReview-1, false, 1, groupTest)
	requireResumeAt(t, rerun, rerunErr, loopRowGate-1, true, 1, groupTest)
}

// ===== Second stage =====

// stageOneWithRouteBackAndInfra logs a complete first stage that contains a
// findings route-back and infrastructure steps.
func stageOneWithRouteBackAndInfra() *runLog {
	return newBuildVerifiedLog().play(buildVerifiedRun, 3).
		infraStep(checkpointAgent, "Test.1").
		play(buildVerifiedRun[3:], 4).
		infraStep(checkpointAgent, "Implementation.1").
		play(buildVerifiedRun[7:], 2).
		infraStep(checkpointAgent, "Implementation.1")
}

// buildVerifiedStageTwo is the same run, in stage 2.
func buildVerifiedStageTwo() []runStep {
	steps := make([]runStep, len(buildVerifiedRun))
	for i, s := range buildVerifiedRun {
		s.stage = strings.TrimSuffix(s.stage, "1") + "2"
		steps[i] = s
	}
	return steps
}

func TestResumePoint_TwoStages_FirstStageCompleteAfterRouteBackAndInfra_ResumesAtStageTwoStart(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := stageOneWithRouteBackAndInfra()

	info, err := resumeLog(aw, twoStageSet("TDD"), log, false)

	requireResumeAt(t, info, err, bvRowTestWriter-1, false, 2, groupTest)
}

func TestResumePoint_TwoStages_SecondStageAfterRouteBack_CleanAndInterrupted(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stageTwo := buildVerifiedStageTwo()
	for _, tc := range buildVerifiedResumeCases[:5] {
		t.Run(tc.name, func(t *testing.T) {
			log := stageOneWithRouteBackAndInfra().play(stageTwo, tc.n)

			clean, cleanErr := resumeLog(aw, twoStageSet("TDD"), log, false)
			rerun, rerunErr := resumeLog(aw, twoStageSet("TDD"), log, true)

			requireResumeAt(t, clean, cleanErr, tc.cleanRow, false, 2, tc.cleanGroup)
			requireResumeAt(t, rerun, rerunErr, stageTwo[tc.n-1].row-1, true, 2, tc.rerunGroup)
		})
	}
}

func TestResumePoint_TwoStages_SecondStageLastRow_CompletesRun(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := stageOneWithRouteBackAndInfra().play(buildVerifiedStageTwo(), len(buildVerifiedRun))

	info, err := resumeLog(aw, twoStageSet("TDD"), log, false)

	requireResumeAt(t, info, err, len(aw.Table.Rows), false, 0, -1)
}

// ===== Stop and report =====

// resumeStopCases are last-entry defects of the Test-group build-review
// (build-review fills rows 9 and 12). Each makes resume stop.
var resumeStopCases = []struct {
	name      string
	stage     string
	row       int
	wantCause domain.PositionUnresolvedCause
}{
	{"no recorded row", "Test.1", int(domain.NoWorkflowRow), domain.CauseNoRecordedRow},
	{"row outside table", "Test.1", 14, domain.CauseRecordedRowInvalid},
	{"row far outside table", "Test.1", 99, domain.CauseRecordedRowInvalid},
	{"row holds another agent", "Test.1", bvRowTestsReview, domain.CauseRecordedRowInvalid},
	{"row in other group than stage", "Implementation.1", bvRowTestBuild, domain.CauseRecordedRowInvalid},
	{"empty stage", "", bvRowTestBuild, domain.CauseStagedRowWithoutStage},
	{"legacy stage value", "Stage-1", bvRowTestBuild, domain.CauseRecordedRowInvalid},
}

// requireResumeStop asserts that resume returned no row and an error naming
// the agent instance and, when recorded, the row.
func requireResumeStop(
	t *testing.T,
	info domain.ResumeInfo,
	err error,
	agentInstance string,
	row int,
	wantCause domain.PositionUnresolvedCause,
) {
	t.Helper()
	if err == nil {
		t.Fatalf("ResumePoint must stop and report, got row %d", info.RowIndex)
	}
	if info != (domain.ResumeInfo{}) {
		t.Errorf("a stop must not carry a resume position, got %+v", info)
	}
	var pue *domain.PositionUnresolvedError
	if !errors.As(err, &pue) {
		t.Fatalf("error must wrap *domain.PositionUnresolvedError, got %T: %v", err, err)
	}
	if pue.Cause != wantCause {
		t.Errorf("Cause: want %v, got %v", wantCause, pue.Cause)
	}
	if !strings.Contains(err.Error(), agentInstance) {
		t.Errorf("error must name agent %q, got %q", agentInstance, err.Error())
	}
	// Strip the instance id first: its sequence number must not satisfy the row check.
	if rest := strings.ReplaceAll(err.Error(), agentInstance, ""); row != 0 &&
		!strings.Contains(rest, strconv.Itoa(row)) {
		t.Errorf("error must name recorded row %d, got %q", row, err.Error())
	}
}

func TestResumePoint_MultiRowAgentWithUnusableRecordedRow_StopsAndReports(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	for _, tc := range resumeStopCases {
		for _, interrupted := range []bool{false, true} {
			name := tc.name + "/clean"
			if interrupted {
				name = tc.name + "/interrupted"
			}
			t.Run(name, func(t *testing.T) {
				log := newBuildVerifiedLog().
					workflowStep("test-writer-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestWriter).
					workflowStep("build-review", tc.stage, domain.StatusSUCCESS, tc.row)

				info, err := resumeLog(aw, singleStageSet("TDD"), log, interrupted)

				requireResumeStop(t, info, err, "build-review#9", tc.row, tc.wantCause)
			})
		}
	}
}

func TestResumePoint_StagedFindingsLoop_RecordedRowInOtherGroupThanStage_StopsAndReports(t *testing.T) {
	aw := mustParseAndAdmit(t, stagedFindingsLoopContent, "staged-findings-loop", "1.0")
	// Row 4 is the Implementation row; the entry's stage names the Test group.
	log := (&runLog{}).
		workflowStep("loop-agent", "Test.1", domain.StatusSUCCESS, loopRowWriter).
		workflowStep("loop-agent", "Test.1", domain.StatusSUCCESS, loopRowImpl)

	info, err := resumeLog(aw, singleStageSet("TDD"), log, false)

	requireResumeStop(t, info, err, "loop-agent#2", loopRowImpl, domain.CauseRecordedRowInvalid)
}

func TestResumePoint_MultiRowAgentWithUnusableRecordedRow_TrailingInfrastructureStillStops(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	log := newBuildVerifiedLog().
		workflowStep("test-writer-tdd", "Test.1", domain.StatusSUCCESS, bvRowTestWriter).
		workflowStep("build-review", "Test.1", domain.StatusSUCCESS, int(domain.NoWorkflowRow)).
		infraStep(checkpointAgent, "Test.1")

	info, err := resumeLog(aw, singleStageSet("TDD"), log, false)

	requireResumeStop(t, info, err, "build-review#9", 0, domain.CauseNoRecordedRow)
}
