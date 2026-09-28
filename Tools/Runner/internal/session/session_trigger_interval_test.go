package session_test

// Tests for INVOCATION_INTERVAL infrastructure agent trigger evaluation.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== INVOCATION_INTERVAL trigger tests =====
//
// Coverage:
//
//   INVOCATION_INTERVAL trigger:
//   - Fires after each workflow step when param=1 (interval=1 test).
//   - Fires at the exact threshold when global_sequence reaches param (boundary test).
//   - Does not fire when global_sequence is below param (vacuous RED).

// newHighIntervalSession builds a session backed by high-interval-orch.md,
// which declares checkpoint-manager-git with INVOCATION_INTERVAL:3 (halt policy).
// With only 2 workflow steps, this trigger must not fire.
func newHighIntervalSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "high-interval-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
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

// newTwoIntervalSession builds a session backed by two-interval-orch.md,
// which declares checkpoint-manager-git with INVOCATION_INTERVAL:2 (halt policy).
// The trigger fires once after the 2nd workflow step (global_sequence == param).
func newTwoIntervalSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "two-interval-orch.md")
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

// TestSession_Start_TriggerEval_INVOCATION_INTERVAL1_FiresAfterEachWorkflowStep
// verifies that an infrastructure agent with INVOCATION_INTERVAL:1 is dispatched
// after each workflow step, interleaved in the dispatch sequence. With 2 workflow
// steps, the infra agent must be dispatched exactly twice -- at positions 2 and 4
// in the invocation sequence (after each workflow step).
//
// The trigger matching rule: fires when (global_sequence - seq_of_last_infra_row) >= 1.
// With no prior infra row, fires when global_sequence >= 1 (i.e., after every step).
func TestSession_Start_TriggerEval_INVOCATION_INTERVAL1_FiresAfterEachWorkflowStep(t *testing.T) {
	ses, f, _, orchPath := newIntervalAgentSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// Triggered after agent-a (global_sequence=1 >= param=1, no prior infra row).
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// Triggered after agent-b (global_sequence=3, last infra at seq=2; 3-2=1 >= 1).
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true // enable checkpoint class so its triggers are evaluated

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) != 4 {
		t.Fatalf("want 4 invocations (agent-a, checkpoint, agent-b, checkpoint), got %d", len(invs))
	}
	if invs[1].Agent.Identifier != "checkpoint-manager-git" {
		t.Errorf("invocations[1]: want checkpoint-manager-git (fired after agent-a), got %q", invs[1].Agent.Identifier)
	}
	if invs[3].Agent.Identifier != "checkpoint-manager-git" {
		t.Errorf("invocations[3]: want checkpoint-manager-git (fired after agent-b), got %q", invs[3].Agent.Identifier)
	}
}

// TestSession_Start_TriggerEval_INVOCATION_INTERVAL_BoundaryAtExactThreshold
// verifies that an infrastructure agent with INVOCATION_INTERVAL:2 fires exactly
// once after the 2nd workflow step, when global_sequence reaches exactly param=2.
// This is a boundary test: the trigger fires at the threshold (>=), not strictly above.
func TestSession_Start_TriggerEval_INVOCATION_INTERVAL_BoundaryAtExactThreshold(t *testing.T) {
	ses, f, _, orchPath := newTwoIntervalSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// After agent-a (global_sequence=1), the rule checks: 1 >= 2? No -> does not fire.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// After agent-b (global_sequence=2), the rule checks: 2 >= 2? Yes -> fires.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) != 3 {
		t.Fatalf("want 3 invocations (agent-a, agent-b, checkpoint), got %d", len(invs))
	}
	if invs[2].Agent.Identifier != "checkpoint-manager-git" {
		t.Errorf("invocations[2]: want checkpoint-manager-git (fired at exact threshold), got %q", invs[2].Agent.Identifier)
	}
}

// TestSession_Start_TriggerEval_INVOCATION_INTERVAL_HighThreshold_DoesNotFireInShortRun
// verifies that an infrastructure agent with INVOCATION_INTERVAL:3 does not fire
// during a 2-step workflow run (global_sequence=2 < param=3).
//
// RED phase: this test passes vacuously because no trigger evaluation occurs.
// Once implementation is added, a bug firing below the threshold would be caught.
func TestSession_Start_TriggerEval_INVOCATION_INTERVAL_HighThreshold_DoesNotFireInShortRun(t *testing.T) {
	ses, f, _, orchPath := newHighIntervalSession(t)

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

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "checkpoint-manager-git" {
			t.Errorf("checkpoint-manager-git dispatched unexpectedly: INVOCATION_INTERVAL:3 must not fire with only 2 workflow steps (global_sequence=2 < param=3)")
		}
	}
}
