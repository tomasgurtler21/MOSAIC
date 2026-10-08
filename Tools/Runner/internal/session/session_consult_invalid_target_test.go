package session_test

// Tests for the session's defensive check of a consultation-routed dispatch
// instruction against the stage set in force: an instruction that does not name
// a valid row and stage is never dispatched and never recorded, and the run
// ends as a failed consultation.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

func TestSession_ConsultRoute_InvalidInstruction_IsNeitherDispatchedNorRecorded(t *testing.T) {
	cases := []struct {
		name       string
		agent      string
		row        int
		stage      domain.StageNumber
		wantReason domain.DispatchTargetReason
	}{
		{"stage not in the stage set", "implementer", rsImplementer, 9, domain.ReasonUnknownStage},
		{"stage on a non-staged row", "plan-review", rsPlanReview, 1, domain.ReasonStageOnNonStagedRow},
		{"staged row without a stage", "implementer", rsImplementer, 0, domain.ReasonMissingStage},
		{"row outside the routing table", "implementer", 40, 1, domain.ReasonUnknownRow},
		{"agent that does not hold the row", "build-review", rsImplementer, 1, domain.ReasonAgentMismatch},
		{"stage whose approach lacks the row's group", "test-writer", rsTestWriter, 3, domain.ReasonStageGroupMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			consultant := &scriptedRoutingConsultant{}
			consultant.queueStagedDispatch(tc.agent, "do the work", tc.row, tc.stage)
			seed := rowStageSeed("plan-review", 2, "PLANNING", "")
			rig := newRowStageRig(t, consultant, domain.ExecutionModeOrchestrated, seed)
			queueSuccess(rig.f, tc.agent, tc.agent+"#3")

			got, err := rig.ses.Start(context.Background(), rig.cfg)

			requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
			if n := len(rig.f.Invocations()); n != 0 {
				t.Errorf("want no harness invocation for an invalid instruction, got %d", n)
			}
			if n := len(rig.store.Applied); n != 0 {
				t.Errorf("want no recorded step for an invalid instruction, got %d: %+v", n, rig.store.Applied)
			}
			var consultErr *domain.ConsultationError
			if !errors.As(got.Cause, &consultErr) || consultErr.Failure != domain.ConsultFailMalformedJSON {
				t.Fatalf("want Cause to be a ConsultationError of class %q, got %T: %v",
					domain.ConsultFailMalformedJSON, got.Cause, got.Cause)
			}
			var targetErr *domain.DispatchTargetError
			if !errors.As(got.Cause, &targetErr) || targetErr.Reason != tc.wantReason {
				t.Errorf("want Cause to wrap a DispatchTargetError with reason %q, got %v", tc.wantReason, got.Cause)
			}
			if !strings.Contains(got.Message, "invalid dispatch target") {
				t.Errorf("want the outcome message to name the invalid dispatch target, got %q", got.Message)
			}
		})
	}
}

// The check runs against the stage set in force when the instruction arrives,
// on every consultation, not only the first.
func TestSession_ConsultRoute_InvalidInstructionAfterValidStep_StopsWithoutSecondRecord(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStagedDispatch("implementer", "implement stage 1", rsImplementer, 1)
	consultant.queueStagedDispatch("implementer", "implement stage 7", rsImplementer, 7)
	seed := rowStageSeed("plan-review", 2, "PLANNING", "")
	rig := newRowStageRig(t, consultant, domain.ExecutionModeOrchestrated, seed)
	queueSuccess(rig.f, "implementer", "implementer#3")
	queueSuccess(rig.f, "implementer", "implementer#4")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	steps := workflowSteps(rig.store)
	if len(steps) != 1 {
		t.Fatalf("want only the valid step recorded, got %d: %+v", len(steps), steps)
	}
	requireStepAt(t, steps[0], "EXECUTION", 4, "Implementation.1")
	if n := len(rig.f.Invocations()); n != 1 {
		t.Errorf("want exactly 1 harness invocation, got %d", n)
	}
}
