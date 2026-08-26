package integration_test

// End-to-end integration tests for the mosaic-run stack.
//
// These tests exercise the full stack with the file-based ArtifactStore
// (not the in-memory stub used by session unit tests), and verify that the
// produced Orchestration.md files match pre-computed golden files byte-exactly.
//
// Coverage:
//
//   Golden file comparison (T11.1):
//   - Linear workflow end-to-end: produced Orchestration.md matches the golden
//     file when a fixed clock and scripted fake harness are used.
//
//   Staged workflow (T11.1):
//   - A two-stage implementation-only workflow completes successfully and
//     dispatches the correct number of agents.
//
//   All four approaches — staged workflow golden file (T11.1 / AC11.4):
//   - A two-group staged workflow runs four stages (TDD, Implementation-First,
//     Implementation-Only, Tests-Only) end-to-end and produces an
//     Orchestration.md matching the golden file.
//
//   All five supported workflows end-to-end (T11.1 / AC11.6):
//   - brownfield-tdd, brownfield-tdd-build-verified, greenfield-tdd,
//     implementation-only, and quick-fix each run end-to-end against the fake
//     adapter with scripted responses and return RunCompleted.
//
//   Version-drift override (T11.1):
//   - An existing artifact with a mismatched workflow_version is refused when
//     AllowVersionDrift is false, but proceeds when it is true.
//
//   No Plan.md for staged workflow (T11.1):
//   - Requesting a staged workflow when the adjacent Plan.md does not exist
//     returns RunRefused with a message that names Plan.md (FR-16).
//
//   FR-41 optional-row regression (T11.1):
//   - A row described as "optional" in workflow prose that is present in the
//     routing table is still dispatched by the runner and contributes to a
//     non-SUCCESS deviation if it returns a non-SUCCESS status.
//
//   Build-review On Findings loop-back (T11.1 / AC11.8):
//   - In a workflow where build-review has On Findings = paired-writer-agent,
//     a COMPLETED_NEEDS_ACTION response from build-review causes the engine to
//     return a Dispatch (not a Deviation), and the deviation resolver is NOT
//     called. The paired writer agent is reinvoked without escalation.

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-common/interaction"
	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// update is set by -update to regenerate golden files rather than compare against them.
// Usage: go test -run TestIntegration_... -update
var update = flag.Bool("update", false, "regenerate golden files")

// ---- fixed-time Clock ----

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// ---- always-approved ApprovalReader ----

// alwaysApprovedReader approves every artifact unconditionally. Integration
// tests exercise routing and harness dispatch, not HITL gate enforcement; this
// reader prevents the HITL check from redispatching agents whose output files
// do not exist on disk (which is the normal state in integration tests).
type alwaysApprovedReader struct{}

func (alwaysApprovedReader) ReadApproval(_ context.Context, _ string) domain.HumanApproval {
	return domain.ApprovalTrue
}

// ---- no-op Interaction ----

type noopInteraction struct{}

func (n *noopInteraction) SelectOne(_ context.Context, _ interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) SelectMany(_ context.Context, _ interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	return interaction.MultiChoiceAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) AskText(_ context.Context, _ interaction.TextQuestion) (interaction.TextAnswer, error) {
	return interaction.TextAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) Confirm(_ context.Context, _ interaction.Question) (interaction.ConfirmAnswer, error) {
	return interaction.ConfirmAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) Notify(_ context.Context, _ interaction.Notice)          {}
func (n *noopInteraction) Progress(_ context.Context, _ interaction.ProgressEvent) {}

// ---- test helpers ----

// sessionTestdataDir is the path to the session package's testdata directory,
// which contains reusable orchestrator fixture files.
const sessionTestdataDir = "../../testdata/session"

// goldenDir is the path to the integration golden file directory.
const goldenDir = "../../testdata/integration/golden"

// copyFile copies src to dst in the given temp directory with the given name.
func copyFile(t *testing.T, dir, dstName, srcPath string) string {
	t.Helper()
	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("copyFile: read %q: %v", srcPath, err)
	}
	dst := filepath.Join(dir, dstName)
	if err := os.WriteFile(dst, data, 0600); err != nil {
		t.Fatalf("copyFile: write %q: %v", dst, err)
	}
	return dst
}

// writeAgentFile creates a minimal agent definition file for the given ID.
func writeAgentFile(t *testing.T, dir, agentID string) {
	t.Helper()
	path := filepath.Join(dir, agentID+".md")
	if err := os.WriteFile(path, []byte("# Agent: "+agentID+"\n"), 0600); err != nil {
		t.Fatalf("writeAgentFile(%q): %v", agentID, err)
	}
}

// writeOrchFile writes an orchestrator file with an inline workflow section.
func writeOrchFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeOrchFile: %v", err)
	}
	return path
}

// newSession builds a session with a file-based artifact store and scripted components.
// Approvals is wired to alwaysApprovedReader so that HITL checks on workflows
// with HITL=true rows do not redispatch agents for non-existent approval files.
func newSession(
	f *harness.FakeAdapter,
	artifactPath string,
) session.Session {
	store := artifact.NewFileStore(artifactPath)
	return session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Approvals: alwaysApprovedReader{},
	})
}

// requireRunStatus asserts that the RunOutcome has the expected status.
func requireRunStatus(t *testing.T, got domain.RunOutcome, err error, want domain.RunStatus) {
	t.Helper()
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != want {
		t.Errorf("want Status=%q, got %q (message: %q)", want, got.Status, got.Message)
	}
}

// requireRefused asserts that the outcome is RunRefused with a nil error.
func requireRefused(t *testing.T, got domain.RunOutcome, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("want nil error for refusal, got %v", err)
	}
	if got.Status != domain.RunRefused {
		t.Errorf("want RunRefused, got %q (message: %q)", got.Status, got.Message)
	}
	return got.Message
}

// ===== Golden file comparison (linear workflow) =====

// TestIntegration_LinearWorkflow_GoldenFileMatch verifies that a complete
// two-agent linear run produces an Orchestration.md file that matches the
// pre-computed golden file byte-exactly.
//
// Because a fixed clock is injected and the fake harness returns scripted
// responses, the artifact content is fully deterministic.
func TestIntegration_LinearWorkflow_GoldenFileMatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	artifactPath := filepath.Join(dir, "Orchestration.md")
	f := harness.NewFakeAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planning done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})

	sess := newSession(f, artifactPath)
	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Read the produced artifact.
	produced, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("read produced artifact: %v", readErr)
	}

	goldenPath := filepath.Join(goldenDir, "linear-after-run.md")

	if *update {
		if writeErr := os.WriteFile(goldenPath, produced, 0600); writeErr != nil {
			t.Fatalf("update: write golden file %q: %v", goldenPath, writeErr)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	golden, goldenErr := os.ReadFile(goldenPath)
	if goldenErr != nil {
		t.Fatalf("read golden file %q: %v (run with -update to generate it)", goldenPath, goldenErr)
	}

	if !bytes.Equal(produced, golden) {
		t.Errorf("produced artifact does not match golden file\n\nProduced:\n%s\n\nGolden:\n%s",
			string(produced), string(golden))
	}
}

// ===== Staged workflow completion =====

// TestIntegration_StagedWorkflow_TwoStages_Completes verifies that a staged
// implementation-only workflow with two stages dispatches the correct set of
// agents (2 rows × 2 stages = 4 total invocations) and returns RunCompleted.
func TestIntegration_StagedWorkflow_TwoStages_Completes(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "staged-orch.md"))
	writeAgentFile(t, dir, "implementation-tdd")
	writeAgentFile(t, dir, "implementation-review")

	// Write Plan.md with two stages so the session can read the stage set.
	planContent := `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First stage | - | FALSE |
| 2 | Stage Two | Second stage | 1 | FALSE |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(planContent), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	artifactPath := filepath.Join(dir, "Orchestration.md")
	f := harness.NewFakeAdapter()

	// Stage 1: implementation-tdd → implementation-review.
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 1 implemented",
	}})
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 1 reviewed",
	}})

	// Stage 2: implementation-tdd → implementation-review.
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 2 implemented",
	}})
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 2 reviewed",
	}})

	sess := newSession(f, artifactPath)
	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "two-stage task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            dir, // Plan.md was written into dir; the session must resolve it here.
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify 4 harness invocations: 2 rows × 2 stages.
	invs := f.Invocations()
	if len(invs) != 4 {
		t.Errorf("want 4 harness invocations (2 per stage × 2 stages), got %d", len(invs))
	}

	// Verify stage-specific artifacts in the invocation requests.
	if len(invs) >= 1 && !containsArtifact(invs[0].Request.InputArtifacts, "Stage-1/Plan.md") {
		t.Errorf("first invocation should have Stage-1/Plan.md as input, got %v",
			invs[0].Request.InputArtifacts)
	}
	if len(invs) >= 3 && !containsArtifact(invs[2].Request.InputArtifacts, "Stage-2/Plan.md") {
		t.Errorf("third invocation should have Stage-2/Plan.md as input, got %v",
			invs[2].Request.InputArtifacts)
	}
}

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

	f := harness.NewFakeAdapter()
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

	f := harness.NewFakeAdapter()
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

// ===== No Plan.md for staged workflow =====

// TestIntegration_StagedWorkflow_NoPlanMd_StopsCleanlyAtExecution verifies
// that when a staged workflow with no pre-EXECUTION rows is requested but no
// Plan.md exists in the run folder, the session does not refuse to start —
// absence of a plan file is a normal state, never a run-start refusal. The
// run proceeds to the point where the workflow's topology requires a stage
// set (its first, and here only, row is EXECUTION), where it stops cleanly
// with a message naming the missing stage set, instead of panicking or
// dispatching an agent it cannot route.
//
// This is the "never seeded" case of the Stage 5 diagnostic (AC5.5): no
// SeedInputs were given at all, so the stop message must name what was
// expected (Plan.md) and where it was looked for (the run folder path),
// distinct from the "seeded but missing" case exercised elsewhere.
func TestIntegration_StagedWorkflow_NoPlanMd_StopsCleanlyAtExecution(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "staged-orch.md"))
	writeAgentFile(t, dir, "implementation-tdd")
	writeAgentFile(t, dir, "implementation-review")
	// Deliberately do NOT write Plan.md — absence must not refuse the run.
	// SeedInputs is also deliberately unset: this run never had a stage table
	// seeded into it at all.

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSession(harness.NewFakeAdapter(), artifactPath)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            dir, // no Plan.md ever appears here
	}

	got, err := sess.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped when EXECUTION is reached with no stage set, got %q (message: %q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "stage set") {
		t.Errorf("want stop message to name the missing stage set, got %q", got.Message)
	}
	// AC5.5: the message must name where the stage table was looked for, not
	// just that one is missing.
	wantPath := filepath.Join(dir, "Plan.md")
	if !strings.Contains(got.Message, wantPath) {
		t.Errorf("want stop message to name the path looked for (%q); got %q", wantPath, got.Message)
	}
}

// ===== FR-41 optional-row regression =====

// TestIntegration_OptionalRow_IsDispatchedAndContributesToDeviation verifies
// that a row described as "optional" in workflow prose but present in the
// routing table is still dispatched by the runner, and that a non-SUCCESS
// response from it escalates to the deviation resolver (not silently skipped).
//
// This is a regression test for FR-41: the runner must never skip routing-table
// rows based on prose annotations. Every row in the routing table is dispatched.
func TestIntegration_OptionalRow_IsDispatchedAndContributesToDeviation(t *testing.T) {
	dir := t.TempDir()

	// Workflow with three rows: agent-a → optional-agent → agent-b.
	// The middle row is described as "optional" in the workflow prose, but it
	// is present in the routing table and must be dispatched.
	const orchContent = `<Workflow type="core" name="optional-row" version="1.0">
## Optional Row Regression Workflow

**Note:** optional-agent is optional — skip if not needed (this prose is ignored by the runner).

| Phase | Subagent       | HITL | On Success     | On Findings | Input | Output     |
|-------|----------------|:----:|----------------|-------------|-------|------------|
| PLANNING | agent-a     | FALSE | optional-agent | -           | -     | plan.md    |
| DESIGN   | optional-agent | FALSE | agent-b       | -           | plan.md | design.md |
| REVIEW   | agent-b     | FALSE | COMPLETE       | -           | design.md | result.md |
</Workflow>
`
	orchPath := writeOrchFile(t, dir, "orchestrator.md", orchContent)
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "optional-agent")
	writeAgentFile(t, dir, "agent-b")

	artifactPath := filepath.Join(dir, "Orchestration.md")
	f := harness.NewFakeAdapter()
	// No routing consultant: deviation terminates with RunDeviationUnresolved.
	sess := newSession(f, artifactPath)

	// agent-a returns SUCCESS (normal).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planning done",
	}})

	// optional-agent returns PARTIALLY_DONE (a non-SUCCESS response that triggers
	// a deviation, proving the optional row was dispatched AND contributes to a
	// deviation if it fails).
	f.Queue("optional-agent", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "optional-agent#2",
		StatusCode:      domain.StatusPARTIALLY_DONE,
		StatusMessage:   "only partially done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "optional-row",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := sess.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}

	// The run must stop with RunDeviationUnresolved because:
	// 1. optional-agent was dispatched (not skipped).
	// 2. optional-agent's non-SUCCESS response triggered a deviation with no routing consultant.
	if got.Status != domain.RunDeviationUnresolved {
		t.Errorf("want RunDeviationUnresolved (optional row was dispatched and deviated), got %q", got.Status)
	}

	// Verify that both agent-a and optional-agent were actually invoked.
	invs := f.Invocations()
	if len(invs) < 2 {
		t.Fatalf("want at least 2 harness invocations (agent-a + optional-agent), got %d", len(invs))
	}
	if invs[0].Agent.Identifier != "agent-a" {
		t.Errorf("want first invocation to be agent-a, got %q", invs[0].Agent.Identifier)
	}
	if invs[1].Agent.Identifier != "optional-agent" {
		t.Errorf("want second invocation to be optional-agent, got %q", invs[1].Agent.Identifier)
	}
}

// ===== Build-review On Findings loop-back (AC11.8) =====

// TestIntegration_BuildReview_OnFindings_LoopBack_NoDev verifies that in a
// workflow where build-review has On Findings = paired writer agent, a
// COMPLETED_NEEDS_ACTION response from build-review causes the engine to route
// back to the paired writer without invoking the deviation resolver.
//
// This exercises the routine two-iteration loop documented in
// brownfield-tdd-build-verified: build-review returns CNA → engine dispatches
// the paired writer agent → session invokes it without escalating to deviation.
func TestIntegration_BuildReview_OnFindings_LoopBack_NoDev(t *testing.T) {
	dir := t.TempDir()

	// A simplified workflow that mirrors the brownfield-tdd-build-verified
	// EXECUTION phase pattern for one stage: test-writer-tdd → build-review →
	// implementation-tdd (where build-review has On Findings = test-writer-tdd).
	const orchContent = `<Workflow type="core" name="build-review-loop" version="1.0">
## Build-Review Loop Workflow

| Phase | Subagent          | HITL | On Success        | On Findings      | Input | Output         |
|-------|-------------------|:----:|-------------------|------------------|-------|----------------|
| PLANNING | test-writer-tdd | FALSE | build-review      | -                | -     | tests.md       |
| PLANNING | build-review    | FALSE | implementation-tdd| test-writer-tdd  | tests.md | build.md    |
| PLANNING | implementation-tdd | FALSE | COMPLETE         | -                | tests.md | impl.md     |
</Workflow>
`
	orchPath := writeOrchFile(t, dir, "orchestrator.md", orchContent)
	writeAgentFile(t, dir, "test-writer-tdd")
	writeAgentFile(t, dir, "build-review")
	writeAgentFile(t, dir, "implementation-tdd")

	artifactPath := filepath.Join(dir, "Orchestration.md")
	f := harness.NewFakeAdapter()
	sess := newSession(f, artifactPath)

	// test-writer-tdd → SUCCESS.
	f.Queue("test-writer-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "test-writer-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "tests written",
	}})
	// build-review → COMPLETED_NEEDS_ACTION (triggers On Findings loop-back
	// to test-writer-tdd without deviation escalation).
	f.Queue("build-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "build-review#2",
		StatusCode:      domain.StatusCOMPLETED_NEEDS_ACTION,
		StatusMessage:   "build failed: test compilation error",
	}})
	// test-writer-tdd loop-back → SUCCESS.
	f.Queue("test-writer-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "test-writer-tdd#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "tests fixed",
	}})
	// build-review second attempt → SUCCESS.
	f.Queue("build-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "build-review#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "build ok",
	}})
	// implementation-tdd → SUCCESS → COMPLETE.
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "implemented",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "build-review-loop",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAutoReview},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify 5 harness invocations in the expected order.
	invs := f.Invocations()
	wantOrder := []string{
		"test-writer-tdd",   // initial dispatch
		"build-review",      // first build attempt → CNA
		"test-writer-tdd",   // loop-back via On Findings
		"build-review",      // second build attempt → SUCCESS
		"implementation-tdd", // continues to next row
	}
	if len(invs) != len(wantOrder) {
		t.Errorf("want %d harness invocations, got %d", len(wantOrder), len(invs))
	}
	for i, want := range wantOrder {
		if i < len(invs) && invs[i].Agent.Identifier != want {
			t.Errorf("invocation[%d]: want %q, got %q", i, want, invs[i].Agent.Identifier)
		}
	}
}

// ===== All four approaches — staged workflow golden file (AC11.4) =====

// TestIntegration_AllFourApproaches_StagedWorkflow_GoldenFileMatch verifies
// that a two-group staged workflow running through all four approach values
// (TDD, Implementation-First, Implementation-Only, Tests-Only) across four
// stages completes successfully and produces an Orchestration.md matching the
// pre-computed golden file byte-exactly.
//
// This covers AC11.4: "end-to-end tests for a staged workflow with all four
// approaches produce a matching artifact."
//
// Run with -update to regenerate the golden file after intentional format changes:
//
//	go test -run TestIntegration_AllFourApproaches -update
func TestIntegration_AllFourApproaches_StagedWorkflow_GoldenFileMatch(t *testing.T) {
	dir := t.TempDir()

	// Synthetic two-group workflow (test group rows 0-2, impl group rows 3-5),
	// matching the brownfield-tdd-build-verified EXECUTION structure.
	// No pre-execution rows (stage set is read from the Plan.md placed in dir).
	const orchContent = `<Workflow type="core" name="four-approach-staged" version="1.0">
## Four Approach Staged Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | build-review | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/tests.md |
| EXECUTION.Test.[StageNumber] | build-review | FALSE | tests-review-tdd | test-writer-tdd | Stage-{StageNumber}/tests.md | Stage-{StageNumber}/build-review-tests.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/tests.md | Stage-{StageNumber}/tests-review.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | build-review | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/impl.md |
| EXECUTION.Implementation.[StageNumber] | build-review | FALSE | implementation-review | implementation-tdd | Stage-{StageNumber}/impl.md | Stage-{StageNumber}/build-review-impl.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | COMPLETE | implementation-tdd | Stage-{StageNumber}/impl.md | Stage-{StageNumber}/impl-review.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
</Workflow>
`
	orchPath := writeOrchFile(t, dir, "orchestrator.md", orchContent)

	// Plan.md: four stages, one per approach value.
	const planContent = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL | Approach |
|-------|------|------|------------|:----:|----------|
| 1 | TDD Stage | Test-first development | - | FALSE | TDD |
| 2 | IF Stage | Implementation-first | 1 | FALSE | Implementation-First |
| 3 | IO Stage | Implementation only | 2 | FALSE | Implementation-Only |
| 4 | TO Stage | Tests only | 3 | FALSE | Tests-Only |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(planContent), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	for _, a := range []string{
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	} {
		writeAgentFile(t, dir, a)
	}

	artifactPath := filepath.Join(dir, "Orchestration.md")
	f := harness.NewFakeAdapter()

	// Queue scripted SUCCESS responses in dispatch order.
	// Stage 1 (TDD): test group first → impl group
	//   test-writer-tdd → build-review → tests-review-tdd → implementation-tdd → build-review → implementation-review
	queueOK := func(agent, msg string) {
		f.Queue(agent, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: agent + "#scripted",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   msg,
		}})
	}
	queueOK("test-writer-tdd", "s1 tests written")
	queueOK("build-review", "s1 test build ok")
	queueOK("tests-review-tdd", "s1 tests reviewed")
	queueOK("implementation-tdd", "s1 implemented")
	queueOK("build-review", "s1 impl build ok")
	queueOK("implementation-review", "s1 impl reviewed")

	// Stage 2 (Implementation-First): impl group first → test group
	//   implementation-tdd → build-review → implementation-review → test-writer-tdd → build-review → tests-review-tdd
	queueOK("implementation-tdd", "s2 implemented")
	queueOK("build-review", "s2 impl build ok")
	queueOK("implementation-review", "s2 impl reviewed")
	queueOK("test-writer-tdd", "s2 tests written")
	queueOK("build-review", "s2 test build ok")
	queueOK("tests-review-tdd", "s2 tests reviewed")

	// Stage 3 (Implementation-Only): impl group only
	//   implementation-tdd → build-review → implementation-review
	queueOK("implementation-tdd", "s3 implemented")
	queueOK("build-review", "s3 impl build ok")
	queueOK("implementation-review", "s3 impl reviewed")

	// Stage 4 (Tests-Only): test group only
	//   test-writer-tdd → build-review → tests-review-tdd
	queueOK("test-writer-tdd", "s4 tests written")
	queueOK("build-review", "s4 test build ok")
	queueOK("tests-review-tdd", "s4 tests reviewed")

	sess := newSession(f, artifactPath)
	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "four-approach-staged",
		Task:                 "four-approach task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            dir, // Plan.md was written into dir; the session must resolve it here.
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify 18 invocations: 6 (Stage-1 TDD) + 6 (Stage-2 IF) + 3 (Stage-3 IO) + 3 (Stage-4 TO).
	invs := f.Invocations()
	if len(invs) != 18 {
		t.Errorf("want 18 harness invocations (6+6+3+3), got %d", len(invs))
	}

	produced, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("read produced artifact: %v", readErr)
	}

	goldenPath := filepath.Join(goldenDir, "four-approach-after-run.md")

	if *update {
		if writeErr := os.WriteFile(goldenPath, produced, 0600); writeErr != nil {
			t.Fatalf("update: write golden file %q: %v", goldenPath, writeErr)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	golden, goldenErr := os.ReadFile(goldenPath)
	if goldenErr != nil {
		t.Fatalf("read golden file %q: %v (run with -update to generate it)", goldenPath, goldenErr)
	}

	if !bytes.Equal(produced, golden) {
		t.Errorf("produced artifact does not match golden file\n\nProduced:\n%s\n\nGolden:\n%s",
			string(produced), string(golden))
	}
}

// ===== Five supported workflows — end-to-end (AC11.6) =====

// TestIntegration_FiveWorkflows_EndToEnd verifies that each of the five
// supported MOSAIC workflows — brownfield-tdd, brownfield-tdd-build-verified,
// greenfield-tdd, implementation-only, and quick-fix — runs end-to-end against
// the fake adapter and returns RunCompleted after scripted agent responses.
//
// Each workflow case uses a single stage with Implementation-Only approach
// (where the approach is applicable) to minimise the number of scripted
// responses required while still exercising the full session → engine →
// artifact → fake-harness stack.
//
// This covers AC11.6: "all five supported workflows validate and execute
// end-to-end against the fake adapter."
func TestIntegration_FiveWorkflows_EndToEnd(t *testing.T) {
	// planImplOnly is a single-stage Plan.md with Implementation-Only approach,
	// used by two-group workflows (TwoGroup=true, needsApproach=true).
	const planImplOnly = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL | Approach |
|-------|------|------|------------|:----:|----------|
| 1 | Stage One | Run | - | FALSE | Implementation-Only |
`

	// planSingleGroup is a single-stage Plan.md without an Approach column,
	// used by single-group workflows (TwoGroup=false, needsApproach=false).
	const planSingleGroup = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | Run | - | FALSE |
`

	// agentResponse is a scripted SUCCESS response for a named agent.
	type agentResponse struct {
		agent   string
		summary string
	}

	cases := []struct {
		name        string
		workflowID  string
		orchContent string
		plan        string
		agents      []string
		responses   []agentResponse
	}{
		{
			name:       "brownfield-tdd",
			workflowID: "brownfield-tdd",
			// Routing table matches brownfield-tdd v3.4.
			// Two-group: test group (test-writer-tdd, tests-review-tdd) +
			//            impl group (implementation-tdd, implementation-review).
			// Seven pre-exec rows; post-exec row: test-runner.
			orchContent: `<Workflow type="core" name="brownfield-tdd" version="3.5">
## Brownfield TDD Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | FALSE | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | TRUE | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | FALSE | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
</Workflow>
`,
			plan: planImplOnly,
			agents: []string{
				"codebase-research", "requirements-refinement", "requirements-review",
				"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
				"test-writer-tdd", "tests-review-tdd",
				"implementation-tdd", "implementation-review",
				"test-runner",
			},
			// Dispatch order with Implementation-Only approach (impl group only):
			// 7 pre-exec + 2 exec (Stage-1) + 1 post-exec = 10
			responses: []agentResponse{
				{"codebase-research", "research done"},
				{"requirements-refinement", "requirements refined"},
				{"requirements-review", "requirements reviewed"},
				{"planner-tdd-soft", "plan created"},
				{"plan-review", "plan reviewed"},
				{"contracts-designer", "contracts designed"},
				{"contracts-review", "contracts reviewed"},
				{"implementation-tdd", "stage 1 implemented"},
				{"implementation-review", "stage 1 reviewed"},
				{"test-runner", "tests passed"},
			},
		},
		{
			name:       "brownfield-tdd-build-verified",
			workflowID: "brownfield-tdd-build-verified",
			// Two-group: test group (test-writer-tdd, build-review, tests-review-tdd) +
			//            impl group (implementation-tdd, build-review, implementation-review).
			// Seven pre-exec rows; no post-exec rows.
			orchContent: `<Workflow type="core" name="brownfield-tdd-build-verified" version="2.1">
## Brownfield TDD Build-Verified Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | FALSE | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | TRUE | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | FALSE | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | build-review | - | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | build-review | FALSE | tests-review-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-tests.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | build-review | - | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | build-review | FALSE | implementation-review | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-impl.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | COMPLETE | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
</Workflow>
`,
			plan: planImplOnly,
			agents: []string{
				"codebase-research", "requirements-refinement", "requirements-review",
				"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
				"test-writer-tdd", "build-review", "tests-review-tdd",
				"implementation-tdd", "implementation-review",
			},
			// Dispatch order with Implementation-Only (impl group: rows 10-12):
			// 7 pre-exec + 3 exec (Stage-1) = 10
			responses: []agentResponse{
				{"codebase-research", "research done"},
				{"requirements-refinement", "requirements refined"},
				{"requirements-review", "requirements reviewed"},
				{"planner-tdd-soft", "plan created"},
				{"plan-review", "plan reviewed"},
				{"contracts-designer", "contracts designed"},
				{"contracts-review", "contracts reviewed"},
				{"implementation-tdd", "stage 1 implemented"},
				{"build-review", "stage 1 build ok"},
				{"implementation-review", "stage 1 reviewed"},
			},
		},
		{
			name:       "greenfield-tdd",
			workflowID: "greenfield-tdd",
			// Two-group: test group (test-writer-tdd, tests-review-tdd) +
			//            impl group (implementation-tdd, implementation-review).
			// Eight pre-exec rows; post-exec row: test-runner.
			orchContent: `<Workflow type="core" name="greenfield-tdd" version="3.4">
## Greenfield TDD Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Requirements.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | system-designer | requirements-refinement | Requirements.md | requirements-review.md |
| ARCHITECTURE | system-designer | TRUE | system-design-review | - | Requirements.md | SystemDesign.md |
| ARCHITECTURE | system-design-review | FALSE | planner-tdd-soft | system-designer | Requirements.md, SystemDesign.md | system-design-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Requirements.md, SystemDesign.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | TRUE | contracts-review | - | Requirements.md, Plan.md, Stage-*/Plan.md, SystemDesign.md | ContractsDesign.md |
| DESIGN | contracts-review | FALSE | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
</Workflow>
`,
			plan: planImplOnly,
			agents: []string{
				"requirements-refinement", "requirements-review",
				"system-designer", "system-design-review",
				"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
				"test-writer-tdd", "tests-review-tdd",
				"implementation-tdd", "implementation-review",
				"test-runner",
			},
			// Dispatch order with Implementation-Only (impl group rows 10-11):
			// 8 pre-exec + 2 exec (Stage-1) + 1 post-exec = 11
			responses: []agentResponse{
				{"requirements-refinement", "requirements refined"},
				{"requirements-review", "requirements reviewed"},
				{"system-designer", "system designed"},
				{"system-design-review", "system design reviewed"},
				{"planner-tdd-soft", "plan created"},
				{"plan-review", "plan reviewed"},
				{"contracts-designer", "contracts designed"},
				{"contracts-review", "contracts reviewed"},
				{"implementation-tdd", "stage 1 implemented"},
				{"implementation-review", "stage 1 reviewed"},
				{"test-runner", "tests passed"},
			},
		},
		{
			name:       "implementation-only",
			workflowID: "implementation-only",
			// Single-group: implementation-tdd → implementation-review.
			// No pre-exec rows; post-exec row: test-runner.
			orchContent: `<Workflow type="core" name="implementation-only" version="3.1">
## Implementation Only Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | FALSE | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |
</Workflow>
`,
			plan:   planSingleGroup,
			agents: []string{"implementation-tdd", "implementation-review", "test-runner"},
			// Dispatch order: 2 exec (Stage-1) + 1 post-exec = 3
			responses: []agentResponse{
				{"implementation-tdd", "stage 1 implemented"},
				{"implementation-review", "stage 1 reviewed"},
				{"test-runner", "tests passed"},
			},
		},
		{
			name:       "quick-fix",
			workflowID: "quick-fix",
			// Single-group: implementation-tdd (EXECUTION).
			// Two pre-exec rows; post-exec row: test-runner.
			orchContent: `<Workflow type="core" name="quick-fix" version="3.0">
## Quick Fix Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | implementation-tdd | planner-tdd-soft | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | test-runner | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |
</Workflow>
`,
			plan:   planSingleGroup,
			agents: []string{"planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner"},
			// Dispatch order: 2 pre-exec + 1 exec (Stage-1) + 1 post-exec = 4
			responses: []agentResponse{
				{"planner-tdd-soft", "plan created"},
				{"plan-review", "plan reviewed"},
				{"implementation-tdd", "stage 1 implemented"},
				{"test-runner", "tests passed"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			orchPath := writeOrchFile(t, dir, "orchestrator.md", tc.orchContent)

			if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(tc.plan), 0600); err != nil {
				t.Fatalf("write Plan.md: %v", err)
			}

			for _, a := range tc.agents {
				writeAgentFile(t, dir, a)
			}

			artifactPath := filepath.Join(dir, "Orchestration.md")
			f := harness.NewFakeAdapter()
			for _, r := range tc.responses {
				f.Queue(r.agent, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
					AgentInstanceID: r.agent + "#scripted",
					StatusCode:      domain.StatusSUCCESS,
					StatusMessage:   r.summary,
				}})
			}

			sess := newSession(f, artifactPath)
			cfg := domain.RunConfig{
				OrchestratorFilePath: orchPath,
				WorkflowID:           domain.WorkflowID(tc.workflowID),
				Task:                 tc.name + " task",
				IsNewRun:             true,
				RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
				RunFolder:            dir, // Plan.md was written into dir; the session must resolve it here.
			}

			got, err := sess.Start(context.Background(), cfg)
			requireRunStatus(t, got, err, domain.RunCompleted)

			// Verify the correct number of harness invocations.
			invs := f.Invocations()
			if len(invs) != len(tc.responses) {
				t.Errorf("want %d harness invocations, got %d",
					len(tc.responses), len(invs))
			}

			// Verify that all queued responses were consumed. An unconsumed
			// entry means the session dispatched fewer agents than the test
			// expected — a silent under-dispatch would otherwise go unnoticed.
			if remaining := f.RemainingQueueSize(); remaining > 0 {
				t.Errorf("want all queued responses consumed after run; %d entries remain unconsumed",
					remaining)
			}
		})
	}
}

// ===== Deviation mid-run resolved and resumes (file store) =====

// TestIntegration_Deviation_NoConsultant_ReturnsUnresolved verifies that when
// an agent returns a non-SUCCESS status and no routing consultant is wired,
// the session terminates with RunDeviationUnresolved — exercising the full
// stack with the real file-based artifact store.
func TestIntegration_Deviation_NoConsultant_ReturnsUnresolved(t *testing.T) {
	dir := t.TempDir()

	const orchContent = `<Workflow type="core" name="deviation-resume" version="1.0">
## Deviation Resume Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | - | plan.md |
| PLANNING | agent-b | FALSE | COMPLETE | plan.md | result.md |
</Workflow>
`
	orchPath := writeOrchFile(t, dir, "orchestrator.md", orchContent)
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	artifactPath := filepath.Join(dir, "Orchestration.md")
	f := harness.NewFakeAdapter()
	// No routing consultant: deviation terminates with RunDeviationUnresolved.
	sess := newSession(f, artifactPath)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusPARTIALLY_DONE,
		StatusMessage:   "only partially done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "deviation-resume",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := sess.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunDeviationUnresolved {
		t.Errorf("want RunDeviationUnresolved when no consultant is wired, got %q", got.Status)
	}

	// The real artifact file must exist: the session created it.
	if _, statErr := os.Stat(artifactPath); statErr != nil {
		t.Errorf("want artifact file at %s after run, got %v", artifactPath, statErr)
	}
}

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
workflow: linear
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
mode: auto
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
	artifactPath := filepath.Join(dir, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(interruptedArtifact), 0600); err != nil {
		t.Fatalf("write interrupted artifact: %v", err)
	}

	f := harness.NewFakeAdapter()
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
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false, // resume: interrupted artifact already written

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

// ===== Incompatible workflows refused per FR-18a (file store) =====

// TestIntegration_IncompatibleWorkflow_RefusedFR18a verifies that each of the
// FR-18a incompatibility conditions causes the session to return RunRefused
// before any harness invocation, with the full stack including the real
// file-based artifact store.
//
// The compat unit tests cover all seven FR-18a conditions individually.
// This integration test verifies that the refusal correctly propagates through
// the full session → compat → RunRefused path for three representative
// conditions: parallel dispatch, staged non-EXECUTION phase, and agent-with-mode
// notation.
func TestIntegration_IncompatibleWorkflow_RefusedFR18a(t *testing.T) {
	cases := []struct {
		name            string
		workflowID      string
		content         string
		wantMsgContains string // substring expected in the refusal message
	}{
		{
			name:       "parallel-dispatch",
			workflowID: "parallel-dispatch",
			// Condition 5: comma in OnSuccess signals parallel routing.
			content: `<Workflow type="core" name="parallel-dispatch" version="1.0">
## Parallel Dispatch Workflow

| Phase | Subagent | HITL | On Success       | Input | Output |
|-------|----------|:----:|------------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | agent-a, agent-b | Stage-{StageNumber}/Plan.md | out.md |
</Workflow>
`,
			wantMsgContains: "parallel",
		},
		{
			name:       "dynamic-stage-set",
			workflowID: "dynamic-stages",
			// Condition 4: an EXECUTION row produces Stage-*/Plan.md (dynamic stage
			// set — implies stages can be added during execution, which is not
			// supported). The output uses the literal wildcard "Stage-*/Plan.md"
			// rather than the template form "Stage-{StageNumber}/Plan.md" to
			// exercise the exact pattern compat checks for.
			content: `<Workflow type="core" name="dynamic-stages" version="1.0">
## Dynamic Stage Set Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | COMPLETE | Stage-{StageNumber}/Plan.md | Stage-*/Plan.md |
</Workflow>
`,
			wantMsgContains: "dynamic",
		},
		{
			name:       "agent-with-mode-notation",
			workflowID: "mode-notation",
			// Condition 6: parentheses in agent identifier.
			content: `<Workflow type="core" name="mode-notation" version="1.0">
## Mode Notation Workflow

| Phase | Subagent       | HITL | On Success | Input | Output |
|-------|----------------|:----:|------------|-------|--------|
| PLANNING | agent-a(mode) | FALSE | COMPLETE | - | out.md |
</Workflow>
`,
			wantMsgContains: "parentheses",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			orchPath := writeOrchFile(t, dir, "orchestrator.md", tc.content)

			// No artifact file written: these runs are refused before the artifact
			// is created (compat check at step 4 runs before artifact creation at
			// step 8 in the run-start sequence).
			artifactPath := filepath.Join(dir, "Orchestration.md")
			f := harness.NewFakeAdapter()
			sess := newSession(f, artifactPath)

			cfg := domain.RunConfig{
				OrchestratorFilePath: orchPath,
				WorkflowID:           domain.WorkflowID(tc.workflowID),
				Task:                 "task",
				IsNewRun:             true,
				RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
			}

			got, err := sess.Start(context.Background(), cfg)
			msg := requireRefused(t, got, err)

			if tc.wantMsgContains != "" && !strings.Contains(msg, tc.wantMsgContains) {
				t.Errorf("want refusal message to contain %q, got %q",
					tc.wantMsgContains, msg)
			}

			// No harness invocations should have been made before the refusal.
			if len(f.Invocations()) > 0 {
				t.Error("want no harness invocations for incompatible workflow refusal, but got some")
			}
		})
	}
}

// ===== Non-canonical existing file refused per FR-7a (file store) =====

// TestIntegration_NonCanonicalArtifact_Refused_FR7a verifies that when a file
// exists at the artifact location but is not in the canonical Orchestration.md
// format (FR-7a), the real file-based artifact store returns a RefusalError and
// the session returns RunRefused without invoking any agents.
//
// This differs from the session unit test (which injects a mock store that
// returns RefusalError directly) by exercising the real FileStore.Read and
// artifact.Parse path against actual non-canonical file bytes.
func TestIntegration_NonCanonicalArtifact_Refused_FR7a(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Write a file at the artifact path that is NOT in the canonical format:
	// no YAML frontmatter, no <SectionName type="..."> tags. The real FileStore calls
	// artifact.Parse which returns *domain.RefusalError for this content.
	artifactPath := filepath.Join(dir, "Orchestration.md")
	const nonCanonicalContent = `# Orchestration

This file exists but is not in the canonical orchestration-artifact format.
It has no YAML frontmatter and no SECTION boundary tags.
`
	if err := os.WriteFile(artifactPath, []byte(nonCanonicalContent), 0600); err != nil {
		t.Fatalf("write non-canonical artifact: %v", err)
	}

	f := harness.NewFakeAdapter()
	sess := newSession(f, artifactPath)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false, // resume: non-canonical artifact already written
	}

	got, err := sess.Start(context.Background(), cfg)
	msg := requireRefused(t, got, err)

	// The refusal message must describe the non-canonical artifact condition.
	// The artifact.Parse function returns "artifact: refused ...: missing
	// frontmatter ..." which contains both "artifact" and "frontmatter".
	if !strings.Contains(msg, "artifact") && !strings.Contains(msg, "frontmatter") {
		t.Errorf("want refusal message to mention non-canonical artifact condition, got %q", msg)
	}

	// The harness must not have been invoked: refusal happens at the artifact
	// read step (step 3) before any agent is dispatched.
	if len(f.Invocations()) > 0 {
		t.Error("want no harness invocations when artifact is non-canonical, but got some")
	}
}

// ===== Stage-* wildcard resolution on non-EXECUTION rows =====

// TestIntegration_StageWildcardResolution_NonExecutionRow verifies that when a
// workflow uses Stage-* wildcard paths in a non-EXECUTION row's Input column
// (as in brownfield-tdd where plan-review receives Stage-*/Plan.md), the
// session resolves the wildcard to concrete Stage-N paths for the dispatched
// agent — confirming the stage-set re-derivation path end-to-end.
//
// This guards against a regression where the literal Stage-*/Plan.md template
// string is forwarded to the agent unchanged, which would render the agent
// unable to locate the actual plan files. The brownfield-tdd FiveWorkflows
// test exercises this code path but asserts only RunCompleted; this test
// inspects the concrete InputArtifacts slice to catch silent wildcard pass-through.
func TestIntegration_StageWildcardResolution_NonExecutionRow(t *testing.T) {
	dir := t.TempDir()

	// Minimal workflow mirroring the brownfield-tdd PLANNING phase pattern:
	// planner declares Stage-*/Plan.md as output; plan-review takes
	// Stage-*/Plan.md (and Plan.md) as input. The stage-set re-derivation
	// must expand Stage-* to the concrete stage numbers found in Plan.md.
	const orchContent = `<Workflow type="core" name="wildcard-resolve" version="1.0">
## Stage Wildcard Resolution Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | FALSE | plan-review | - | - | Plan.md, Stage-*/Plan.md |
| PLANNING | plan-review | FALSE | COMPLETE | planner | Plan.md, Stage-*/Plan.md | plan-review.md |
</Workflow>
`
	orchPath := writeOrchFile(t, dir, "orchestrator.md", orchContent)
	writeAgentFile(t, dir, "planner")
	writeAgentFile(t, dir, "plan-review")

	// Plan.md with two stages — the stage-set re-derivation reads this table to
	// expand Stage-*/Plan.md into Stage-1/Plan.md and Stage-2/Plan.md.
	const planContent = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First stage | - | FALSE |
| 2 | Stage Two | Second stage | 1 | FALSE |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(planContent), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	// Create Stage-N/Plan.md files to represent what the planner would have
	// written on disk, so the file-based store can resolve the Stage-* paths.
	for _, sub := range []string{"Stage-1", "Stage-2"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
		if err := os.WriteFile(filepath.Join(dir, sub, "Plan.md"), []byte("# Stage Plan\n"), 0600); err != nil {
			t.Fatalf("write %s/Plan.md: %v", sub, err)
		}
	}

	artifactPath := filepath.Join(dir, "Orchestration.md")
	f := harness.NewFakeAdapter()

	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan created with two stages",
	}})
	f.Queue("plan-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "plan-review#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan reviewed",
	}})

	sess := newSession(f, artifactPath)
	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "wildcard-resolve",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            dir, // Plan.md was written into dir; the session must resolve it here.
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) != 2 {
		t.Fatalf("want 2 harness invocations (planner + plan-review), got %d", len(invs))
	}

	// invs[1] is plan-review. Its InputArtifacts must contain the resolved
	// Stage-1/Plan.md and Stage-2/Plan.md, and must NOT contain the literal
	// Stage-*/Plan.md wildcard template.
	planReviewArts := invs[1].Request.InputArtifacts

	if !containsArtifact(planReviewArts, "Stage-1/Plan.md") {
		t.Errorf("plan-review InputArtifacts must contain Stage-1/Plan.md (resolved wildcard); got %v",
			planReviewArts)
	}
	if !containsArtifact(planReviewArts, "Stage-2/Plan.md") {
		t.Errorf("plan-review InputArtifacts must contain Stage-2/Plan.md (resolved wildcard); got %v",
			planReviewArts)
	}
	for _, art := range planReviewArts {
		if strings.Contains(art, "*") {
			t.Errorf("plan-review InputArtifacts must not contain literal wildcard %q; got %v",
				art, planReviewArts)
		}
	}
}

// ===== Seeding: files copied into run folder before first dispatch =====
//
// These integration tests exercise the session's seed-application step against
// the real file-based artifact store. Seeded files must appear at their correct
// relative paths inside the run folder, and they must be present before the
// first agent row is dispatched (Apply runs before the dispatch loop).
//
// Run-folder layout used in all seeding tests:
//   dir/                         ← orchestrator and agent definition files
//   dir/run/                     ← run folder (created by fileStore.Create)
//   dir/run/Orchestration.md     ← artifact (created by fileStore.Create)
//   dir/run/<seeded files>       ← files placed by seed.Apply
//
// RED phase: all tests fail until seed.Apply is inserted into the session's
// run-start sequence, after Store.Create and before the dispatch loop.

// seedDispatchCallbackHarness wraps a FakeAdapter and calls onInvoke immediately
// before each agent invocation. Used to inspect filesystem state at dispatch time.
type seedDispatchCallbackHarness struct {
	delegate *harness.FakeAdapter
	onInvoke func(agentID string)
}

func (h *seedDispatchCallbackHarness) Invoke(ctx context.Context, agent domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	if h.onInvoke != nil {
		h.onInvoke(agent.Identifier)
	}
	return h.delegate.Invoke(ctx, agent, request)
}

// TestIntegration_Seeding_SingleFile_PresentBeforeFirstDispatch verifies that a
// single-file seed source is present in the run folder — with byte-identical
// content to the source — when the first agent row is dispatched.
func TestIntegration_Seeding_SingleFile_PresentBeforeFirstDispatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	seedContent := "# Requirements\n\nThis is the seeded content.\n"
	srcFile := filepath.Join(dir, "Requirements.md")
	if err := os.WriteFile(srcFile, []byte(seedContent), 0600); err != nil {
		t.Fatalf("write seed source: %v", err)
	}

	runDir := filepath.Join(dir, "run")
	artifactPath := filepath.Join(runDir, "Orchestration.md")

	f := harness.NewFakeAdapter()

	// Capture the run-folder state at the moment the first agent is dispatched.
	var seededExistsAtDispatch bool
	var seededContentAtDispatch []byte
	cbH := &seedDispatchCallbackHarness{
		delegate: f,
		onInvoke: func(agentID string) {
			if agentID == "agent-a" {
				got, err := os.ReadFile(filepath.Join(runDir, "Requirements.md"))
				seededExistsAtDispatch = err == nil
				seededContentAtDispatch = got
			}
		},
	}

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:   cbH,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runDir,
		SeedInputs:           []string{srcFile},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// The seeded file must have been present when agent-a was dispatched.
	if !seededExistsAtDispatch {
		t.Error("seeded file must exist in the run folder before the first row is dispatched, but it was absent")
	}
	if string(seededContentAtDispatch) != seedContent {
		t.Errorf("seeded file content at first dispatch: got %q, want %q",
			string(seededContentAtDispatch), seedContent)
	}
}

// TestIntegration_Seeding_DirectorySource_FilesAtCorrectRelativePaths verifies
// that a directory seed source contributes one entry per regular file, with
// destination paths preserving the relative structure of the source directory.
func TestIntegration_Seeding_DirectorySource_FilesAtCorrectRelativePaths(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Source directory:
	//   srcDir/
	//     Top.md
	//     Requirements.md   (top-level Requirement* candidate, so seeding is not refused)
	//     Sub/
	//       Nested.md
	srcDir := filepath.Join(dir, "seed-src")
	if err := os.MkdirAll(srcDir, 0700); err != nil {
		t.Fatalf("mkdir srcDir: %v", err)
	}
	topContent := "# Top level\n"
	if err := os.WriteFile(filepath.Join(srcDir, "Top.md"), []byte(topContent), 0600); err != nil {
		t.Fatalf("write Top.md: %v", err)
	}
	// Requirement* candidate at the top level of the directory source, so the
	// combined candidate pool has exactly one match and seeding is not refused.
	if err := os.WriteFile(filepath.Join(srcDir, "Requirements.md"), []byte("# Requirements\n"), 0600); err != nil {
		t.Fatalf("write Requirements.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "Sub"), 0700); err != nil {
		t.Fatalf("mkdir Sub: %v", err)
	}
	nestedContent := "# Nested\n"
	if err := os.WriteFile(filepath.Join(srcDir, "Sub", "Nested.md"), []byte(nestedContent), 0600); err != nil {
		t.Fatalf("write Nested.md: %v", err)
	}

	runDir := filepath.Join(dir, "run")
	artifactPath := filepath.Join(runDir, "Orchestration.md")

	f := harness.NewFakeAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runDir,
		SeedInputs:           []string{srcDir},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Top.md must be at the root of the run folder.
	if gotTop, err := os.ReadFile(filepath.Join(runDir, "Top.md")); err != nil {
		t.Errorf("want seeded Top.md in run folder: %v", err)
	} else if string(gotTop) != topContent {
		t.Errorf("Top.md content: got %q, want %q", string(gotTop), topContent)
	}

	// Nested.md must be at Sub/Nested.md relative to the run folder.
	if gotNested, err := os.ReadFile(filepath.Join(runDir, "Sub", "Nested.md")); err != nil {
		t.Errorf("want seeded Sub/Nested.md in run folder: %v", err)
	} else if string(gotNested) != nestedContent {
		t.Errorf("Sub/Nested.md content: got %q, want %q", string(gotNested), nestedContent)
	}
}

// TestIntegration_Seeding_MultipleSources_AllFilesPresent verifies that when
// several sources are provided, every seeded file from every source is present
// in the run folder with byte-identical content to its source.
func TestIntegration_Seeding_MultipleSources_AllFilesPresent(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Source 1: individual file.
	content1 := "# Input 1\n"
	src1 := filepath.Join(dir, "Input1.md")
	if err := os.WriteFile(src1, []byte(content1), 0600); err != nil {
		t.Fatalf("write src1: %v", err)
	}

	// Source 2: individual file.
	content2 := "# Input 2\n"
	src2 := filepath.Join(dir, "Input2.md")
	if err := os.WriteFile(src2, []byte(content2), 0600); err != nil {
		t.Fatalf("write src2: %v", err)
	}

	// Source 3: directory containing one file.
	srcDir := filepath.Join(dir, "seed-dir")
	if err := os.MkdirAll(srcDir, 0700); err != nil {
		t.Fatalf("mkdir srcDir: %v", err)
	}
	content3 := "# Dir Input\n"
	if err := os.WriteFile(filepath.Join(srcDir, "DirInput.md"), []byte(content3), 0600); err != nil {
		t.Fatalf("write DirInput.md: %v", err)
	}

	// Source 4: individual file. This is the sole Requirement* candidate across
	// the combined source set, so seeding is not refused for lacking one.
	content4 := "# Requirements\n"
	src4 := filepath.Join(dir, "Requirements.md")
	if err := os.WriteFile(src4, []byte(content4), 0600); err != nil {
		t.Fatalf("write src4: %v", err)
	}

	runDir := filepath.Join(dir, "run")
	artifactPath := filepath.Join(runDir, "Orchestration.md")

	f := harness.NewFakeAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runDir,
		SeedInputs:           []string{src1, src2, srcDir, src4},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	for _, tc := range []struct{ name, dest, content string }{
		{"src1", "Input1.md", content1},
		{"src2", "Input2.md", content2},
		{"srcDir/DirInput.md", "DirInput.md", content3},
		{"src4", "Requirements.md", content4},
	} {
		gotContent, err := os.ReadFile(filepath.Join(runDir, tc.dest))
		if err != nil {
			t.Errorf("want seeded %s at run folder/%s: %v", tc.name, tc.dest, err)
			continue
		}
		if string(gotContent) != tc.content {
			t.Errorf("%s content: got %q, want %q", tc.dest, string(gotContent), tc.content)
		}
	}
}

// ===== Seeding: resume does not re-seed =====

// TestIntegration_Seeding_Resume_ExistingFilesPreservedByteIdentical verifies
// that resuming a run (IsNewRun false) does not re-copy seed inputs into the
// run folder. Any file already in the run folder retains its exact content
// regardless of what SeedInputs contains.
func TestIntegration_Seeding_Resume_ExistingFilesPreservedByteIdentical(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Pre-create the run folder with a valid Orchestration.md that encodes a
	// partially completed run: agent-a done (seq=1), agent-b pending.
	runDir := filepath.Join(dir, "run")
	if err := os.MkdirAll(runDir, 0700); err != nil {
		t.Fatalf("mkdir runDir: %v", err)
	}
	const seedingResumeArtifact = `---
type: orchestration-artifact
workflow: linear
workflow_version: "1.0"
task: "test task"
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
</Artifacts>

<WorkflowNotes type="core">
| Seq | Note |
| --- | ---- |
</WorkflowNotes>
`
	artifactPath := filepath.Join(runDir, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(seedingResumeArtifact), 0600); err != nil {
		t.Fatalf("write resume artifact: %v", err)
	}

	// A file already in the run folder simulating a seeded artifact from the
	// original run that may have been partially modified by the agents.
	preExistingContent := "# pre-existing: this content must survive the resume\n"
	if err := os.WriteFile(filepath.Join(runDir, "seeded.md"), []byte(preExistingContent), 0600); err != nil {
		t.Fatalf("write pre-existing seeded file: %v", err)
	}

	// Seed source: a file named "seeded.md" with different content.
	// If seeding ran on resume, runDir/seeded.md would be overwritten.
	srcFile := filepath.Join(dir, "seeded.md")
	newSeedContent := "# new seed: must NOT overwrite on resume\n"
	if err := os.WriteFile(srcFile, []byte(newSeedContent), 0600); err != nil {
		t.Fatalf("write seed source: %v", err)
	}

	f := harness.NewFakeAdapter()
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false,

		RunFolder:            runDir,
		SeedInputs:           []string{srcFile}, // ignored on resume
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// seeded.md must retain its pre-resume content — the session must not have
	// re-copied it from the seed source.
	gotContent, readErr := os.ReadFile(filepath.Join(runDir, "seeded.md"))
	if readErr != nil {
		t.Fatalf("read seeded.md after resume: %v", readErr)
	}
	if string(gotContent) != preExistingContent {
		t.Errorf("seeded.md after resume: got %q, want pre-existing %q (seeding must be skipped on resume)",
			string(gotContent), preExistingContent)
	}
}

// ===== Seeding: mid-copy failure removes the entire run folder =====

// TestIntegration_Seeding_MidCopyFailure_RunFolderCompletelyRemoved verifies
// that when seed.Apply fails partway through — after at least one file has
// been written — the session removes the entire run folder, including
// Orchestration.md and the already-copied files, and returns a RunRefused
// naming the failing file. No trace of the failed attempt remains on disk.
//
// The copy failure is induced by pre-populating the run folder with a regular
// file at a path that Apply's os.MkdirAll needs to turn into a directory.
// This technique is cross-platform: os.MkdirAll returns an error whenever a
// non-directory occupies any component of the target path.
func TestIntegration_Seeding_MidCopyFailure_RunFolderCompletelyRemoved(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Source directory:
	//   sources/
	//     first.md          → dest "first.md"          (copies successfully)
	//     Requirements.md   → dest "Requirements.md"    (sole candidate, no-op rename; copies successfully)
	//     sub/
	//       second.md       → dest "sub/second.md"      (fails — see sabotage below)
	srcDir := filepath.Join(dir, "sources")
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0700); err != nil {
		t.Fatalf("mkdir sources/sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "first.md"), []byte("first\n"), 0600); err != nil {
		t.Fatalf("write first.md: %v", err)
	}
	// Top-level Requirement* candidate, so the seed set has exactly one match
	// and seeding is not refused before Apply is reached.
	if err := os.WriteFile(filepath.Join(srcDir, "Requirements.md"), []byte("# Requirements\n"), 0600); err != nil {
		t.Fatalf("write Requirements.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sub", "second.md"), []byte("second\n"), 0600); err != nil {
		t.Fatalf("write sub/second.md: %v", err)
	}

	runDir := filepath.Join(dir, "run")

	// Pre-create the run folder so we can plant the sabotage file inside it.
	// fileStore.Create will call os.MkdirAll(runDir), which succeeds even if the
	// directory already exists, then write Orchestration.md.
	if err := os.MkdirAll(runDir, 0700); err != nil {
		t.Fatalf("pre-create runDir: %v", err)
	}
	// Sabotage: put a regular file at runDir/sub so that Apply's
	// os.MkdirAll(runDir/sub) fails. The walk visits first.md before sub/second.md
	// (lexical order), so first.md is successfully copied before the failure.
	if err := os.WriteFile(filepath.Join(runDir, "sub"), []byte("not a directory"), 0600); err != nil {
		t.Fatalf("write sabotage file: %v", err)
	}

	artifactPath := filepath.Join(runDir, "Orchestration.md")
	f := harness.NewFakeAdapter()
	// No responses queued: Apply fails before the dispatch loop begins.

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runDir,
		SeedInputs:           []string{srcDir},
	}

	got, err := sess.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)

	// The entire run folder must be removed — including Orchestration.md and the
	// successfully-copied first.md. A mid-copy failure leaves no trace on disk.
	if _, statErr := os.Stat(runDir); !os.IsNotExist(statErr) {
		t.Errorf("want run folder removed after mid-copy failure, but it still exists at %s", runDir)
	}

	// The refusal message must reference the seed component and name the
	// specific entry that failed to copy. The failing entry is sub/second.md
	// (Apply visits the top-level files before descending into sub/; both
	// Requirements.md and first.md copy successfully, then
	// os.MkdirAll("runDir/sub") fails because the sabotage file occupies that
	// path). Both the component tag and the failing source path must appear
	// so the test distinguishes "named the right file" from "named some seed
	// problem."
	if !strings.Contains(msg, "seed") {
		t.Errorf("want refusal message to reference the seed component; got %q", msg)
	}
	if !strings.Contains(msg, "second.md") {
		t.Errorf("want refusal message to name the failing entry (second.md); got %q", msg)
	}

	// No agents must have been dispatched: the failure occurred before the dispatch loop.
	if len(f.Invocations()) != 0 {
		t.Errorf("want no harness invocations on seed copy failure, got %d", len(f.Invocations()))
	}
}

// ===== Seeding: invalid seed set leaves no run folder behind =====

// TestIntegration_Seeding_InvalidSeedSet_NoRunFolderLeftBehind verifies that
// when the seed set fails BuildPlan's validation (here: a source path that
// does not exist), the run is refused and no run folder is created on disk
// at all -- seed planning happens, and is refused, before Store.Create ever
// runs. This is the ordering guarantee the Stage 5 reorder must preserve:
// only seed.Apply moves to after Store.Create, never seed.BuildPlan.
func TestIntegration_Seeding_InvalidSeedSet_NoRunFolderLeftBehind(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	nonExistent := filepath.Join(dir, "does-not-exist.md")

	runDir := filepath.Join(dir, "run") // deliberately never created by this test
	artifactPath := filepath.Join(runDir, "Orchestration.md")

	f := harness.NewFakeAdapter()
	// No responses queued: BuildPlan must refuse before any dispatch.

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runDir,
		SeedInputs:           []string{nonExistent},
	}

	got, err := sess.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)
	if !strings.Contains(msg, "does-not-exist.md") {
		t.Errorf("want refusal message to name the missing source path; got %q", msg)
	}

	// No run folder must exist at all -- not even Orchestration.md.
	if _, statErr := os.Stat(runDir); !os.IsNotExist(statErr) {
		t.Errorf("want no run folder created for an invalid seed set, but one exists at %s", runDir)
	}

	if len(f.Invocations()) != 0 {
		t.Errorf("want no harness invocations for an invalid seed set, got %d", len(f.Invocations()))
	}
}

// ===== Seeding: a seeded stage table dispatches on the first attempt =====

// TestIntegration_Seeding_StagedWorkflow_SeededStageTable_DispatchesFirstAttempt
// verifies the Stage 5 defect's absence directly: a staged workflow whose
// stage table arrives via SeedInputs (never pre-populated on disk by the
// test itself) dispatches successfully on a single Start call -- no stop, no
// start-then-resume sequence, and the stage-driven dispatches actually
// occur. This is the coverage gap the defect exploited: nothing previously
// seeded a stage table into a brand-new staged run and then dispatched
// against it in one call.
//
// Before the fix, the session reads the run folder's Plan.md (Step 6) before
// the run folder is created and the seed plan is applied (Step 8), so the
// stage set stays nil and the engine stops with no stage set available,
// before any harness invocation.
func TestIntegration_Seeding_StagedWorkflow_SeededStageTable_DispatchesFirstAttempt(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "staged-orch.md"))
	writeAgentFile(t, dir, "implementation-tdd")
	writeAgentFile(t, dir, "implementation-review")

	// Seed sources: a Plan.md with a one-stage stage table, plus the mandatory
	// Requirement* candidate so BuildPlan's naming rule is satisfied.
	seedDir := filepath.Join(dir, "seed")
	if err := os.MkdirAll(seedDir, 0700); err != nil {
		t.Fatalf("mkdir seedDir: %v", err)
	}
	planContent := `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`
	planSrc := filepath.Join(seedDir, "Plan.md")
	if err := os.WriteFile(planSrc, []byte(planContent), 0600); err != nil {
		t.Fatalf("write seed Plan.md: %v", err)
	}
	reqSrc := filepath.Join(seedDir, "Requirements.md")
	if err := os.WriteFile(reqSrc, []byte("# Requirements\n"), 0600); err != nil {
		t.Fatalf("write seed Requirements.md: %v", err)
	}

	// runDir does not exist before Start is called: it is created by
	// Store.Create as part of this one run-start sequence.
	runDir := filepath.Join(dir, "run")
	artifactPath := filepath.Join(runDir, "Orchestration.md")

	f := harness.NewFakeAdapter()
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "implemented",
	}})
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "reviewed",
	}})

	store := artifact.NewFileStore(artifactPath)
	sess := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runDir,
		SeedInputs:           []string{planSrc, reqSrc},
	}

	got, err := sess.Start(context.Background(), cfg)

	// The single Start call must complete the run -- no stop, no resume needed.
	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least 1 harness invocation on the first Start call, got 0 (stage-driven dispatch never occurred)")
	}
	if invs[0].Agent.Identifier != "implementation-tdd" {
		t.Errorf("want first dispatch to be implementation-tdd (stage 1), got %q", invs[0].Agent.Identifier)
	}

	// The seeded stage table itself must be present in the run folder,
	// byte-identical to its source.
	gotPlan, readErr := os.ReadFile(filepath.Join(runDir, "Plan.md"))
	if readErr != nil {
		t.Fatalf("read seeded Plan.md from run folder: %v", readErr)
	}
	if string(gotPlan) != planContent {
		t.Errorf("seeded Plan.md content: got %q, want %q", string(gotPlan), planContent)
	}
}

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

	f := harness.NewFakeAdapter()
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

	// On disk: agent-a completed, then checkpoint-manager-git fired cleanly
	// afterward. Per the fix, current_state still names agent-a (the last
	// WORKFLOW step) even though checkpoint-manager-git is the execution log's
	// last entry -- this is a clean stop, not an interruption.
	const artifactContent = `---
type: orchestration-artifact
workflow: linear
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 2
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
	artifactPath := filepath.Join(dir, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(artifactContent), 0600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	f := harness.NewFakeAdapter()
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
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false,
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

	// On disk: agent-a completed and is recorded in current_state.
	// checkpoint-manager-git then fired (does not move current_state). agent-b
	// was then dispatched and completed, but the runner was interrupted before
	// current_state could be updated to reflect it.
	const artifactContent = `---
type: orchestration-artifact
workflow: linear
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 3
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
	artifactPath := filepath.Join(dir, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(artifactContent), 0600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	f := harness.NewFakeAdapter()
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
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false,
		RunSettings:          domain.RunSettings{Checkpoints: true}, // enable checkpoint class so its triggers are evaluated
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) == 0 || invs[0].Agent.Identifier != "agent-b" {
		t.Fatalf("want first invocation to re-run agent-b (interrupted row), got %+v", invs)
	}
}

// ---- small utilities ----

// containsArtifact reports whether the slice contains the given artifact path.
func containsArtifact(arts []string, target string) bool {
	for _, a := range arts {
		if a == target {
			return true
		}
	}
	return false
}

// ===== Stage 9: End-to-End Mode Coverage =====
//
// These tests verify assembled-run behaviours across execution modes, HITL
// verification, consultant-driven routing, and run lifecycle management.
// They exercise the full stack with the real file-based ArtifactStore so that
// the orchestration artifact on disk can be inspected and resumed.
//
// Coverage:
//
//   Orchestrated mode (T9.1):
//   - A run in orchestrated mode routes every step through the routing
//     consultant; the engine never produces an auto-route.
//
//   Auto and auto-review all-SUCCESS / deviation (T9.2):
//   - An all-SUCCESS run in auto and auto-review needs zero consultations.
//   - A non-SUCCESS response triggers exactly one consultation per deviation.
//
//   COMPLETED_NEEDS_ACTION auto-route-back (T9.3):
//   - In auto-review, a CNA with an unambiguous OnFindings target auto-routes
//     back without invoking the routing consultant, and the review agent's
//     output artifacts are injected into the target's inputs.
//   - In auto mode the same CNA triggers a routing consultation instead.
//
//   HITL verification (T9.4):
//   - A HITL-dispatched step whose output artifact records human_approved:false
//     triggers exactly one same-agent redispatch; a second non-compliant result
//     becomes a deviation, and the run never advances past the unverified step.
//
//   Stop and orchestrator failure (T9.5):
//   - A consultant stop leaves a resumable artifact on disk.
//   - A consultant failure (transport error) leaves a resumable artifact.
//
//   Commit setup (T9.6):
//   - A commits-enabled run records the branch name in the artifact frontmatter
//     and adds the commit setup dispatch as the first execution log row.
//   - A failed commit setup refuses the run and leaves no artifact.
//
//   Resume (T9.7):
//   - A resumed run reads its mode and all other settings from the artifact
//     frontmatter; the caller-supplied RunConfig values are overwritten.
//   - An interrupted step is re-dispatched; nothing is re-asked.

// ---- Stage 9 test doubles ----

// intScriptedRoutingConsultant is a scripted routing consultant for integration
// tests. It returns RoutingInstructions from its queue in FIFO order and
// records every ConsultRouting call so tests can assert call counts.
type intScriptedRoutingConsultant struct {
	instructions []domain.RoutingInstruction
	errors       []error
	idx          int
	CallCount    int
	Requests     []domain.ConsultationRequest
}

func (s *intScriptedRoutingConsultant) ConsultRouting(_ context.Context, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	s.CallCount++
	s.Requests = append(s.Requests, req)
	if s.idx >= len(s.instructions) {
		return domain.RoutingInstruction{}, &domain.ConsultationError{
			Failure: domain.ConsultFailTransport,
			Detail:  fmt.Sprintf("intScriptedRoutingConsultant: queue exhausted (call #%d)", s.CallCount),
		}
	}
	instr := s.instructions[s.idx]
	var err error
	if s.idx < len(s.errors) {
		err = s.errors[s.idx]
	}
	s.idx++
	return instr, err
}

func (s *intScriptedRoutingConsultant) queueDispatch(agent, taskDesc string, rowIndex int) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:           agent,
			RowIndex:        rowIndex,
			TaskDescription: taskDesc,
		},
	})
	s.errors = append(s.errors, nil)
}

func (s *intScriptedRoutingConsultant) queueStop(reason string) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Stop: &domain.StopInstruction{Reason: reason},
	})
	s.errors = append(s.errors, nil)
}

func (s *intScriptedRoutingConsultant) queueError(failure domain.ConsultationFailure) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{})
	s.errors = append(s.errors, &domain.ConsultationError{
		Failure: failure,
		Detail:  "scripted consultation error",
	})
}

// intFixedApprovalReader implements domain.ApprovalReader, always returning the
// same HumanApproval value for every artifact path.
type intFixedApprovalReader struct {
	approval domain.HumanApproval
}

func (r intFixedApprovalReader) ReadApproval(_ context.Context, _ string) domain.HumanApproval {
	return r.approval
}

// ---- Stage 9 session constructors ----

// newSessionWithRouting builds a session with the real file-based artifact
// store and a routing consultant wired into session.Deps.
func newSessionWithRouting(
	f *harness.FakeAdapter,
	artifactPath string,
	routing domain.RoutingConsultant,
) session.Session {
	store := artifact.NewFileStore(artifactPath)
	return session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		Approvals: alwaysApprovedReader{},
		Routing:   routing,
	})
}

// newSessionWithApprovals builds a session with the real file-based artifact
// store and a custom ApprovalReader (no routing consultant wired).
func newSessionWithApprovals(
	f *harness.FakeAdapter,
	artifactPath string,
	approvals domain.ApprovalReader,
) session.Session {
	store := artifact.NewFileStore(artifactPath)
	return session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		Approvals: approvals,
	})
}

// ===== T9.1: Orchestrated mode — every routing decision from consultant =====

// TestIntegration_OrchestratedMode_AllRoutingFromConsultant verifies that a
// run in orchestrated mode routes every step through the routing consultant
// and never auto-routes via the engine.
//
// In orchestrated mode the engine never produces a routing decision: the
// consultant is invoked before every dispatch (including the very first step)
// and again after every completion to get the next instruction. A two-agent
// linear workflow therefore results in three consultant calls: initial,
// after agent-a, and after agent-b (which returns the final stop). Every call
// proves routing was consultant-driven.
func TestIntegration_OrchestratedMode_AllRoutingFromConsultant(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planning done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	consultant := &intScriptedRoutingConsultant{}
	// linear-orch.md: row 0 = agent-a, row 1 = agent-b.
	// The consultant directs all three routing decisions:
	//   call 1 (initial): dispatch agent-a
	//   call 2 (after agent-a): dispatch agent-b
	//   call 3 (after agent-b): stop — the orchestrator signals all work is done
	consultant.queueDispatch("agent-a", "Proceed.", 0)
	consultant.queueDispatch("agent-b", "Proceed.", 1)
	consultant.queueStop("all workflow steps completed")

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSessionWithRouting(f, artifactPath, consultant)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "orchestrated task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}

	got, err := sess.Start(context.Background(), cfg)

	// In orchestrated mode the consultant signals termination via a stop
	// instruction; the terminal status is RunStoppedByConsultant.
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	// Every routing decision — including the initial one and the final stop —
	// must have come from the consultant: three calls for two workflow steps.
	if consultant.CallCount != 3 {
		t.Errorf("want 3 consultant calls (initial + after agent-a + after agent-b), got %d",
			consultant.CallCount)
	}

	// AC9.2: The orchestrated-mode run records a consultation for every routing
	// decision. Read back the produced artifact and confirm the execution log
	// contains entries beyond the two workflow agents, proving the consultant
	// stop (and any consultation rows the session writes per decision) are
	// durably recorded — not just counted in memory.
	artifactData, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("read produced artifact: %v", readErr)
	}
	state, parseErr := artifact.Parse(artifactData)
	if parseErr != nil {
		t.Fatalf("parse produced artifact: %v", parseErr)
	}

	// The execution log must contain at least the two workflow agents.
	if len(state.ExecutionLog) < 2 {
		t.Fatalf("want at least 2 execution log entries (workflow agents), got %d", len(state.ExecutionLog))
	}

	// Verify both workflow agent entries appear in the log.
	agentSeen := map[string]bool{}
	for _, entry := range state.ExecutionLog {
		if strings.Contains(entry.Agent, "agent-a") || strings.Contains(entry.Agent, "agent-b") {
			agentSeen[entry.Agent] = true
		}
	}
	if !agentSeen["agent-a#1"] {
		t.Errorf("want agent-a#1 in execution log; log = %v", state.ExecutionLog)
	}
	if !agentSeen["agent-b#2"] {
		t.Errorf("want agent-b#2 in execution log; log = %v", state.ExecutionLog)
	}

	// There must be at least one execution log entry that is not one of the two
	// workflow agents. This entry is the consultation row the session records for
	// the consultant's stop instruction, fulfilling AC9.2's "records a consultation
	// for every routing decision" requirement at the artifact level.
	nonWorkflowEntries := 0
	for _, entry := range state.ExecutionLog {
		if !strings.Contains(entry.Agent, "agent-a") && !strings.Contains(entry.Agent, "agent-b") {
			nonWorkflowEntries++
		}
	}
	if nonWorkflowEntries == 0 {
		t.Errorf("want at least one consultation row in execution log beyond the two workflow agents, got none (total entries: %d)", len(state.ExecutionLog))
	}

	// Both workflow agents must have been dispatched, in order.
	invs := f.Invocations()
	if len(invs) != 2 {
		t.Fatalf("want 2 harness invocations (agent-a + agent-b), got %d", len(invs))
	}
	if invs[0].Agent.Identifier != "agent-a" {
		t.Errorf("want first dispatch to agent-a, got %q", invs[0].Agent.Identifier)
	}
	if invs[1].Agent.Identifier != "agent-b" {
		t.Errorf("want second dispatch to agent-b, got %q", invs[1].Agent.Identifier)
	}
}

// ===== T9.2: Auto and auto-review — zero consultations on SUCCESS, consult on deviation =====

// TestIntegration_AutoMode_AllSuccess_ZeroConsultations verifies that in auto
// and auto-review modes an all-SUCCESS run never invokes the routing consultant.
// The absence of a wired consultant means any consultation would end the run
// with RunDeviationUnresolved; RunCompleted proves none occurred.
func TestIntegration_AutoMode_AllSuccess_ZeroConsultations(t *testing.T) {
	for _, mode := range []domain.ExecutionMode{
		domain.ExecutionModeAuto,
		domain.ExecutionModeAutoReview,
	} {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			dir := t.TempDir()
			orchPath := copyFile(t, dir, "orchestrator.md",
				filepath.Join(sessionTestdataDir, "linear-orch.md"))
			writeAgentFile(t, dir, "agent-a")
			writeAgentFile(t, dir, "agent-b")

			f := harness.NewFakeAdapter()
			f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
				AgentInstanceID: "agent-a#1",
				StatusCode:      domain.StatusSUCCESS,
				StatusMessage:   "done",
			}})
			f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
				AgentInstanceID: "agent-b#2",
				StatusCode:      domain.StatusSUCCESS,
				StatusMessage:   "done",
			}})

			// No routing consultant: any deviation would produce
			// RunDeviationUnresolved. RunCompleted proves zero consultations.
			artifactPath := filepath.Join(dir, "Orchestration.md")
			sess := newSession(f, artifactPath)
			cfg := domain.RunConfig{
				OrchestratorFilePath: orchPath,
				WorkflowID:           "linear",
				Task:                 "all-success task",
				IsNewRun:             true,
				RunSettings:          domain.RunSettings{Mode: mode},
			}

			got, err := sess.Start(context.Background(), cfg)
			requireRunStatus(t, got, err, domain.RunCompleted)
		})
	}
}

// TestIntegration_AutoMode_Deviation_ConsultCalledOnce verifies that in auto
// and auto-review modes a non-SUCCESS response triggers exactly one routing
// consultation. The scripted consultant returns stop; the run ends with a
// resumable artifact.
func TestIntegration_AutoMode_Deviation_ConsultCalledOnce(t *testing.T) {
	for _, mode := range []domain.ExecutionMode{
		domain.ExecutionModeAuto,
		domain.ExecutionModeAutoReview,
	} {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			dir := t.TempDir()
			orchPath := copyFile(t, dir, "orchestrator.md",
				filepath.Join(sessionTestdataDir, "linear-orch.md"))
			writeAgentFile(t, dir, "agent-a")
			writeAgentFile(t, dir, "agent-b")

			f := harness.NewFakeAdapter()
			// agent-a returns a non-SUCCESS, triggering a deviation.
			f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
				AgentInstanceID: "agent-a#1",
				StatusCode:      domain.StatusPARTIALLY_DONE,
				StatusMessage:   "only partially done",
			}})

			consultant := &intScriptedRoutingConsultant{}
			consultant.queueStop("operator requested stop after deviation")

			artifactPath := filepath.Join(dir, "Orchestration.md")
			sess := newSessionWithRouting(f, artifactPath, consultant)
			cfg := domain.RunConfig{
				OrchestratorFilePath: orchPath,
				WorkflowID:           "linear",
				Task:                 "deviation task",
				IsNewRun:             true,
				RunSettings:          domain.RunSettings{Mode: mode},
			}

			got, err := sess.Start(context.Background(), cfg)
			requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

			// The deviation must trigger exactly one consultation.
			if consultant.CallCount != 1 {
				t.Errorf("want exactly 1 consultation for the deviation, got %d",
					consultant.CallCount)
			}
		})
	}
}

// ===== T9.3: COMPLETED_NEEDS_ACTION — auto-review auto-routes, auto consults =====

// TestIntegration_AutoReview_CNA_AutoRouteBack_NoConsultAndArtifactInjected
// verifies that in auto-review mode a COMPLETED_NEEDS_ACTION response from a
// review agent with an unambiguous OnFindings target causes the engine to route
// back to that agent automatically, without invoking the routing consultant,
// and that the review agent's output artifacts are injected into the target's
// InputArtifacts.
func TestIntegration_AutoReview_CNA_AutoRouteBack_NoConsultAndArtifactInjected(t *testing.T) {
	dir := t.TempDir()

	// Workflow: test-writer-tdd → build-review (OnFindings=test-writer-tdd) → impl-tdd.
	// build-review's output is "build.md" — the artifact injected on loop-back.
	const orchContent = `<Workflow type="core" name="cna-review" version="1.0">
## CNA Review Workflow

| Phase | Subagent          | HITL | On Success  | On Findings      | Input    | Output   |
|-------|-------------------|:----:|-------------|------------------|----------|----------|
| PLANNING | test-writer-tdd | FALSE | build-review | -               | -        | tests.md |
| PLANNING | build-review    | FALSE | impl-tdd    | test-writer-tdd  | tests.md | build.md |
| PLANNING | impl-tdd        | FALSE | COMPLETE    | -                | tests.md | impl.md  |
</Workflow>
`
	orchPath := writeOrchFile(t, dir, "orchestrator.md", orchContent)
	writeAgentFile(t, dir, "test-writer-tdd")
	writeAgentFile(t, dir, "build-review")
	writeAgentFile(t, dir, "impl-tdd")

	f := harness.NewFakeAdapter()
	// test-writer-tdd initial dispatch → SUCCESS.
	f.Queue("test-writer-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "test-writer-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "tests written",
	}})
	// build-review → COMPLETED_NEEDS_ACTION with unambiguous OnFindings.
	// In auto-review the engine auto-routes back to test-writer-tdd.
	f.Queue("build-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "build-review#2",
		StatusCode:      domain.StatusCOMPLETED_NEEDS_ACTION,
		StatusMessage:   "build failed",
	}})
	// test-writer-tdd loop-back → SUCCESS.
	f.Queue("test-writer-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "test-writer-tdd#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "tests fixed",
	}})
	// build-review second attempt → SUCCESS.
	f.Queue("build-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "build-review#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "build ok",
	}})
	// impl-tdd → SUCCESS → COMPLETE.
	f.Queue("impl-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "impl-tdd#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "implemented",
	}})

	// A routing consultant is wired but must NOT be called: the OnFindings
	// auto-route-back is engine-owned in auto-review mode.
	consultant := &intScriptedRoutingConsultant{}

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSessionWithRouting(f, artifactPath, consultant)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "cna-review",
		Task:                 "cna auto-review task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAutoReview},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// The routing consultant must not have been invoked: the CNA auto-route-back
	// is the engine's decision in auto-review mode.
	if consultant.CallCount != 0 {
		t.Errorf("want 0 consultant calls for auto-review OnFindings auto-route, got %d",
			consultant.CallCount)
	}

	invs := f.Invocations()
	wantOrder := []string{
		"test-writer-tdd", // initial dispatch
		"build-review",    // first attempt → CNA
		"test-writer-tdd", // loop-back via OnFindings
		"build-review",    // second attempt → SUCCESS
		"impl-tdd",        // continues normally
	}
	if len(invs) != len(wantOrder) {
		t.Fatalf("want %d harness invocations, got %d", len(wantOrder), len(invs))
	}
	for i, want := range wantOrder {
		if invs[i].Agent.Identifier != want {
			t.Errorf("invocation[%d]: want %q, got %q", i, want, invs[i].Agent.Identifier)
		}
	}

	// The test-writer-tdd loop-back (invs[2]) must have build-review's output
	// artifact ("build.md") injected into its InputArtifacts.
	if !containsArtifact(invs[2].Request.InputArtifacts, "build.md") {
		t.Errorf("test-writer-tdd loop-back must have build.md injected (review artifact injection); got InputArtifacts=%v",
			invs[2].Request.InputArtifacts)
	}
}

// TestIntegration_Auto_CNA_ConsultsForDeviation verifies that in auto mode a
// COMPLETED_NEEDS_ACTION response triggers a routing consultation, not an
// engine auto-route-back. The OnFindings auto-route is exclusive to auto-review.
func TestIntegration_Auto_CNA_ConsultsForDeviation(t *testing.T) {
	dir := t.TempDir()

	const orchContent = `<Workflow type="core" name="cna-auto" version="1.0">
## CNA Auto Workflow

| Phase | Subagent          | HITL | On Success  | On Findings      | Input    | Output   |
|-------|-------------------|:----:|-------------|------------------|----------|----------|
| PLANNING | test-writer-tdd | FALSE | build-review | -               | -        | tests.md |
| PLANNING | build-review    | FALSE | impl-tdd    | test-writer-tdd  | tests.md | build.md |
| PLANNING | impl-tdd        | FALSE | COMPLETE    | -                | tests.md | impl.md  |
</Workflow>
`
	orchPath := writeOrchFile(t, dir, "orchestrator.md", orchContent)
	writeAgentFile(t, dir, "test-writer-tdd")
	writeAgentFile(t, dir, "build-review")
	writeAgentFile(t, dir, "impl-tdd")

	f := harness.NewFakeAdapter()
	f.Queue("test-writer-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "test-writer-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "tests written",
	}})
	// build-review returns CNA — in auto mode this is a deviation requiring consultation.
	f.Queue("build-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "build-review#2",
		StatusCode:      domain.StatusCOMPLETED_NEEDS_ACTION,
		StatusMessage:   "build failed",
	}})

	// Consultant is called once for the CNA deviation and returns stop.
	consultant := &intScriptedRoutingConsultant{}
	consultant.queueStop("build-review returned CNA in auto mode — stopping")

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSessionWithRouting(f, artifactPath, consultant)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "cna-auto",
		Task:                 "cna auto task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	// In auto mode CNA is a deviation and must route through the consultant.
	if consultant.CallCount != 1 {
		t.Errorf("want 1 consultant call when CNA deviates in auto mode, got %d",
			consultant.CallCount)
	}
}

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

	f := harness.NewFakeAdapter()
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

	f := harness.NewFakeAdapter()
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

	f := harness.NewFakeAdapter()
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

// ===== T9.6: Commit setup — branch recorded, failed setup leaves no artifact =====

// TestIntegration_CommitSetup_BranchRecordedInArtifact verifies that when a
// commits-enabled run starts and the commit-class agent reports a
// [branch:{name}] marker, the branch name is stored in the artifact frontmatter
// and the commit setup dispatch appears as the first execution log row.
func TestIntegration_CommitSetup_BranchRecordedInArtifact(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "commit-agent-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "commit-manager-git")

	const wantBranch = "mosaic/run/test-branch-001"

	f := harness.NewFakeAdapter()
	// commit setup dispatch — must carry the [branch:{name}] marker.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit setup complete [branch:" + wantBranch + "]",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSession(f, artifactPath)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "commit task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAuto,
			Commits:             true,
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Read and parse the produced artifact to verify frontmatter and log.
	data, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("read artifact: %v", readErr)
	}
	state, parseErr := artifact.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse artifact: %v", parseErr)
	}

	if state.CommitBranch != wantBranch {
		t.Errorf("want CommitBranch=%q in artifact frontmatter, got %q",
			wantBranch, state.CommitBranch)
	}

	// The commit setup dispatch must be the first execution log row (Seq=1) and
	// its agent identifier must name the commit-class agent.
	if len(state.ExecutionLog) == 0 {
		t.Fatal("want at least one execution log entry (commit setup row), got none")
	}
	first := state.ExecutionLog[0]
	if first.Seq != 1 {
		t.Errorf("want commit setup row Seq=1, got Seq=%d", first.Seq)
	}
	if !strings.Contains(first.Agent, "commit-manager-git") {
		t.Errorf("want commit setup row agent to contain commit-manager-git, got %q",
			first.Agent)
	}
}

// TestIntegration_CommitSetup_FailedSetup_NoArtifactCreated verifies that when
// the commit setup dispatch fails (harness-level error), the run is refused and
// no orchestration artifact is left behind. A failed setup is terminal and
// leaves no trace.
func TestIntegration_CommitSetup_FailedSetup_NoArtifactCreated(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "commit-agent-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "commit-manager-git")

	f := harness.NewFakeAdapter()
	// commit-manager-git fails at the harness level (e.g. agent process crashed).
	f.Queue("commit-manager-git", harness.ScriptedEntry{
		Err: errors.New("harness: commit setup agent crashed"),
	})

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSession(f, artifactPath)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "commit-fail task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAuto,
			Commits:             true,
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRefused(t, got, err)

	// No artifact must be created: a failed commit setup leaves no trace.
	if _, statErr := os.Stat(artifactPath); statErr == nil {
		t.Errorf("want no artifact file after failed commit setup, but file exists at %s",
			artifactPath)
	}

	// Only commit-manager-git may have been invoked; no workflow agents.
	invs := f.Invocations()
	for _, inv := range invs {
		if inv.Agent.Identifier != "commit-manager-git" {
			t.Errorf("want no workflow agent dispatched after failed commit setup, got %q",
				inv.Agent.Identifier)
		}
	}
}

// TestIntegration_CommitSetup_MarkerMissing_Refused verifies that when the
// commit-class agent returns StatusSUCCESS but its status_message contains no
// [branch:{name}] marker, the run is refused and no orchestration artifact is
// left behind. This exercises the marker-parsing failure path (path (b)),
// distinct from the harness-level crash covered by
// TestIntegration_CommitSetup_FailedSetup_NoArtifactCreated.
func TestIntegration_CommitSetup_MarkerMissing_Refused(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "commit-agent-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "commit-manager-git")

	f := harness.NewFakeAdapter()
	// Commit agent returns SUCCESS but omits the [branch:{name}] marker entirely.
	// An absent marker must refuse the run.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit setup complete — no branch marker here",
	}})

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSession(f, artifactPath)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "marker-missing task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAuto,
			Commits:             true,
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRefused(t, got, err)

	// No artifact must be created: an absent branch marker refuses the run terminally.
	if _, statErr := os.Stat(artifactPath); statErr == nil {
		t.Errorf("want no artifact file when [branch:{name}] marker is absent, but file exists at %s",
			artifactPath)
	}

	// No workflow agents must have been dispatched; only the commit setup agent.
	invs := f.Invocations()
	for _, inv := range invs {
		if inv.Agent.Identifier != "commit-manager-git" {
			t.Errorf("want no workflow agent dispatched after marker-missing refusal, got %q",
				inv.Agent.Identifier)
		}
	}
}

// TestIntegration_CommitSetup_EmptyBranchName_Refused verifies that when the
// commit-class agent returns StatusSUCCESS with a [branch:] marker whose branch
// name is empty, the run is refused and no orchestration artifact is left behind.
// An empty branch name is equivalent to an absent marker per the design contract:
// "An absent marker or an empty name refuses the run."
func TestIntegration_CommitSetup_EmptyBranchName_Refused(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "commit-agent-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "commit-manager-git")

	f := harness.NewFakeAdapter()
	// Commit agent returns SUCCESS with a [branch:] marker but no branch name.
	// An empty name must refuse the run.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit setup complete [branch:]",
	}})

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSession(f, artifactPath)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "empty-branch task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAuto,
			Commits:             true,
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRefused(t, got, err)

	// No artifact must be created: an empty branch name refuses the run terminally.
	if _, statErr := os.Stat(artifactPath); statErr == nil {
		t.Errorf("want no artifact file when [branch:] has empty name, but file exists at %s",
			artifactPath)
	}

	// No workflow agents must have been dispatched; only the commit setup agent.
	invs := f.Invocations()
	for _, inv := range invs {
		if inv.Agent.Identifier != "commit-manager-git" {
			t.Errorf("want no workflow agent dispatched after empty-branch-name refusal, got %q",
				inv.Agent.Identifier)
		}
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
workflow: linear
workflow_version: "1.0"
task: "resume task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
mode: auto
checkpoints: disabled
commits: disabled
pre_consultation: disabled
manual_resolution: disabled
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
	artifactPath := filepath.Join(dir, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(resumeArtifact), 0600); err != nil {
		t.Fatalf("write resume artifact: %v", err)
	}

	f := harness.NewFakeAdapter()
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
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "resume task",
		IsNewRun:             false, // resume: artifact already on disk
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
workflow: linear
workflow_version: "1.0"
task: "interrupted task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
mode: auto
checkpoints: disabled
commits: disabled
pre_consultation: disabled
manual_resolution: disabled
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
	artifactPath := filepath.Join(dir, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(interruptedArtifact), 0600); err != nil {
		t.Fatalf("write interrupted artifact: %v", err)
	}

	f := harness.NewFakeAdapter()
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
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "interrupted task",
		IsNewRun:             false, // resume: interrupted artifact on disk
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
