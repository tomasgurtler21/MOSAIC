package session_test

// Tests for STAGE_END infrastructure agent trigger evaluation.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== STAGE_END trigger tests =====
//
// Coverage:
//
//   STAGE_END trigger:
//   - Fires after the last workflow step of a stage (prospective: the completed
//     step is the last step of its stage, determined by look-ahead into the
//     admitted workflow and stage set).
//   - Fires once at the end of a single-stage workflow (single stage counts as a
//     complete stage boundary).
//   - Does not fire after intermediate steps within a stage; fires only at the
//     last step of each stage.
//   - Fires exactly once per stage in a multi-stage workflow.

// newStageEndStagedSession builds a session backed by stage-end-staged-orch.md
// (staged workflow + commit-manager-git STAGE_END halt). Plan.md with 2 stages is
// written into the same temp dir. Agent files for implementation-tdd and
// implementation-review are also written.
func newStageEndStagedSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "stage-end-staged-orch.md")
	writeAgentFile(t, dir, "implementation-tdd")
	writeAgentFile(t, dir, "implementation-review")
	writeAgentFile(t, dir, "commit-manager-git")

	const planContent = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First stage | - | FALSE |
| 2 | Stage Two | Second stage | 1 | FALSE |
`
	planPath := filepath.Join(filepath.Dir(orchPath), "Plan.md")
	if err := os.WriteFile(planPath, []byte(planContent), 0600); err != nil {
		t.Fatalf("newStageEndStagedSession: write Plan.md: %v", err)
	}

	f = harness.NewMockAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	return
}

// TestSession_Start_TriggerEval_STAGE_END_FiresOnStageTransition verifies that a
// commit-class infrastructure agent with STAGE_END trigger is dispatched after the
// last step of a stage (prospective semantics). STAGE_END fires when the completed
// step is the last step of its stage, determined by look-ahead into the admitted
// workflow and stage set -- not by comparing against a previous step.
//
// Dispatch sequence with 2 stages (Stage-1: tdd, review; Stage-2: tdd, review):
//   - implementation-tdd  Stage-1 (not the last Stage-1 step -> no STAGE_END)
//   - implementation-review Stage-1 (last Stage-1 step -> STAGE_END fires -> commit-manager-git)
//   - commit-manager-git  (infra dispatch, IsInfrastructure=true -> no cascade)
//   - implementation-tdd  Stage-2 (not the last Stage-2 step -> no STAGE_END)
//   - implementation-review Stage-2 (last Stage-2 step -> STAGE_END fires -> commit-manager-git)
//   - commit-manager-git  (infra dispatch for Stage-2 end)
func TestSession_Start_TriggerEval_STAGE_END_FiresOnStageTransition(t *testing.T) {
	ses, f, _, orchPath := newStageEndStagedSession(t)

	// Stage 1
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 1 tdd done",
	}})
	// Last step of Stage 1: STAGE_END fires after this step.
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 1 review done",
	}})
	// Infrastructure dispatch triggered by STAGE_END at end of Stage 1.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit after stage 1",
	}})
	// Stage 2 first step: not the last Stage-2 step -> no STAGE_END.
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 2 tdd done",
	}})
	// Last step of Stage 2: STAGE_END fires after this step.
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 2 review done",
	}})
	// Infrastructure dispatch triggered by STAGE_END at end of Stage 2.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit after stage 2",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            filepath.Dir(orchPath), // Plan.md was written next to the orchestrator file.
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) != 6 {
		t.Fatalf("want 6 invocations (tdd1, review1, commit1, tdd2, review2, commit2), got %d", len(invs))
	}

	// commit-manager-git must appear twice: once after Stage-1's last step and
	// once after Stage-2's last step.
	commitIndices := []int{}
	for i, inv := range invs {
		if inv.Agent.Identifier == "commit-manager-git" {
			commitIndices = append(commitIndices, i)
		}
	}
	if len(commitIndices) != 2 {
		t.Fatalf("want 2 commit-manager-git dispatches (one per stage end), got %d", len(commitIndices))
	}
	// First commit must follow tdd1, review1 (index 2 in 0-based).
	if commitIndices[0] != 2 {
		t.Errorf("want first commit-manager-git at invocation[2] (after Stage-1 last step), got at invocation[%d]", commitIndices[0])
	}
	// Second commit must follow tdd2, review2 (index 5 in 0-based).
	if commitIndices[1] != 5 {
		t.Errorf("want second commit-manager-git at invocation[5] (after Stage-2 last step), got at invocation[%d]", commitIndices[1])
	}
}

// TestSession_Start_TriggerEval_STAGE_END_FiresAtEndOfSingleStage verifies that a
// commit-class infrastructure agent with STAGE_END trigger is dispatched exactly
// once at the end of a single-stage workflow. Under prospective semantics, STAGE_END
// fires when the completed step is the last step of its stage; a single-stage
// workflow has exactly one stage boundary at the end of that stage.
//
// Dispatch sequence (1 stage: tdd, review):
//   - implementation-tdd  Stage-1 (not the last Stage-1 step -> no STAGE_END)
//   - implementation-review Stage-1 (last Stage-1 step -> STAGE_END fires -> commit-manager-git)
//   - commit-manager-git  (infra dispatch, IsInfrastructure=true -> no cascade)
func TestSession_Start_TriggerEval_STAGE_END_FiresAtEndOfSingleStage(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "stage-end-staged-orch.md")
	writeAgentFile(t, dir, "implementation-tdd")
	writeAgentFile(t, dir, "implementation-review")
	writeAgentFile(t, dir, "commit-manager-git")
	const singleStagePlan = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(singleStagePlan), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}
	f := harness.NewMockAdapter()
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// First step: not the last step of Stage-1, so STAGE_END must not fire yet.
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// Last step of Stage-1: STAGE_END fires after this completes.
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// Infrastructure dispatch triggered by STAGE_END at end of the single stage.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit done",
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

	invs := f.Invocations()
	if len(invs) != 3 {
		t.Fatalf("want 3 invocations (tdd, review, commit), got %d: STAGE_END must fire exactly once at the end of the single stage", len(invs))
	}
	// commit-manager-git must be the third invocation (after tdd and review).
	if invs[2].Agent.Identifier != "commit-manager-git" {
		t.Errorf("want invocation[2] to be commit-manager-git (STAGE_END fired at end of stage), got %q", invs[2].Agent.Identifier)
	}
}

// TestSession_Start_TriggerEval_STAGE_END_DoesNotFireBeforeLastStep verifies that
// STAGE_END does not fire after intermediate steps within a stage -- only after the
// final step of a stage. In a single-stage workflow with two steps (tdd, review),
// STAGE_END must not fire after tdd (index 0); it fires only after review (index 1).
//
// This is a regression guard for the prospective look-ahead implementation: a bug
// that fires STAGE_END eagerly (e.g., after every step rather than at the last step)
// would produce commit-manager-git at index 0 instead of index 2, failing the
// position assertion.
func TestSession_Start_TriggerEval_STAGE_END_DoesNotFireBeforeLastStep(t *testing.T) {
	// Single-stage workflow, 2 steps: tdd then review. STAGE_END must fire only
	// after the review step (the last step of the stage), not after tdd.
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "stage-end-staged-orch.md")
	writeAgentFile(t, dir, "implementation-tdd")
	writeAgentFile(t, dir, "implementation-review")
	writeAgentFile(t, dir, "commit-manager-git")
	const singleStagePlan = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(singleStagePlan), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}
	f := harness.NewMockAdapter()
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// Intermediate step (not the last step of Stage-1): STAGE_END must not fire.
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// Last step of Stage-1: STAGE_END fires after this step, not before.
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// commit-manager-git dispatched only after review (the last stage step).
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit done",
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

	invs := f.Invocations()
	if len(invs) != 3 {
		t.Fatalf("want exactly 3 invocations (tdd, review, commit), got %d", len(invs))
	}
	// commit-manager-git must be at index 2, not at index 0 or 1.
	if invs[2].Agent.Identifier != "commit-manager-git" {
		t.Errorf("want invocation[2] = commit-manager-git (fires only at last stage step), got %q; STAGE_END must not fire before the last step", invs[2].Agent.Identifier)
	}
}

// TestSession_Start_TriggerEval_STAGE_END_FiresOncePerStageInMultiStageWorkflow
// verifies that STAGE_END fires exactly once per stage in a multi-stage workflow,
// positioned after the last step of each stage and before the first step of the
// next stage. This validates the prospective look-ahead semantics across stage
// boundaries.
//
// The workflow has 2 stages (Stage-1: tdd, review; Stage-2: tdd, review).
// Expected dispatch sequence:
//   - implementation-tdd  Stage-1 (not last Stage-1 step -> no STAGE_END)
//   - implementation-review Stage-1 (last Stage-1 step -> STAGE_END fires)
//   - commit-manager-git  Stage-1 end (infra dispatch)
//   - implementation-tdd  Stage-2 (not last Stage-2 step -> no STAGE_END)
//   - implementation-review Stage-2 (last Stage-2 step -> STAGE_END fires)
//   - commit-manager-git  Stage-2 end (infra dispatch)
//
// commit-manager-git must fire exactly twice: once at index 2 (after Stage-1's
// last step) and once at index 5 (after Stage-2's last step).
func TestSession_Start_TriggerEval_STAGE_END_FiresOncePerStageInMultiStageWorkflow(t *testing.T) {
	ses, f, _, orchPath := newStageEndStagedSession(t)

	// Stage 1, step 1: not the last step of Stage-1.
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 1 tdd done",
	}})
	// Stage 1, step 2 (last step): STAGE_END fires after this completes.
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 1 review done",
	}})
	// Commit triggered by STAGE_END at Stage-1 boundary.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit stage 1",
	}})
	// Stage 2, step 1: not the last step of Stage-2.
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 2 tdd done",
	}})
	// Stage 2, step 2 (last step): STAGE_END fires after this completes.
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 2 review done",
	}})
	// Commit triggered by STAGE_END at Stage-2 boundary.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit stage 2",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            filepath.Dir(orchPath),
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) != 6 {
		t.Fatalf("want 6 invocations (tdd1, review1, commit1, tdd2, review2, commit2), got %d", len(invs))
	}

	// Collect commit dispatch positions.
	var commitIndices []int
	for i, inv := range invs {
		if inv.Agent.Identifier == "commit-manager-git" {
			commitIndices = append(commitIndices, i)
		}
	}

	// Exactly two commits: one per stage.
	if len(commitIndices) != 2 {
		t.Fatalf("want exactly 2 commit-manager-git dispatches (one per stage end), got %d at positions %v",
			len(commitIndices), commitIndices)
	}

	// First commit must immediately follow Stage-1's last step (index 2 in 0-based).
	if commitIndices[0] != 2 {
		t.Errorf("want first commit-manager-git at invocation[2] (after Stage-1 last step, before Stage-2 first step), got invocation[%d]",
			commitIndices[0])
	}

	// Second commit must follow Stage-2's last step (index 5 in 0-based).
	if commitIndices[1] != 5 {
		t.Errorf("want second commit-manager-git at invocation[5] (after Stage-2 last step), got invocation[%d]",
			commitIndices[1])
	}

	// Verify Stage-2's first step (tdd2) follows the first commit without
	// an extra commit in between.
	if invs[3].Agent.Identifier != "implementation-tdd" {
		t.Errorf("want invocation[3] = implementation-tdd (Stage-2 first step after commit), got %q",
			invs[3].Agent.Identifier)
	}
}
