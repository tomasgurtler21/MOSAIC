package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

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
	f := harness.NewMockAdapter()

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
		RunID: integrationRunID,
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
	sess := newSession(harness.NewMockAdapter(), artifactPath)

	cfg := domain.RunConfig{
		RunID: integrationRunID,
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
	f := harness.NewMockAdapter()

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
		RunID: integrationRunID,
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
