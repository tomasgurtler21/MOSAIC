package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

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
	f := harness.NewMockAdapter()
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
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	assertGoldenMatches(t, artifactPath, "linear-after-run.md")
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
	f := harness.NewMockAdapter()

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
		RunID: integrationRunID,
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

	assertGoldenMatches(t, artifactPath, "four-approach-after-run.md")
}
