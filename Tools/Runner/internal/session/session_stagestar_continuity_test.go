package session_test

// Tests for stage-set continuity: how the session handles Stage-* outputs that
// appear during a run and trigger plan re-derivation. These tests use the
// stage-continuity-orch.md fixture.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Stage set continuity across a run =====

// TestSession_Start_StageStarOutputPrecedesStagedExecution_EntersExecution
// verifies that when a pre-EXECUTION planning row produces Stage-* outputs
// and the resulting stage set is successfully re-derived, the run goes on to
// enter the EXECUTION phase and dispatch the stage 1 rows rather than
// stopping for an unavailable stage set.
func TestSession_Start_StageStarOutputPrecedesStagedExecution_EntersExecution(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir()

	orchPath := copyOrchestratorFile(t, orchDir, "stage-continuity-orch.md")
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "reviewer")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	// Plan.md does not exist yet: this is a genuinely new run, and the
	// planner has not produced it until its own invocation completes. A
	// single stage keeps the run's EXECUTION phase to one pass through the
	// stage 1 rows.
	planContent := `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`
	planPath := filepath.Join(runFolder, "Plan.md")

	fake := harness.NewMockAdapter()
	fake.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan and stage dirs created",
	}})
	fake.Queue("reviewer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "reviewer#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "reviewed",
	}})
	fake.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "tests written",
	}})
	fake.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "implementation approved",
	}})

	// Write Plan.md as a side effect of the planner's own invocation
	// succeeding, exactly as the planner's own tooling would produce it
	// mid-run rather than it pre-existing before the run starts.
	f := &callbackHarness{
		delegate: fake,
		onInvoke: func(agentID string) {
			if agentID == "planner" {
				if err := os.WriteFile(planPath, []byte(planContent), 0600); err != nil {
					t.Fatalf("write Plan.md after planner invocation: %v", err)
				}
			}
		},
	}

	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "stage-continuity",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := fake.Invocations()
	if len(invs) != 4 {
		t.Fatalf("want 4 harness invocations (planner, reviewer, implementation-tdd, implementation-review), got %d", len(invs))
	}
	if invs[2].Agent.Identifier != "implementation-tdd" {
		t.Errorf("want the third invocation to be implementation-tdd (EXECUTION reached and dispatched), got %q", invs[2].Agent.Identifier)
	}
}

// TestSession_Start_FailedStageStarRederivation_RetainsExistingStageSet
// verifies that when a stage set has already been successfully derived
// earlier in a run, a later failed re-read of the plan file (triggered by a
// further Stage-* output) does not discard it: the run still reaches
// EXECUTION and dispatches the stage 1 rows instead of stopping for an
// unavailable stage set.
func TestSession_Start_FailedStageStarRederivation_RetainsExistingStageSet(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir()

	orchPath := copyOrchestratorFile(t, orchDir, "stage-continuity-orch.md")
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "reviewer")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	// Plan.md does not exist yet: this is a genuinely new run. A single
	// stage keeps the run's EXECUTION phase to one pass through the stage 1
	// rows.
	planContent := `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`
	planPath := filepath.Join(runFolder, "Plan.md")

	fake := harness.NewMockAdapter()
	fake.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan and stage dirs created",
	}})
	fake.Queue("reviewer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "reviewer#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "reviewed",
	}})
	fake.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "tests written",
	}})
	fake.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "implementation approved",
	}})

	// Plan.md is written after the planner's invocation succeeds (its
	// Stage-* output triggers the first, successful re-derivation) and then
	// removed after the reviewer's invocation succeeds (its own Stage-*
	// output triggers a second re-derivation attempt that must fail). Only
	// a stage set retained from the first, successful re-derivation lets the
	// run go on to reach EXECUTION.
	f := &callbackHarness{
		delegate: fake,
		onInvoke: func(agentID string) {
			switch agentID {
			case "planner":
				if err := os.WriteFile(planPath, []byte(planContent), 0600); err != nil {
					t.Fatalf("write Plan.md after planner invocation: %v", err)
				}
			case "reviewer":
				if err := os.Remove(planPath); err != nil {
					t.Fatalf("remove Plan.md after reviewer invocation: %v", err)
				}
			}
		},
	}

	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "stage-continuity",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := fake.Invocations()
	if len(invs) != 4 {
		t.Fatalf("want 4 harness invocations (planner, reviewer, implementation-tdd, implementation-review), got %d", len(invs))
	}
	if invs[2].Agent.Identifier != "implementation-tdd" {
		t.Errorf("want the third invocation to be implementation-tdd (EXECUTION reached despite the failed re-read), got %q", invs[2].Agent.Identifier)
	}
}
