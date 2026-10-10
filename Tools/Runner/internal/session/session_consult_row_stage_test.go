package session_test

// Tests that a consultation-routed step is dispatched and recorded at exactly
// the row and stage of the validated dispatch instruction: a staged row records
// the instruction's stage in group form, a non-staged row records no stage, and
// nothing is inherited from the stage in force when the consultation started.

import (
	"context"
	"errors"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

func TestSession_ConsultRoute_StagedRowAfterNonStagedStep_RecordsInstructionStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStagedDispatch("implementer", "implement stage 2", rsImplementer, 2)
	consultant.queueStop("done")
	seed := rowStageSeed("plan-review", 2, "PLANNING", "")
	rig := newRowStageRig(t, consultant, domain.ExecutionModeOrchestrated, seed)
	queueSuccess(rig.f, "implementer", "implementer#3")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	steps := workflowSteps(rig.store)
	if len(steps) != 1 {
		t.Fatalf("want 1 workflow step, got %d: %+v", len(steps), steps)
	}
	requireStepAt(t, steps[0], "EXECUTION", 4, "Implementation.2")
}

func TestSession_ConsultRoute_ReviewRoutedAcrossGroupAndStage_RecordsInstructionRowAndStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStagedDispatch("build-review", "review the build", rsBuildReview, 3)
	consultant.queueStop("done")
	seed := rowStageSeed("test-writer", 3, "EXECUTION", "Test.1")
	rig := newRowStageRig(t, consultant, domain.ExecutionModeOrchestrated, seed)
	queueSuccess(rig.f, "build-review", "build-review#3")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	steps := workflowSteps(rig.store)
	if len(steps) != 1 {
		t.Fatalf("want 1 workflow step, got %d: %+v", len(steps), steps)
	}
	requireStepAt(t, steps[0], "EXECUTION", 5, "Implementation.3")
}

func TestSession_ConsultRoute_NonStagedRowAfterStagedStep_RecordsNoStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStagedDispatch("test-writer", "write the tests for stage 2", rsTestWriter, 2)
	consultant.queueDispatch("plan-review", "re-review the plan", rsPlanReview)
	consultant.queueStop("done")
	seed := rowStageSeed("planner", 1, "PLANNING", "")
	rig := newRowStageRig(t, consultant, domain.ExecutionModeOrchestrated, seed)
	queueSuccess(rig.f, "test-writer", "test-writer#3")
	queueSuccess(rig.f, "plan-review", "plan-review#4")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	steps := workflowSteps(rig.store)
	if len(steps) != 2 {
		t.Fatalf("want 2 workflow steps, got %d: %+v", len(steps), steps)
	}
	requireStepAt(t, steps[0], "EXECUTION", 3, "Test.2")
	requireStepAt(t, steps[1], "PLANNING", 2, "")
}

func TestSession_ConsultRoute_NonStagedRowAfterStagedEntry_RecordsNoStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("final-review", "close out the run", rsFinalReview)
	consultant.queueStop("done")
	seed := rowStageSeed("impl-review", 6, "EXECUTION", "Implementation.3")
	rig := newRowStageRig(t, consultant, domain.ExecutionModeOrchestrated, seed)
	queueSuccess(rig.f, "final-review", "final-review#3")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	steps := workflowSteps(rig.store)
	if len(steps) != 1 {
		t.Fatalf("want 1 workflow step, got %d: %+v", len(steps), steps)
	}
	requireStepAt(t, steps[0], "REVIEW", 7, "")
}

// A harness error recorded for a consultation-routed attempt and the step the
// follow-up consultation routes each carry their own instruction's row and
// stage; the second does not inherit the first.
func TestSession_ConsultRoute_HarnessErrorRecursion_RecordsEachInstructionRowAndStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStagedDispatch("implementer", "implement stage 2", rsImplementer, 2)
	consultant.queueStagedDispatch("test-writer", "write the tests for stage 1", rsTestWriter, 1)
	consultant.queueStop("done")
	seed := rowStageSeed("plan-review", 2, "PLANNING", "")
	rig := newRowStageRig(t, consultant, domain.ExecutionModeOrchestrated, seed)
	rig.f.Queue("implementer", harness.ScriptedEntry{Err: errors.New("simulated harness failure")})
	queueSuccess(rig.f, "test-writer", "test-writer#4")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	steps := workflowSteps(rig.store)
	if len(steps) != 2 {
		t.Fatalf("want 2 workflow steps (failed attempt, routed step), got %d: %+v", len(steps), steps)
	}
	requireStepAt(t, steps[0], "EXECUTION", 4, "Implementation.2")
	if steps[0].Status != domain.StatusBLOCKED {
		t.Errorf("want the failed attempt recorded BLOCKED, got %q", steps[0].Status)
	}
	requireStepAt(t, steps[1], "EXECUTION", 3, "Test.1")
}

// In auto mode a deviation is resolved by a consultation that routes a review
// to a staged row. The step is recorded at the instruction's row and stage, so
// the engine then advances from that row to the next one at the same stage.
func TestSession_ConsultRoute_AutoDeviationToStagedReview_RecordsRowAndAdvancesFromIt(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStagedDispatch("build-review", "review the build", rsBuildReview, 3)
	consultant.queueStop("done")
	seed := rowStageSeed("planner", 1, "PLANNING", "")
	rig := newRowStageRig(t, consultant, domain.ExecutionModeAuto, seed)
	rig.f.Queue("plan-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "plan-review#3",
		StatusCode:      domain.StatusNEEDS_CLARIFICATION,
		StatusMessage:   "need a decision",
	}})
	queueSuccess(rig.f, "build-review", "build-review#4")
	queueSuccess(rig.f, "impl-review", "impl-review#5")
	queueSuccess(rig.f, "final-review", "final-review#6")

	_, err := rig.ses.Start(context.Background(), rig.cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	var review, next *domain.CompletedStep
	steps := workflowSteps(rig.store)
	for i := range steps {
		switch steps[i].AgentInstance {
		case "build-review#4":
			review = &steps[i]
		case "impl-review#5":
			next = &steps[i]
		}
	}
	if review == nil {
		t.Fatalf("want a recorded step for the routed build-review, got %+v", steps)
	}
	requireStepAt(t, *review, "EXECUTION", 5, "Implementation.3")
	if next == nil {
		t.Fatalf("want the engine to dispatch impl-review after the routed build-review, invoked %v", invokedAgents(rig.f))
	}
	requireStepAt(t, *next, "EXECUTION", 6, "Implementation.3")
}
