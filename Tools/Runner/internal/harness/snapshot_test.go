package harness_test

// Tests for the SnapshotDirPath helper.
//
// SnapshotDirPath computes the run-scoped agent snapshot directory path given
// a working directory, the harness's agents-directory convention, and a run
// ID. The snapshot directory is a sibling of the regular agents directory:
//
//   agentsDir = ".opencode/agents"
//   runID     = "20260727T170000Z-a3f9"
//   result    = "{workDir}/.opencode/agents-runner-20260727T170000Z-a3f9"
//
// This follows the naming pattern of domain.RunScopedFolder.

import (
	"path/filepath"
	"testing"

	"mosaic-run/internal/harness"
)

// TestSnapshotDirPath_OpenCode verifies that the opencode agents directory
// convention produces the expected snapshot sibling path.
func TestSnapshotDirPath_OpenCode(t *testing.T) {
	workDir := "/projects/my-workspace"
	agentsDir := ".opencode/agents"
	runID := "20260727T170000Z-a3f9"

	want := filepath.Join(workDir, ".opencode", "agents-runner-20260727T170000Z-a3f9")
	got := harness.SnapshotDirPath(workDir, agentsDir, runID)

	if got != want {
		t.Errorf("SnapshotDirPath opencode: want %q, got %q", want, got)
	}
}

// TestSnapshotDirPath_ClaudeCode verifies that the claude-code agents
// directory convention produces the expected snapshot sibling path.
func TestSnapshotDirPath_ClaudeCode(t *testing.T) {
	workDir := "/projects/my-workspace"
	agentsDir := ".claude/agents"
	runID := "20260727T170000Z-a3f9"

	want := filepath.Join(workDir, ".claude", "agents-runner-20260727T170000Z-a3f9")
	got := harness.SnapshotDirPath(workDir, agentsDir, runID)

	if got != want {
		t.Errorf("SnapshotDirPath claude-code: want %q, got %q", want, got)
	}
}

// TestSnapshotDirPath_GHCPCli verifies that the ghcp-cli agents directory
// convention produces the expected snapshot sibling path. The agents
// directory is ".github/agents" (not ".ghcp/agents").
func TestSnapshotDirPath_GHCPCli(t *testing.T) {
	workDir := "/projects/my-workspace"
	agentsDir := ".github/agents"
	runID := "20260727T170000Z-a3f9"

	want := filepath.Join(workDir, ".github", "agents-runner-20260727T170000Z-a3f9")
	got := harness.SnapshotDirPath(workDir, agentsDir, runID)

	if got != want {
		t.Errorf("SnapshotDirPath ghcp-cli: want %q, got %q", want, got)
	}
}

// TestSnapshotDirPath_WorkDirIsIncluded verifies that the working directory
// is prepended to the result, so the returned path is absolute when workDir
// is absolute. t.TempDir() provides a real, cross-platform absolute path so
// that filepath.IsAbs is satisfied on both Unix and Windows.
func TestSnapshotDirPath_WorkDirIsIncluded(t *testing.T) {
	workDir := t.TempDir()
	agentsDir := ".opencode/agents"
	runID := "run-123"

	got := harness.SnapshotDirPath(workDir, agentsDir, runID)

	if !filepath.IsAbs(got) {
		t.Errorf("SnapshotDirPath with absolute workDir: want absolute path, got %q", got)
	}
	if len(got) < len(workDir) || got[:len(workDir)] != workDir {
		t.Errorf("SnapshotDirPath: want result to begin with workDir %q, got %q", workDir, got)
	}
}

// TestSnapshotDirPath_RunIDAppearsInName verifies that the run ID is
// embedded in the snapshot directory name as the suffix after "agents-runner-".
func TestSnapshotDirPath_RunIDAppearsInName(t *testing.T) {
	workDir := "/tmp/ws"
	agentsDir := ".opencode/agents"
	runID := "20260801T120000Z-beef"

	got := harness.SnapshotDirPath(workDir, agentsDir, runID)
	base := filepath.Base(got)

	wantBase := "agents-runner-20260801T120000Z-beef"
	if base != wantBase {
		t.Errorf("SnapshotDirPath: want final path segment %q, got %q (full path: %q)", wantBase, base, got)
	}
}

// TestSnapshotDirPath_SnapshotIsSiblingOfAgentsDir verifies that the
// snapshot directory is a sibling of the regular agents directory (they
// share the same parent directory).
func TestSnapshotDirPath_SnapshotIsSiblingOfAgentsDir(t *testing.T) {
	workDir := "/projects/ws"
	agentsDir := ".opencode/agents"
	runID := "abc"

	got := harness.SnapshotDirPath(workDir, agentsDir, runID)

	regularAgentsDir := filepath.Join(workDir, agentsDir)
	wantParent := filepath.Dir(regularAgentsDir)
	gotParent := filepath.Dir(got)

	if gotParent != wantParent {
		t.Errorf("SnapshotDirPath: want snapshot parent dir == regular agents parent dir %q, got %q", wantParent, gotParent)
	}
}
