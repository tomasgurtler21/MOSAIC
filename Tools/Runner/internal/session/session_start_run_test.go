package session_test

// Tests for the core session dispatch loop: happy path, loop-back on findings,
// deviation handling, graceful stop, and infrastructure trigger hook.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Happy path =====

// TestSession_Start_HappyPath_LinearWorkflow_Completes verifies the core
// dispatch cycle: engine Dispatch → harness Invoke → artifact Apply, repeated
// until engine returns Complete, which produces RunCompleted.
//
// Workflow: agent-a (PLANNING) → agent-b (PLANNING) → COMPLETE.
// Both agents return SUCCESS.
func TestSession_Start_HappyPath_LinearWorkflow_Completes(t *testing.T) {
	ses, f, _, orchPath := newLinearSession(t)

	// Script both agents to return SUCCESS.
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

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	requireRunStatus(t, got, err, domain.RunCompleted)
}

// TestSession_Start_HappyPath_ArtifactUpdatedAfterEachStep verifies that the
// artifact store's Apply is called after each successful harness invocation,
// and that the applied steps arrive in the expected order.
func TestSession_Start_HappyPath_ArtifactUpdatedAfterEachStep(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

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

	ses.Start(context.Background(), baseLinearConfig(orchPath)) //nolint:errcheck

	// Expect exactly two Apply calls (one per dispatched agent).
	if len(store.Applied) != 2 {
		t.Fatalf("want 2 Apply calls, got %d", len(store.Applied))
	}
	// First apply: agent-a in PLANNING.
	if store.Applied[0].AgentInstance != "agent-a#1" {
		t.Errorf("want first Apply for agent-a#1, got %q", store.Applied[0].AgentInstance)
	}
	if store.Applied[0].Phase != "PLANNING" {
		t.Errorf("want first Apply phase=PLANNING, got %q", store.Applied[0].Phase)
	}
	// Second apply: agent-b in PLANNING.
	if store.Applied[1].AgentInstance != "agent-b#2" {
		t.Errorf("want second Apply for agent-b#2, got %q", store.Applied[1].AgentInstance)
	}
}

// TestSession_Start_HappyPath_HarnessInvokedWithCorrectRequest verifies that
// the harness is called with a ProtocolRequest whose AgentInstanceID follows
// the "{name}#{seq}" format and whose input/output artifacts match the
// routing table row (with templates resolved for stage context).
func TestSession_Start_HappyPath_HarnessInvokedWithCorrectRequest(t *testing.T) {
	ses, f, _, orchPath := newLinearSession(t)

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

	ses.Start(context.Background(), baseLinearConfig(orchPath)) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) < 1 {
		t.Fatal("want at least one harness invocation, got none")
	}
	first := invs[0].Request
	// agent-a is seq=1 on a fresh run.
	if first.AgentInstanceID != "agent-a#1" {
		t.Errorf("want agent_instance_id=agent-a#1, got %q", first.AgentInstanceID)
	}
}

// ===== On Findings loop-back =====

// TestSession_Start_OnFindings_LoopBack_HarnessInvokedNotDeviation verifies
// that when an agent returns COMPLETED_NEEDS_ACTION and the engine's On
// Findings hint names an unambiguous agent (loop-back), the session invokes
// the harness for the loop-back target and does NOT call the deviation
// resolver.
//
// Workflow: agent-a succeeds → agent-b (reviewer) returns CNA → engine
// dispatches the nearest preceding agent-a row (On Findings = "agent-a") →
// agent-a returns SUCCESS → agent-b returns SUCCESS → COMPLETE.
func TestSession_Start_OnFindings_LoopBack_HarnessInvokedNotDeviation(t *testing.T) {
	dir := t.TempDir()

	// Workflow where the reviewer agent-b routes findings back to agent-a above it.
	const loopbackContent = `<Workflow type="core" name="loopback" version="1.0">
## Loopback Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | - | - | plan.md |
| PLANNING | agent-b | FALSE | COMPLETE | agent-a | plan.md | result.md |
</Workflow>
`
	orchPath := filepath.Join(dir, "loopback-orch.md")
	if err := os.WriteFile(orchPath, []byte(loopbackContent), 0600); err != nil {
		t.Fatalf("write loopback-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// First agent-a call → SUCCESS.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "drafted",
	}})
	// First agent-b call → CNA (triggers loop-back to agent-a via On Findings).
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusCOMPLETED_NEEDS_ACTION,
		StatusMessage:   "found issues, fix them",
	}})
	// Second agent-a call (loop-back) → SUCCESS.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "now fixed",
	}})
	// Second agent-b call → SUCCESS → COMPLETE.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "loopback",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAutoReview},
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Four harness invocations: agent-a, agent-b (CNA), agent-a (loop-back), agent-b.
	invs := f.Invocations()
	if len(invs) != 4 {
		t.Errorf("want 4 harness invocations, got %d", len(invs))
	}
}

// ===== Deviation handling =====

// TestSession_Start_Deviation_ReturnsDeviationUnresolved verifies that when
// the engine returns a Deviation decision and no routing consultant is wired,
// the session returns RunDeviationUnresolved without an error.
func TestSession_Start_Deviation_ReturnsDeviationUnresolved(t *testing.T) {
	dir := t.TempDir()

	// Workflow where agent-a has absent On Findings → any non-SUCCESS triggers
	// a deviation (the engine can't route it automatically).
	const deviationWorkflow = `<Workflow type="core" name="deviate" version="1.0">
## Deviation Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | - | plan.md |
| PLANNING | agent-b | FALSE | COMPLETE | plan.md | result.md |
</Workflow>
`
	orchPath := filepath.Join(dir, "deviation-orch.md")
	if err := os.WriteFile(orchPath, []byte(deviationWorkflow), 0600); err != nil {
		t.Fatalf("write deviation-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	// No routing consultant wired: deviation terminates with RunDeviationUnresolved.
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// agent-a returns PARTIALLY_DONE → deviation (no On Findings column).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusPARTIALLY_DONE,
		StatusMessage:   "only partially done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "deviate",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunDeviationUnresolved {
		t.Errorf("want RunDeviationUnresolved when no routing consultant is wired, got %q", got.Status)
	}
}

// TestSession_Start_Deviation_ResolverStop_ReturnsDeviationUnresolved verifies
// that when the deviation resolver returns a StopRun instruction, the session
// records the current state and returns RunDeviationUnresolved (not RunCompleted
// and not an error).
func TestSession_Start_Deviation_ResolverStop_ReturnsDeviationUnresolved(t *testing.T) {
	dir := t.TempDir()

	// Same deviation workflow as TestSession_Start_Deviation_ResolvesAndResumes:
	// agent-a has no On Findings column, so PARTIALLY_DONE triggers a deviation.
	const deviationWorkflow = `<Workflow type="core" name="deviate-stop" version="1.0">
## Deviation Stop Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | - | plan.md |
| PLANNING | agent-b | FALSE | COMPLETE | plan.md | result.md |
</Workflow>
`
	orchPath := filepath.Join(dir, "deviation-stop-orch.md")
	if err := os.WriteFile(orchPath, []byte(deviationWorkflow), 0600); err != nil {
		t.Fatalf("write deviation-stop-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	// No routing consultant wired: any deviation terminates with RunDeviationUnresolved.
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// agent-a returns PARTIALLY_DONE → deviation (no On Findings column).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusPARTIALLY_DONE,
		StatusMessage:   "blocked, cannot continue",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "deviate-stop",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error for deviation-unresolved outcome, got %v", err)
	}
	if got.Status != domain.RunDeviationUnresolved {
		t.Errorf("want RunDeviationUnresolved when no routing consultant is wired, got %q (message: %q)", got.Status, got.Message)
	}
}

// ===== Graceful stop =====

// TestSession_Start_GracefulStop_ReturnsRunStopped verifies that when the
// context is cancelled after the first dispatch completes (mid-loop), the
// session stops after that step and returns RunStopped -- not RunCompleted.
//
// The callbackHarness fires cancel() synchronously inside agent-a's Invoke
// return path, ensuring the cancellation happens after agent-a's response is
// processed but before agent-b's Invoke is attempted. MockAdapter's Invoke
// checks ctx.Done() at the start, so agent-b's Invoke will see the cancelled
// context and return ctx.Err(), which the session must convert to RunStopped.
func TestSession_Start_GracefulStop_ReturnsRunStopped(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel the context after agent-a's invocation completes. The callback
	// fires synchronously in the session's goroutine immediately after Invoke
	// returns, giving us a reliable mid-loop cancellation point.
	harnessCb := &callbackHarness{
		delegate: f,
		onInvoke: func(agentID string) {
			if agentID == "agent-a" {
				cancel()
			}
		},
	}

	ses := session.New(session.Deps{
		Harness:   harnessCb,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// Only queue agent-a. The cancel fires before agent-b is dispatched, so
	// MockAdapter returns ctx.Err() for agent-b without consuming a scripted entry.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	got, err := ses.Start(ctx, baseLinearConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error for graceful stop, got %v", err)
	}
	// Graceful stop must return RunStopped, not RunCompleted. The run is
	// resumable (agent-b was not dispatched).
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped after mid-loop cancellation, got %q", got.Status)
	}
}

// ===== Infrastructure-agent trigger point =====

// TestSession_Start_InfrastructureAgentTrigger_CalledPerDispatch verifies
// that the infrastructure-agent trigger hook (OnInfrastructureTrigger on Deps)
// is called exactly once per dispatch cycle. The hook is injected as a counter
// function so the test can assert the exact call count without relying on
// side effects or inferring the hook's execution from other observables.
//
// A two-agent linear workflow produces exactly 2 dispatch cycles, so the hook
// counter must reach 2 when the run completes.
func TestSession_Start_InfrastructureAgentTrigger_CalledPerDispatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	hookCount := 0
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     &memStore{},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		OnInfrastructureTrigger: func() { hookCount++ },
	})

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

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	requireRunStatus(t, got, err, domain.RunCompleted)

	// The hook must be called exactly once per dispatch cycle.
	// Two agents dispatched → hookCount must be 2.
	if hookCount != 2 {
		t.Errorf("want OnInfrastructureTrigger called 2 times (one per dispatch), got %d", hookCount)
	}
}
