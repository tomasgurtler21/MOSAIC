package session_test

// Tests for Session.Start infrastructure filtering (RunConfig.InfrastructureFilter).
//
// Coverage:
//
//   Nil filter -- backwards-compatible:
//   - nil InfrastructureFilter with a declared checkpoint-class agent and
//     Checkpoints=true: all declared agents remain active; validation runs
//     against the full declared set; run proceeds. Existing behavior is
//     preserved.
//
//   Filter excludes checkpoint-class agent:
//   - InfrastructureFilter names no checkpoint-class agent, Checkpoints=true:
//     validation against the filtered set finds no checkpoint provider and
//     refuses the run.
//
//   Filter includes checkpoint-class agent:
//   - InfrastructureFilter names the checkpoint-class agent, Checkpoints=true:
//     validation passes and the run proceeds to completion.
//
//   Filter excludes commit-class agent:
//   - InfrastructureFilter names no commit-class agent, Commits=true:
//     validation against the filtered set finds no commit provider and refuses
//     the run.
//
//   Filter includes commit-class agent:
//   - InfrastructureFilter names the commit-class agent, Commits=true:
//     validation passes and the run proceeds (including commit setup dispatch).
//
//   Empty filter with Checkpoints=true (A-8 semantics):
//   - InfrastructureFilter=[]string{} (non-nil empty), Checkpoints=true:
//     filtering reduces the declared set to empty; validation against the empty
//     set finds no checkpoint provider and refuses the run. A test declaring
//     Checkpoints=true with an empty infrastructure filter is a configuration
//     error (A-8: validation runs against the filtered set, not "skips
//     validation").
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
//     review-class agent is excluded from the filtered set and never dispatched,
//     even though it would fire on every workflow step without the filter.
//
//   Filter names undeclared agent -- silently ignored:
//   - InfrastructureFilter contains "checkpoint-manager-git" (declared) and
//     "nonexistent-agent" (not declared): "nonexistent-agent" is simply absent
//     from the filtered set; no error occurs; the run proceeds with only
//     "checkpoint-manager-git" active. A debug event is emitted for the
//     unmatched key.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ---- helpers specific to infrastructure-filter tests ----

// newCheckpointReviewSession builds a session backed by the
// filter-checkpoint-review-orch.md fixture, which declares both a
// checkpoint-class agent (checkpoint-manager-git, INVOCATION_INTERVAL:1) and a
// review-class agent (review-agent, INVOCATION_INTERVAL:1). Agent definition
// files for all four participants -- agent-a, agent-b, checkpoint-manager-git,
// and review-agent -- are created in the temp directory. The FakeAdapter and
// memStore are returned so callers can configure scripted responses.
func newCheckpointReviewSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "filter-checkpoint-review-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")
	writeAgentFile(t, dir, "review-agent")

	f = harness.NewFakeAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	return
}

// newCheckpointReviewSessionWithDebug builds a session backed by the
// filter-checkpoint-review-orch.md fixture with the supplied debug logger
// wired into Deps.Debug. All four agent definition files are created in the
// temp directory. The FakeAdapter and memStore are returned so callers can
// configure scripted responses and inspect the run outcome.
func newCheckpointReviewSessionWithDebug(t *testing.T, debug domain.DebugLogger) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "filter-checkpoint-review-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")
	writeAgentFile(t, dir, "review-agent")

	f = harness.NewFakeAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    debug,
	})
	return
}

// newIntervalAgentSessionWithDebug builds a session backed by the
// interval-agent-orch.md fixture (declares checkpoint-manager-git with
// INVOCATION_INTERVAL:1) with the supplied debug logger wired into
// Deps.Debug. Agent definition files for agent-a, agent-b, and
// checkpoint-manager-git are created in the temp directory.
func newIntervalAgentSessionWithDebug(t *testing.T, debug domain.DebugLogger) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "interval-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")

	f = harness.NewFakeAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    debug,
	})
	return
}

// infraFilterInvocationCountFor returns the number of harness invocations
// whose agent identifier equals agentID.
func infraFilterInvocationCountFor(invocations []harness.Invocation, agentID string) int {
	count := 0
	for _, inv := range invocations {
		if inv.Agent.Identifier == agentID {
			count++
		}
	}
	return count
}

// ===== Nil filter (backwards-compatible) =====

// TestSession_Start_NilInfrastructureFilter_AllDeclaredAgentsActive verifies
// that when InfrastructureFilter is nil, all declared infrastructure agents
// remain active and the run proceeds without modification to the existing
// checkpoint-validation behavior. A nil filter must be a no-op (backwards-
// compatible).
func TestSession_Start_NilInfrastructureFilter_AllDeclaredAgentsActive(t *testing.T) {
	ses, f, _, orchPath := newCheckpointAgentSession(t)

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
	// InfrastructureFilter is nil (zero value) -- no filtering, all agents active.

	got, err := ses.Start(context.Background(), cfg)

	// checkpoint-agent-orch.md declares checkpoint-manager-git; with nil filter
	// all agents are active, so the checkpoint validation check sees a provider
	// and does not refuse.
	requireRunStatus(t, got, err, domain.RunCompleted)
}

// ===== Filter excludes checkpoint-class agent =====

// TestSession_Start_InfrastructureFilter_ExcludesCheckpointClass_CheckpointsEnabled_ReturnsRefusal
// verifies that when InfrastructureFilter names no checkpoint-class agent and
// Checkpoints=true, the session refuses the run. After applying the filter, the
// declared agent set contains no checkpoint provider, so the checkpoint
// validation check fires.
//
// RED-phase note: Without the filter implementation, checkpoint-manager-git
// remains in the declared set, checkpoint validation passes, and the run
// starts. The queued agent-a and agent-b responses allow the run to complete
// normally, so this test fails in RED with RunCompleted vs RunRefused -- a
// clear failure that precisely indicates the filter is not applied. After
// implementation, the filter removes checkpoint-manager-git before validation,
// validation finds no checkpoint provider, and RunRefused is returned.
func TestSession_Start_InfrastructureFilter_ExcludesCheckpointClass_CheckpointsEnabled_ReturnsRefusal(t *testing.T) {
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
	// Filter names a key that does not match checkpoint-manager-git; after
	// filtering, the declared set has no checkpoint-class agent.
	cfg.InfrastructureFilter = []string{"nonexistent-agent"}

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_InfrastructureFilter_ExcludesCheckpointClass_NoHarnessInvocation
// verifies that the infrastructure-filter refusal fires before any harness
// invocation. The filter is applied in the run-start sequence before the
// dispatch loop, so the harness must remain idle.
func TestSession_Start_InfrastructureFilter_ExcludesCheckpointClass_NoHarnessInvocation(t *testing.T) {
	ses, f, _, orchPath := newCheckpointAgentSession(t)

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	cfg.InfrastructureFilter = []string{"nonexistent-agent"}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if len(f.Invocations()) != 0 {
		t.Errorf("want zero harness invocations (filter refusal must fire before dispatch), got %d", len(f.Invocations()))
	}
}

// ===== Filter includes checkpoint-class agent =====

// TestSession_Start_InfrastructureFilter_IncludesCheckpointClass_CheckpointsEnabled_RunProceeds
// verifies that when InfrastructureFilter names the checkpoint-class agent and
// Checkpoints=true, the session does not refuse and the run proceeds to
// completion. The filter includes the required checkpoint provider, so the
// checkpoint validation check passes.
func TestSession_Start_InfrastructureFilter_IncludesCheckpointClass_CheckpointsEnabled_RunProceeds(t *testing.T) {
	ses, f, _, orchPath := newCheckpointAgentSession(t)

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
	// Filter explicitly names the checkpoint-class agent; the filtered set
	// contains the required provider.
	cfg.InfrastructureFilter = []string{"checkpoint-manager-git"}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
}

// ===== Filter excludes commit-class agent =====

// TestSession_Start_InfrastructureFilter_ExcludesCommitClass_CommitsEnabled_ReturnsRefusal
// verifies that when InfrastructureFilter names no commit-class agent and
// Commits=true, the session refuses the run. After applying the filter, the
// declared agent set contains no commit provider, so the commit validation
// check fires.
//
// RED-phase note: Without the filter implementation, commit-manager-git
// remains in the declared set, commit validation passes, commit setup dispatch
// fires and receives its queued response, and the workflow runs to completion.
// This test therefore fails in RED with RunCompleted vs RunRefused -- a clear,
// precise failure indicating the filter is not applied. After implementation,
// the filter removes commit-manager-git before validation, validation finds no
// commit provider, and RunRefused is returned before any dispatch.
func TestSession_Start_InfrastructureFilter_ExcludesCommitClass_CommitsEnabled_ReturnsRefusal(t *testing.T) {
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
	// Filter names a key that does not match commit-manager-git; after
	// filtering, the declared set has no commit-class agent.
	cfg.InfrastructureFilter = []string{"nonexistent-agent"}

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_InfrastructureFilter_ExcludesCommitClass_NoHarnessInvocation
// verifies that the commit-class exclusion refusal fires before any harness
// invocation, including the commit setup dispatch that precedes artifact
// creation.
func TestSession_Start_InfrastructureFilter_ExcludesCommitClass_NoHarnessInvocation(t *testing.T) {
	ses, f, _, orchPath := newCommitSession(t)

	cfg := baseCommitConfig(orchPath)
	cfg.InfrastructureFilter = []string{"nonexistent-agent"}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if len(f.Invocations()) != 0 {
		t.Errorf("want zero harness invocations (filter refusal must fire before commit setup dispatch), got %d", len(f.Invocations()))
	}
}

// ===== Filter includes commit-class agent =====

// TestSession_Start_InfrastructureFilter_IncludesCommitClass_CommitsEnabled_RunProceeds
// verifies that when InfrastructureFilter names the commit-class agent and
// Commits=true, the session does not refuse and the run proceeds to completion,
// including the commit setup dispatch. The filter includes the required commit
// provider, so the commit validation check passes.
func TestSession_Start_InfrastructureFilter_IncludesCommitClass_CommitsEnabled_RunProceeds(t *testing.T) {
	ses, f, _, orchPath := newCommitSession(t)

	const wantBranch = "mosaic/run/filter-test"
	// Commit setup dispatch happens before the main dispatch loop.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "branch established [branch:" + wantBranch + "]",
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
	// Filter explicitly names the commit-class agent; the filtered set contains
	// the required provider.
	cfg.InfrastructureFilter = []string{"commit-manager-git"}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
}

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
