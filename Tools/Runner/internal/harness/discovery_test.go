package harness_test

// Tests for the DiscoverOrchestrator helper.
//
// DiscoverOrchestrator computes the expected orchestrator-script.md path from
// the harness convention and verifies the file exists on disk. It returns a
// *domain.RefusalError for unknown harness IDs or when the expected file is
// absent.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// writeTempOrchestrator creates a temp working directory with the expected
// agents directory structure for the given harness and writes an
// orchestrator-script.md file in it. Returns the working directory path.
func writeTempOrchestrator(t *testing.T, harnessID, agentsDir string) string {
	t.Helper()
	workDir := t.TempDir()
	agentsDirFull := filepath.Join(workDir, agentsDir)
	if err := os.MkdirAll(agentsDirFull, 0o755); err != nil {
		t.Fatalf("writeTempOrchestrator: mkdir %v: %v", agentsDirFull, err)
	}
	orchPath := filepath.Join(agentsDirFull, "orchestrator-script.md")
	if err := os.WriteFile(orchPath, []byte("# orchestrator\n"), 0o644); err != nil {
		t.Fatalf("writeTempOrchestrator: write %v: %v", orchPath, err)
	}
	return workDir
}

// asRefusalError asserts that err is (or wraps) a *domain.RefusalError and
// returns it. Calls t.Fatal on failure.
func asRefusalError(t *testing.T, err error) *domain.RefusalError {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	return re
}

// ---------------------------------------------------------------------------
// Happy-path tests: correct path computed for each harness
// ---------------------------------------------------------------------------

// TestDiscoverOrchestrator_ClaudeCode_CorrectPath verifies that the claude-code
// harness produces the correct orchestrator-script.md path.
func TestDiscoverOrchestrator_ClaudeCode_CorrectPath(t *testing.T) {
	workDir := writeTempOrchestrator(t, "claude-code", ".claude/agents")
	got, err := harness.DiscoverOrchestrator(workDir, "claude-code")
	if err != nil {
		t.Fatalf("DiscoverOrchestrator claude-code: unexpected error: %v", err)
	}
	want := filepath.Join(workDir, ".claude", "agents", "orchestrator-script.md")
	if got != want {
		t.Errorf("DiscoverOrchestrator claude-code: got %q, want %q", got, want)
	}
}

// TestDiscoverOrchestrator_OpenCode_CorrectPath verifies that the opencode
// harness produces the correct orchestrator-script.md path.
func TestDiscoverOrchestrator_OpenCode_CorrectPath(t *testing.T) {
	workDir := writeTempOrchestrator(t, "opencode", ".opencode/agents")
	got, err := harness.DiscoverOrchestrator(workDir, "opencode")
	if err != nil {
		t.Fatalf("DiscoverOrchestrator opencode: unexpected error: %v", err)
	}
	want := filepath.Join(workDir, ".opencode", "agents", "orchestrator-script.md")
	if got != want {
		t.Errorf("DiscoverOrchestrator opencode: got %q, want %q", got, want)
	}
}

// TestDiscoverOrchestrator_GHCPCli_CorrectPath verifies that the ghcp-cli
// harness produces the correct orchestrator-script.md path.
func TestDiscoverOrchestrator_GHCPCli_CorrectPath(t *testing.T) {
	workDir := writeTempOrchestrator(t, "ghcp-cli", ".github/agents")
	got, err := harness.DiscoverOrchestrator(workDir, "ghcp-cli")
	if err != nil {
		t.Fatalf("DiscoverOrchestrator ghcp-cli: unexpected error: %v", err)
	}
	want := filepath.Join(workDir, ".github", "agents", "orchestrator-script.md")
	if got != want {
		t.Errorf("DiscoverOrchestrator ghcp-cli: got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Error-path tests: file not found produces RefusalError
// ---------------------------------------------------------------------------

// TestDiscoverOrchestrator_FileNotFound_ReturnsRefusalError verifies that a
// missing orchestrator-script.md file produces a *domain.RefusalError.
func TestDiscoverOrchestrator_FileNotFound_ReturnsRefusalError(t *testing.T) {
	workDir := t.TempDir()
	// Create the agents directory but not the orchestrator-script.md.
	if err := os.MkdirAll(filepath.Join(workDir, ".claude", "agents"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := harness.DiscoverOrchestrator(workDir, "claude-code")
	if err == nil {
		t.Fatal("DiscoverOrchestrator: expected error for missing file, got nil")
	}
	asRefusalError(t, err)
}

// TestDiscoverOrchestrator_FileNotFound_ErrorMentionsHarnessID verifies that the
// RefusalError message names the harness ID so the user knows which harness
// was expected to be deployed.
func TestDiscoverOrchestrator_FileNotFound_ErrorMentionsHarnessID(t *testing.T) {
	workDir := t.TempDir()

	_, err := harness.DiscoverOrchestrator(workDir, "claude-code")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if re := asRefusalError(t, err); re != nil {
		if re.Component != "harness" {
			t.Errorf("RefusalError.Component = %q, want %q", re.Component, "harness")
		}
		if !strings.Contains(re.Reason, "claude-code") {
			t.Errorf("RefusalError.Reason = %q does not contain harness ID %q; "+
				"the error must name the harness so it is actionable",
				re.Reason, "claude-code")
		}
	}
}

// ---------------------------------------------------------------------------
// Error-path tests: unknown harness ID
// ---------------------------------------------------------------------------

// TestDiscoverOrchestrator_UnknownHarness_ReturnsRefusalError verifies that an
// unrecognised harness ID returns a *domain.RefusalError.
func TestDiscoverOrchestrator_UnknownHarness_ReturnsRefusalError(t *testing.T) {
	_, err := harness.DiscoverOrchestrator(t.TempDir(), "unknown-harness")
	if err == nil {
		t.Fatal("DiscoverOrchestrator unknown harness: expected error, got nil")
	}
	asRefusalError(t, err)
}

// TestDiscoverOrchestrator_FakeHarness_ReturnsRefusalError verifies that the
// test-double "fake" harness (which is not a CLI-backed harness) returns a
// *domain.RefusalError from auto-discovery.
func TestDiscoverOrchestrator_FakeHarness_ReturnsRefusalError(t *testing.T) {
	_, err := harness.DiscoverOrchestrator(t.TempDir(), "fake")
	if err == nil {
		t.Fatal("DiscoverOrchestrator fake harness: expected error, got nil")
	}
	asRefusalError(t, err)
}

// TestDiscoverOrchestrator_FileExists_ReturnsAbsolutePath verifies that the
// returned path is absolute when workDir is absolute.
func TestDiscoverOrchestrator_FileExists_ReturnsAbsolutePath(t *testing.T) {
	workDir := writeTempOrchestrator(t, "claude-code", ".claude/agents")
	got, err := harness.DiscoverOrchestrator(workDir, "claude-code")
	if err != nil {
		t.Fatalf("DiscoverOrchestrator: unexpected error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("DiscoverOrchestrator: returned path %q is not absolute", got)
	}
}
