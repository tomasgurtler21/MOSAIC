package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

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

	f := harness.NewMockAdapter()
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

			f := harness.NewMockAdapter()
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

			f := harness.NewMockAdapter()
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

	f := harness.NewMockAdapter()
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

	f := harness.NewMockAdapter()
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
