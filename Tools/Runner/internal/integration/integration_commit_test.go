package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== Commit setup: branch recorded; a failed setup keeps the artifact and its row =====

// TestIntegration_CommitSetup_BranchRecordedInArtifact verifies that when a
// commits-enabled run starts and the commit-class agent reports a
// [branch:{name}] marker, the branch name is stored in the artifact frontmatter
// and the commit setup dispatch appears as the first execution log row.
func TestIntegration_CommitSetup_BranchRecordedInArtifact(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "commit-agent-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "commit-manager-git")

	const wantBranch = "mosaic/run/test-branch-001"

	f := harness.NewMockAdapter()
	// commit setup dispatch — must carry the [branch:{name}] marker.
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

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSession(f, artifactPath)

	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "commit task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAuto,
			Commits:             true,
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Read and parse the produced artifact to verify frontmatter and log.
	data, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("read artifact: %v", readErr)
	}
	state, parseErr := artifact.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse artifact: %v", parseErr)
	}

	if state.CommitBranch != wantBranch {
		t.Errorf("want CommitBranch=%q in artifact frontmatter, got %q",
			wantBranch, state.CommitBranch)
	}

	// The commit setup dispatch must be the first execution log row (Seq=1) and
	// its agent identifier must name the commit-class agent.
	if len(state.ExecutionLog) == 0 {
		t.Fatal("want at least one execution log entry (commit setup row), got none")
	}
	first := state.ExecutionLog[0]
	if first.Seq != 1 {
		t.Errorf("want commit setup row Seq=1, got Seq=%d", first.Seq)
	}
	if !strings.Contains(first.Agent, "commit-manager-git") {
		t.Errorf("want commit setup row agent to contain commit-manager-git, got %q",
			first.Agent)
	}
}

// commitFailureConfig returns a new-run configuration with commits enabled.
func commitFailureConfig(orchPath, task string) domain.RunConfig {
	return domain.RunConfig{
		RunID:                integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 task,
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAuto,
			Commits:             true,
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		},
	}
}

// requireFailedSetupKeptOnDisk asserts what a failed commit setup must leave on
// disk: a start-failed outcome, the artifact with the setup row as its only
// Execution Log row (Seq 1), no commit_branch, an untouched current_state, and
// no workflow agent dispatched.
func requireFailedSetupKeptOnDisk(t *testing.T, artifactPath string, got domain.RunOutcome, err error, f *harness.MockAdapter) domain.ArtifactState {
	t.Helper()
	requireStartFailed(t, got, err)

	data, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("want the artifact kept after a failed commit setup, read error: %v", readErr)
	}
	state, parseErr := artifact.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse artifact: %v", parseErr)
	}
	if state.CommitBranch != "" {
		t.Errorf("want commit_branch absent after a failed setup, got %q", state.CommitBranch)
	}
	if len(state.ExecutionLog) != 1 {
		t.Fatalf("want exactly the setup row in the Execution Log, got %d rows", len(state.ExecutionLog))
	}
	row := state.ExecutionLog[0]
	if row.Seq != 1 || !strings.Contains(row.Agent, "commit-manager-git") {
		t.Errorf("want setup row commit-manager-git with Seq=1, got %q Seq=%d", row.Agent, row.Seq)
	}
	if state.GlobalSequence != 1 {
		t.Errorf("want global_sequence=1, got %d", state.GlobalSequence)
	}
	if state.CurrentState.LastAgent != "" {
		t.Errorf("want current_state untouched by the setup row, got last_agent=%q", state.CurrentState.LastAgent)
	}
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier != "commit-manager-git" {
			t.Errorf("want no workflow agent dispatched after failed commit setup, got %q", inv.Agent.Identifier)
		}
	}
	return state
}

func newCommitFailureDir(t *testing.T) (dir, orchPath string) {
	t.Helper()
	dir = t.TempDir()
	orchPath = copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "commit-agent-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "commit-manager-git")
	return dir, orchPath
}

// TestIntegration_CommitSetup_FailedSetup_KeepsArtifactAndRow verifies that when
// the commit setup dispatch fails at the harness level, the run stops as a
// resumable start failure: the artifact and the BLOCKED setup row stay on disk.
func TestIntegration_CommitSetup_FailedSetup_KeepsArtifactAndRow(t *testing.T) {
	dir, orchPath := newCommitFailureDir(t)
	f := harness.NewMockAdapter()
	f.Queue("commit-manager-git", harness.ScriptedEntry{
		Err: errors.New("harness: commit setup agent crashed"),
	})
	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSession(f, artifactPath)

	got, err := sess.Start(context.Background(), commitFailureConfig(orchPath, "commit-fail task"))

	state := requireFailedSetupKeptOnDisk(t, artifactPath, got, err, f)
	if len(state.ExecutionLog) == 1 && state.ExecutionLog[0].Status != domain.StatusBLOCKED {
		t.Errorf("want the harness failure recorded as BLOCKED, got %q", state.ExecutionLog[0].Status)
	}
}

// TestIntegration_CommitSetup_MarkerMissing_KeepsArtifactAndRow verifies that a
// SUCCESS setup response without a [branch:{name}] marker keeps the artifact and
// the setup row and dispatches no workflow step.
func TestIntegration_CommitSetup_MarkerMissing_KeepsArtifactAndRow(t *testing.T) {
	dir, orchPath := newCommitFailureDir(t)
	f := harness.NewMockAdapter()
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit setup complete - no branch marker here",
	}})
	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSession(f, artifactPath)

	got, err := sess.Start(context.Background(), commitFailureConfig(orchPath, "marker-missing task"))

	requireFailedSetupKeptOnDisk(t, artifactPath, got, err, f)
}

// TestIntegration_CommitSetup_EmptyBranchName_KeepsArtifactAndRow verifies that
// a [branch:] marker with an empty name is treated like a missing marker.
func TestIntegration_CommitSetup_EmptyBranchName_KeepsArtifactAndRow(t *testing.T) {
	dir, orchPath := newCommitFailureDir(t)
	f := harness.NewMockAdapter()
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit setup complete [branch:]",
	}})
	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSession(f, artifactPath)

	got, err := sess.Start(context.Background(), commitFailureConfig(orchPath, "empty-branch task"))

	requireFailedSetupKeptOnDisk(t, artifactPath, got, err, f)
}

// TestIntegration_CommitSetup_FailedSetup_ResumeRetriesSetupThenRuns verifies
// the full recovery path on the real file store: after a failed setup, a resume
// retries setup (next sequence, earlier row kept), records the branch, and runs
// the workflow to completion with strictly increasing sequences.
func TestIntegration_CommitSetup_FailedSetup_ResumeRetriesSetupThenRuns(t *testing.T) {
	dir, orchPath := newCommitFailureDir(t)
	artifactPath := filepath.Join(scopedRunFolder(t, dir), "Orchestration.md")

	first := harness.NewMockAdapter()
	first.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "dirty working tree",
		ErrorCode:       domain.ErrorPERMISSION_DENIED,
		ErrorReason:     "uncommitted changes",
	}})
	got, err := newSession(first, artifactPath).Start(context.Background(), commitFailureConfig(orchPath, "retry task"))
	requireFailedSetupKeptOnDisk(t, artifactPath, got, err, first)

	const wantBranch = "mosaic/run/retry-branch"
	second := harness.NewMockAdapter()
	second.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#2", StatusCode: domain.StatusSUCCESS,
		StatusMessage: "ready [branch:" + wantBranch + "]",
	}})
	second.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#3", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	second.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	resumeCfg := commitFailureConfig(orchPath, "retry task")
	resumeCfg.IsNewRun = false
	resumeCfg.RunFolder = filepath.Dir(artifactPath)
	resumeCfg.Supplied.CommitBranchVariant = true // a pending setup retry needs the variant

	got, err = newSession(second, artifactPath).Start(context.Background(), resumeCfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	data, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("read artifact: %v", readErr)
	}
	state, parseErr := artifact.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse artifact: %v", parseErr)
	}
	if state.CommitBranch != wantBranch {
		t.Errorf("want commit_branch=%q after the retry, got %q", wantBranch, state.CommitBranch)
	}
	if len(state.ExecutionLog) != 4 {
		t.Fatalf("want 4 rows (failed setup, retried setup, 2 workflow), got %d", len(state.ExecutionLog))
	}
	for i, row := range state.ExecutionLog {
		if row.Seq != i+1 {
			t.Errorf("row %d: want Seq=%d, got %d", i, i+1, row.Seq)
		}
	}
	if state.ExecutionLog[0].Status != domain.StatusBLOCKED || !strings.Contains(state.ExecutionLog[1].Agent, "commit-manager-git") {
		t.Errorf("want the failed row kept first and the retried setup second, got %+v", state.ExecutionLog[:2])
	}
}
