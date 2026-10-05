package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== Position immune to infrastructure activity, end-to-end (T3.5) =====
//
// These use interval-agent-orch.md, which declares checkpoint-manager-git
// (checkpoint class, INVOCATION_INTERVAL:1 -- fires after every workflow step)
// alongside a two-row linear workflow (agent-a -> agent-b -> COMPLETE).

// TestIntegration_InfrastructureAgent_MidWorkflow_RunsToCorrectEnd verifies
// AC3.2-AC3.4: dispatching an infrastructure agent mid-workflow does not stop
// the run, and the on-disk artifact's recorded position names the last
// WORKFLOW step while every invocation -- workflow and infrastructure alike --
// remains in the on-disk execution log, in sequence and attributable.
func TestIntegration_InfrastructureAgent_MidWorkflow_RunsToCorrectEnd(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "interval-agent-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSession(f, artifactPath)
	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto, Checkpoints: true}, // enable checkpoint class so its triggers are evaluated
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	store := artifact.NewFileStore(artifactPath)
	final, err := store.Read(context.Background())
	if err != nil {
		t.Fatalf("Read final artifact: %v", err)
	}

	// AC3.2: the on-disk recorded position names the last WORKFLOW step, never
	// the infrastructure agent that ran after it.
	if final.CurrentState.LastAgent != "agent-b#3" {
		t.Errorf("on-disk CurrentState.LastAgent: want %q (last workflow step), got %q",
			"agent-b#3", final.CurrentState.LastAgent)
	}

	// AC3.3: both checkpoint invocations remain in the on-disk execution log,
	// in sequence, alongside the workflow steps.
	wantAgents := []string{"agent-a#1", "checkpoint-manager-git#2", "agent-b#3", "checkpoint-manager-git#4"}
	if len(final.ExecutionLog) != len(wantAgents) {
		t.Fatalf("on-disk ExecutionLog length: want %d, got %d", len(wantAgents), len(final.ExecutionLog))
	}
	for i, want := range wantAgents {
		if final.ExecutionLog[i].Agent != want {
			t.Errorf("on-disk ExecutionLog[%d].Agent: want %q, got %q", i, want, final.ExecutionLog[i].Agent)
		}
	}
}

// TestIntegration_InfrastructureAgent_ResumeAfterCleanStop_NotMisdiagnosedAsInterrupted
// verifies AC3.5's first variant: a run resumed from an on-disk artifact whose
// execution log ends with an infrastructure entry (recorded after a clean
// workflow step completion) continues at the next workflow row, without
// re-dispatching the already-completed workflow step and without the
// infrastructure agent being treated as the interrupted step.
func TestIntegration_InfrastructureAgent_ResumeAfterCleanStop_NotMisdiagnosedAsInterrupted(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "interval-agent-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")

	// On disk: agent-a completed, then checkpoint-manager-git fired cleanly
	// afterward. Per the fix, current_state still names agent-a (the last
	// WORKFLOW step) even though checkpoint-manager-git is the execution log's
	// last entry -- this is a clean stop, not an interruption.
	const artifactContent = `---
type: orchestration-artifact
run_id: 20260727T170000Z-a3f9
workflow: linear
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 2
runner_mode: auto
runner_pre_consultation: disabled
runner_manual_resolution: disabled
checkpoints: disabled
current_state:
  phase: PLANNING
  stage: null
  last_status: SUCCESS
  last_agent: "agent-a#1"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Checkpoint |
| --- | ----- | ----- | ----- | ------ | --------- | ------- | ---------- |
| 1 | agent-a#1 | PLANNING | - | SUCCESS | 2026-01-01T00:00:00Z | planning done | - |
| 2 | checkpoint-manager-git#2 | PLANNING | - | SUCCESS | 2026-01-01T00:00:00Z | checkpoint taken | - |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
| plan.md | PLANNING | agent-a#1 |
</Artifacts>

<WorkflowNotes type="core">
| Seq | Note |
| --- | ---- |
</WorkflowNotes>
`
	runFolder := scopedRunFolder(t, dir)
	artifactPath := filepath.Join(runFolder, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(artifactContent), 0600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	f := harness.NewMockAdapter()
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	sess := newSession(f, artifactPath)
	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false,
		RunFolder:            runFolder,
		RunSettings:          domain.RunSettings{Checkpoints: true}, // enable checkpoint class so its triggers are evaluated
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) == 0 || invs[0].Agent.Identifier != "agent-b" {
		t.Fatalf("want first invocation to be agent-b (the row after the last workflow step), got %+v", invs)
	}
	for _, inv := range invs {
		if inv.Agent.Identifier == "agent-a" {
			t.Errorf("agent-a re-dispatched: resume misdiagnosed the trailing infrastructure entry as an interruption")
		}
	}
}

// TestIntegration_InfrastructureAgent_InterruptedAfterActivity_ResumesCorrectly
// verifies AC3.5's second variant: a genuine mid-flight interruption occurring
// after infrastructure activity is still detected and the interrupted workflow
// row is re-dispatched, without the interleaved infrastructure entry being
// mistaken for the interrupted step.
func TestIntegration_InfrastructureAgent_InterruptedAfterActivity_ResumesCorrectly(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "interval-agent-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")

	// On disk: agent-a completed and is recorded in current_state.
	// checkpoint-manager-git then fired (does not move current_state). agent-b
	// was then dispatched and completed, but the runner was interrupted before
	// current_state could be updated to reflect it.
	const artifactContent = `---
type: orchestration-artifact
run_id: 20260727T170000Z-a3f9
workflow: linear
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 3
runner_mode: auto
runner_pre_consultation: disabled
runner_manual_resolution: disabled
checkpoints: disabled
current_state:
  phase: PLANNING
  stage: null
  last_status: SUCCESS
  last_agent: "agent-a#1"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Checkpoint |
| --- | ----- | ----- | ----- | ------ | --------- | ------- | ---------- |
| 1 | agent-a#1 | PLANNING | - | SUCCESS | 2026-01-01T00:00:00Z | planning done | - |
| 2 | checkpoint-manager-git#2 | PLANNING | - | SUCCESS | 2026-01-01T00:00:00Z | checkpoint taken | - |
| 3 | agent-b#3 | PLANNING | - | SUCCESS | 2026-01-01T00:00:00Z | done | - |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
| plan.md | PLANNING | agent-a#1 |
</Artifacts>

<WorkflowNotes type="core">
| Seq | Note |
| --- | ---- |
</WorkflowNotes>
`
	runFolder := scopedRunFolder(t, dir)
	artifactPath := filepath.Join(runFolder, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(artifactContent), 0600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	f := harness.NewMockAdapter()
	// agent-b is re-dispatched (it was interrupted before current_state recorded
	// it), then the run completes.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "re-done after interruption",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	sess := newSession(f, artifactPath)
	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false,
		RunFolder:            runFolder,
		RunSettings:          domain.RunSettings{Checkpoints: true}, // enable checkpoint class so its triggers are evaluated
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) == 0 || invs[0].Agent.Identifier != "agent-b" {
		t.Fatalf("want first invocation to re-run agent-b (interrupted row), got %+v", invs)
	}
}
