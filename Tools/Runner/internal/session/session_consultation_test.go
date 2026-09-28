package session_test

// Tests for consultation recording (infrastructure rows, sequence consumption,
// current_state preservation), failure handling (terminal failures, artifact
// left resumable, ManualResolution fallback, StopInstruction, harness errors as
// deviations), and write discipline (Apply-before-consult ordering, re-read
// after consultation returns).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Consultation recording =====

// TestSession_Consultation_RecordedAsInfrastructureRow verifies that each
// RoutingConsultant invocation is recorded as an Execution Log row with
// IsInfrastructure=true. Workflow steps must not be flagged as infrastructure.
func TestSession_Consultation_RecordedAsInfrastructureRow(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do planning", 0)
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// Verify at least one infrastructure row exists (the consultation).
	infraRows := 0
	for _, step := range store.Applied {
		if step.IsInfrastructure {
			infraRows++
		}
	}
	if infraRows == 0 {
		t.Error("want at least one infrastructure-flagged row for consultation, got 0")
	}

	// The Agent column of each infrastructure row must carry the resolved
	// orchestrator identifier (the file stem of the orchestrator file) followed
	// by "#N". The fixture writes the file as "orchestrator.md", so the stem is
	// "orchestrator".
	for _, step := range store.Applied {
		if !step.IsInfrastructure {
			continue
		}
		if !strings.HasPrefix(step.AgentInstance, "orchestrator#") {
			t.Errorf("want consultation row AgentInstance to match orchestrator#N, got %q",
				step.AgentInstance)
		}
	}

	// The agent-a step must not be flagged as infrastructure.
	for _, step := range store.Applied {
		if strings.Contains(step.AgentInstance, "agent-a") && step.IsInfrastructure {
			t.Errorf("want agent-a step not flagged as infrastructure, but it is")
		}
	}
}

// TestSession_Consultation_ConsumesGlobalSequence verifies that a consultation
// row consumes a global_sequence slot, so the sequence number of the next
// workflow step is higher than it would be without the consultation.
func TestSession_Consultation_ConsumesGlobalSequence(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "step 1", 0)
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// With one consultation (first step) and one workflow step, the final
	// GlobalSequence is exactly 2 (consultation=1 + agent-a=2). The sequence is
	// fully deterministic in this setup, so an exact assertion catches off-by-one
	// errors (e.g. double-counting the consultation would produce 3).
	if store.state.GlobalSequence != 2 {
		t.Errorf("want GlobalSequence == 2 after one consultation + one workflow step, got %d",
			store.state.GlobalSequence)
	}
}

// TestSession_Consultation_DoesNotMoveCurrentState verifies that after a
// consultation is recorded, current_state continues to name the last WORKFLOW
// step (not the consultation row).
func TestSession_Consultation_DoesNotMoveCurrentState(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "step 1", 0)
	// After agent-a completes, a second consultation is triggered.
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// After the run ends, current_state.LastAgent must name agent-a, not any
	// consultation-derived identifier.
	if !strings.Contains(store.state.CurrentState.LastAgent, "agent-a") {
		t.Errorf("want current_state.LastAgent to name agent-a (last workflow step), got %q",
			store.state.CurrentState.LastAgent)
	}
}

// ===== Failure handling =====

// TestSession_ConsultationFailure_ReturnsStoppedByConsultant verifies that a
// consultation error (any failure class) is terminal by default and returns
// RunStoppedByConsultant.
func TestSession_ConsultationFailure_ReturnsStoppedByConsultant(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueError(domain.ConsultFailMalformedJSON) // first consultation fails

	ses, _, _, orchPath := newOrchestratedSession(t, consultant)

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error (consultation failure encoded in RunOutcome), got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant for consultation failure, got %q (message: %q)",
			got.Status, got.Message)
	}
}

// TestSession_ConsultationFailure_ArtifactLeftResumable verifies that after a
// consultation failure the artifact state is left intact and the run is
// resumable. The artifact must still exist (not deleted) and have a valid state.
func TestSession_ConsultationFailure_ArtifactLeftResumable(t *testing.T) {
	// Dispatch agent-a successfully, then fail on the second consultation.
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "step 1", 0)
	consultant.queueError(domain.ConsultFailTransport) // second consultation fails

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant, got %q", got.Status)
	}
	// The artifact must still be present (store.exists=true) and have the
	// agent-a step recorded. A deleted or reset artifact is not resumable.
	if !store.exists {
		t.Error("want artifact to exist (run is resumable after consultation failure)")
	}
	agentARecorded := false
	for _, step := range store.Applied {
		if strings.Contains(step.AgentInstance, "agent-a") {
			agentARecorded = true
			break
		}
	}
	if !agentARecorded {
		t.Error("want agent-a's completed step recorded in the artifact (run is resumable)")
	}
}

// TestSession_ManualResolutionEnabled_ConsultationFailure_UsesManualResolver
// verifies that in orchestrated mode with ManualResolution=true, a consultation
// failure falls back to the Manual resolver instead of terminating.
func TestSession_ManualResolutionEnabled_ConsultationFailure_UsesManualResolver(t *testing.T) {
	// Primary consultant: fails on first call.
	primary := &scriptedRoutingConsultant{}
	primary.queueError(domain.ConsultFailTransport)

	// Manual resolver: dispatches agent-a after the primary fails.
	manual := &scriptedRoutingConsultant{}
	manual.queueDispatch("agent-a", "manual resolution fallback", 0)
	manual.queueStop("done")

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  primary,
		Manual:   manual,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done via manual",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode:             domain.ExecutionModeOrchestrated,
			ManualResolution: true,
		},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// The manual resolver must have been invoked after the primary consultant failed.
	if manual.CallCount == 0 {
		t.Error("want ManualResolver called after primary consultation failure with ManualResolution=true, got 0 calls")
	}
}

// TestSession_StopInstruction_ReturnsStoppedByConsultant verifies that a
// StopInstruction from the RoutingConsultant causes the session to return
// RunStoppedByConsultant.
func TestSession_StopInstruction_ReturnsStoppedByConsultant(t *testing.T) {
	const stopReason = "orchestrator decided to pause here"

	consultant := &scriptedRoutingConsultant{}
	consultant.queueStop(stopReason)

	ses, _, _, orchPath := newOrchestratedSession(t, consultant)

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant after stop instruction, got %q (message: %q)",
			got.Status, got.Message)
	}
	if got.StopReason != stopReason {
		t.Errorf("want StopReason=%q, got %q", stopReason, got.StopReason)
	}
}

// TestSession_SubagentHarnessError_IsDeviation_NotCrash verifies that a
// harness-level error while invoking a subagent is treated as a deviation
// (the consultant is invoked to decide what to do next) and does NOT crash
// the session or return RunFailed.
func TestSession_SubagentHarnessError_IsDeviation_NotCrash(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// In orchestrated mode: dispatch agent-a; when it fails at harness level,
	// the session must consult again (deviation). The consultant then dispatches
	// agent-a again for a retry.
	consultant.queueDispatch("agent-a", "try the work", 0)
	// After the harness error, consultation is triggered again:
	consultant.queueDispatch("agent-a", "retry after harness error", 0)
	consultant.queueStop("done after retry")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	// First agent-a invocation: harness error.
	f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New("harness: subprocess timed out")})
	// Second agent-a invocation (after retry): SUCCESS.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done on retry",
	}})

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error (harness error is a deviation, not a crash), got %v", err)
	}
	// The session must NOT return RunFailed for a harness error.
	if got.Status == domain.RunFailed {
		t.Errorf("want harness error treated as deviation (not RunFailed), got RunFailed (message: %q)",
			got.Message)
	}
}

// ===== Write discipline =====

// TestSession_WriteBeforeConsult_PrecedingResultWrittenBeforeConsultation
// verifies that the preceding agent's result is written to the artifact store
// (via Apply) before the RoutingConsultant is invoked. This guarantees the
// orchestrator reads an up-to-date artifact.
func TestSession_WriteBeforeConsult_PrecedingResultWrittenBeforeConsultation(t *testing.T) {
	inner := &scriptedRoutingConsultant{}
	inner.queueDispatch("agent-a", "step 1", 0)
	inner.queueStop("done") // second consultation, after agent-a

	store := &memStore{}
	tracking := &applyBeforeConsultConsultant{inner: inner, store: store}

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  tracking,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}) //nolint:errcheck

	if len(tracking.ApplyAtCall) < 2 {
		t.Fatalf("want at least 2 ConsultRouting calls to observe ordering, got %d", len(tracking.ApplyAtCall))
	}
	// First consultation: no workflow step has been completed yet (Apply=0).
	if tracking.ApplyAtCall[0] != 0 {
		t.Errorf("want 0 workflow Apply calls at first consultation, got %d", tracking.ApplyAtCall[0])
	}
	// Second consultation (after agent-a): agent-a's result must already be written (Apply >= 1).
	if tracking.ApplyAtCall[1] < 1 {
		t.Errorf("want >= 1 Apply calls at second consultation (agent-a result written first), got %d",
			tracking.ApplyAtCall[1])
	}
}

// TestSession_RereadAfterConsult_ArtifactRereadAfterConsultation verifies that
// the artifact is re-read from the store after a consultation returns, so any
// Workflow Notes the orchestrator appended during its deliberation are visible
// in the session's next iteration.
func TestSession_RereadAfterConsult_ArtifactRereadAfterConsultation(t *testing.T) {
	inner := &scriptedRoutingConsultant{}
	inner.queueDispatch("agent-a", "step 1", 0)
	inner.queueStop("done")

	store := &memStore{}
	tracking := &applyBeforeConsultConsultant{inner: inner, store: store}

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  tracking,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}) //nolint:errcheck

	if len(tracking.ReadAtCall) < 2 {
		t.Fatalf("want at least 2 ConsultRouting calls, got %d", len(tracking.ReadAtCall))
	}
	// After the first consultation returns (which dispatched agent-a), the session
	// must re-read the artifact before the second consultation. So the Read count
	// at the second consultation must be higher than at the first.
	if tracking.ReadAtCall[1] <= tracking.ReadAtCall[0] {
		t.Errorf("want ReadCount to increase between consultations (re-read after each), "+
			"got ReadAtCall[0]=%d ReadAtCall[1]=%d", tracking.ReadAtCall[0], tracking.ReadAtCall[1])
	}
}
