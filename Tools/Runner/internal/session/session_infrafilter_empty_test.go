package session_test

// Tests for empty InfrastructureFilter, populated filter, and undeclared-agent
// key handling (split from session_infrafilter_test.go).
//
// Coverage:
//
//   Empty filter with Checkpoints=true (A-8 semantics):
//   - InfrastructureFilter=[]string{} (non-nil empty), Checkpoints=true:
//     filtering reduces the declared set to empty; validation against the empty
//     set finds no checkpoint provider and refuses the run.
//
//   Empty filter with Commits=true (A-8 semantics):
//   - InfrastructureFilter=[]string{} (non-nil empty), Commits=true: same A-8
//     semantics; refusal because filtered set is empty.
//
//   Empty filter with checkpoints and commits disabled:
//   - InfrastructureFilter=[]string{} (non-nil empty), Checkpoints=false,
//     Commits=false: filtered set is empty; no agents can trigger; run proceeds
//     to completion and no infrastructure agent is dispatched.
//
//   Populated filter restricts trigger evaluation:
//   - InfrastructureFilter=["checkpoint-manager-git"] with an orchestrator that
//     also declares a review-class agent (INVOCATION_INTERVAL:1, always active):
//     only checkpoint-manager-git participates in trigger evaluation; the
//     review-class agent is excluded from the filtered set and never dispatched.
//
//   Filter names undeclared agent -- silently ignored:
//   - InfrastructureFilter contains "checkpoint-manager-git" (declared) and
//     "nonexistent-agent" (not declared): the unknown key is simply absent from
//     the filtered set; no error occurs; the run proceeds with only
//     "checkpoint-manager-git" active. A debug event is emitted for the
//     unmatched key.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== Empty filter with Checkpoints=true (A-8 semantics) =====

// TestSession_Start_EmptyInfrastructureFilter_CheckpointsEnabled_ReturnsRefusal
// verifies that InfrastructureFilter=[]string{} (non-nil empty) combined with
// Checkpoints=true causes a run refusal. The empty filter reduces the declared
// agent set to empty; validation runs against that empty set and finds no
// checkpoint provider (A-8 semantics: validation against empty set, not "skip
// validation"). A test declaring Checkpoints=true with an empty infrastructure
// filter is a configuration error.
//
// RED-phase note: Without the filter implementation, checkpoint-manager-git
// remains in the declared set, checkpoint validation passes, and the run
// starts. The queued agent-a and agent-b responses allow the run to complete
// normally, so this test fails in RED with RunCompleted vs RunRefused -- a
// clear failure indicating the filter is not applied. After implementation,
// the empty filter removes all agents before validation, validation finds no
// checkpoint provider, and RunRefused is returned.
func TestSession_Start_EmptyInfrastructureFilter_CheckpointsEnabled_ReturnsRefusal(t *testing.T) {
	ses, f, _, orchPath := newCheckpointAgentSession(t)

	// Queue responses so that without the filter implementation the run proceeds
	// to RunCompleted, producing a clear RED failure (RunCompleted vs RunRefused)
	// rather than an incidental deviation-unresolved error.
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
	// Non-nil empty slice: all declared agents are filtered out.
	cfg.InfrastructureFilter = []string{}

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_EmptyInfrastructureFilter_CheckpointsEnabled_RefusalBeforeDispatch
// verifies that the empty-filter checkpoint refusal fires before any harness
// invocation, consistent with the run-start sequence contract.
func TestSession_Start_EmptyInfrastructureFilter_CheckpointsEnabled_RefusalBeforeDispatch(t *testing.T) {
	ses, f, _, orchPath := newCheckpointAgentSession(t)

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	cfg.InfrastructureFilter = []string{}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if len(f.Invocations()) != 0 {
		t.Errorf("want zero harness invocations (empty-filter refusal must fire before dispatch), got %d", len(f.Invocations()))
	}
}

// ===== Empty filter with Commits=true (A-8 semantics) =====

// TestSession_Start_EmptyInfrastructureFilter_CommitsEnabled_ReturnsRefusal
// verifies that InfrastructureFilter=[]string{} (non-nil empty) combined with
// Commits=true causes a run refusal. The empty filter produces an empty
// declared-agent set; validation finds no commit provider (A-8 semantics).
//
// RED-phase note: Without the filter implementation, commit-manager-git
// remains in the declared set, commit validation passes, commit setup dispatch
// fires and receives its queued response, and the workflow runs to completion.
// This test therefore fails in RED with RunCompleted vs RunRefused -- a clear,
// precise failure indicating the filter is not applied. After implementation,
// the empty filter removes all agents before validation, validation finds no
// commit provider, and RunRefused is returned before any dispatch.
func TestSession_Start_EmptyInfrastructureFilter_CommitsEnabled_ReturnsRefusal(t *testing.T) {
	ses, f, _, orchPath := newCommitSession(t)

	// Queue responses so that without the filter implementation the run proceeds
	// to RunCompleted, producing a clear RED failure (RunCompleted vs RunRefused)
	// rather than an incidental harness error from missing commit setup response.
	// The [branch:mosaic/run/filter-test] marker is required by doCommitSetupDispatch
	// to parse the branch name; without it commit setup fails before the workflow
	// executes, producing a vacuous pass rather than a clear RED failure.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "branch established [branch:mosaic/run/filter-test]",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseCommitConfig(orchPath)
	// Non-nil empty slice: commit-manager-git is filtered out.
	cfg.InfrastructureFilter = []string{}

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_EmptyInfrastructureFilter_CommitsEnabled_RefusalBeforeDispatch
// verifies that the empty-filter commit refusal fires before any harness
// invocation, including the commit setup dispatch.
func TestSession_Start_EmptyInfrastructureFilter_CommitsEnabled_RefusalBeforeDispatch(t *testing.T) {
	ses, f, _, orchPath := newCommitSession(t)

	cfg := baseCommitConfig(orchPath)
	cfg.InfrastructureFilter = []string{}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if len(f.Invocations()) != 0 {
		t.Errorf("want zero harness invocations (empty-filter refusal must fire before commit setup dispatch), got %d", len(f.Invocations()))
	}
}

// ===== Empty filter with checkpoints and commits disabled =====

// TestSession_Start_EmptyInfrastructureFilter_NoCheckpointsNoCommits_RunProceeds
// verifies that InfrastructureFilter=[]string{} (non-nil empty) combined with
// Checkpoints=false and Commits=false allows the run to proceed. The empty
// filter produces an empty declared-agent set; with no enabled classes, no
// validation check refuses the run and no triggers fire.
//
// This test uses the filter-checkpoint-review-orch.md fixture, which declares
// both a checkpoint-class agent (INVOCATION_INTERVAL:1) and a review-class
// agent (INVOCATION_INTERVAL:1, always active). With a nil filter, the
// review-class agent would fire on every workflow step (review class is not
// gated by Checkpoints). With an empty InfrastructureFilter, both agents are
// excluded from the filtered set and neither fires.
func TestSession_Start_EmptyInfrastructureFilter_NoCheckpointsNoCommits_RunProceeds(t *testing.T) {
	ses, f, _, orchPath := newCheckpointReviewSession(t)

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
	cfg.Checkpoints = false
	cfg.Commits = false
	// Non-nil empty filter: all declared agents are excluded from the active set.
	cfg.InfrastructureFilter = []string{}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// No infrastructure agent should have been dispatched. With an empty
	// InfrastructureFilter the declared set is empty, so evaluateTriggers sees
	// no agents and dispatches none.
	invs := f.Invocations()
	for _, inv := range invs {
		if inv.Agent.Identifier == "checkpoint-manager-git" || inv.Agent.Identifier == "review-agent" {
			t.Errorf("want no infrastructure agent dispatched with empty filter + checkpoints/commits disabled, got dispatch of %q", inv.Agent.Identifier)
		}
	}
}

// ===== Populated filter restricts trigger evaluation =====

// TestSession_Start_PopulatedInfrastructureFilter_OnlyFilteredAgentsTrigger
// verifies that when InfrastructureFilter names a specific agent, only that
// agent participates in trigger evaluation. Other declared agents -- even those
// of non-gated classes that would otherwise fire unconditionally -- are excluded
// from the active set and never dispatched.
//
// Fixture: filter-checkpoint-review-orch.md declares checkpoint-manager-git
// (checkpoint class, INVOCATION_INTERVAL:1) and review-agent (review class,
// INVOCATION_INTERVAL:1, always active without the filter).
//
// With InfrastructureFilter=["checkpoint-manager-git"] and Checkpoints=true:
// - checkpoint-manager-git is in the filtered set and fires after each of the
//   two workflow steps (INVOCATION_INTERVAL:1 condition: seq >= 1 from last
//   infra row).
// - review-agent is excluded from the filtered set and is never dispatched,
//   even though it would fire on every step without the filter.
func TestSession_Start_PopulatedInfrastructureFilter_OnlyFilteredAgentsTrigger(t *testing.T) {
	ses, f, _, orchPath := newCheckpointReviewSession(t)

	// Workflow step 1: agent-a.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// Infrastructure trigger after agent-a: checkpoint-manager-git (seq=2).
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})
	// Workflow step 2: agent-b.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// Infrastructure trigger after agent-b: checkpoint-manager-git (seq=4).
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	// Filter names only checkpoint-manager-git; review-agent is excluded.
	cfg.InfrastructureFilter = []string{"checkpoint-manager-git"}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// review-agent must not have been dispatched: it is excluded from the
	// filtered declared set, so evaluateTriggers never evaluates its triggers.
	reviewCount := infraFilterInvocationCountFor(f.Invocations(), "review-agent")
	if reviewCount != 0 {
		t.Errorf("want review-agent not dispatched (excluded from InfrastructureFilter), got %d invocation(s)", reviewCount)
	}

	// checkpoint-manager-git must have been dispatched exactly twice (after each
	// workflow step, matching INVOCATION_INTERVAL:1 semantics for a 2-step run).
	checkpointCount := infraFilterInvocationCountFor(f.Invocations(), "checkpoint-manager-git")
	if checkpointCount != 2 {
		t.Errorf("want checkpoint-manager-git dispatched exactly 2 times (INVOCATION_INTERVAL:1, 2 workflow steps), got %d", checkpointCount)
	}
}

// ===== Filter names undeclared agent -- silently ignored =====

// TestSession_Start_InfrastructureFilter_UndeclaredAgentKey_RunProceeds verifies
// that when InfrastructureFilter contains a key that does not match any declared
// infrastructure agent, the unknown key is silently absent from the filtered set
// (no error) and the run proceeds normally. Declared agents that ARE named in
// the filter continue to participate normally.
//
// The filter is an allowlist: an unmatched key does not generate a refusal.
// A debug event (EventSessionFilterUnmatched) is emitted for diagnostic
// purposes.
func TestSession_Start_InfrastructureFilter_UndeclaredAgentKey_RunProceeds(t *testing.T) {
	logger := &sessionRecordingLogger{}
	ses, f, _, orchPath := newIntervalAgentSessionWithDebug(t, logger)

	// Workflow step 1: agent-a.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// Infrastructure trigger after agent-a: checkpoint-manager-git (seq=2).
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})
	// Workflow step 2: agent-b.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// Infrastructure trigger after agent-b: checkpoint-manager-git (seq=4).
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	// Filter names the declared checkpoint agent and one undeclared key.
	// The undeclared key must be silently ignored; the declared agent must
	// remain active.
	cfg.InfrastructureFilter = []string{"checkpoint-manager-git", "nonexistent-agent"}

	got, err := ses.Start(context.Background(), cfg)

	// The undeclared key must not cause a refusal: the run proceeds normally.
	requireRunStatus(t, got, err, domain.RunCompleted)

	// checkpoint-manager-git must have fired twice (declared + in filter).
	checkpointCount := infraFilterInvocationCountFor(f.Invocations(), "checkpoint-manager-git")
	if checkpointCount != 2 {
		t.Errorf("want checkpoint-manager-git dispatched 2 times, got %d", checkpointCount)
	}

	// A debug event must be emitted for the unmatched filter key. The session
	// should log EventSessionFilterUnmatched for "nonexistent-agent" to provide
	// a diagnostic signal for typos in the filter.
	if !logger.eventLogged(domain.EventSessionFilterUnmatched) {
		t.Errorf("want EventSessionFilterUnmatched event logged for unmatched filter key %q, got no such event; logged events: %v",
			"nonexistent-agent", logger.allEvents())
	}

	// The field value for the unmatched key must identify it.
	if keyVal, ok := logger.fieldValue(domain.EventSessionFilterUnmatched, "key"); !ok || keyVal != "nonexistent-agent" {
		t.Errorf("want EventSessionFilterUnmatched event with key=%q, got key=%q (found=%v)",
			"nonexistent-agent", keyVal, ok)
	}
}
