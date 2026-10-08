package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

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
	f := harness.NewMockAdapter()
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
	// It stays PARTIALLY_DONE through the original dispatch and all 3 engine
	// re-dispatches, so the deviation follows.
	for i := 0; i < 4; i++ {
		f.Queue("optional-agent", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "optional-agent#2",
			StatusCode:      domain.StatusPARTIALLY_DONE,
			StatusMessage:   "only partially done",
		}})
	}

	cfg := domain.RunConfig{
		RunID: integrationRunID,
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
	f := harness.NewMockAdapter()
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
		RunID: integrationRunID,
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
		"test-writer-tdd",    // initial dispatch
		"build-review",       // first build attempt → CNA
		"test-writer-tdd",    // loop-back via On Findings
		"build-review",       // second build attempt → SUCCESS
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
	f := harness.NewMockAdapter()
	// No routing consultant: deviation terminates with RunDeviationUnresolved.
	sess := newSession(f, artifactPath)

	// PARTIALLY_DONE on the original dispatch and all 3 engine re-dispatches.
	for i := 0; i < 4; i++ {
		f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#1",
			StatusCode:      domain.StatusPARTIALLY_DONE,
			StatusMessage:   "only partially done",
		}})
	}

	cfg := domain.RunConfig{
		RunID: integrationRunID,
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
