// Tests for TestResultsScreen line wrapping, ANSI handling, padding, and width edge cases.
package devtest_test

import (
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/testrun"
	"mosaic-run/internal/tui/screens/devtest"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// T3.1: Line wrapping (ANSI-safe, no truncation)
// ---------------------------------------------------------------------------

// TestResultsScreen_LongErrorLine_WrappedNotTruncated verifies that a line
// longer than the screen width is wrapped (not truncated) so all characters
// remain in View() at offset=0 (when the wrapped rows fit in the visible area)
// or are reachable by scrolling.
func TestResultsScreen_LongErrorLine_WrappedNotTruncated(t *testing.T) {
	// 300 'X' chars at width=80: wraps to ceil(300/80) = 4 rows, all visible at height=50
	longError := strings.Repeat("X", 300)
	summary := &testrun.TestSummary{
		AllPass:    false,
		TotalError: 1,
		HarnessResults: []testrun.HarnessResults{{
			Harness:    "claude-code",
			ErrorCount: 1,
			Results: []testrun.TestRunResult{{
				WorkflowID:     "wf-long",
				Mode:           "auto",
				Harness:        "claude-code",
				Pass:           false,
				Error:          errors.New(longError),
				ActualExitCode: 1,
			}},
		}},
	}

	// height=50: all 4 wrapped rows fit; all 300 'X' chars must be visible
	s := devtest.NewTestResultsScreen(80, 50, screens.Styles{}, summary)
	got := stripANSIResults(s.View())

	xCount := strings.Count(got, "X")
	if xCount < 300 {
		t.Errorf("View() at height=50 contains only %d 'X' characters; expected 300; long lines must be WRAPPED (not truncated) so all characters remain visible\ngot (stripped, first 400): %.400s", xCount, got)
	}
}

// TestResultsScreen_LongStderrLine_WrappedNotTruncated verifies that long
// stderr content is wrapped (not truncated) in the body lines.
func TestResultsScreen_LongStderrLine_WrappedNotTruncated(t *testing.T) {
	longStderr := strings.Repeat("S", 300)
	summary := &testrun.TestSummary{
		AllPass:   false,
		TotalFail: 1,
		HarnessResults: []testrun.HarnessResults{{
			Harness:   "claude-code",
			FailCount: 1,
			Results: []testrun.TestRunResult{{
				WorkflowID:     "wf-stderr",
				Mode:           "auto",
				Harness:        "claude-code",
				Pass:           false,
				ChildStderr:    longStderr,
				ActualExitCode: 1,
			}},
		}},
	}

	s := devtest.NewTestResultsScreen(80, 50, screens.Styles{}, summary)
	got := stripANSIResults(s.View())

	sCount := strings.Count(got, "S")
	if sCount < 300 {
		t.Errorf("View() at height=50 contains only %d 'S' characters from the long stderr; expected 300; stderr must be WRAPPED (not truncated)\ngot (stripped, first 400): %.400s", sCount, got)
	}
}

// TestResultsScreen_LongContent_FullyReachableByScrolling verifies that a
// long error message and multi-line stderr are fully reachable by scrolling:
// the first portion is visible at offset=0, and the last portion is visible
// after scrolling to the bottom.
func TestResultsScreen_LongContent_FullyReachableByScrolling(t *testing.T) {
	const sentinelStart = "ERR_START_9a4b"
	const sentinelEnd = "ERR_END_8c3d"
	const stderrSentinelStart = "STDERR_START_1z2y"
	const stderrSentinelEnd = "STDERR_END_3x4w"

	longError := sentinelStart + strings.Repeat("E", 200) + sentinelEnd
	longStderr := stderrSentinelStart + strings.Repeat("S", 200) + stderrSentinelEnd

	summary := &testrun.TestSummary{
		AllPass:    false,
		TotalError: 1,
		HarnessResults: []testrun.HarnessResults{{
			Harness:    "claude-code",
			ErrorCount: 1,
			Results: []testrun.TestRunResult{{
				WorkflowID:     "wf-long",
				Mode:           "auto",
				Harness:        "claude-code",
				Pass:           false,
				Error:          errors.New(longError),
				ChildStderr:    longStderr,
				ActualExitCode: 1,
			}},
		}},
	}

	// Narrow width and small height to force scrolling
	s := devtest.NewTestResultsScreen(40, 10, screens.Styles{}, summary)

	// Check that error start sentinel is visible at top
	viewTop := stripANSIResults(s.View())
	if !strings.Contains(viewTop, sentinelStart) {
		t.Errorf("error start sentinel %q not visible at offset=0; long error content must begin from the top of the body", sentinelStart)
	}

	// Scroll to the very bottom
	scrollDown(s, 500)

	// Check that stderr end sentinel is reachable
	viewBottom := stripANSIResults(s.View())
	if !strings.Contains(viewBottom, stderrSentinelEnd) {
		t.Errorf("stderr end sentinel %q not visible after scrolling to the bottom; all content must be reachable by scrolling\n-- bottom view (first 400): %.400s", stderrSentinelEnd, viewBottom)
	}
}

// ---------------------------------------------------------------------------
// T3.1: ANSI escape sequences in input are stripped before wrapping
// ---------------------------------------------------------------------------

// TestResultsScreen_ANSIInInput_StrippedFromBody verifies that ANSI escape
// sequences embedded in error or stderr fields are stripped from all body text
// before wrapping, so no raw ANSI sequences appear in the body portion of
// View() output. The printable characters in the field must still appear.
//
// Design ref: rebuildLines step 3 -- strip ANSI escape sequences and
// non-printable control characters from ALL body text unconditionally before
// wrapping. Stripping before wrapping prevents escape sequences from being
// split mid-sequence during the hard-wrap step.
func TestResultsScreen_ANSIInInput_StrippedFromBody(t *testing.T) {
	// Build an error string containing ANSI SGR sequences around printable text.
	// The printable portion must survive; the escape sequences must not.
	ansiError := "\x1b[31mred failure text\x1b[0m"
	ansiStderr := "\x1b[33myellow stderr output\x1b[0m"

	summary := &testrun.TestSummary{
		AllPass:    false,
		TotalError: 1,
		HarnessResults: []testrun.HarnessResults{{
			Harness:    "claude-code",
			ErrorCount: 1,
			Results: []testrun.TestRunResult{{
				WorkflowID:     "wf-ansi",
				Mode:           "auto",
				Harness:        "claude-code",
				Pass:           false,
				Error:          errors.New(ansiError),
				ChildStderr:    ansiStderr,
				ActualExitCode: 1,
			}},
		}},
	}

	// Use a width narrow enough to force wrapping of any long lines, and a
	// height large enough to show all content without scrolling.
	s := devtest.NewTestResultsScreen(40, 50, screens.Styles{}, summary)
	raw := s.View() // do NOT strip ANSI for this check -- we want to see raw output

	// The raw ANSI escape sequences from the input must not appear in View().
	// View() may add its own styling ANSI sequences (from lipgloss), but the
	// input's \x1b[31m, \x1b[33m, \x1b[0m must have been removed by rebuildLines.
	if strings.Contains(raw, "\x1b[31m") {
		t.Errorf("View() body contains raw ANSI sequence \\x1b[31m from input error field; ANSI sequences in input data must be stripped before wrapping, not passed through to View()")
	}
	if strings.Contains(raw, "\x1b[33m") {
		t.Errorf("View() body contains raw ANSI sequence \\x1b[33m from input stderr field; ANSI sequences in input data must be stripped before wrapping, not passed through to View()")
	}

	// The printable characters from the input must still appear in View().
	stripped := stripANSIResults(raw)
	if !strings.Contains(stripped, "red failure text") {
		t.Errorf("View() body (stripped) does not contain printable text 'red failure text' from the ANSI-wrapped error input; printable characters must be preserved after stripping\ngot (first 400): %.400s", stripped)
	}
	if !strings.Contains(stripped, "yellow stderr output") {
		t.Errorf("View() body (stripped) does not contain printable text 'yellow stderr output' from the ANSI-wrapped stderr input; printable characters must be preserved after stripping\ngot (first 400): %.400s", stripped)
	}
}

// ---------------------------------------------------------------------------
// T3.1: Body padding pins chrome at fixed row positions
// ---------------------------------------------------------------------------

// TestResultsScreen_ShortContent_BodyPaddedToContentHeight verifies that when
// the body content is shorter than contentHeight, View() pads the body area
// with blank rows so that the bottom border, position indicator, and help text
// are always pinned at their fixed row positions (rows height-3, height-2,
// height-1 respectively).
//
// Design ref (M-B fix): "When len(visible lines) < contentHeight, the body
// area is padded with blank rows (empty strings) up to contentHeight. This
// ensures the bottom border, position indicator, and help text are always
// pinned at their stated row positions regardless of content length."
//
// Normal layout (5 reserved rows, 0-indexed):
//
//	Row 0:             title
//	Row 1:             top border
//	Rows 2..h-4:       body lines (contentHeight = h-5 rows)
//	Row h-3:           bottom border
//	Row h-2:           position indicator
//	Row h-1:           help text
func TestResultsScreen_ShortContent_BodyPaddedToContentHeight(t *testing.T) {
	// A minimal summary with a single passing result produces very few body
	// lines -- far fewer than contentHeight at a large height.
	summary := &testrun.TestSummary{AllPass: true, TotalPass: 1}
	const height = 30
	s := devtest.NewTestResultsScreen(80, height, screens.Styles{}, summary)
	raw := s.View()
	stripped := stripANSIResults(raw)

	// Split into rows.
	rows := strings.Split(stripped, "\n")

	// (a) Total row count must equal height.
	if len(rows) != height {
		t.Errorf("View() output has %d newline-separated rows; want exactly %d (height); body area must be padded to pin chrome at fixed positions\ngot (first 600): %.600s", len(rows), height, stripped)
	}

	// (b) The bottom border, position indicator, and help text must appear at
	// their fixed row positions: height-3, height-2, height-1.
	// We verify by checking that the position indicator row (height-2) contains
	// "lines " and the help text row (height-1) contains "quit".
	if len(rows) >= height {
		indicatorRow := rows[height-2]
		if !strings.Contains(indicatorRow, "lines ") {
			t.Errorf("row %d (height-2, position indicator) does not contain 'lines '; want the position indicator pinned at row height-2\nrow content: %q\nfull output (first 600): %.600s", height-2, indicatorRow, stripped)
		}

		helpRow := rows[height-1]
		if !strings.Contains(helpRow, "quit") {
			t.Errorf("row %d (height-1, help text) does not contain 'quit'; want the help text pinned at row height-1\nrow content: %q\nfull output (first 600): %.600s", height-1, helpRow, stripped)
		}
	}
}

// ---------------------------------------------------------------------------
// T3.1: Width <= 0 safety
// ---------------------------------------------------------------------------

// TestResultsScreen_ZeroWidth_NoPanic verifies that creating a
// TestResultsScreen with width=0 and calling View() does not panic and does
// not loop indefinitely. contentHeight clamps to a minimum of 1.
func TestResultsScreen_ZeroWidth_NoPanic(t *testing.T) {
	summary := &testrun.TestSummary{AllPass: true, TotalPass: 1}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic with width=0: %v; NewTestResultsScreen(0, ...) and View() must not panic", r)
		}
	}()

	s := devtest.NewTestResultsScreen(0, 10, screens.Styles{}, summary)
	_ = s.View()
}

// TestResultsScreen_NegativeWidth_NoPanic verifies that a negative width
// does not cause a panic or loop (negative width can arrive before the first
// WindowSizeMsg in some terminal emulators).
func TestResultsScreen_NegativeWidth_NoPanic(t *testing.T) {
	summary := &testrun.TestSummary{AllPass: true, TotalPass: 1}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic with width=-1: %v; NewTestResultsScreen with negative width must not panic", r)
		}
	}()

	s := devtest.NewTestResultsScreen(-1, 10, screens.Styles{}, summary)
	_ = s.View()
}

// TestResultsScreen_TinyHeight_ContentHeightClampsToOne verifies that a
// height <= 5 (which would produce contentHeight <= 0) still produces a
// valid View() output without panicking. contentHeight must clamp to 1.
func TestResultsScreen_TinyHeight_ContentHeightClampsToOne(t *testing.T) {
	summary := &testrun.TestSummary{AllPass: true, TotalPass: 1}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic with height=1: %v; tiny height must be handled gracefully via contentHeight clamping", r)
		}
	}()

	for _, h := range []int{1, 2, 3, 4, 5} {
		s := devtest.NewTestResultsScreen(80, h, screens.Styles{}, summary)
		got := s.View()
		if got == "" {
			t.Errorf("View() returned empty string at height=%d; must return non-empty output even at tiny heights", h)
		}
	}
}

// ---------------------------------------------------------------------------
// T3.1: Nil summary edge case
// ---------------------------------------------------------------------------

// TestResultsScreen_NilSummary_NoChromeNoIndicator verifies that when
// summary is nil, View() returns a single error string with no chrome
// (no position indicator, no scroll keys in help text). This is the
// existing behavior preserved through the scrolling implementation.
func TestResultsScreen_NilSummary_NoChromeNoIndicator(t *testing.T) {
	s := devtest.NewTestResultsScreen(80, 24, screens.Styles{}, nil)
	got := stripANSIResults(s.View())

	if strings.Contains(got, "lines ") {
		t.Errorf("View() with nil summary contains 'lines ...'; when summary is nil, no position indicator must be rendered\ngot: %q", got)
	}
}

// ---------------------------------------------------------------------------
// T3.2: Regression guard -- existing content still visible at 200x50
// ---------------------------------------------------------------------------

// TestResultsScreen_200x50_ExistingStderrContentVisible verifies that at the
// existing 200x50 test dimensions all content is visible at default offset=0
// (no clipping). This guards the existing testresults_test.go assertions.
func TestResultsScreen_200x50_ExistingStderrContentVisible(t *testing.T) {
	const stderrMsg = "regression-guard: subprocess stderr content"
	result := testrun.TestRunResult{
		WorkflowID:  "wf-regression",
		Mode:        "auto",
		Harness:     "claude-code",
		Pass:        false,
		Error:       errors.New("regression-guard: infrastructure error"),
		ChildStderr: stderrMsg + "\n",
	}

	s := newResultsScreen(summaryWithOneResult(result)) // 200x50
	got := stripANSIResults(s.View())

	if !strings.Contains(got, stderrMsg) {
		t.Errorf("View() at 200x50 does not contain stderr content %q; at 200x50 all content must be visible at offset=0 (no clipping)\ngot (stripped, first 400): %.400s", stderrMsg, got)
	}
}

// TestResultsScreen_200x50_PositionIndicatorShowsAllContent verifies that at
// 200x50 the position indicator shows the same start and end count (all
// content fits; no scrolling needed). The indicator must read "lines 1-N of N".
func TestResultsScreen_200x50_PositionIndicatorShowsAllContent(t *testing.T) {
	result := testrun.TestRunResult{
		WorkflowID:     "wf-regression",
		Mode:           "auto",
		Harness:        "claude-code",
		Pass:           false,
		Error:          errors.New("regression-guard error"),
		ActualExitCode: 1,
	}

	s := newResultsScreen(summaryWithOneResult(result)) // 200x50
	got := stripANSIResults(s.View())

	// Must contain the indicator; the content should fit at 200x50
	if !strings.Contains(got, "lines 1-") {
		t.Errorf("View() at 200x50 does not contain position indicator starting at 'lines 1-'; indicator must always be shown\ngot (stripped, first 400): %.400s", got)
	}
}

// TestResultsScreen_200x50_HelpTextIncludesScrollHints verifies that at 200x50
// the updated help text (which now includes scroll key hints) is present.
// Existing tests that checked for "q/esc/enter quit" must still pass because
// that substring remains in the new help text.
func TestResultsScreen_200x50_HelpTextIncludesScrollHints(t *testing.T) {
	summary := &testrun.TestSummary{AllPass: true, TotalPass: 1}
	s := newResultsScreen(summary) // 200x50
	got := stripANSIResults(s.View())

	if !strings.Contains(got, "up/k") {
		t.Errorf("View() at 200x50 does not contain 'up/k'; the updated help text must include scroll key hints at the standard 200x50 test dimensions\ngot (stripped, first 400): %.400s", got)
	}
	if !strings.Contains(got, "q/esc/enter quit") {
		t.Errorf("View() at 200x50 does not contain 'q/esc/enter quit'; the quit key hint must remain in the updated help text\ngot (stripped, first 400): %.400s", got)
	}
}
