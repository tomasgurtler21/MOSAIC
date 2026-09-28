// Tests for TestResultsScreen rendering of resolved binary paths.
package devtest_test

import (
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/testrun"
)

// =============================================================================
// T4.4: TUI results screen -- resolved paths rendering
// =============================================================================

// TestResultsScreen_ResolvedPaths_NonEmpty_ShowsSection verifies that when
// TestSummary.ResolvedPaths is non-nil and non-empty, View() renders a
// "Resolved binaries:" section containing each harness ID and its path.
func TestResultsScreen_ResolvedPaths_NonEmpty_ShowsSection(t *testing.T) {
	summary := &testrun.TestSummary{
		AllPass:   true,
		TotalPass: 1,
		ResolvedPaths: map[string]string{
			"claude-code": "/usr/local/bin/claude",
		},
	}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	if !strings.Contains(got, "Resolved binaries:") {
		t.Errorf("TestResultsScreen.View() with ResolvedPaths set: does not contain \"Resolved binaries:\"\ngot (stripped): %.300s",
			got)
	}
}

// TestResultsScreen_ResolvedPaths_NonEmpty_ShowsHarnessID verifies that the
// harness ID appears in the resolved paths section.
func TestResultsScreen_ResolvedPaths_NonEmpty_ShowsHarnessID(t *testing.T) {
	const harnessID = "claude-code"
	const absPath = "/usr/local/bin/claude"
	summary := &testrun.TestSummary{
		AllPass:   true,
		TotalPass: 1,
		ResolvedPaths: map[string]string{
			harnessID: absPath,
		},
	}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	if !strings.Contains(got, harnessID) {
		t.Errorf("TestResultsScreen.View() with ResolvedPaths: does not contain harness ID %q\ngot (stripped): %.300s",
			harnessID, got)
	}
}

// TestResultsScreen_ResolvedPaths_NonEmpty_ShowsAbsolutePath verifies that
// the resolved absolute path appears in the results screen output.
func TestResultsScreen_ResolvedPaths_NonEmpty_ShowsAbsolutePath(t *testing.T) {
	const absPath = "/usr/local/bin/claude"
	summary := &testrun.TestSummary{
		AllPass:   true,
		TotalPass: 1,
		ResolvedPaths: map[string]string{
			"claude-code": absPath,
		},
	}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	if !strings.Contains(got, absPath) {
		t.Errorf("TestResultsScreen.View() with ResolvedPaths: does not contain path %q\ngot (stripped): %.300s",
			absPath, got)
	}
}

// TestResultsScreen_ResolvedPaths_Nil_OmitsSection verifies that when
// TestSummary.ResolvedPaths is nil, the "Resolved binaries:" section is
// absent from the results screen. A nil map means resolution was not performed
// (backwards-compatible zero value).
func TestResultsScreen_ResolvedPaths_Nil_OmitsSection(t *testing.T) {
	summary := &testrun.TestSummary{
		AllPass:       true,
		TotalPass:     1,
		ResolvedPaths: nil, // no resolution performed
	}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	if strings.Contains(got, "Resolved binaries:") {
		t.Errorf("TestResultsScreen.View() with nil ResolvedPaths: contains \"Resolved binaries:\"; want section absent\ngot (stripped): %.300s",
			got)
	}
}

// TestResultsScreen_ResolvedPaths_Empty_OmitsSection verifies that when
// TestSummary.ResolvedPaths is an empty (non-nil) map, the section is still
// absent. An empty map arises when all harnesses were "fake" (skipped).
func TestResultsScreen_ResolvedPaths_Empty_OmitsSection(t *testing.T) {
	summary := &testrun.TestSummary{
		AllPass:       true,
		TotalPass:     1,
		ResolvedPaths: map[string]string{}, // empty: all-fake run
	}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	if strings.Contains(got, "Resolved binaries:") {
		t.Errorf("TestResultsScreen.View() with empty ResolvedPaths: contains \"Resolved binaries:\"; want section absent when map is empty\ngot (stripped): %.300s",
			got)
	}
}

// TestResultsScreen_ResolvedPaths_MultipleHarnesses_SortedOrder verifies that
// when multiple harnesses are resolved, they appear in sorted (lexicographic)
// order in the results screen. The results screen uses sorted order because it
// only has the map (no input-order slice); sorted order is deterministic and
// predictable.
func TestResultsScreen_ResolvedPaths_MultipleHarnesses_SortedOrder(t *testing.T) {
	summary := &testrun.TestSummary{
		AllPass:   true,
		TotalPass: 2,
		ResolvedPaths: map[string]string{
			"opencode":    "/usr/local/bin/opencode",
			"claude-code": "/usr/local/bin/claude",
		},
	}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	claudeIdx := strings.Index(got, "claude-code")
	opencodeIdx := strings.Index(got, "opencode")

	if claudeIdx == -1 {
		t.Error("TestResultsScreen.View() does not contain \"claude-code\"")
	}
	if opencodeIdx == -1 {
		t.Error("TestResultsScreen.View() does not contain \"opencode\"")
	}
	// "claude-code" sorts before "opencode" lexicographically.
	if claudeIdx != -1 && opencodeIdx != -1 && claudeIdx > opencodeIdx {
		t.Errorf("TestResultsScreen.View() sorted order: claude-code (idx %d) appears after opencode (idx %d); want claude-code first (lexicographic order)",
			claudeIdx, opencodeIdx)
	}
}

// TestResultsScreen_ResolvedPaths_DeployError_WithResolvedPaths_ShowsSection
// verifies that when DeployError is set AND ResolvedPaths is non-empty
// (resolution succeeded but a later step failed), the "Resolved binaries:"
// section still appears. This supports AC4.11: resolved paths are visible on
// the fallback summary path.
func TestResultsScreen_ResolvedPaths_DeployError_WithResolvedPaths_ShowsSection(t *testing.T) {
	summary := &testrun.TestSummary{
		AllPass:     false,
		DeployError: errors.New("run infrastructure failed after resolution"),
		ResolvedPaths: map[string]string{
			"claude-code": "/usr/local/bin/claude",
		},
	}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	// Deploy error section must be present.
	if !strings.Contains(got, "Deploy failed") {
		t.Errorf("TestResultsScreen.View() DeployError: does not contain \"Deploy failed\"\ngot: %.300s", got)
	}
	// Resolved binaries section must also be present.
	if !strings.Contains(got, "Resolved binaries:") {
		t.Errorf("TestResultsScreen.View() DeployError + ResolvedPaths: does not contain \"Resolved binaries:\"\ngot (stripped): %.300s",
			got)
	}
}

// TestResultsScreen_ResolvedPaths_DeployError_WithNilResolvedPaths_OmitsSection
// verifies that when DeployError is set AND ResolvedPaths is nil (resolution
// itself failed -- the error IS the resolution error), the "Resolved binaries:"
// section is absent.
func TestResultsScreen_ResolvedPaths_DeployError_WithNilResolvedPaths_OmitsSection(t *testing.T) {
	summary := &testrun.TestSummary{
		AllPass:       false,
		DeployError:   errors.New("harness binary not found on PATH"),
		ResolvedPaths: nil, // resolution failed: nil paths
	}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	if strings.Contains(got, "Resolved binaries:") {
		t.Errorf("TestResultsScreen.View() DeployError + nil ResolvedPaths: contains \"Resolved binaries:\"; want section absent when resolution failed\ngot (stripped): %.300s",
			got)
	}
}
