package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== Interrupted run resumes correctly (file store) =====

// TestIntegration_InterruptedRun_ResumesFromRerunLast verifies that when an
// existing Orchestration.md reflects a mid-invocation interruption — the
// current_state.last_agent is one step ahead of the execution log's last entry
// — the session re-dispatches the interrupted row (FR-33 RerunLast) and then
// continues normally, using the real file-based artifact store.
//
// The mismatch between execution log and current_state triggers
// engine.ResumePoint to return RerunLast=true, causing the session to call
// rewindStateForRerun and re-dispatch the last logged row before advancing.
func TestIntegration_InterruptedRun_ResumesFromRerunLast(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Write an artifact that simulates a mid-invocation interruption:
	//   - Execution log: only agent-a#1 has been recorded (global_sequence=1).
	//   - current_state.last_agent: "agent-b#2" — one step ahead of the log.
	//
	// When current_state.last_agent differs from the execution log's last entry,
	// engine.ResumePoint detects an interruption (RerunLast=true) and returns
	// the row of the last logged agent so the session re-dispatches it rather
	// than advancing to the next row.
	const interruptedArtifact = `---
type: orchestration-artifact
run_id: 20260727T170000Z-a3f9
workflow: linear
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
runner_mode: auto
runner_pre_consultation: disabled
runner_manual_resolution: disabled
checkpoints: disabled
current_state:
  phase: PLANNING
  stage: null
  last_status: SUCCESS
  last_agent: "agent-b#2"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent     | Phase    | Stage | Status  | Timestamp            | Summary       | Checkpoint |
| --- | --------- | -------- | ----- | ------- | -------------------- | ------------- | ---------- |
| 1   | agent-a#1 | PLANNING | -     | SUCCESS | 2026-01-01T00:00:00Z | planning done | -          |
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
	runFolder := scopedRunFolder(t, dir)
	artifactPath := filepath.Join(runFolder, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(interruptedArtifact), 0600); err != nil {
		t.Fatalf("write interrupted artifact: %v", err)
	}

	f := harness.NewMockAdapter()
	// agent-a is re-dispatched first (RerunLast), then agent-b completes the run.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "re-done after interruption",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	sess := newSession(f, artifactPath)
	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false, // resume: interrupted artifact already written
		RunFolder:            runFolder,

	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Two harness invocations: agent-a (re-run of interrupted row) then agent-b.
	invs := f.Invocations()
	if len(invs) != 2 {
		t.Errorf("want 2 harness invocations (agent-a re-run + agent-b), got %d", len(invs))
	}
	if len(invs) >= 1 && invs[0].Agent.Identifier != "agent-a" {
		t.Errorf("want first invocation to re-run agent-a (interrupted row), got %q",
			invs[0].Agent.Identifier)
	}
	if len(invs) >= 2 && invs[1].Agent.Identifier != "agent-b" {
		t.Errorf("want second invocation to be agent-b, got %q", invs[1].Agent.Identifier)
	}
}

// ===== T9.7: Resume — configuration from frontmatter, interrupted step re-dispatched =====

// TestIntegration_Resume_ConfigFromFrontmatterNothingReAsked verifies that a
// resumed run reads its execution mode and all other RunSettings from the
// artifact frontmatter, not from the caller-supplied RunConfig. It also verifies
// that no step already completed in the artifact is re-dispatched.
//
// The artifact records mode=auto. The caller supplies an empty RunSettings
// (Mode unset, which for a new run would be a refusal). The session must apply
// auto semantics from the artifact; RunCompleted — without a routing consultant —
// proves auto mode was in effect.
func TestIntegration_Resume_ConfigFromFrontmatterNothingReAsked(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Artifact: agent-a already completed, mode=auto in frontmatter.
	const resumeArtifact = `---
type: orchestration-artifact
run_id: 20260727T170000Z-a3f9
workflow: linear
workflow_version: "1.0"
task: "resume task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
runner_mode: auto
runner_pre_consultation: disabled
runner_manual_resolution: disabled
checkpoints: disabled
commits: disabled
current_state:
  phase: PLANNING
  stage: null
  last_status: SUCCESS
  last_agent: "agent-a#1"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent     | Phase    | Stage | Status  | Timestamp            | Summary       | Checkpoint |
| --- | --------- | -------- | ----- | ------- | -------------------- | ------------- | ---------- |
| 1   | agent-a#1 | PLANNING | -     | SUCCESS | 2026-01-01T00:00:00Z | planning done | -          |
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
	runFolder := scopedRunFolder(t, dir)
	artifactPath := filepath.Join(runFolder, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(resumeArtifact), 0600); err != nil {
		t.Fatalf("write resume artifact: %v", err)
	}

	f := harness.NewMockAdapter()
	// Only agent-b needs dispatching: agent-a is already recorded in the artifact.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	// No routing consultant: if the session ignored the frontmatter and treated
	// the run as orchestrated (requiring a consultant), it would return
	// RunStoppedByConsultant. RunCompleted proves auto mode was applied from
	// the artifact frontmatter.
	sess := newSession(f, artifactPath)

	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "resume task",
		IsNewRun:             false, // resume: artifact already on disk
		RunFolder:            runFolder,
		// RunSettings intentionally empty: frontmatter value must take precedence.
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Only agent-b must have been dispatched.
	invs := f.Invocations()
	if len(invs) != 1 {
		t.Errorf("want 1 harness invocation (agent-b only, agent-a is already completed), got %d",
			len(invs))
	}
	if len(invs) == 1 && invs[0].Agent.Identifier != "agent-b" {
		t.Errorf("want agent-b dispatched on resume, got %q", invs[0].Agent.Identifier)
	}
}

// TestIntegration_Resume_InterruptedStep_ReDispatched verifies that a run
// resumed from an artifact recording a mid-invocation interruption re-dispatches
// the interrupted step before continuing, using settings read from the artifact
// frontmatter, without re-asking the user for any configuration.
//
// The interruption signature: the execution log ends at agent-a#1 but
// current_state.last_agent is already agent-b#2, meaning agent-b was dispatched
// but the runner was interrupted before recording its result. ResumePoint
// detects this as RerunLast=true and the session re-dispatches agent-a.
func TestIntegration_Resume_InterruptedStep_ReDispatched(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	const interruptedArtifact = `---
type: orchestration-artifact
run_id: 20260727T170000Z-a3f9
workflow: linear
workflow_version: "1.0"
task: "interrupted task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
runner_mode: auto
runner_pre_consultation: disabled
runner_manual_resolution: disabled
checkpoints: disabled
commits: disabled
current_state:
  phase: PLANNING
  stage: null
  last_status: SUCCESS
  last_agent: "agent-b#2"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent     | Phase    | Stage | Status  | Timestamp            | Summary       | Checkpoint |
| --- | --------- | -------- | ----- | ------- | -------------------- | ------------- | ---------- |
| 1   | agent-a#1 | PLANNING | -     | SUCCESS | 2026-01-01T00:00:00Z | planning done | -          |
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
	runFolder := scopedRunFolder(t, dir)
	artifactPath := filepath.Join(runFolder, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(interruptedArtifact), 0600); err != nil {
		t.Fatalf("write interrupted artifact: %v", err)
	}

	f := harness.NewMockAdapter()
	// agent-a is re-dispatched (RerunLast for the interrupted row), then
	// agent-b completes the run.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "re-done after interruption",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	// No routing consultant: mode=auto (read from frontmatter) routes autonomously.
	// RunConfig carries no RunSettings — the session reads them from the artifact.
	sess := newSession(f, artifactPath)

	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "interrupted task",
		IsNewRun:             false, // resume: interrupted artifact on disk
		RunFolder:            runFolder,
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) != 2 {
		t.Errorf("want 2 harness invocations (agent-a re-run + agent-b), got %d", len(invs))
	}
	if len(invs) >= 1 && invs[0].Agent.Identifier != "agent-a" {
		t.Errorf("want first invocation to re-run agent-a (interrupted row), got %q",
			invs[0].Agent.Identifier)
	}
	if len(invs) >= 2 && invs[1].Agent.Identifier != "agent-b" {
		t.Errorf("want second invocation to be agent-b, got %q", invs[1].Agent.Identifier)
	}
}
