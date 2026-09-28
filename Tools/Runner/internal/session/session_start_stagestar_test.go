package session_test

// Tests for Stage-* output re-derivation and the EXECUTION-reached-with-no-stage-set
// stop condition.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Stage-set re-derivation after Stage-* output =====

// TestSession_Start_StageStarOutput_TriggersStageSetRederivation verifies
// that after a row produces Stage-* output artifacts, the session re-reads the
// plan artifact via planstages and passes the refreshed stage set to the engine
// for the subsequent row.
//
// The refreshed stage set enables the engine to expand Stage-* wildcards in
// the next row's input artifacts against stage folders that did not exist when
// the run started.
func TestSession_Start_StageStarOutput_TriggersStageSetRederivation(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "stage-star-output-orch.md")
	writeAgentFile(t, dir, "planner")
	writeAgentFile(t, dir, "reviewer")

	// Write a Plan.md with 2 stages so planstages can read it.
	// The session is expected to look for Plan.md adjacent to the orchestrator
	// file or at a well-known relative path.
	planContent := `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First | - | FALSE |
| 2 | Stage Two | Second | 1 | FALSE |
`
	planPath := filepath.Join(dir, "Plan.md")
	if err := os.WriteFile(planPath, []byte(planContent), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// planner produces Stage-*/Plan.md in its output.
	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan and stage dirs created",
	}})
	// reviewer receives the expanded Stage-*/Plan.md as input (Stage-1/Plan.md,
	// Stage-2/Plan.md) and returns SUCCESS → COMPLETE.
	f.Queue("reviewer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "reviewer#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "stage-star-output",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            dir, // Plan.md was written into dir; the session must resolve it here.
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify that the reviewer was invoked with expanded Stage-* input paths.
	// After the planner completes (with Stage-* output), the session should
	// re-derive the stage set (2 stages) and pass it to the engine. The engine
	// then expands Stage-*/Plan.md → [Stage-1/Plan.md, Stage-2/Plan.md].
	invs := f.Invocations()
	if len(invs) < 2 {
		t.Fatalf("want 2 harness invocations, got %d", len(invs))
	}
	reviewerReq := invs[1].Request
	// The reviewer's input should contain expanded stage-specific paths, not
	// the literal Stage-*/Plan.md wildcard.
	if containsInput(reviewerReq.InputArtifacts, "Stage-*/Plan.md") {
		t.Error("want Stage-*/Plan.md expanded to per-stage paths in reviewer input, but got literal wildcard")
	}
	if !containsInput(reviewerReq.InputArtifacts, "Stage-1/Plan.md") {
		t.Error("want Stage-1/Plan.md in reviewer input after stage-set re-derivation with 2 stages")
	}
	if !containsInput(reviewerReq.InputArtifacts, "Stage-2/Plan.md") {
		t.Error("want Stage-2/Plan.md in reviewer input after stage-set re-derivation with 2 stages")
	}
}

// ===== Stage-* re-derivation resolves from the run folder =====

// TestSession_Start_StageStarRederivation_ResolvesFromRunFolder verifies that
// the Stage-* output re-derivation read site (triggered after a row emits
// Stage-* outputs) also resolves Plan.md from config.RunFolder rather than
// the orchestrator file's directory.
func TestSession_Start_StageStarRederivation_ResolvesFromRunFolder(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir() // deliberately distinct from orchDir

	orchPath := copyOrchestratorFile(t, orchDir, "stage-star-output-orch.md")
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "reviewer")

	planContent := `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First | - | FALSE |
| 2 | Stage Two | Second | 1 | FALSE |
`
	planPath := filepath.Join(runFolder, "Plan.md")
	if err := os.WriteFile(planPath, []byte(planContent), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan and stage dirs created",
	}})
	f.Queue("reviewer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "reviewer#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "stage-star-output",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) < 2 {
		t.Fatalf("want 2 harness invocations, got %d", len(invs))
	}
	reviewerReq := invs[1].Request
	if containsInput(reviewerReq.InputArtifacts, "Stage-*/Plan.md") {
		t.Error("want Stage-*/Plan.md expanded to per-stage paths in reviewer input, but got literal wildcard")
	}
	if !containsInput(reviewerReq.InputArtifacts, "Stage-1/Plan.md") {
		t.Error("want Stage-1/Plan.md in reviewer input after run-folder-based stage-set re-derivation")
	}
	if !containsInput(reviewerReq.InputArtifacts, "Stage-2/Plan.md") {
		t.Error("want Stage-2/Plan.md in reviewer input after run-folder-based stage-set re-derivation")
	}
}

// ===== EXECUTION reached with no stage set =====

// TestSession_Start_ExecutionReached_NoStageSet_StopsCleanly verifies that
// when a staged workflow's pre-EXECUTION rows complete without ever producing
// a readable Plan.md, reaching the EXECUTION phase produces a clear
// RunStopped outcome naming the missing stage set -- not a panic and not a
// dispatch of the EXECUTION row.
func TestSession_Start_ExecutionReached_NoStageSet_StopsCleanly(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir() // no Plan.md ever appears here

	orchPath := copyOrchestratorFile(t, orchDir, "pre-exec-staged-orch.md")
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "reviewer")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// Both pre-EXECUTION rows succeed but neither produces a readable Plan.md,
	// so the stage set remains nil when the EXECUTION row is reached.
	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planned (no Plan.md written in this scenario)",
	}})
	f.Queue("reviewer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "reviewer#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "reviewed",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "pre-exec-staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped when EXECUTION is reached with no stage set, got %q (message: %q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "stage set") {
		t.Errorf("want stop message to name the missing stage set, got %q", got.Message)
	}

	invs := f.Invocations()
	if len(invs) != 2 {
		t.Fatalf("want exactly 2 harness invocations (planner, reviewer) before the stop, got %d", len(invs))
	}
	if invs[len(invs)-1].Agent.Identifier == "implementation-tdd" {
		t.Error("want implementation-tdd NOT dispatched when no stage set is available")
	}
}
