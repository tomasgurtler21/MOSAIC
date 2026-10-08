package session_test

// Tests that the session hands the routing consultant the stage set currently
// in force with every consultation, including a stage set that was derived or
// refreshed mid-run. Without it a consultant cannot validate a staged dispatch.

import (
	"context"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// TestSession_ConsultationRequest_CarriesCurrentStageSet verifies that the
// request given to the routing consultant holds the run's stage set.
func TestSession_ConsultationRequest_CarriesCurrentStageSet(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueStop("nothing to do")
	ses, _, store, orchPath, runFolder := newConsultStagedSession(t, consultant)
	store.state = consultStagedStage2State()
	store.exists = true

	_, err := ses.Start(context.Background(), baseConsultStagedConfig(orchPath, runFolder))
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}

	if len(consultant.Requests) == 0 {
		t.Fatal("want at least one consultation request, got none")
	}
	got := consultant.Requests[0].Stages
	if got == nil {
		t.Fatal("want ConsultationRequest.Stages populated from the run's Plan.md, got nil")
	}
	if got.Count() != 2 {
		t.Errorf("want 2 stages in the consultation request, got %d", got.Count())
	}
}

// TestSession_ConsultationRequest_CarriesStageSetDerivedMidRun verifies that a
// stage set that appears during the run (a planner row writing Stage-* outputs)
// reaches every later consultation.
func TestSession_ConsultationRequest_CarriesStageSetDerivedMidRun(t *testing.T) {
	tmpDir := chdirWorkspace(t)
	writePlanMD(t, tmpDir)
	writeStages := func() {
		writeStageArtifact(t, tmpDir, 1, approvedArtifactContent)
		writeStageArtifact(t, tmpDir, 2, approvedArtifactContent)
	}
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("planner", "create the plan", 0)
	consultant.queueDispatch("agent-a", "do the work", 1)
	consultant.queueStop("done")
	ses, f, _, orchPath := newHITLGlobStagedSession(t, consultant, artifact.NewApprovalReader(), tmpDir, writeStages)
	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1", StatusCode: domain.StatusSUCCESS, StatusMessage: "plan created",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "work done",
	}})

	ses.Start(context.Background(), hitlGlobOrchestratedConfig(orchPath, tmpDir)) //nolint:errcheck

	if len(consultant.Requests) < 3 {
		t.Fatalf("want 3 consultation requests, got %d", len(consultant.Requests))
	}
	for i := 1; i < 3; i++ {
		got := consultant.Requests[i].Stages
		if got == nil || got.Count() != 2 {
			t.Errorf("request %d: want the 2-stage set derived after the planner row, got %+v", i, got)
		}
	}
}
