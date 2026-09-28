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

// ===== T9.6: Commit setup — branch recorded, failed setup leaves no artifact =====

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

// TestIntegration_CommitSetup_FailedSetup_NoArtifactCreated verifies that when
// the commit setup dispatch fails (harness-level error), the run is refused and
// no orchestration artifact is left behind. A failed setup is terminal and
// leaves no trace.
func TestIntegration_CommitSetup_FailedSetup_NoArtifactCreated(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "commit-agent-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "commit-manager-git")

	f := harness.NewMockAdapter()
	// commit-manager-git fails at the harness level (e.g. agent process crashed).
	f.Queue("commit-manager-git", harness.ScriptedEntry{
		Err: errors.New("harness: commit setup agent crashed"),
	})

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSession(f, artifactPath)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "commit-fail task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAuto,
			Commits:             true,
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRefused(t, got, err)

	// No artifact must be created: a failed commit setup leaves no trace.
	if _, statErr := os.Stat(artifactPath); statErr == nil {
		t.Errorf("want no artifact file after failed commit setup, but file exists at %s",
			artifactPath)
	}

	// Only commit-manager-git may have been invoked; no workflow agents.
	invs := f.Invocations()
	for _, inv := range invs {
		if inv.Agent.Identifier != "commit-manager-git" {
			t.Errorf("want no workflow agent dispatched after failed commit setup, got %q",
				inv.Agent.Identifier)
		}
	}
}

// TestIntegration_CommitSetup_MarkerMissing_Refused verifies that when the
// commit-class agent returns StatusSUCCESS but its status_message contains no
// [branch:{name}] marker, the run is refused and no orchestration artifact is
// left behind. This exercises the marker-parsing failure path (path (b)),
// distinct from the harness-level crash covered by
// TestIntegration_CommitSetup_FailedSetup_NoArtifactCreated.
func TestIntegration_CommitSetup_MarkerMissing_Refused(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "commit-agent-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "commit-manager-git")

	f := harness.NewMockAdapter()
	// Commit agent returns SUCCESS but omits the [branch:{name}] marker entirely.
	// An absent marker must refuse the run.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit setup complete — no branch marker here",
	}})

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSession(f, artifactPath)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "marker-missing task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAuto,
			Commits:             true,
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRefused(t, got, err)

	// No artifact must be created: an absent branch marker refuses the run terminally.
	if _, statErr := os.Stat(artifactPath); statErr == nil {
		t.Errorf("want no artifact file when [branch:{name}] marker is absent, but file exists at %s",
			artifactPath)
	}

	// No workflow agents must have been dispatched; only the commit setup agent.
	invs := f.Invocations()
	for _, inv := range invs {
		if inv.Agent.Identifier != "commit-manager-git" {
			t.Errorf("want no workflow agent dispatched after marker-missing refusal, got %q",
				inv.Agent.Identifier)
		}
	}
}

// TestIntegration_CommitSetup_EmptyBranchName_Refused verifies that when the
// commit-class agent returns StatusSUCCESS with a [branch:] marker whose branch
// name is empty, the run is refused and no orchestration artifact is left behind.
// An empty branch name is equivalent to an absent marker per the design contract:
// "An absent marker or an empty name refuses the run."
func TestIntegration_CommitSetup_EmptyBranchName_Refused(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "commit-agent-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "commit-manager-git")

	f := harness.NewMockAdapter()
	// Commit agent returns SUCCESS with a [branch:] marker but no branch name.
	// An empty name must refuse the run.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit setup complete [branch:]",
	}})

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSession(f, artifactPath)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "empty-branch task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAuto,
			Commits:             true,
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRefused(t, got, err)

	// No artifact must be created: an empty branch name refuses the run terminally.
	if _, statErr := os.Stat(artifactPath); statErr == nil {
		t.Errorf("want no artifact file when [branch:] has empty name, but file exists at %s",
			artifactPath)
	}

	// No workflow agents must have been dispatched; only the commit setup agent.
	invs := f.Invocations()
	for _, inv := range invs {
		if inv.Agent.Identifier != "commit-manager-git" {
			t.Errorf("want no workflow agent dispatched after empty-branch-name refusal, got %q",
				inv.Agent.Identifier)
		}
	}
}
