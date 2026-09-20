// Tests for TestResultsScreen.View rendering of ChildStderr, ActualExitCode,
// and LogPath. Tests are in package screens_test (blackbox) because View() is
// exported, and because the existing TestMain (in package screens) already
// forces a fixed ANSI color profile for the test binary.
//
// Test coverage:
//
//   - Non-empty ChildStderr on ERROR and FAIL results appears in View output
//     under the mandatory "stderr:" header.
//   - ActualExitCode appears on ERROR results (always, even when 0).
//   - ActualExitCode appears on FAIL results when non-zero.
//   - PASS results do not display stderr or exit code.
//   - ChildStderr exceeding MaxStderrDisplayBytes (8192 bytes) is truncated
//     with the [truncated...] marker.
//   - summary.LogPath appears in View when non-empty.
//   - No log path line when summary.LogPath is empty.
package screens_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"mosaic-run/internal/testrun"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ansiSeq matches SGR ANSI escape sequences so test assertions can inspect
// the rendered View output without ANSI color codes obscuring substring matches.
var ansiSeq = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripANSIResults removes ANSI SGR sequences from a string.
// Named to avoid collision with the stripANSI helper in package screens tests.
func stripANSIResults(s string) string {
	return ansiSeq.ReplaceAllString(s, "")
}

// newResultsScreen constructs a TestResultsScreen at a wide width (200) to
// avoid line-wrap interference in substring assertions.
func newResultsScreen(summary *testrun.TestSummary) *screens.TestResultsScreen {
	return screens.NewTestResultsScreen(200, 50, screens.Styles{}, summary)
}

// summaryWithOneResult builds a TestSummary containing a single HarnessResults
// with one TestRunResult. Helper to reduce boilerplate in test cases.
func summaryWithOneResult(result testrun.TestRunResult) *testrun.TestSummary {
	return &testrun.TestSummary{
		AllPass:    result.Pass,
		TotalPass:  boolToInt(result.Pass),
		TotalFail:  boolToInt(!result.Pass && result.Error == nil),
		TotalError: boolToInt(result.Error != nil),
		HarnessResults: []testrun.HarnessResults{
			{
				Harness:    result.Harness,
				Results:    []testrun.TestRunResult{result},
				PassCount:  boolToInt(result.Pass),
				FailCount:  boolToInt(!result.Pass && result.Error == nil),
				ErrorCount: boolToInt(result.Error != nil),
			},
		},
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// viewOf returns the stripped View output for a summary with one result.
func viewOf(result testrun.TestRunResult) string {
	s := newResultsScreen(summaryWithOneResult(result))
	return stripANSIResults(s.View())
}

// =============================================================================
// T2.3: TestResultsScreen.View -- ERROR result stderr display
// =============================================================================

// TestResultsScreen_ErrorResult_NonEmptyStderr_DisplaysStderrHeader verifies
// that when a result is ERROR and ChildStderr is non-empty, View() contains
// the mandatory "stderr:" header.
func TestResultsScreen_ErrorResult_NonEmptyStderr_DisplaysStderrHeader(t *testing.T) {
	result := testrun.TestRunResult{
		WorkflowID:  "wf-test",
		Mode:        "auto",
		Harness:     "claude-code",
		Pass:        false,
		Error:       errors.New("run folder not found"),
		ChildStderr: "subprocess wrote this to stderr\n",
	}
	got := viewOf(result)

	if !strings.Contains(got, "stderr:") {
		t.Errorf("TestResultsScreen.View() ERROR with non-empty ChildStderr: does not contain mandatory header %q\ngot (stripped): %q\n(the stderr: header distinguishes the dedicated stderr block from error text)",
			"stderr:", got)
	}
}

// TestResultsScreen_ErrorResult_NonEmptyStderr_DisplaysContent verifies that
// the actual stderr content appears in View() for an ERROR result.
func TestResultsScreen_ErrorResult_NonEmptyStderr_DisplaysContent(t *testing.T) {
	const stderrMsg = "subprocess: run folder discovery failed unexpectedly"
	result := testrun.TestRunResult{
		WorkflowID:  "wf-test",
		Mode:        "auto",
		Harness:     "claude-code",
		Pass:        false,
		Error:       errors.New("infrastructure error"),
		ChildStderr: stderrMsg + "\n",
	}
	got := viewOf(result)

	if !strings.Contains(got, stderrMsg) {
		t.Errorf("TestResultsScreen.View() ERROR with non-empty ChildStderr: does not contain stderr content %q\ngot (stripped): %.200s",
			stderrMsg, got)
	}
}

// TestResultsScreen_ErrorResult_DisplaysExitCode_EvenWhenZero verifies that
// for ERROR results, View() displays the exit code even when it is 0.
// Exit code 0 on an ERROR is a diagnostic clue.
func TestResultsScreen_ErrorResult_DisplaysExitCode_EvenWhenZero(t *testing.T) {
	result := testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           false,
		Error:          errors.New("no run folder created"),
		ActualExitCode: 0,
	}
	got := viewOf(result)

	if !strings.Contains(got, "exit code: 0") {
		t.Errorf("TestResultsScreen.View() ERROR with ActualExitCode=0: does not contain %q\ngot (stripped): %.200s\n(exit code 0 on ERROR is diagnostic; must be displayed even when 0)",
			"exit code: 0", got)
	}
}

// TestResultsScreen_ErrorResult_DisplaysExitCode_WhenNonZero verifies that
// for ERROR results with a non-zero exit code, View() displays the exit code.
func TestResultsScreen_ErrorResult_DisplaysExitCode_WhenNonZero(t *testing.T) {
	result := testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           false,
		Error:          errors.New("discovery failed"),
		ActualExitCode: 1,
	}
	got := viewOf(result)

	if !strings.Contains(got, "exit code: 1") {
		t.Errorf("TestResultsScreen.View() ERROR with ActualExitCode=1: does not contain %q\ngot (stripped): %.200s",
			"exit code: 1", got)
	}
}

// =============================================================================
// T2.3: TestResultsScreen.View -- FAIL result stderr and exit code display
// =============================================================================

// TestResultsScreen_FailResult_NonZeroExit_DisplaysExitCode verifies that for
// FAIL results with non-zero ActualExitCode, View() displays the exit code.
func TestResultsScreen_FailResult_NonZeroExit_DisplaysExitCode(t *testing.T) {
	result := testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           false,
		Error:          nil,
		ActualExitCode: 2,
	}
	got := viewOf(result)

	if !strings.Contains(got, "exit code: 2") {
		t.Errorf("TestResultsScreen.View() FAIL with ActualExitCode=2: does not contain %q\ngot (stripped): %.200s",
			"exit code: 2", got)
	}
}

// TestResultsScreen_FailResult_ZeroExit_DoesNotDisplayExitCode verifies that
// for FAIL results with zero exit code, the exit code is not displayed.
func TestResultsScreen_FailResult_ZeroExit_DoesNotDisplayExitCode(t *testing.T) {
	result := testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           false,
		Error:          nil,
		ActualExitCode: 0,
	}
	got := viewOf(result)

	if strings.Contains(got, "exit code:") {
		t.Errorf("TestResultsScreen.View() FAIL with ActualExitCode=0: contains exit code; want no exit code display\ngot (stripped): %.200s",
			got)
	}
}

// TestResultsScreen_FailResult_NonEmptyStderr_DisplaysStderr verifies that
// a FAIL result with non-empty ChildStderr shows the "stderr:" header and
// the stderr content.
func TestResultsScreen_FailResult_NonEmptyStderr_DisplaysStderr(t *testing.T) {
	const stderrContent = "workflow: dispatcher loop exited without posting result"
	result := testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           false,
		Error:          nil,
		ChildStderr:    stderrContent + "\n",
		ActualExitCode: 1,
	}
	got := viewOf(result)

	if !strings.Contains(got, "stderr:") {
		t.Errorf("TestResultsScreen.View() FAIL with non-empty ChildStderr: does not contain %q\ngot (stripped): %.200s",
			"stderr:", got)
	}
	if !strings.Contains(got, stderrContent) {
		t.Errorf("TestResultsScreen.View() FAIL with non-empty ChildStderr: does not contain stderr content %q\ngot (stripped): %.200s",
			stderrContent, got)
	}
}

// TestResultsScreen_FailResult_ZeroExit_NonEmptyStderr_DisplaysStderr verifies
// that a FAIL result with non-empty ChildStderr and a zero exit code still
// shows the "stderr:" header and content in View(). Stderr display on FAIL is
// independent of the exit code; only the exit code line is gated on non-zero.
// An implementation that gates stderr display on ActualExitCode != 0 would pass
// TestResultsScreen_FailResult_NonEmptyStderr_DisplaysStderr (which uses
// ActualExitCode: 1) but fail here.
func TestResultsScreen_FailResult_ZeroExit_NonEmptyStderr_DisplaysStderr(t *testing.T) {
	const stderrContent = "workflow: checker produced no deviation file"
	result := testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           false,
		Error:          nil,
		ChildStderr:    stderrContent + "\n",
		ActualExitCode: 0, // zero exit; stderr must still be shown
	}
	got := viewOf(result)

	if !strings.Contains(got, "stderr:") {
		t.Errorf("TestResultsScreen.View() FAIL with non-empty ChildStderr and exit 0: does not contain %q\ngot (stripped): %.200s\n(stderr display on FAIL is independent of exit code; gating on exit code != 0 is a bug)",
			"stderr:", got)
	}
	if !strings.Contains(got, stderrContent) {
		t.Errorf("TestResultsScreen.View() FAIL with non-empty ChildStderr and exit 0: does not contain stderr content %q\ngot (stripped): %.200s",
			stderrContent, got)
	}
}

// TestResultsScreen_FailResult_EmptyStderr_NoStderrHeader verifies that when
// a FAIL result has empty ChildStderr, View() does NOT contain the "stderr:"
// header. An implementation that emits "stderr:" unconditionally (or guards on
// result.Error != nil instead of ChildStderr != "") would pass the positive
// FAIL+stderr tests (which only check for header presence when ChildStderr is
// non-empty) but fail here.
func TestResultsScreen_FailResult_EmptyStderr_NoStderrHeader(t *testing.T) {
	result := testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           false,
		Error:          nil,
		ChildStderr:    "", // empty: no stderr section must be emitted
		ActualExitCode: 1,
	}
	got := viewOf(result)

	if strings.Contains(got, "stderr:") {
		t.Errorf("TestResultsScreen.View() FAIL with empty ChildStderr: contains \"stderr:\"; want no stderr section when ChildStderr is empty\ngot (stripped): %.200s\n(the guard must be on ChildStderr != \"\", not on result.Error != nil)",
			got)
	}
}

// =============================================================================
// T2.3: TestResultsScreen.View -- PASS result (no diagnostics)
// =============================================================================

// TestResultsScreen_PassResult_NoStderrOrExitCode verifies that PASS results
// do not display the "stderr:" header or an exit code, even when ChildStderr
// is non-empty and ActualExitCode is non-zero.
func TestResultsScreen_PassResult_NoStderrOrExitCode(t *testing.T) {
	result := testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           true,
		ChildStderr:    "benign warning: using deprecated config path\n",
		ActualExitCode: 1,
	}
	got := viewOf(result)

	if strings.Contains(got, "stderr:") {
		t.Errorf("TestResultsScreen.View() PASS: contains stderr section; want no diagnostics on PASS\ngot (stripped): %.200s",
			got)
	}
	if strings.Contains(got, "exit code:") {
		t.Errorf("TestResultsScreen.View() PASS: contains exit code; want no diagnostics on PASS\ngot (stripped): %.200s",
			got)
	}
}

// =============================================================================
// T2.3: TestResultsScreen.View -- stderr truncation at 8 KiB
// =============================================================================

// TestResultsScreen_StderrOver8KiB_Truncated verifies that when ChildStderr
// exceeds MaxStderrDisplayBytes (8192 bytes), View() contains the truncation
// marker and the tail sentinel is shown (not the prefix).
func TestResultsScreen_StderrOver8KiB_Truncated(t *testing.T) {
	prefix := strings.Repeat("X", 8200)
	sentinel := "RESULTS_SENTINEL_END"
	stderrContent := prefix + sentinel

	result := testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           false,
		Error:          errors.New("infra failure"),
		ChildStderr:    stderrContent,
		ActualExitCode: 1,
	}
	// Use a tall screen (height=100) so that the scrollable window is large
	// enough to show all content at default offset=0.  The 8KB+ stderr body
	// produces ~48 body rows at width=200; the sentinel is at the last row.
	// A 200x50 screen has contentHeight=45 which clips the sentinel out of
	// the default view, so we need height > 53 (48 body + 5 reserved rows).
	s := screens.NewTestResultsScreen(200, 100, screens.Styles{}, summaryWithOneResult(result))
	got := stripANSIResults(s.View())

	if !strings.Contains(got, "[truncated, showing last") {
		t.Errorf("TestResultsScreen.View() ERROR with stderr > 8KiB: does not contain truncation marker\ngot (stripped, first 200): %.200s\n(stderr exceeding MaxStderrDisplayBytes must be truncated via TruncateTail)",
			got)
	}

	if !strings.Contains(got, sentinel) {
		t.Errorf("TestResultsScreen.View() ERROR with stderr > 8KiB: does not contain tail sentinel %q\n(TruncateTail must show the LAST bytes, not the first)",
			sentinel)
	}
}

// =============================================================================
// T2.5: TestResultsScreen.View -- LogPath display
// =============================================================================

// TestResultsScreen_NonEmptyLogPath_DisplaysLogFile verifies that when
// summary.LogPath is non-empty, View() contains "Log file: <path>".
func TestResultsScreen_NonEmptyLogPath_DisplaysLogFile(t *testing.T) {
	logPath := "/workspace/RunnerLogs/startup-20260918T155328.log"

	summary := summaryWithOneResult(testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           false,
		Error:          errors.New("discovery failed"),
		ActualExitCode: 1,
	})
	summary.AllPass = false
	summary.LogPath = logPath

	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	wantSubstring := "Log file: " + logPath
	if !strings.Contains(got, wantSubstring) {
		t.Errorf("TestResultsScreen.View() with non-empty LogPath: does not contain %q\ngot (stripped, first 300): %.300s\n(mandatory output substring per reporter display contract)",
			wantSubstring, got)
	}
}

// TestResultsScreen_EmptyLogPath_NoLogFileLine verifies that when
// summary.LogPath is empty, View() does not contain "Log file:".
func TestResultsScreen_EmptyLogPath_NoLogFileLine(t *testing.T) {
	summary := summaryWithOneResult(testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           false,
		Error:          errors.New("discovery failed"),
		ActualExitCode: 1,
	})
	summary.AllPass = false
	summary.LogPath = "" // explicitly empty

	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	if strings.Contains(got, "Log file:") {
		t.Errorf("TestResultsScreen.View() with empty LogPath: contains \"Log file:\"; want no log path line when LogPath is empty\ngot (stripped): %.200s",
			got)
	}
}

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
