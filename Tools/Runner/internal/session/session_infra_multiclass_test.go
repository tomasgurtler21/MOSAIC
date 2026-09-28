package session_test

// Tests for multi-class infrastructure agent selection: two gated classes
// simultaneously and mixed gated/non-gated classes.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// newMultiClassMixedSession builds a session backed by multi-class-mixed-orch.md,
// which declares two checkpoint-class agents (checkpoint-manager-git halt,
// checkpoint-manager-alt continue) and one review-class agent (review-agent
// continue), all with INVOCATION_INTERVAL:1.
// Agent files for agent-a and agent-b are written into the temp dir.
func newMultiClassMixedSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "multi-class-mixed-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")
	writeAgentFile(t, dir, "checkpoint-manager-alt")
	writeAgentFile(t, dir, "review-agent")
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

// newTwoGatedClassesSession builds a session backed by multi-two-gated-classes-orch.md,
// which declares two checkpoint-class agents (checkpoint-manager-git halt,
// checkpoint-manager-alt continue) AND two commit-class agents (commit-manager-git halt,
// commit-manager-alt continue), all with INVOCATION_INTERVAL:1.
// Agent files for agent-a and agent-b are written into the temp dir.
func newTwoGatedClassesSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "multi-two-gated-classes-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")
	writeAgentFile(t, dir, "checkpoint-manager-alt")
	writeAgentFile(t, dir, "commit-manager-git")
	writeAgentFile(t, dir, "commit-manager-alt")
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

// TestSession_Start_TwoGatedClasses_BothSelectedFire verifies the boundary case
// where TWO gated classes each have multiple declared agents simultaneously:
// two checkpoint-class agents and two commit-class agents. The filter must handle
// both classes independently -- the selected checkpoint agent fires, the selected
// commit agent fires, and neither non-selected agent fires.
//
// This is a regression guard against cross-class interaction bugs: a bug that
// causes one class's filter to bleed into another class's filter would be caught
// by verifying that the commit-class selection is honoured independently of the
// checkpoint-class selection.
//
// In RED (nil filter): all four infrastructure agents are evaluated and dispatched
// on each trigger. The assertions that non-selected agents were not dispatched
// catch the RED failures.
//
// In GREEN: buildActiveAgentsFilter returns
//   {"checkpoint-manager-git": true, "commit-manager-git": true}
// and only those two agents' triggers are evaluated per workflow step.
func TestSession_Start_TwoGatedClasses_BothSelectedFire(t *testing.T) {
	ses, f, _, orchPath := newTwoGatedClassesSession(t)

	// Queue the expected GREEN dispatch sequence.
	// With INVOCATION_INTERVAL:1, one checkpoint agent and one commit agent fire
	// after each of the two workflow steps:
	//   agent-a -> checkpoint-manager-git -> commit-manager-git ->
	//   agent-b -> checkpoint-manager-git -> commit-manager-git -> COMPLETE
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
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit done",
	}})
	// The non-selected agents (checkpoint-manager-alt, commit-manager-alt) are
	// intentionally NOT queued. In RED they fire (nil filter) and MockAdapter records
	// their invocations, allowing the assertions below to catch the RED failure.

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	cfg.InfraClassSelections = map[string]string{
		"checkpoint": "checkpoint-manager-git",
		"commit":     "commit-manager-git",
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Neither non-selected agent may be dispatched.
	for _, inv := range f.Invocations() {
		switch inv.Agent.Identifier {
		case "checkpoint-manager-alt":
			t.Errorf("checkpoint-manager-alt dispatched: only the selected checkpoint agent (checkpoint-manager-git) may fire")
		case "commit-manager-alt":
			t.Errorf("commit-manager-alt dispatched: only the selected commit agent (commit-manager-git) may fire")
		}
	}

	// Both selected agents must have been dispatched (at least once).
	checkpointFired := false
	commitFired := false
	for _, inv := range f.Invocations() {
		switch inv.Agent.Identifier {
		case "checkpoint-manager-git":
			checkpointFired = true
		case "commit-manager-git":
			commitFired = true
		}
	}
	if !checkpointFired {
		t.Error("want checkpoint-manager-git dispatched (selected for checkpoint class), but it was not")
	}
	if !commitFired {
		t.Error("want commit-manager-git dispatched (selected for commit class), but it was not")
	}
}

// TestSession_Start_MixedClasses_SelectedGatedFires_NonGatedAlwaysFires verifies
// the combined case: two checkpoint-class agents (gated) and one review-class agent
// (non-gated). With InfraClassSelections specifying checkpoint-manager-git, only
// that agent's triggers are evaluated for the checkpoint class; review-agent fires
// unconditionally.
//
// In RED (nil filter): all three agents are evaluated. checkpoint-manager-alt fires
// after each workflow step (INVOCATION_INTERVAL:1) but has no scripted response;
// MockAdapter records the invocation and returns an error treated as BLOCKED+continue.
// The assertion "checkpoint-manager-alt not dispatched" catches the RED failure.
//
// In GREEN: buildActiveAgentsFilter returns {checkpoint-manager-git: true,
// review-agent: true}; checkpoint-manager-alt is skipped; only checkpoint-manager-git
// and review-agent fire. The queued responses are consumed and the run completes.
func TestSession_Start_MixedClasses_SelectedGatedFires_NonGatedAlwaysFires(t *testing.T) {
	ses, f, _, orchPath := newMultiClassMixedSession(t)

	// Queue the expected GREEN dispatch sequence:
	// agent-a -> checkpoint-manager-git -> review-agent -> agent-b ->
	// checkpoint-manager-git -> review-agent -> COMPLETE.
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
	f.Queue("review-agent", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})
	f.Queue("review-agent", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	// checkpoint-manager-alt intentionally NOT queued. In RED it fires and its
	// invocation is recorded (MockAdapter records before checking the queue),
	// allowing the assertion below to detect the RED failure.

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	cfg.InfraClassSelections = map[string]string{"checkpoint": "checkpoint-manager-git"}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// checkpoint-manager-alt must never be dispatched (not selected).
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "checkpoint-manager-alt" {
			t.Errorf("checkpoint-manager-alt dispatched: only the selected checkpoint agent (checkpoint-manager-git) may fire; checkpoint-manager-alt was not selected")
		}
	}

	// review-agent must always be dispatched (non-gated class, not filtered).
	reviewAgentDispatched := false
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "review-agent" {
			reviewAgentDispatched = true
			break
		}
	}
	if !reviewAgentDispatched {
		t.Error("want review-agent dispatched (non-gated class, always fires regardless of selection), but it was not")
	}
}
