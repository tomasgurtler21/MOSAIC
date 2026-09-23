// Whitebox tests for cliProgressReporter and printLogPath. These tests live
// in package cli (not cli_test) to access the unexported cliProgressReporter
// type, its OnTestDone method, and the printLogPath helper.
//
// Test coverage:
//
//   - cliProgressReporter.OnTestDone: stderr display on ERROR results (always
//     shown when non-empty, including exit code even when 0), stderr and exit
//     code on FAIL results with non-zero exit, no diagnostic output on PASS,
//     no extra output when stderr is empty, and stderr over 8 KiB is truncated
//     with the rune-safe marker.
//
//   - printLogPath: log path printed when !AllPass and path non-empty, not
//     printed when AllPass, not printed when path is empty, not printed when
//     summary is nil.
package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/testrun"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newErrorResult returns a TestRunResult in ERROR state (infrastructure
// failure). Pass includes a non-nil Error and the given ChildStderr and
// ActualExitCode.
func newErrorResult(stderrContent string, exitCode int) testrun.TestRunResult {
	return testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           false,
		Error:          errors.New("infrastructure failure"),
		ChildStderr:    stderrContent,
		ActualExitCode: exitCode,
	}
}

// newFailResult returns a TestRunResult in FAIL state (mismatch, no infra
// error). Pass is false, Error is nil, Mismatch is nil (omitted for brevity).
func newFailResult(stderrContent string, exitCode int) testrun.TestRunResult {
	return testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           false,
		Error:          nil,
		ChildStderr:    stderrContent,
		ActualExitCode: exitCode,
	}
}

// newPassResult returns a TestRunResult in PASS state.
func newPassResult() testrun.TestRunResult {
	return testrun.TestRunResult{
		WorkflowID: "wf-test",
		Mode:       "auto",
		Harness:    "claude-code",
		Pass:       true,
	}
}

// callOnTestDone invokes cliProgressReporter.OnTestDone and returns the
// output written to the underlying buffer.
func callOnTestDone(result testrun.TestRunResult) string {
	var buf bytes.Buffer
	r := &cliProgressReporter{out: &buf}
	r.OnTestDone(result.Harness, result.WorkflowID, result.Mode, result)
	return buf.String()
}

// =============================================================================
// T2.2: cliProgressReporter.OnTestDone -- ERROR result stderr display
// =============================================================================

// TestOnTestDone_ErrorResult_NonEmptyStderr_DisplaysStderrHeader verifies
// that when a result is ERROR and ChildStderr is non-empty, the output
// contains the mandatory "  stderr:" header line.
func TestOnTestDone_ErrorResult_NonEmptyStderr_DisplaysStderrHeader(t *testing.T) {
	result := newErrorResult("child process wrote this to stderr\n", 0)
	got := callOnTestDone(result)

	if !strings.Contains(got, "  stderr:") {
		t.Errorf("OnTestDone ERROR with non-empty ChildStderr: output does not contain mandatory header %q\ngot: %q\n(the stderr: header distinguishes the dedicated stderr block from error text embeds)",
			"  stderr:", got)
	}
}

// TestOnTestDone_ErrorResult_NonEmptyStderr_DisplaysStderrContent verifies
// that the actual stderr content appears in the output for an ERROR result.
func TestOnTestDone_ErrorResult_NonEmptyStderr_DisplaysStderrContent(t *testing.T) {
	const stderrMsg = "subprocess: unexpected output format"
	result := newErrorResult(stderrMsg+"\n", 0)
	got := callOnTestDone(result)

	if !strings.Contains(got, stderrMsg) {
		t.Errorf("OnTestDone ERROR with non-empty ChildStderr: output does not contain stderr content %q\ngot: %q",
			stderrMsg, got)
	}
}

// TestOnTestDone_ErrorResult_DisplaysExitCode_EvenWhenZero verifies that for
// ERROR results, ActualExitCode is always displayed -- even when it is 0.
// Exit code 0 on an ERROR is a diagnostic clue (child ran to completion
// without creating a run folder, or child never started).
func TestOnTestDone_ErrorResult_DisplaysExitCode_EvenWhenZero(t *testing.T) {
	result := newErrorResult("", 0)
	got := callOnTestDone(result)

	if !strings.Contains(got, "  exit code: 0") {
		t.Errorf("OnTestDone ERROR with ActualExitCode=0: output does not contain %q\ngot: %q\n(exit code 0 on ERROR is a diagnostic clue: must be shown even when 0)",
			"  exit code: 0", got)
	}
}

// TestOnTestDone_ErrorResult_DisplaysExitCode_WhenNonZero verifies that for
// ERROR results with a non-zero exit code, the exit code is displayed.
func TestOnTestDone_ErrorResult_DisplaysExitCode_WhenNonZero(t *testing.T) {
	result := newErrorResult("child stderr\n", 137)
	got := callOnTestDone(result)

	if !strings.Contains(got, "  exit code: 137") {
		t.Errorf("OnTestDone ERROR with ActualExitCode=137: output does not contain %q\ngot: %q",
			"  exit code: 137", got)
	}
}

// TestOnTestDone_ErrorResult_EmptyStderr_NoStderrHeader verifies that when
// an ERROR result has empty ChildStderr, the "  stderr:" header is NOT
// emitted (no empty stderr section).
func TestOnTestDone_ErrorResult_EmptyStderr_NoStderrHeader(t *testing.T) {
	result := newErrorResult("", 0) // empty stderr
	got := callOnTestDone(result)

	if strings.Contains(got, "  stderr:") {
		t.Errorf("OnTestDone ERROR with empty ChildStderr: output contains %q; want no stderr section when stderr is empty\ngot: %q",
			"  stderr:", got)
	}
}

// TestOnTestDone_FailResult_EmptyStderr_NoStderrHeader verifies that when
// a FAIL result has empty ChildStderr, the "  stderr:" header is NOT emitted.
// This complements TestOnTestDone_ErrorResult_EmptyStderr_NoStderrHeader which
// covers the ERROR+empty-stderr case. An implementation that guards stderr
// display on result.Error != nil instead of result.ChildStderr != "" would
// incorrectly suppress the header on ERROR (where Error is non-nil) but emit
// a spurious (empty) block on FAIL (where Error is nil).
func TestOnTestDone_FailResult_EmptyStderr_NoStderrHeader(t *testing.T) {
	result := newFailResult("", 1) // empty stderr; non-zero exit to enable diagnostics
	got := callOnTestDone(result)

	if strings.Contains(got, "  stderr:") {
		t.Errorf("OnTestDone FAIL with empty ChildStderr: output contains %q; want no stderr section when stderr is empty\ngot: %q\n(the guard must be on ChildStderr != \"\", not on result.Error != nil)",
			"  stderr:", got)
	}
}

// =============================================================================
// T2.2: cliProgressReporter.OnTestDone -- FAIL result stderr and exit code
// =============================================================================

// TestOnTestDone_FailResult_NonZeroExit_DisplaysExitCode verifies that when
// a FAIL result has a non-zero ActualExitCode, the exit code is displayed.
func TestOnTestDone_FailResult_NonZeroExit_DisplaysExitCode(t *testing.T) {
	result := newFailResult("", 1)
	got := callOnTestDone(result)

	if !strings.Contains(got, "  exit code: 1") {
		t.Errorf("OnTestDone FAIL with ActualExitCode=1: output does not contain %q\ngot: %q",
			"  exit code: 1", got)
	}
}

// TestOnTestDone_FailResult_ZeroExit_DoesNotDisplayExitCode verifies that
// when a FAIL result has a zero exit code, the exit code is NOT displayed
// (exit code 0 on FAIL is not a useful diagnostic for the operator).
func TestOnTestDone_FailResult_ZeroExit_DoesNotDisplayExitCode(t *testing.T) {
	result := newFailResult("", 0)
	got := callOnTestDone(result)

	if strings.Contains(got, "exit code:") {
		t.Errorf("OnTestDone FAIL with ActualExitCode=0: output contains exit code; want no exit code on FAIL with exit 0\ngot: %q",
			got)
	}
}

// TestOnTestDone_FailResult_NonEmptyStderr_DisplaysStderr verifies that a
// FAIL result with non-empty ChildStderr shows the stderr content.
func TestOnTestDone_FailResult_NonEmptyStderr_DisplaysStderr(t *testing.T) {
	const stderrContent = "agent exited with unexpected state"
	result := newFailResult(stderrContent+"\n", 1)
	got := callOnTestDone(result)

	if !strings.Contains(got, "  stderr:") {
		t.Errorf("OnTestDone FAIL with non-empty ChildStderr: output does not contain %q\ngot: %q",
			"  stderr:", got)
	}
	if !strings.Contains(got, stderrContent) {
		t.Errorf("OnTestDone FAIL with non-empty ChildStderr: output does not contain stderr content %q\ngot: %q",
			stderrContent, got)
	}
}

// TestOnTestDone_FailResult_ZeroExit_NonEmptyStderr_StillDisplaysStderr verifies
// that a FAIL result with non-empty ChildStderr and a zero exit code still
// shows the "  stderr:" header and content. Stderr display for FAIL results is
// independent of the exit code; only the exit code display is gated on non-zero.
// An implementation that gates stderr display on ActualExitCode != 0 would pass
// TestOnTestDone_FailResult_NonEmptyStderr_DisplaysStderr (which uses exitCode=1)
// but fail here.
func TestOnTestDone_FailResult_ZeroExit_NonEmptyStderr_StillDisplaysStderr(t *testing.T) {
	const stderrContent = "agent reported failure but exited cleanly"
	result := newFailResult(stderrContent+"\n", 0) // zero exit code
	got := callOnTestDone(result)

	if !strings.Contains(got, "  stderr:") {
		t.Errorf("OnTestDone FAIL with non-empty ChildStderr and exit 0: output does not contain %q\ngot: %q\n(stderr display on FAIL is independent of exit code; gating stderr on exit code != 0 is a bug)",
			"  stderr:", got)
	}
	if !strings.Contains(got, stderrContent) {
		t.Errorf("OnTestDone FAIL with non-empty ChildStderr and exit 0: output does not contain stderr content %q\ngot: %q",
			stderrContent, got)
	}
}

// =============================================================================
// T2.2: cliProgressReporter.OnTestDone -- PASS result (no diagnostics)
// =============================================================================

// TestOnTestDone_PassResult_NoStderrOrExitCode verifies that PASS results do
// not display stderr content or exit code, even when ChildStderr is non-empty
// and ActualExitCode is non-zero (a passing test with non-zero exit is
// possible; the checker decides pass/fail).
func TestOnTestDone_PassResult_NoStderrOrExitCode(t *testing.T) {
	result := testrun.TestRunResult{
		WorkflowID:     "wf-test",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           true,
		ChildStderr:    "benign warning from child\n",
		ActualExitCode: 1,
	}
	got := callOnTestDone(result)

	if strings.Contains(got, "  stderr:") {
		t.Errorf("OnTestDone PASS: output contains stderr section; want no diagnostics on PASS\ngot: %q", got)
	}
	if strings.Contains(got, "exit code:") {
		t.Errorf("OnTestDone PASS: output contains exit code; want no diagnostics on PASS\ngot: %q", got)
	}
}

// =============================================================================
// T2.2: cliProgressReporter.OnTestDone -- stderr truncation at 8 KiB
// =============================================================================

// TestOnTestDone_ErrorResult_StderrOver8KiB_Truncated verifies that when
// ChildStderr exceeds MaxStderrDisplayBytes (8192 bytes), the output
// contains the [truncated, showing last ... bytes] marker and only the
// tail of the stderr content is shown.
func TestOnTestDone_ErrorResult_StderrOver8KiB_Truncated(t *testing.T) {
	// Build stderr that exceeds 8192 bytes: 8200 bytes of 'A' followed by a
	// unique sentinel string. TruncateTail will retain only the last 8192 bytes.
	prefix := strings.Repeat("A", 8200)
	sentinel := "SENTINEL_END_OF_STDERR"
	stderrContent := prefix + sentinel

	result := newErrorResult(stderrContent, 1)
	got := callOnTestDone(result)

	// The truncation marker must appear.
	if !strings.Contains(got, "[truncated, showing last") {
		t.Errorf("OnTestDone ERROR with stderr > 8KiB: output does not contain truncation marker\ngot (first 200 chars): %q\n(stderr exceeding MaxStderrDisplayBytes must be truncated via TruncateTail)",
			safeFirst(got, 200))
	}

	// The sentinel at the end must be present (tail is shown).
	if !strings.Contains(got, sentinel) {
		t.Errorf("OnTestDone ERROR with stderr > 8KiB: output does not contain tail sentinel %q\n(TruncateTail shows the LAST bytes, not the first)",
			sentinel)
	}

	// The first 8200 'A' bytes should NOT all appear (they are truncated away).
	// We check that the pure-A prefix (first 8200 bytes) is not present in its
	// entirety by verifying the total length of 'A' runs in the output is < 8200.
	aCount := 0
	for _, b := range got {
		if b == 'A' {
			aCount++
		}
	}
	if aCount >= 8200 {
		t.Errorf("OnTestDone ERROR with stderr > 8KiB: output contains %d 'A' characters, want fewer than 8200 (truncation must drop the leading portion)",
			aCount)
	}
}

// =============================================================================
// T2.4: printLogPath -- log path display
// =============================================================================

// TestPrintLogPath_NotAllPass_NonEmptyPath_PrintsLogLine verifies that
// printLogPath prints the log file path when !AllPass and logPath is non-empty.
// The output must contain the mandatory substring "Log file: <path>".
func TestPrintLogPath_NotAllPass_NonEmptyPath_PrintsLogLine(t *testing.T) {
	var buf bytes.Buffer
	logPath := "/workspace/RunnerLogs/startup-20260918T155328.log"
	summary := &testrun.TestSummary{
		AllPass:    false,
		TotalFail:  1,
		TotalError: 0,
	}

	printLogPath(&buf, summary, logPath)

	got := buf.String()
	wantSubstring := "Log file: " + logPath
	if !strings.Contains(got, wantSubstring) {
		t.Errorf("printLogPath(!AllPass, non-empty path): output does not contain %q\ngot: %q\n(mandatory output substring per reporter display contract)",
			wantSubstring, got)
	}
}

// TestPrintLogPath_AllPass_NonEmptyPath_PrintsNothing verifies that
// printLogPath prints nothing when AllPass is true, even when logPath is
// non-empty. Log path is only shown on failure.
func TestPrintLogPath_AllPass_NonEmptyPath_PrintsNothing(t *testing.T) {
	var buf bytes.Buffer
	logPath := "/workspace/RunnerLogs/startup-20260918T155328.log"
	summary := &testrun.TestSummary{
		AllPass: true,
	}

	printLogPath(&buf, summary, logPath)

	got := buf.String()
	if got != "" {
		t.Errorf("printLogPath(AllPass=true, non-empty path): expected no output, got: %q\n(log path must not be displayed when all tests pass)",
			got)
	}
}

// TestPrintLogPath_NotAllPass_EmptyPath_PrintsNothing verifies that
// printLogPath prints nothing when logPath is empty, even when !AllPass.
// An empty path means no invocations occurred (e.g., deploy failure).
func TestPrintLogPath_NotAllPass_EmptyPath_PrintsNothing(t *testing.T) {
	var buf bytes.Buffer
	summary := &testrun.TestSummary{
		AllPass:   false,
		TotalFail: 1,
	}

	printLogPath(&buf, summary, "") // empty path: deploy failure, no log writes

	got := buf.String()
	if got != "" {
		t.Errorf("printLogPath(!AllPass, empty path): expected no output, got: %q\n(empty path guard: no invocations occurred, no log file to show)",
			got)
	}
}

// TestPrintLogPath_NilSummary_PrintsNothing verifies that printLogPath does
// not panic and produces no output when summary is nil.
func TestPrintLogPath_NilSummary_PrintsNothing(t *testing.T) {
	var buf bytes.Buffer

	// This must not panic.
	printLogPath(&buf, nil, "/workspace/RunnerLogs/startup-20260918.log")

	got := buf.String()
	if got != "" {
		t.Errorf("printLogPath(nil summary): expected no output, got: %q", got)
	}
}

// =============================================================================
// Helpers for test file
// =============================================================================

// safeFirst returns the first n bytes of s (or all of s when len(s) <= n).
// Used to truncate long output strings in error messages.
func safeFirst(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
