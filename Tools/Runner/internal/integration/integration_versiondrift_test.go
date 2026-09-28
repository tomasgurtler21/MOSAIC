package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== Version-drift scenarios =====

// versionDriftArtifact is an existing Orchestration.md that records workflow_version
// "2.0" with agent-a already completed. Used by both version-drift tests.
const versionDriftArtifact = `---
type: orchestration-artifact
workflow: linear
workflow_version: "2.0"
task: "old task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
mode: auto
checkpoints: disabled
current_state:
  phase: PLANNING
  stage: null
  last_status: SUCCESS
  last_agent: "agent-a#1"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent     | Phase    | Stage | Status  | Timestamp            | Summary | Checkpoint |
| --- | --------- | -------- | ----- | ------- | -------------------- | ------- | ---------- |
| 1   | agent-a#1 | PLANNING | -     | SUCCESS | 2026-01-01T00:00:00Z | done    | -          |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
| plan.md  | PLANNING   | agent-a#1  |
</Artifacts>

<WorkflowNotes type="core">
| Seq | Note |
| --- | ---- |
</WorkflowNotes>
`

// TestIntegration_VersionDrift_Refused verifies that when an existing artifact
// records a different workflow_version than the current workflow definition and
// AllowVersionDrift is false (the default), the session returns RunRefused
// before dispatching any agents.
func TestIntegration_VersionDrift_Refused(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Existing artifact records workflow_version "2.0"; the current workflow
	// definition is "1.0". Without AllowVersionDrift the session must refuse.
	artifactPath := filepath.Join(dir, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(versionDriftArtifact), 0600); err != nil {
		t.Fatalf("write existing artifact: %v", err)
	}

	f := harness.NewMockAdapter()
	sess := newSession(f, artifactPath)
	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false, // resume: versionDrift artifact already written
		AllowVersionDrift:    false,
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRefused(t, got, err)

	// No agents must have been dispatched before the refusal.
	if len(f.Invocations()) != 0 {
		t.Errorf("want 0 harness invocations on version-drift refusal, got %d", len(f.Invocations()))
	}
}

// TestIntegration_VersionDrift_AllowOverride_Resumes verifies that when
// AllowVersionDrift is true the session resumes the run despite a
// workflow_version mismatch, dispatching only the agents not yet completed in
// the existing artifact.
func TestIntegration_VersionDrift_AllowOverride_Resumes(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Existing artifact has agent-a completed; agent-b is pending.
	// With AllowVersionDrift=true the session must resume and dispatch only agent-b.
	artifactPath := filepath.Join(dir, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(versionDriftArtifact), 0600); err != nil {
		t.Fatalf("write existing artifact: %v", err)
	}

	f := harness.NewMockAdapter()
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	sess := newSession(f, artifactPath)
	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false, // resume: versionDrift artifact already written
		AllowVersionDrift:    true,
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Only agent-b should have been dispatched (agent-a is already completed).
	invs := f.Invocations()
	if len(invs) != 1 {
		t.Errorf("want 1 harness invocation (agent-b only), got %d", len(invs))
	}
	if len(invs) == 1 && invs[0].Agent.Identifier != "agent-b" {
		t.Errorf("want agent-b dispatched, got %q", invs[0].Agent.Identifier)
	}
}
