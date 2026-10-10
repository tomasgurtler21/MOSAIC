package session_test

// Tests that no dispatch request lists the same artifact twice, whichever
// form (with or without the run folder prefix) the duplicate entries use.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// countPath counts the entries of paths equal to want.
func countPath(paths []string, want string) int {
	n := 0
	for _, p := range paths {
		if p == want {
			n++
		}
	}
	return n
}

func TestSession_ConsultRoute_ExplicitInputsInBothPathForms_ListedOnce(t *testing.T) {
	seed := rowStageSeed("build-review", 4, "EXECUTION", "Implementation.1")
	seed.RunID = testRunID
	explicit := []string{
		"Stage-1/Plan.md",
		domain.RunScopedFolder(testRunID) + "/Stage-1/Plan.md",
		"Stage-1/other.md",
	}
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatchWithInputs("test-writer", "write the tests for stage 2", cdTestWriter, &explicit)
	consultant.stageLastDispatch(2)
	consultant.queueStop("done")
	rig := newDefaultsRig(t, consultant, domain.ExecutionModeOrchestrated, seed)
	queueSuccess(rig.f, "test-writer", "test-writer#3")

	got, err := rig.ses.Start(context.Background(), rig.cfg)

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	req := firstRequestTo(t, rig.f, "test-writer")
	inputs := bare(req, req.InputArtifacts)
	if n := countPath(inputs, "Stage-1/Plan.md"); n != 1 {
		t.Errorf("want Stage-1/Plan.md exactly once, got %d in %v", n, inputs)
	}
	if n := countPath(inputs, "Stage-1/other.md"); n != 1 {
		t.Errorf("want Stage-1/other.md kept once (the list was not dropped), got %d in %v", n, inputs)
	}
}

// The engine-routed test-writer's output (injected into the next decision in
// run-prefixed form) is also a table input of the implementer, and the
// consult-routed review that follows declares the same file as its output. The
// route-back to the implementer lists the file once.
func TestSession_AutoReview_RouteBackWhoseInjectedOutputIsATableInput_ListsItOnce(t *testing.T) {
	seed := rowStageSeed("planner", 1, "PLANNING", "")
	seed.RunID = testRunID
	reviewOutputs := []string{"Stage-1/tests.md"}
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatchWithOutputs("build-review", "review the tests", cdBuildReview, &reviewOutputs)
	consultant.stageLastDispatch(1)
	for i := 0; i < 3; i++ {
		consultant.queueStop("done")
	}
	rig := newDefaultsRig(t, consultant, domain.ExecutionModeAutoReview, seed)
	rig.f.Queue("test-writer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "test-writer#3", StatusCode: domain.StatusNEEDS_CLARIFICATION, StatusMessage: "need guidance",
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
	if len(reqs) < 1 {
		t.Fatalf("want the implementer routed back after the review, got none; consultations: %d", len(consultant.Requests))
	}
	inputs := bare(reqs[0], reqs[0].InputArtifacts)
	if n := countPath(inputs, "Stage-1/tests.md"); n != 1 {
		t.Errorf("want Stage-1/tests.md exactly once in the route-back inputs, got %d in %v", n, inputs)
	}
}
