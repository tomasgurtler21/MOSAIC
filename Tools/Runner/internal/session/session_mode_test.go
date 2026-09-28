package session_test

// Tests for mode and commit-availability validation at run start.
// Covers: ModeUnset refusals, CommitsEnabled without a commit-class agent,
// and CommitsDefault (silently disabled) when no commit provider is declared.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== Run configuration settling =====

// TestSession_Start_ModeUnset_ReturnsRefusal verifies that when RunSettings.Mode
// is not set (ExecutionModeUnset = ""), session.Start refuses the run. Mode is
// required with no default; its absence is always a refusal, regardless of the
// workflow or the presence of infrastructure agents.
func TestSession_Start_ModeUnset_ReturnsRefusal(t *testing.T) {
	ses, f, _, orchPath := newLinearSession(t)

	// Script both agents to return SUCCESS, so the test fails only on the
	// refusal assertion and not by running out of scripted responses.
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
	cfg.Mode = domain.ExecutionModeUnset // intentionally unset to trigger the refusal

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_ModeUnset_RefusalBeforeHarnessInvocation verifies the ordering
// contract: the mode check must occur before any harness invocation. A missing
// mode must be caught in the run-start sequence, before the dispatch loop begins.
//
// The test asserts two things: (1) the outcome is RunRefused (not
// RunDeviationUnresolved or any other status that would indicate the run
// stopped for a different reason) and (2) no harness invocation occurred.
// Without an explicit RunRefused assertion, the test could pass vacuously if
// an earlier engine change stops the run via deviation before dispatch, making
// it appear the ordering constraint is enforced when it is not.
func TestSession_Start_ModeUnset_RefusalBeforeHarnessInvocation(t *testing.T) {
	ses, f, _, orchPath := newLinearSession(t)

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeUnset // intentionally unset to trigger the refusal

	got, err := ses.Start(context.Background(), cfg)

	// Must return RunRefused — not RunDeviationUnresolved — confirming that the
	// run-start mode check is what stopped the run, not a later engine decision.
	requireRefused(t, got, err)

	if len(f.Invocations()) != 0 {
		t.Errorf("want zero harness invocations when mode is unset, got %d "+
			"(ordering violation: mode check must precede harness dispatch)",
			len(f.Invocations()))
	}
}

// TestSession_Start_ModeUnset_RefusalBeforeArtifactCreated verifies that the mode
// check fires before ArtifactStore.Create is called. No artifact should exist when
// the run is refused for a missing mode.
func TestSession_Start_ModeUnset_RefusalBeforeArtifactCreated(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeUnset // intentionally unset to trigger the refusal

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if store.exists {
		t.Error("want no artifact created when mode is unset (refusal must precede Store.Create)")
	}
}

// TestSession_Start_CommitsEnabled_NoCommitClassAgent_ReturnsRefusal verifies that
// requesting commits when no commit-class infrastructure agent is declared in the
// orchestrator file refuses the run. A run cannot enable commits if no commit
// provider is available.
func TestSession_Start_CommitsEnabled_NoCommitClassAgent_ReturnsRefusal(t *testing.T) {
	ses, _, _, orchPath := newLinearSession(t) // linear-orch.md has no infra agents

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.Commits = true // request commits — but no commit-class agent is declared

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_CommitsEnabled_NoCommitClassAgent_NoArtifactCreated verifies
// that the commits-without-provider refusal fires before any artifact is created.
// The run-start sequence must enforce this precondition before Store.Create so
// that no partial run state is left behind.
func TestSession_Start_CommitsEnabled_NoCommitClassAgent_NoArtifactCreated(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.Commits = true

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if store.exists {
		t.Error("want no artifact created when commits are enabled but no commit-class agent is declared")
	}
}

// TestSession_Start_CommitsDefault_NoCommitClassAgent_RunProceeds verifies that
// when no commit-class agent is declared and commits are disabled (the zero value
// of RunSettings.Commits), the run proceeds normally. Commits are silently
// disabled — no refusal occurs and the workflow completes.
//
// The spec says "silently false" — the silence is about not asking the user, but
// the created artifact must reflect the actual settled value (Commits=false).
func TestSession_Start_CommitsDefault_NoCommitClassAgent_RunProceeds(t *testing.T) {
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

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	// Commits is false by default — no commit provider required.

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// The artifact must record Commits=false. The "silently disabled" contract
	// means the user is not asked, but the persisted setting must still reflect
	// the actual value so a resumed run does not misread its configuration.
	if store.state.Commits {
		t.Error("want artifact Commits=false when no commit-class agent is declared (silently disabled), got Commits=true")
	}
}
