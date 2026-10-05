package session_test

// Tests for mode-driven routing decisions: orchestrated mode always consults,
// auto mode routes via the engine on SUCCESS and escalates to consultant on
// non-SUCCESS, and auto-review mode engine-routes unambiguous On-Findings hints.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Mode-driven routing decision =====

// TestSession_OrchestratedMode_ConsultsRoutingOnEveryStep verifies that in
// orchestrated mode the RoutingConsultant is invoked for every step,
// including the first step of a new run, and the engine is never asked for
// routing (the consultant drives all decisions).
func TestSession_OrchestratedMode_ConsultsRoutingOnEveryStep(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do planning", 0)
	consultant.queueDispatch("agent-b", "do review", 1)
	consultant.queueStop("workflow complete")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

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

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant after orchestrator stop, got %q (message: %q)", got.Status, got.Message)
	}
	// Consultant must be called once per step decision: first step, after agent-a, after agent-b.
	if consultant.CallCount != 3 {
		t.Errorf("want 3 ConsultRouting calls, got %d", consultant.CallCount)
	}
}

// TestSession_OrchestratedMode_FirstStepConsultsWithNilLastMessage verifies
// that the very first consultation of a new orchestrated run carries a nil
// LastStatusMessage (there is no prior agent result at this point).
func TestSession_OrchestratedMode_FirstStepConsultsWithNilLastMessage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "start the work", 0)
	consultant.queueStop("done")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	if consultant.CallCount == 0 {
		t.Fatal("want at least one ConsultRouting call for the first step, got 0")
	}
	if consultant.Requests[0].LastStatusMessage != nil {
		t.Errorf("want nil LastStatusMessage on first consultation of a new run, got %v",
			consultant.Requests[0].LastStatusMessage)
	}
}

// TestSession_AutoMode_AllSuccess_EngineRoutesWithNoConsultation verifies that
// in auto mode an all-SUCCESS run completes entirely via the engine's routing
// without any consultation. The RoutingConsultant must not be called.
func TestSession_AutoMode_AllSuccess_EngineRoutesWithNoConsultation(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// No queue entries: any call to ConsultRouting returns a transport error.

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
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

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	if consultant.CallCount > 0 {
		t.Errorf("want 0 ConsultRouting calls for all-SUCCESS auto run, got %d", consultant.CallCount)
	}
}

// TestSession_AutoMode_NonSuccessStatus_TriggersConsultation verifies that in
// auto mode a non-SUCCESS agent result causes the RoutingConsultant to be
// called instead of the engine resolving the deviation.
func TestSession_AutoMode_NonSuccessStatus_TriggersConsultation(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// After the BLOCKED result, the consultant dispatches agent-a again then agent-b.
	consultant.queueDispatch("agent-a", "retry the work", 0)
	consultant.queueDispatch("agent-b", "now proceed", 1)
	// After agent-b completes, the consultant has no more entries; that is
	// acceptable if the engine completes the run instead.

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// First agent-a call returns BLOCKED (non-SUCCESS) → should trigger consultation.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "tool unavailable",
	}})
	// Second agent-a call (after consultant redispatches) → SUCCESS.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done on retry",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// The consultant must have been invoked at least once for the BLOCKED deviation.
	if consultant.CallCount == 0 {
		t.Errorf("want at least 1 ConsultRouting call after BLOCKED result in auto mode, got 0")
	}
}

// TestSession_AutoReviewMode_CNAWithUnambiguousHint_EngineAutoRoutes verifies
// that in auto-review mode a COMPLETED_NEEDS_ACTION result with an unambiguous
// On Findings hint is auto-routed by the engine without consulting the
// RoutingConsultant.
func TestSession_AutoReviewMode_CNAWithUnambiguousHint_EngineAutoRoutes(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// No entries: any unexpected consultation triggers an error response.

	dir := t.TempDir()
	// Reuse the loopback-style workflow content used in the existing On Findings test.
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
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "drafted",
	}})
	// agent-b returns CNA → On Findings hint "agent-a" (row above) → engine auto-routes (no consult).
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusCOMPLETED_NEEDS_ACTION,
		StatusMessage:   "found issues",
	}})
	// Second agent-a call (loop-back) → SUCCESS.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "issues resolved",
	}})
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

	if consultant.CallCount > 0 {
		t.Errorf("want 0 ConsultRouting calls for CNA with unambiguous On Findings in auto-review mode, got %d",
			consultant.CallCount)
	}
}
