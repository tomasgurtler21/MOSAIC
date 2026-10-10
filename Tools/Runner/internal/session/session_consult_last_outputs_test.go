package session_test

// Tests that the outputs of a consultation-routed step are what the next
// engine decision sees: the auto-review route-back that follows a
// consultation-routed review injects that review's outputs, not the outputs
// of an earlier engine-routed step.

import (
	"context"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

func TestSession_AutoReview_RouteBackAfterConsultRoutedReview_InjectsThatReviewsOutputs(t *testing.T) {
	// The engine dispatches the implementer (outputs: Stage-1/impl.md); it asks
	// for clarification, so the consultant routes the build-review instead
	// (outputs: Stage-1/build-review.md). The review reports findings and the
	// engine routes back to the implementer.
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStagedDispatch("build-review", "review the implementation", cdBuildReview, 1)
	for i := 0; i < 3; i++ {
		consultant.queueStop("done")
	}
	rig := newDefaultsRig(t, consultant, domain.ExecutionModeAutoReview,
		rowStageSeed("test-writer", 2, "EXECUTION", "Test.1"))
	rig.f.Queue("implementer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementer#3", StatusCode: domain.StatusNEEDS_CLARIFICATION, StatusMessage: "need guidance",
	}})
	rig.f.Queue("build-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "build-review#4", StatusCode: domain.StatusCOMPLETED_NEEDS_ACTION, StatusMessage: "findings",
	}})
	queueSuccess(rig.f, "implementer", "implementer#5")

	_, err := rig.ses.Start(context.Background(), rig.cfg)

	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	reqs := requestsTo(rig.f, "implementer")
	if len(reqs) < 2 {
		t.Fatalf("want the implementer routed back after the review, got %d implementer dispatch(es); consultations: %d",
			len(reqs), len(consultant.Requests))
	}
	inputs := bare(reqs[1], reqs[1].InputArtifacts)
	if !containsPath(inputs, "Stage-1/build-review.md") {
		t.Errorf("route-back should receive the consult-routed review's output Stage-1/build-review.md, got %v", inputs)
	}
	if containsPath(inputs, "Stage-1/impl.md") {
		t.Errorf("route-back should not receive the earlier engine-routed step's output Stage-1/impl.md, got %v", inputs)
	}
}

// A consult-routed step that declares no outputs replaces the earlier
// engine-routed step's outputs with nothing; the stale file is not injected.
func TestSession_AutoReview_RouteBackAfterConsultRoutedStepWithNoOutputs_InjectsNoStaleOutputs(t *testing.T) {
	noOutputs := []string{}
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatchWithOutputs("build-review", "review the implementation", cdBuildReview, &noOutputs)
	consultant.stageLastDispatch(1)
	for i := 0; i < 3; i++ {
		consultant.queueStop("done")
	}
	rig := newDefaultsRig(t, consultant, domain.ExecutionModeAutoReview,
		rowStageSeed("test-writer", 2, "EXECUTION", "Test.1"))
	rig.f.Queue("implementer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementer#3", StatusCode: domain.StatusNEEDS_CLARIFICATION, StatusMessage: "need guidance",
	}})
	rig.f.Queue("build-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "build-review#4", StatusCode: domain.StatusCOMPLETED_NEEDS_ACTION, StatusMessage: "findings",
	}})
	queueSuccess(rig.f, "implementer", "implementer#5")

	_, err := rig.ses.Start(context.Background(), rig.cfg)

	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	reqs := requestsTo(rig.f, "implementer")
	if len(reqs) < 2 {
		t.Fatalf("want the implementer routed back after the review, got %d implementer dispatch(es)", len(reqs))
	}
	inputs := bare(reqs[1], reqs[1].InputArtifacts)
	if containsPath(inputs, "Stage-1/impl.md") {
		t.Errorf("route-back should not receive the earlier step's output Stage-1/impl.md, got %v", inputs)
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if strings.TrimSpace(p) == want {
			return true
		}
	}
	return false
}
