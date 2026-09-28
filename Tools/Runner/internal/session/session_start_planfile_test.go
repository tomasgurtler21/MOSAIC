package session_test

// Tests for Plan.md path resolution and staged workflow execution: plan file
// found vs absent, plan file in wrong directory, malformed plan file, and
// staged workflow completion.

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

// ===== Staged workflow run with planstages =====

// TestSession_Start_StagedWorkflow_Completes verifies that a staged
// (implementation-only) workflow runs successfully end-to-end: the session
// reads the stage set from Plan.md at the start, enters the EXECUTION phase,
// dispatches all rows across all stages, and returns RunCompleted.
func TestSession_Start_StagedWorkflow_Completes(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "staged-orch.md")
	writeAgentFile(t, dir, "implementation-tdd")
	writeAgentFile(t, dir, "implementation-review")

	// Write Plan.md with a single stage so the session can read the stage set.
	planPath := filepath.Join(dir, "Plan.md")
	if err := os.WriteFile(planPath, []byte(`# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`), 0600); err != nil {
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

	// Stage 1: implementation-tdd → implementation-review → COMPLETE.
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

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            dir, // Plan.md was written into dir; the session must resolve it here.
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
}

// ===== Plan.md path resolution =====

// TestSession_Start_PlanFile_ResolvedFromRunFolder_StagesApplied verifies
// that when Plan.md lives in config.RunFolder -- a directory distinct from
// the orchestrator file's directory -- the session reads it from there and
// applies the resulting stage set, letting a staged-only workflow (no
// pre-EXECUTION rows) run to completion.
func TestSession_Start_PlanFile_ResolvedFromRunFolder_StagesApplied(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir() // deliberately distinct from orchDir
	orchPath := copyOrchestratorFile(t, orchDir, "staged-orch.md")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	planPath := filepath.Join(runFolder, "Plan.md")
	if err := os.WriteFile(planPath, []byte(`# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`), 0600); err != nil {
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

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
}

// TestSession_Start_PlanFile_InOrchestratorDir_NotPickedUp verifies that a
// Plan.md placed in the orchestrator file's directory is not treated as the
// run's plan file when config.RunFolder points elsewhere. The stage set must
// stay nil, so a staged-only workflow (no pre-EXECUTION rows) stops with a
// clear reason instead of silently reading the wrong file.
func TestSession_Start_PlanFile_InOrchestratorDir_NotPickedUp(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir() // Plan.md is intentionally absent here

	orchPath := copyOrchestratorFile(t, orchDir, "staged-orch.md")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	// Plan.md sits next to the orchestrator file, NOT in RunFolder.
	planPath := filepath.Join(orchDir, "Plan.md")
	if err := os.WriteFile(planPath, []byte(`# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`), 0600); err != nil {
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

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	// The run must not be refused (a plan file "exists" only from the wrong
	// directory's point of view; from RunFolder's point of view it is absent).
	if got.Status == domain.RunRefused {
		t.Fatalf("want run not refused, got RunRefused (message: %q)", got.Message)
	}
	// staged-orch.md has no pre-EXECUTION rows, so the first row is already an
	// EXECUTION row. With no stage set available, the engine must stop cleanly
	// rather than dispatch against the orchestrator-directory Plan.md.
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped (no stage set available), got %q (message: %q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "stage set") {
		t.Errorf("want stop message to name the missing stage set, got %q", got.Message)
	}
	if len(f.Invocations()) != 0 {
		t.Errorf("want no harness invocations (stopped before dispatch), got %d", len(f.Invocations()))
	}
}

// ===== Absence tolerance for pre-EXECUTION rows =====

// TestSession_Start_NoPlanFile_NewRun_DispatchesFirstPreExecutionRow verifies
// that a new run of a staged workflow with pre-EXECUTION rows is not refused
// when Plan.md does not exist anywhere: the artifact store's Create is called
// and the first pre-EXECUTION row is dispatched normally.
func TestSession_Start_NoPlanFile_NewRun_DispatchesFirstPreExecutionRow(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir() // no Plan.md written here or anywhere else

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

	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planned",
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
	if got.Status == domain.RunRefused {
		t.Fatalf("want run not refused when Plan.md is absent, got RunRefused (message: %q)", got.Message)
	}

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least 1 harness invocation (planner dispatched), got 0")
	}
	if invs[0].Agent.Identifier != "planner" {
		t.Errorf("want first invocation to be planner, got %q", invs[0].Agent.Identifier)
	}
	if !store.exists {
		t.Error("want artifact store's Create to have been called for a new run, but no artifact was created")
	}
}

// TestSession_Start_NoPlanFile_ResumedRun_DispatchesNextPreExecutionRow
// verifies that a run resumed at a pre-EXECUTION row of a staged workflow is
// not refused when Plan.md does not exist: the session continues from the
// next pre-EXECUTION row without treating the missing plan file as a fault.
func TestSession_Start_NoPlanFile_ResumedRun_DispatchesNextPreExecutionRow(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir() // no Plan.md written here or anywhere else

	orchPath := copyOrchestratorFile(t, orchDir, "pre-exec-staged-orch.md")
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "reviewer")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	f := harness.NewMockAdapter()
	store := &memStore{
		state: domain.ArtifactState{
			Type:            "orchestration-artifact",
			Workflow:        "pre-exec-staged",
			WorkflowVersion: "1.0",
			Task:            "task",
			GlobalSequence:  1,
			RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
			CurrentState: domain.CurrentState{
				Phase:      "PLANNING",
				LastStatus: domain.StatusSUCCESS,
				LastAgent:  "planner#1",
			},
			ExecutionLog: []domain.ExecutionLogEntry{
				{Seq: 1, Agent: "planner#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			},
		},
		exists: true,
	}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	f.Queue("reviewer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "reviewer#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "reviewed",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "pre-exec-staged",
		Task:                 "task",
		IsNewRun:             false, // resume: artifact already exists

		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status == domain.RunRefused {
		t.Fatalf("want resumed run not refused when Plan.md is absent, got RunRefused (message: %q)", got.Message)
	}

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least 1 harness invocation (reviewer dispatched), got 0")
	}
	if invs[0].Agent.Identifier != "reviewer" {
		t.Errorf("want resumed invocation to be reviewer, got %q", invs[0].Agent.Identifier)
	}
}

// ===== Preserved refusal on a malformed plan file =====

// TestSession_Start_MalformedPlanFile_NewRun_ReturnsRefusal verifies that a
// Plan.md that exists in the run folder but cannot be parsed (no ## Stages
// heading) still refuses a new run of a staged workflow.
func TestSession_Start_MalformedPlanFile_NewRun_ReturnsRefusal(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir()

	orchPath := copyOrchestratorFile(t, orchDir, "staged-orch.md")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	planPath := filepath.Join(runFolder, "Plan.md")
	if err := os.WriteFile(planPath, []byte("# Plan\n\nNo stages table here.\n"), 0600); err != nil {
		t.Fatalf("write malformed Plan.md: %v", err)
	}

	ses := session.New(session.Deps{
		Harness:   harness.NewMockAdapter(),
		Store:     &memStore{},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,

		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_MalformedPlanFile_ResumedRun_ReturnsRefusal verifies that
// the same malformed-plan-file refusal applies to a resumed run, not only a
// new one.
func TestSession_Start_MalformedPlanFile_ResumedRun_ReturnsRefusal(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir()

	orchPath := copyOrchestratorFile(t, orchDir, "staged-orch.md")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	planPath := filepath.Join(runFolder, "Plan.md")
	if err := os.WriteFile(planPath, []byte("# Plan\n\nNo stages table here.\n"), 0600); err != nil {
		t.Fatalf("write malformed Plan.md: %v", err)
	}

	store := &memStore{
		state: domain.ArtifactState{
			Type:            "orchestration-artifact",
			Workflow:        "staged",
			WorkflowVersion: "1.0",
			Task:            "task",
			GlobalSequence:  0,
		},
		exists: true,
	}
	ses := session.New(session.Deps{
		Harness:   harness.NewMockAdapter(),
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             false, // resume: artifact already exists

		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}
