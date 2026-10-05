package session_test

// Tests for run-start agent-per-class selection: gated class refusal when
// multiple agents are declared without a selection, auto-selection of a single
// agent, and non-gated review agents firing unconditionally.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Run-start agent-per-class selection =====
//
// Coverage for buildActiveAgentsFilter behaviour surfaced through the
// session dispatch loop, and session-level refusal when multiple agents of the
// same gated class are declared without a class selection.

// newMultiCheckpointSession builds a session backed by multi-checkpoint-orch.md,
// which declares two checkpoint-class infrastructure agents
// (checkpoint-manager-git with INVOCATION_INTERVAL:1 halt, and
// checkpoint-manager-alt with INVOCATION_INTERVAL:1 continue).
// Agent files for agent-a and agent-b are written into the temp dir.
func newMultiCheckpointSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "multi-checkpoint-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")
	writeAgentFile(t, dir, "checkpoint-manager-alt")
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

// newReviewClassSession builds a session backed by review-class-orch.md,
// which declares two review-class infrastructure agents (review-agent-a and
// review-agent-b, both with INVOCATION_INTERVAL:1 continue).
// Agent files for agent-a and agent-b are written into the temp dir.
//
// Routing is intentionally not wired (nil). Tests that use this helper
// verify review-class behavior without post-review routing consultation;
// Stage-2 behavior (consultation when Routing != nil) is covered by
// session_review_consult_test.go.
func newReviewClassSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "review-class-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "review-agent-a")
	writeAgentFile(t, dir, "review-agent-b")
	f = harness.NewMockAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		// Routing: nil -- no post-review consultation; run proceeds without consultant.
	})
	return
}

// --- Refusal when multiple same-class agents, no selection ---

// TestSession_Start_MultipleGatedClassAgents_NoClassSelection_ReturnsRefusal
// verifies that when multiple agents of the same gated class (checkpoint) are
// declared and RunConfig.InfraClassSelections contains no entry for that class,
// the session refuses to start before dispatching any workflow step.
//
// The refusal must be a run-start refusal: no harness invocations may occur.
// Without the check the session proceeds, checkpoint-manager-git fires after
// agent-a with INVOCATION_INTERVAL:1 -- but the MockAdapter has no response
// queued for it, causing a halt (RunStopped). The test asserts RunRefused, so
// the RED failure is clear: got RunStopped instead of RunRefused.
func TestSession_Start_MultipleGatedClassAgents_NoClassSelection_ReturnsRefusal(t *testing.T) {
	ses, f, _, orchPath := newMultiCheckpointSession(t)

	// Queue only the workflow agents. In RED, trigger evaluation fires for
	// checkpoint-manager-git after agent-a completes; MockAdapter returns an error
	// (no entry queued), which is treated as halt -> RunStopped. In GREEN the
	// session refuses at run-start, so agent-a is never dispatched.
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

	// No InfraClassSelections: multiple checkpoint agents declared, no selection.
	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	cfg.InfraClassSelections = nil

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)

	// The refusal must fire before any dispatch. Zero invocations in GREEN;
	// non-zero in RED (agent-a IS dispatched before trigger fires).
	if len(f.Invocations()) != 0 {
		t.Errorf("want zero harness invocations (class-selection refusal must fire before dispatch), got %d", len(f.Invocations()))
	}
}

// TestSession_Start_MultipleGatedClassAgents_WithClassSelection_OnlySelectedFires
// verifies that when multiple checkpoint-class agents are declared and
// InfraClassSelections specifies one of them, only the selected agent's triggers
// are evaluated and it is the only checkpoint agent dispatched.
//
// In RED (nil activeAgents filter): both agents are evaluated, checkpoint-manager-alt
// is dispatched. MockAdapter has no response queued for checkpoint-manager-alt; it
// returns an error treated as BLOCKED with continue policy, recording an invocation.
// The assertion "checkpoint-manager-alt not dispatched" catches the RED failure.
//
// In GREEN: buildActiveAgentsFilter returns {checkpoint-manager-git: true};
// checkpoint-manager-alt is skipped by evaluateTriggers; only checkpoint-manager-git
// fires. The MockAdapter queue is consumed without error and the run completes.
func TestSession_Start_MultipleGatedClassAgents_WithClassSelection_OnlySelectedFires(t *testing.T) {
	ses, f, _, orchPath := newMultiCheckpointSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
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
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	cfg.InfraClassSelections = map[string]string{"checkpoint": "checkpoint-manager-git"}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// checkpoint-manager-alt must never be dispatched (it was not selected).
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "checkpoint-manager-alt" {
			t.Errorf("checkpoint-manager-alt dispatched: only the selected agent (checkpoint-manager-git) may fire; the non-selected checkpoint agent must be inactive")
		}
	}
}

// --- Single gated-class agent: auto-selected, no refusal ---

// TestSession_Start_SingleGatedClassAgent_AutoSelected_RunProceeds verifies that
// when exactly one checkpoint-class agent is declared and InfraClassSelections
// is nil, the session auto-selects that agent without prompting and the run
// proceeds normally. The single agent's triggers are evaluated.
//
// This is a regression test: the session must not refuse when every gated class
// has exactly one declared agent, regardless of whether InfraClassSelections is nil.
func TestSession_Start_SingleGatedClassAgent_AutoSelected_RunProceeds(t *testing.T) {
	// checkpoint-agent-orch.md declares one checkpoint-class agent.
	ses, f, _, orchPath := newCheckpointAgentSession(t)

	// Expected GREEN dispatch: agent-a -> agent-b. The STAGE_END trigger on
	// checkpoint-manager-git does not fire here because this is a linear (non-staged)
	// PLANNING workflow -- STAGE_END only applies within EXECUTION phases that have
	// a stage structure. Without EXECUTION stages, the look-ahead finds no stage
	// boundary and the checkpoint agent is never dispatched. That is correct
	// behaviour: the trigger contract is unrelated to auto-selection.
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
	cfg.InfraClassSelections = nil // no selection needed for a single-agent class

	got, err := ses.Start(context.Background(), cfg)

	// Must not refuse: single checkpoint-class agent is auto-selected.
	requireRunStatus(t, got, err, domain.RunCompleted)
}

// --- Non-gated review agents: always fire, unaffected by selection ---

// TestSession_Start_NonGatedReviewAgents_MultipleAgents_BothFireWithoutSelection
// verifies that review-class agents are not subject to the at-most-one-per-class
// selection rule. When multiple review-class agents are declared and
// InfraClassSelections is nil, the session proceeds without refusal and both
// review agents are dispatched (their triggers evaluate unconditionally).
//
// This test passes in both RED and GREEN because the selection logic does not
// apply to non-gated classes. It provides regression protection: if the selection
// logic were incorrectly applied to the review class, the run would either refuse
// or filter out one of the review agents.
func TestSession_Start_NonGatedReviewAgents_MultipleAgents_BothFireWithoutSelection(t *testing.T) {
	ses, f, _, orchPath := newReviewClassSession(t)

	// With INVOCATION_INTERVAL:1, both review agents fire after each workflow step.
	// Dispatch sequence: agent-a -> review-a -> review-b -> agent-b -> review-a -> review-b.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.InfraClassSelections = nil // no selection required for non-gated classes

	got, err := ses.Start(context.Background(), cfg)

	// Must not refuse: review class is non-gated, multiple agents are fine.
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Both review agents must have been dispatched.
	reviewADispatched := false
	reviewBDispatched := false
	for _, inv := range f.Invocations() {
		switch inv.Agent.Identifier {
		case "review-agent-a":
			reviewADispatched = true
		case "review-agent-b":
			reviewBDispatched = true
		}
	}
	if !reviewADispatched {
		t.Error("want review-agent-a dispatched (non-gated class, always fires), but it was not")
	}
	if !reviewBDispatched {
		t.Error("want review-agent-b dispatched (non-gated class, always fires), but it was not")
	}
}
