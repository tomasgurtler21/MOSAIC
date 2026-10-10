package session_test

// Tests that the consultation paths beyond a plain orchestrated dispatch record
// the instruction's row and stage on every step they write: HITL-gated routed
// steps (rejected attempt and accepted re-dispatch), the step routed after an
// anti-loop escalation, and the step routed by a review-class infrastructure
// consultation. In each case the instruction's stage differs from the stage in
// force when the consultation started, so an inherited stage is visible.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// hitlRig builds a rig over the fixture whose plan-review and implementer rows
// are HITL-gated. The first approval check fails and every later one passes, so
// the routed step is rejected once and accepted on its re-dispatch.
func hitlRig(t *testing.T, consultant *scriptedRoutingConsultant, seed domain.ArtifactState) *rowStageRig {
	t.Helper()
	approvals := &switchingApprovalReader{firstApproval: domain.ApprovalFalse, restApproval: domain.ApprovalTrue}
	return newRowStageRigWith(t, consultant, domain.ExecutionModeOrchestrated, seed,
		rowStageOpts{fixture: "consult-row-stage-hitl-orch.md", approvals: approvals})
}

func TestSession_ConsultRoute_HITLStagedRowAfterStagedStep_RejectedAndAcceptedRecordInstructionStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStagedDispatch("implementer", "implement stage 2", rsImplementer, 2)
	consultant.queueStop("done")
	rig := hitlRig(t, consultant, rowStageSeed("test-writer", 3, "EXECUTION", "Test.1"))
	queueSuccess(rig.f, "implementer", "implementer#3")
	queueSuccess(rig.f, "implementer", "implementer#4")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	steps := stepsByAgent(rig.store, "implementer")
	if len(steps) != 2 {
		t.Fatalf("want a rejected attempt and an accepted re-dispatch, got %d: %+v", len(steps), steps)
	}
	if !steps[0].HITLRejected || steps[1].HITLRejected {
		t.Errorf("want first attempt rejected and second accepted, got rejected=%v/%v", steps[0].HITLRejected, steps[1].HITLRejected)
	}
	requireStepAt(t, steps[0], "EXECUTION", 4, "Implementation.2")
	requireStepAt(t, steps[1], "EXECUTION", 4, "Implementation.2")
}

func TestSession_ConsultRoute_HITLNonStagedRowAfterStagedStep_RejectedAndAcceptedRecordNoStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("plan-review", "re-review the plan", rsPlanReview)
	consultant.queueStop("done")
	rig := hitlRig(t, consultant, rowStageSeed("impl-review", 6, "EXECUTION", "Implementation.3"))
	queueSuccess(rig.f, "plan-review", "plan-review#3")
	queueSuccess(rig.f, "plan-review", "plan-review#4")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	steps := stepsByAgent(rig.store, "plan-review")
	if len(steps) != 2 {
		t.Fatalf("want a rejected attempt and an accepted re-dispatch, got %d: %+v", len(steps), steps)
	}
	if !steps[0].HITLRejected || steps[1].HITLRejected {
		t.Errorf("want first attempt rejected and second accepted, got rejected=%v/%v", steps[0].HITLRejected, steps[1].HITLRejected)
	}
	requireStepAt(t, steps[0], "PLANNING", 2, "")
	requireStepAt(t, steps[1], "PLANNING", 2, "")
}

// The consultant is escalated to after four consecutive dispatches of one agent
// for the same step; the step it then routes carries its own instruction's row
// and stage, and the capped dispatches keep theirs.
func TestSession_ConsultRoute_AntiLoopEscalation_RoutedStepRecordsInstructionRowAndStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	for i := 0; i < 5; i++ {
		consultant.queueStagedDispatch("implementer", "implement stage 2", rsImplementer, 2)
	}
	consultant.queueStagedDispatch("test-writer", "write the tests for stage 1", rsTestWriter, 1)
	consultant.queueStop("done")
	rig := newRowStageRig(t, consultant, domain.ExecutionModeOrchestrated, rowStageSeed("plan-review", 2, "PLANNING", ""))
	for _, id := range []string{"implementer#3", "implementer#4", "implementer#5", "implementer#6"} {
		queueSuccess(rig.f, "implementer", id)
	}
	queueSuccess(rig.f, "test-writer", "test-writer#7")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	impl := stepsByAgent(rig.store, "implementer")
	if len(impl) != 4 {
		t.Fatalf("want the guard to cap the agent at 4 recorded dispatches, got %d", len(impl))
	}
	for _, s := range impl {
		requireStepAt(t, s, "EXECUTION", 4, "Implementation.2")
	}
	routed := stepsByAgent(rig.store, "test-writer")
	if len(routed) != 1 {
		t.Fatalf("want the step routed after the escalation recorded once, got %d", len(routed))
	}
	requireStepAt(t, routed[0], "EXECUTION", 3, "Test.1")
	var escalated bool
	for _, req := range consultant.Requests {
		escalated = escalated || req.Deviation != nil
	}
	if !escalated {
		t.Errorf("want the escalation to reach the consultant as a deviation, got %d consultation(s) without one", len(consultant.Requests))
	}
}

// A review-class infrastructure agent fires after the first routed step; the
// consultation it triggers routes a step at the instruction's row and stage,
// not the stage of the step that triggered the review.
func TestSession_ConsultRoute_InfrastructureReviewConsult_RoutedStepRecordsInstructionRowAndStage(t *testing.T) {
	cases := []struct {
		name    string
		agent   string
		row     int
		stage   domain.StageNumber
		phase   string
		wantRow domain.WorkflowRow
		wantStg string
	}{
		{"staged row", "test-writer", rsTestWriter, 1, "EXECUTION", 3, "Test.1"},
		{"non-staged row", "plan-review", rsPlanReview, 0, "PLANNING", 2, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			consultant := &scriptedRoutingConsultant{}
			consultant.queueStagedDispatch("implementer", "implement stage 2", rsImplementer, 2)
			consultant.queueStagedDispatch(tc.agent, "routed by review", tc.row, tc.stage)
			for i := 0; i < 3; i++ {
				consultant.queueStop("done")
			}
			opts := rowStageOpts{fixture: "consult-row-stage-review-orch.md", extraAgents: []string{"review-agent"}}
			rig := newRowStageRigWith(t, consultant, domain.ExecutionModeOrchestrated,
				rowStageSeed("plan-review", 2, "PLANNING", ""), opts)
			queueSuccess(rig.f, "implementer", "implementer#3")
			for _, id := range []string{"review-agent#4", "review-agent#6", "review-agent#7"} {
				rig.f.Queue("review-agent", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
					AgentInstanceID: id, StatusCode: domain.StatusSUCCESS, StatusMessage: "review notes",
				}})
			}
			queueSuccess(rig.f, tc.agent, tc.agent+"#5")

			got, err := rig.ses.Start(context.Background(), rig.cfg)

			requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
			if len(consultant.Requests) < 2 || consultant.Requests[1].LastStatusMessage == nil ||
				*consultant.Requests[1].LastStatusMessage != "review notes" {
				t.Fatalf("want the second consultation to be the review consultation, got %+v", consultant.Requests)
			}
			routed := stepsByAgent(rig.store, tc.agent)
			if len(routed) != 1 {
				t.Fatalf("want the review-routed step recorded once, got %d: %+v", len(routed), routed)
			}
			requireStepAt(t, routed[0], tc.phase, tc.wantRow, tc.wantStg)
		})
	}
}
