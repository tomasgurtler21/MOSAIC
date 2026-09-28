package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== T9.4: HITL — non-compliant produces one redispatch then escalates =====

// TestIntegration_HITL_NonCompliant_OneRedispatchThenDeviation verifies that
// when an agent dispatched with HITL=true returns SUCCESS but its output
// artifact is not human-approved, the session re-dispatches the agent exactly
// once. A second non-compliant result escalates to a deviation; the run never
// advances past the unverified step and agent-b is never dispatched.
func TestIntegration_HITL_NonCompliant_OneRedispatchThenDeviation(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "hitl-linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	// agent-a is queued twice: initial dispatch and one HITL redispatch.
	// Both return SUCCESS, but the approval reader always reports ApprovalFalse.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done — awaiting human review",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "redispatched — still awaiting review",
	}})

	// ApprovalReader always reports ApprovalFalse: the human gate is never closed.
	approvals := intFixedApprovalReader{approval: domain.ApprovalFalse}

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSessionWithApprovals(f, artifactPath, approvals)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "hitl task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := sess.Start(context.Background(), cfg)

	// After the single allowed redispatch the escalation produces a deviation.
	// With no routing consultant wired, the run ends with RunDeviationUnresolved.
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunDeviationUnresolved {
		t.Errorf("want RunDeviationUnresolved after HITL redispatch exhausted, got %q (msg: %q)",
			got.Status, got.Message)
	}

	// agent-a must be dispatched exactly twice: once initially, once on redispatch.
	// agent-b must never be dispatched: the run must not advance past the
	// unverified step.
	invs := f.Invocations()
	agentACount, agentBCount := 0, 0
	for _, inv := range invs {
		switch inv.Agent.Identifier {
		case "agent-a":
			agentACount++
		case "agent-b":
			agentBCount++
		}
	}
	if agentACount != 2 {
		t.Errorf("want agent-a dispatched exactly twice (initial + one redispatch), got %d",
			agentACount)
	}
	if agentBCount != 0 {
		t.Errorf("want agent-b never dispatched (run must not advance past unverified step), got %d",
			agentBCount)
	}
}

// ===== T9.5: Stop and orchestrator failure — artifact left resumable =====

// TestIntegration_ConsultantStop_ArtifactResumable verifies that when the
// routing consultant returns a stop instruction, the run ends with
// RunStoppedByConsultant and the orchestration artifact remains on disk in a
// state from which it can be resumed. A stop is never silent data loss.
func TestIntegration_ConsultantStop_ArtifactResumable(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planning done",
	}})

	// Consultant dispatches agent-a then stops after it completes.
	consultant := &intScriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "Proceed.", 0)
	consultant.queueStop("workflow paused by operator")

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSessionWithRouting(f, artifactPath, consultant)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "stop task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	// The stop reason must surface in the run outcome.
	if !strings.Contains(got.StopReason, "paused") {
		t.Errorf("want StopReason to contain the consultant's reason; got %q", got.StopReason)
	}

	// The artifact must exist on disk: a stop is never silent data loss.
	if _, statErr := os.Stat(artifactPath); statErr != nil {
		t.Errorf("want artifact file to exist after consultant stop (resumable state), got %v",
			statErr)
	}
}

// TestIntegration_ConsultantFailure_ArtifactResumable verifies that when the
// routing consultant returns a transport error (simulating an orchestrator
// failure), the run ends with RunStoppedByConsultant and the artifact remains
// on disk, parseable and ready to resume.
func TestIntegration_ConsultantFailure_ArtifactResumable(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planning done",
	}})

	// Consultant dispatches agent-a, then fails with a transport error on the
	// next call — simulating an orchestrator crash.
	consultant := &intScriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "Proceed.", 0)
	consultant.queueError(domain.ConsultFailTransport)

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSessionWithRouting(f, artifactPath, consultant)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "failure task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	// The artifact must exist and be parseable: the run can be resumed after
	// the orchestrator failure is resolved.
	data, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("want artifact to exist after orchestrator failure (resumable state), got %v",
			readErr)
	}
	if _, parseErr := artifact.Parse(data); parseErr != nil {
		t.Errorf("want artifact parseable after orchestrator failure, got %v", parseErr)
	}
}
