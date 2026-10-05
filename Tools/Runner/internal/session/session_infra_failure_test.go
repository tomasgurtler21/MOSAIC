package session_test

// Tests for infrastructure agent on_failure policy handling and
// checkpoint content-reference extraction.

import (
	"context"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== on_failure handling =====
//
// Coverage:
//
//   halt policy:
//   - When an infrastructure agent dispatch returns a non-SUCCESS status and the
//     agent's on_failure is "halt", the run stops with RunStopped after the
//     Execution Log row is written.
//   - Halt does not enter the deviation resolver.
//
//   continue policy:
//   - When an infrastructure agent dispatch returns a non-SUCCESS status and the
//     agent's on_failure is "continue", the dispatch loop continues to the next
//     workflow step. The run completes normally.

// newContinueInfraSession builds a session backed by continue-infra-orch.md,
// which declares checkpoint-manager-git with INVOCATION_INTERVAL:1 and
// on_failure=continue. A non-SUCCESS response from this agent must not halt
// the run; the dispatch loop should proceed to the next workflow step.
func newContinueInfraSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "continue-infra-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")
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

// TestSession_Start_InfraOnFailureHalt_StopsRun verifies that when an infrastructure
// agent dispatch returns a non-SUCCESS status and the declared on_failure policy is
// "halt", the session stops the run and returns RunStopped. The Execution Log row
// for the failed dispatch is written before the halt.
//
// The halt must stop the run before the next workflow agent (agent-b) is dispatched.
func TestSession_Start_InfraOnFailureHalt_StopsRun(t *testing.T) {
	ses, f, store, orchPath := newIntervalAgentSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// checkpoint-manager-git returns BLOCKED (non-SUCCESS). on_failure=halt -> run stops.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "checkpoint storage unavailable",
	}})
	// Queue agent-b so that the run would succeed if the halt were missing.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	// Halt policy must stop the run.
	requireRunStatus(t, got, err, domain.RunStopped)

	// The Execution Log row for the failed infra agent must be written before halt.
	if len(store.Applied) < 2 {
		t.Errorf("want at least 2 Applied steps (agent-a + failed checkpoint-manager-git) before halt, got %d", len(store.Applied))
	}

	// agent-b must not have been dispatched (halt fired before reaching it).
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-b" {
			t.Error("agent-b dispatched after halt: halt policy must stop the run before dispatching further workflow steps")
		}
	}
}

// TestSession_Start_InfraOnFailureContinue_WorkflowCompletesAfterInfraFailure verifies
// that when an infrastructure agent dispatch returns a non-SUCCESS status and the
// declared on_failure policy is "continue", the dispatch loop proceeds to the next
// workflow step without halting. The run completes normally (RunCompleted).
//
// The infra agent's Execution Log row must be written (failure is on record), but
// the session continues to dispatch the remaining workflow steps.
func TestSession_Start_InfraOnFailureContinue_WorkflowCompletesAfterInfraFailure(t *testing.T) {
	ses, f, _, orchPath := newContinueInfraSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// checkpoint-manager-git fails (non-SUCCESS). on_failure=continue -> proceed.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "checkpoint storage unavailable",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// After agent-b, INVOCATION_INTERVAL:1 fires again.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	// Continue policy must not halt; the run must complete.
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify that checkpoint-manager-git was dispatched and agent-b was dispatched
	// after the infra failure.
	infraCount := 0
	agentBDispatched := false
	for _, inv := range f.Invocations() {
		switch inv.Agent.Identifier {
		case "checkpoint-manager-git":
			infraCount++
		case "agent-b":
			agentBDispatched = true
		}
	}
	if infraCount == 0 {
		t.Error("want checkpoint-manager-git dispatched (even with on_failure=continue, the dispatch must occur before the policy is applied)")
	}
	if !agentBDispatched {
		t.Error("want agent-b dispatched after infra failure with continue policy, but it was not dispatched")
	}
}

// TestSession_Start_InfraOnFailureHalt_DoesNotInvokeDeviationResolver verifies that
// infrastructure agent failures are handled exclusively by the on_failure policy and
// never enter the deviation resolver. The deviation resolver handles workflow agent
// deviations only; infra failures follow their own path.
func TestSession_Start_InfraOnFailureHalt_DoesNotInvokeDeviationResolver(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "interval-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")

	f := harness.NewMockAdapter()
	store := &memStore{}

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// checkpoint-manager-git fails with halt policy.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "checkpoint storage unavailable",
	}})
	// Queue agent-b to prevent a deviation from an empty harness queue in RED phase.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	// Halt must produce RunStopped (not RunCompleted or RunDeviationUnresolved).
	requireRunStatus(t, got, err, domain.RunStopped)
}

// ===== Checkpoint content-reference extraction =====
//
// Coverage:
//
//   Marker present:
//   - When a checkpoint-class infrastructure agent's status_message contains the
//     [checkpoint:{sha}] marker pattern, the sha is extracted and recorded in the
//     Checkpoint column of that agent's own Execution Log row.
//
//   Marker absent:
//   - When the status_message contains no [checkpoint:{sha}] marker, the
//     Checkpoint column for that row remains empty (vacuous RED).

// TestSession_Start_CheckpointExtraction_MarkerPresent_ShaRecordedOnInfraRow verifies
// that when checkpoint-manager-git's status_message contains a [checkpoint:{sha}]
// marker, the sha is extracted and recorded in the CompletedStep.Checkpoint field of
// that agent's own dispatch row. The sha must appear only on the infra agent's row,
// not on the preceding workflow step's row.
func TestSession_Start_CheckpointExtraction_MarkerPresent_ShaRecordedOnInfraRow(t *testing.T) {
	ses, f, store, orchPath := newIntervalAgentSession(t)

	const wantSHA = "a1b2c3d4e5f6"

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planning done",
	}})
	// checkpoint-manager-git returns a message with the [checkpoint:{sha}] marker.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint saved [checkpoint:" + wantSHA + "] successfully",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint saved, no marker in this response",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Find the Applied step for the first checkpoint-manager-git dispatch.
	var infraStep *domain.CompletedStep
	for i := range store.Applied {
		step := &store.Applied[i]
		if strings.Contains(step.AgentInstance, "checkpoint-manager-git") && infraStep == nil {
			infraStep = step
		}
	}
	if infraStep == nil {
		t.Fatal("want checkpoint-manager-git Applied step recorded, but none found in store.Applied")
	}
	if infraStep.Checkpoint != wantSHA {
		t.Errorf("checkpoint-manager-git Applied step Checkpoint: want %q (extracted from [checkpoint:{sha}] marker), got %q",
			wantSHA, infraStep.Checkpoint)
	}
}

// TestSession_Start_CheckpointExtraction_MarkerAbsent_CheckpointColumnEmpty verifies
// that when checkpoint-manager-git's status_message contains no [checkpoint:{sha}]
// marker, the Checkpoint field of its Applied step remains empty (the Checkpoint
// column shows "-" in the rendered Execution Log).
//
// RED phase: this test passes vacuously because no infra agent is dispatched.
// Once implementation is added, a bug that always extracts a sha (even when absent)
// would populate the Checkpoint field incorrectly and cause this test to fail.
func TestSession_Start_CheckpointExtraction_MarkerAbsent_CheckpointColumnEmpty(t *testing.T) {
	ses, f, store, orchPath := newIntervalAgentSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// No [checkpoint:{sha}] marker in the status_message.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint attempted but no sha available",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "no checkpoint taken",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify that no checkpoint-manager-git step has a non-empty Checkpoint field.
	for i := range store.Applied {
		step := &store.Applied[i]
		if strings.Contains(step.AgentInstance, "checkpoint-manager-git") && step.Checkpoint != "" {
			t.Errorf("checkpoint-manager-git Applied step Checkpoint: want empty (no [checkpoint:{sha}] marker in response), got %q", step.Checkpoint)
		}
	}
}
