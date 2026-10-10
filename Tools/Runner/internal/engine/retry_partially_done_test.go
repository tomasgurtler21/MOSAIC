package engine_test

// PARTIALLY_DONE in the auto and auto-review modes: the engine re-dispatches
// the same assignment mechanically, carrying the previous output as context,
// bounded by PartiallyDoneRedispatchLimit over the trailing run of
// PARTIALLY_DONE rows in the Execution Log. The orchestrated mode consults.

import (
	"strconv"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

func TestNext_PartiallyDone_AutoModes_RedispatchesSameRow(t *testing.T) {
	f := newRetryFixture(t)
	for _, mode := range autoModes {
		t.Run(modeName(mode), func(t *testing.T) {
			log := (&runLog{}).partial(rtPlanner, "", rtRowPlanner).inPhase(rtPlanPhase)

			step := requireRetryStep(t, f.next(mode, log, false), rtRowPlanner, "", domain.RetryPartiallyDone)

			if step.Request.AgentInstanceID != rtPlanner+"#2" {
				t.Errorf("agent instance: want %s#2, got %s", rtPlanner, step.Request.AgentInstanceID)
			}
		})
	}
}

func TestNext_PartiallyDone_StagedRow_RedispatchesSameRowAndStage(t *testing.T) {
	f := newRetryFixture(t)
	for _, mode := range autoModes {
		t.Run(modeName(mode), func(t *testing.T) {
			log := (&runLog{}).partial(rtTestWriter, "Test.2", rtRowTestWriter)

			requireRetryStep(t, f.next(mode, log, false), rtRowTestWriter, "Test.2", domain.RetryPartiallyDone)
		})
	}
}

// The reviewer's On Findings target is for COMPLETED_NEEDS_ACTION only; a
// PARTIALLY_DONE reviewer is re-run itself.
func TestNext_PartiallyDone_Reviewer_RedispatchesReviewerNotOnFindingsTarget(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).partial(rtTestsReview, "Test.1", rtRowTestsReview)

	step := requireRetryStep(t, f.next(domain.ExecutionModeAutoReview, log, false),
		rtRowTestsReview, "Test.1", domain.RetryPartiallyDone)

	if got := agentName(step.Request.AgentInstanceID); got != rtTestsReview {
		t.Errorf("want %s re-run, got %s", rtTestsReview, got)
	}
}

func TestNext_PartiallyDone_Orchestrated_Consults(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).partial(rtPlanner, "", rtRowPlanner).inPhase(rtPlanPhase)

	consult := requireConsult(t, f.next(domain.ExecutionModeOrchestrated, log, false))

	if consult.Trigger != domain.ConsultTriggerOrchestratedMode {
		t.Errorf("want ConsultTriggerOrchestratedMode, got %q", consult.Trigger)
	}
}

func TestNext_PartiallyDone_Redispatch_CarriesPreviousOutputAsInputsAndTaskText(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).partial(rtPlanner, "", rtRowPlanner).inPhase(rtPlanPhase)

	// Plan.md is an output of the planner row, not one of its default inputs.
	step := requireRetryStep(t, f.next(domain.ExecutionModeAuto, log, false, "Plan.md"),
		rtRowPlanner, "", domain.RetryPartiallyDone)

	inputs := step.Request.InputArtifacts
	if !containsPath(inputs, "Research.md") || !containsPath(inputs, "Requirements.md") {
		t.Errorf("row default inputs must stay, got %v", inputs)
	}
	if n := len(inputs); n == 0 || inputs[n-1] != "Plan.md" {
		t.Errorf("previous output Plan.md must follow the row defaults, got %v", inputs)
	}
	task := step.Request.TaskDescription
	for _, want := range []string{domain.GenericTaskDescription, engine.PartiallyDoneContextPrefix, rtLastMessage} {
		if !hasText(task, want) {
			t.Errorf("task description must contain %q, got %q", want, task)
		}
	}
}

func TestNext_PartiallyDone_Redispatch_DoesNotDuplicateOutputAlreadyAnInput(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).partial(rtPlanner, "", rtRowPlanner).inPhase(rtPlanPhase)

	step := requireRetryStep(t, f.next(domain.ExecutionModeAuto, log, false, "Research.md", "Plan.md"),
		rtRowPlanner, "", domain.RetryPartiallyDone)

	count := 0
	for _, p := range step.Request.InputArtifacts {
		if p == "Research.md" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Research.md must appear once, got %d in %v", count, step.Request.InputArtifacts)
	}
	if !containsPath(step.Request.InputArtifacts, "Plan.md") {
		t.Errorf("Plan.md missing from %v", step.Request.InputArtifacts)
	}
}

// With no response (resume) the previous status message comes from the log.
func TestNext_PartiallyDone_Resume_TaskTextCarriesLoggedSummary(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).partial(rtPlanner, "", rtRowPlanner).inPhase(rtPlanPhase)

	step := requireRetryStep(t, f.next(domain.ExecutionModeAuto, log, true),
		rtRowPlanner, "", domain.RetryPartiallyDone)

	task := step.Request.TaskDescription
	if !hasText(task, engine.PartiallyDoneContextPrefix) || !hasText(task, rtLastMessage) {
		t.Errorf("resume task description must carry the logged summary, got %q", task)
	}
	if len(step.Request.InputArtifacts) != 2 {
		t.Errorf("resume appends no previous outputs, want the 2 row defaults, got %v",
			step.Request.InputArtifacts)
	}
}

func TestNext_PartiallyDone_TrailingRun_BoundIsLimit(t *testing.T) {
	f := newRetryFixture(t)
	for k := 1; k <= engine.PartiallyDoneRedispatchLimit+2; k++ {
		for _, resume := range []bool{false, true} {
			t.Run("k="+strconv.Itoa(k)+"/resume="+strconv.FormatBool(resume), func(t *testing.T) {
				log := (&runLog{}).repeat(k, func(l *runLog) { l.partial(rtPlanner, "", rtRowPlanner) }).
					inPhase(rtPlanPhase)

				dec := f.next(domain.ExecutionModeAuto, log, resume)

				if k <= engine.PartiallyDoneRedispatchLimit {
					requireRetryStep(t, dec, rtRowPlanner, "", domain.RetryPartiallyDone)
					return
				}
				dev := requireNonSuccessDeviation(t, dec)
				if dev.Info.CurrentRow != rtRowPlanner-1 {
					t.Errorf("deviation row index: want %d, got %d", rtRowPlanner-1, dev.Info.CurrentRow)
				}
			})
		}
	}
}

func TestNext_PartiallyDone_BoundExhausted_AutoReviewDeviatesToo(t *testing.T) {
	f := newRetryFixture(t)
	n := engine.PartiallyDoneRedispatchLimit + 1
	log := (&runLog{}).repeat(n, func(l *runLog) { l.partial(rtTestWriter, "Test.1", rtRowTestWriter) })

	requireNonSuccessDeviation(t, f.next(domain.ExecutionModeAutoReview, log, false))
}

// Another status, row or stage inside the tail ends the run: the count starts
// again, so the latest PARTIALLY_DONE is the first of a new run.
func TestNext_PartiallyDone_OtherStatusRowOrStageEndsRun(t *testing.T) {
	f := newRetryFixture(t)
	n := engine.PartiallyDoneRedispatchLimit
	plannerRun := func() *runLog {
		return (&runLog{}).repeat(n, func(l *runLog) { l.partial(rtPlanner, "", rtRowPlanner) })
	}
	cases := []struct {
		name      string
		build     func() *runLog
		wantRow   int
		wantStage string
	}{
		{"success between", func() *runLog {
			return plannerRun().ok(rtPlanner, "", rtRowPlanner).partial(rtPlanner, "", rtRowPlanner).inPhase(rtPlanPhase)
		}, rtRowPlanner, ""},
		{"needs action between", func() *runLog {
			return plannerRun().
				stepWith(rtPlanner, "", domain.StatusCOMPLETED_NEEDS_ACTION, domain.ErrorNone, rtRowPlanner, "x").
				partial(rtPlanner, "", rtRowPlanner).inPhase(rtPlanPhase)
		}, rtRowPlanner, ""},
		{"blocked between", func() *runLog {
			return plannerRun().blocked(rtPlanner, "", domain.ErrorPERMISSION_DENIED, rtRowPlanner).
				partial(rtPlanner, "", rtRowPlanner).inPhase(rtPlanPhase)
		}, rtRowPlanner, ""},
		{"other row", func() *runLog {
			return plannerRun().partial(rtPlanReview, "", rtRowPlanReview).inPhase(rtPlanPhase)
		}, rtRowPlanReview, ""},
		{"other stage", func() *runLog {
			return (&runLog{}).repeat(n, func(l *runLog) { l.partial(rtTestWriter, "Test.1", rtRowTestWriter) }).
				partial(rtTestWriter, "Test.2", rtRowTestWriter)
		}, rtRowTestWriter, "Test.2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := f.next(domain.ExecutionModeAuto, tc.build(), false)

			requireRetryStep(t, dec, tc.wantRow, tc.wantStage, domain.RetryPartiallyDone)
		})
	}
}

// Infrastructure rows (recorded row "-") neither extend nor end the run: with
// one between PARTIALLY_DONE rows the run still reaches the bound.
// Guard paired with InfrastructureRowsDoNotCountTowardRun below: that test fails
// until retry exists; this one fails once retry exists if an infrastructure row
// is wrongly treated as ending the run (the run would restart and re-dispatch).
func TestNext_PartiallyDone_InfrastructureRowInsideRun_IsSkipped(t *testing.T) {
	f := newRetryFixture(t)
	n := engine.PartiallyDoneRedispatchLimit + 1
	log := &runLog{}
	for i := 0; i < n; i++ {
		if i == 1 {
			log.infraStep("checkpoint", "")
		}
		log.partial(rtPlanner, "", rtRowPlanner)
	}
	log.inPhase(rtPlanPhase)

	dev := requireNonSuccessDeviation(t, f.next(domain.ExecutionModeAuto, log, false))

	if dev.Info.CurrentRow != rtRowPlanner-1 {
		t.Errorf("deviation row index: want %d, got %d", rtRowPlanner-1, dev.Info.CurrentRow)
	}
}

// Infrastructure rows are not PARTIALLY_DONE rows and add nothing to the run.
func TestNext_PartiallyDone_InfrastructureRowsDoNotCountTowardRun(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).
		partial(rtPlanner, "", rtRowPlanner).
		infraStep("checkpoint", "").
		infraStep("review-trigger", "").
		partial(rtPlanner, "", rtRowPlanner).inPhase(rtPlanPhase)

	requireRetryStep(t, f.next(domain.ExecutionModeAuto, log, false), rtRowPlanner, "", domain.RetryPartiallyDone)
}

// The count is a function of the log alone: the same log gives the same
// decision whether Next follows a live response or a resume.
func TestNext_PartiallyDone_LiveAndResumeAgree(t *testing.T) {
	f := newRetryFixture(t)
	for k := 1; k <= engine.PartiallyDoneRedispatchLimit+1; k++ {
		log := (&runLog{}).repeat(k, func(l *runLog) { l.partial(rtTestWriter, "Test.1", rtRowTestWriter) })

		live := f.next(domain.ExecutionModeAuto, log, false)
		resumed := f.next(domain.ExecutionModeAuto, log, true)

		wantDispatch := k <= engine.PartiallyDoneRedispatchLimit
		if (live.Dispatch != nil) != wantDispatch || (resumed.Dispatch != nil) != wantDispatch {
			t.Errorf("k=%d: live and resume decisions differ (live dispatch=%v, resume dispatch=%v)",
				k, live.Dispatch != nil, resumed.Dispatch != nil)
		}
	}
}
