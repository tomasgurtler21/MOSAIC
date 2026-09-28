package session_test

// Tests for commit setup dispatch at run start.
// Covers: branch marker extraction, first-log-row recording, harness errors,
// Apply failures, disabled commits, and .agent.md extension resolution.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Commit setup dispatch =====

// TestSession_Start_CommitsEnabled_SuccessfulSetup_BranchRecordedInArtifact
// verifies that when commits are enabled and the commit-class agent returns a
// [branch:{name}] marker in its status_message, the reported branch name is
// stored as CommitBranch in the created artifact. This is the core success path
// for the commit setup dispatch.
func TestSession_Start_CommitsEnabled_SuccessfulSetup_BranchRecordedInArtifact(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	const wantBranch = "mosaic/run/test-run-id"

	// Commit setup dispatch returns a branch marker.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "branch established [branch:" + wantBranch + "]",
	}})
	// Workflow agents for the dispatch loop that follows setup.
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
	cfg.RunID = "test-run-id"

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if store.state.CommitBranch != wantBranch {
		t.Errorf("want CommitBranch=%q in artifact after successful commit setup, got %q",
			wantBranch, store.state.CommitBranch)
	}
}

// TestSession_Start_CommitsEnabled_SuccessfulSetup_IsFirstLogRow verifies that
// the commit setup dispatch is recorded as the first execution log row, with
// IsInfrastructure=true, before any workflow step is logged.
func TestSession_Start_CommitsEnabled_SuccessfulSetup_IsFirstLogRow(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	const wantBranch = "mosaic/run/test-run-id"

	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "branch ready [branch:" + wantBranch + "]",
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
	cfg.RunID = "test-run-id"

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// store.Applied records every CompletedStep passed to Apply, including
	// infrastructure steps (IsInfrastructure=true). The commit setup dispatch
	// must be the first applied step and must be marked as infrastructure.
	if len(store.Applied) == 0 {
		t.Fatal("want at least one applied step, got none")
	}
	first := store.Applied[0]
	if !first.IsInfrastructure {
		t.Errorf("want first applied step to be infrastructure (commit setup), got IsInfrastructure=false (agent=%q)", first.AgentInstance)
	}
	if !strings.Contains(first.AgentInstance, "commit-manager-git") {
		t.Errorf("want first applied step agent to contain \"commit-manager-git\", got %q", first.AgentInstance)
	}
	// Seq must be 1: the commit setup dispatch is the first recorded row.
	// The ContractsDesign specifies the commit setup dispatch Seq is always 1.
	if first.Seq != 1 {
		t.Errorf("want commit setup dispatch Seq=1 (first row), got Seq=%d", first.Seq)
	}
	// Status must mirror the dispatch's own status code (SUCCESS in this case).
	if first.Status != domain.StatusSUCCESS {
		t.Errorf("want commit setup dispatch Status=%q, got %q", domain.StatusSUCCESS, first.Status)
	}
}

// TestSession_Start_CommitsEnabled_MissingBranchMarker_ReturnsRefusal verifies
// that when the commit-class agent's setup dispatch returns SUCCESS but its
// status_message does not contain a [branch:{name}] marker, the run is refused.
// A missing marker means the branch was not established, so the run cannot
// safely proceed.
func TestSession_Start_CommitsEnabled_MissingBranchMarker_ReturnsRefusal(t *testing.T) {
	ses, f, _, orchPath := newCommitSession(t)

	// The commit agent returns SUCCESS but omits the [branch:{name}] marker.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "setup complete (no branch marker)",
	}})

	cfg := baseCommitConfig(orchPath)

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_CommitsEnabled_MissingBranchMarker_NoArtifactCreated verifies
// that a missing branch marker refuses the run before ArtifactStore.Create is
// called. No artifact should exist so a refused run leaves no trace.
func TestSession_Start_CommitsEnabled_MissingBranchMarker_NoArtifactCreated(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "no marker here",
	}})

	cfg := baseCommitConfig(orchPath)

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if store.exists {
		t.Error("want no artifact created when commit setup dispatch has no branch marker")
	}
}

// TestSession_Start_CommitsEnabled_HarnessError_ReturnsRefusal verifies that
// when the harness fails during the commit setup dispatch, the run is refused.
// The failure is immediately terminal with no retry.
func TestSession_Start_CommitsEnabled_HarnessError_ReturnsRefusal(t *testing.T) {
	ses, f, _, orchPath := newCommitSession(t)

	// The harness fails entirely for the commit setup dispatch.
	f.Queue("commit-manager-git", harness.ScriptedEntry{
		Err: errors.New("git: authentication failed"),
	})

	cfg := baseCommitConfig(orchPath)

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_CommitsEnabled_HarnessError_NoArtifactCreated verifies that
// a harness error during commit setup refuses the run before any artifact is
// created. The failure is terminal and no state survives.
func TestSession_Start_CommitsEnabled_HarnessError_NoArtifactCreated(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	f.Queue("commit-manager-git", harness.ScriptedEntry{
		Err: errors.New("git: timeout"),
	})

	cfg := baseCommitConfig(orchPath)

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if store.exists {
		t.Error("want no artifact created when commit setup dispatch harness fails")
	}
}

// TestSession_Start_CommitsEnabled_SetupApplyFailure_ReturnsRefusal verifies that
// when the commit setup dispatch succeeds (branch marker extracted) but recording
// the dispatch as the first execution log row fails (ArtifactStore.Apply returns
// an error), the run is refused and the run folder is removed. Recording failure
// is terminal per the Plan's Risks table (§1): the run folder is removed and no
// partial state survives.
//
// The test is in the RED phase: without commit setup row recording (I5.2), the
// first Apply call happens for a workflow step, not the commit setup row, and the
// run does not return RunRefused from this path.
func TestSession_Start_CommitsEnabled_SetupApplyFailure_ReturnsRefusal(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	// Create a real run folder that the session must remove on Apply failure.
	dir := t.TempDir()
	runFolder := filepath.Join(dir, "run")
	if err := os.MkdirAll(runFolder, 0o755); err != nil {
		t.Fatalf("setup: failed to create run folder: %v", err)
	}

	// Configure the store to fail the very first Apply call. When I5.2 is
	// implemented, that first call will be the commit setup row recording;
	// the session must treat this as a terminal failure and refuse the run.
	store.applyErrOnFirst = true
	store.applyFirstErr = errors.New("disk full: unable to record commit setup row")

	const wantBranch = "mosaic/run/test-run-id"
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "branch ready [branch:" + wantBranch + "]",
	}})

	cfg := baseCommitConfig(orchPath)
	cfg.RunID = "test-run-id"
	cfg.RunFolder = runFolder

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)

	// The session must remove the run folder on Apply failure so that a refused
	// run leaves no trace on disk. This mirrors the pre-consultation failure
	// contract (os.RemoveAll(cfg.RunFolder) on any terminal pre-dispatch failure).
	if _, statErr := os.Stat(runFolder); !os.IsNotExist(statErr) {
		t.Error("want run folder removed when commit setup row Apply fails (failure must remove any run folder)")
	}
}

// TestSession_Start_CommitsDisabled_NoCommitAgentDispatchedAtStart verifies that
// when commits are disabled, the commit-class agent is not invoked during run
// start. Only workflow agents appear in the harness invocation log.
func TestSession_Start_CommitsDisabled_NoCommitAgentDispatchedAtStart(t *testing.T) {
	ses, f, _, orchPath := newCommitSession(t)

	// No commit-manager-git entry queued: any invocation would return a harness
	// error, causing the test to observe the wrong failure mode.
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

	cfg := baseCommitConfig(orchPath)
	cfg.Commits = false // disable commits

	ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := f.Invocations()
	for _, inv := range invs {
		if strings.Contains(inv.Agent.Identifier, "commit-manager-git") {
			t.Errorf("want no commit-manager-git invocation when commits are disabled, got invocation with agent_instance_id=%q",
				inv.Request.AgentInstanceID)
		}
	}
}

// TestSession_Start_CommitsEnabled_CommitAgentDotAgentMdExtension_SetupSucceeds
// verifies that doCommitSetupDispatch resolves commit-class agent definition
// files named with the .agent.md compound extension (e.g.
// commit-manager-git.agent.md) just as it would a .md file. This
// regression-locks the extension-agnostic behavior introduced by the switch
// to agentresolve.ResolveOne in doCommitSetupDispatch.
func TestSession_Start_CommitsEnabled_CommitAgentDotAgentMdExtension_SetupSucceeds(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "commit-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	// Use .agent.md extension for the commit agent to verify extension-agnostic resolution.
	agentFilePath := filepath.Join(dir, "commit-manager-git.agent.md")
	if err := os.WriteFile(agentFilePath, []byte("# Agent: commit-manager-git\n"), 0600); err != nil {
		t.Fatalf("write commit-manager-git.agent.md: %v", err)
	}

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	const wantBranch = "mosaic/run/agent-md-ext-test"
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit setup complete [branch:" + wantBranch + "]",
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

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify commit-manager-git was dispatched from the .agent.md file path.
	invs := f.Invocations()
	dispatched := false
	for _, inv := range invs {
		if inv.Agent.Identifier == "commit-manager-git" {
			dispatched = true
			if !strings.HasSuffix(inv.Agent.DefinitionPath, "commit-manager-git.agent.md") {
				t.Errorf("want DefinitionPath ending in .agent.md, got %q", inv.Agent.DefinitionPath)
			}
		}
	}
	if !dispatched {
		t.Error("want commit-manager-git dispatched (commit setup), but it was not")
	}
}
