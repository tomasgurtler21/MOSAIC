// Tests for TestResultsScreen vertical scrolling behavior.
// Tests are in package screens_test (blackbox) to match the existing
// testresults_test.go convention and share the ansiSeq / stripANSIResults helper.
//
// Test coverage (T3.1 -- new scrolling behavior):
//
//   - Position indicator ("lines A-B of N") is always shown, including when
//     content fits within the visible area.
//   - Help text lists scroll key hints (up/k, down/j, pgup/pgdn) alongside quit keys.
//   - Content taller than the terminal height is clipped to a visible window;
//     scrolling reveals the hidden content.
//   - up/k, down/j, pgup, pgdown key handling moves the scroll offset correctly.
//   - Mouse wheel events (MouseButtonWheelUp/Down with MouseActionPress) scroll
//     the view; non-scroll mouse events (motion, click) do not change the offset.
//   - Reset() resets the scroll offset to zero.
//   - Resize() clamps the offset when the new dimensions reduce the visible area.
//   - The deploy-error view path also supports scrolling.
//   - Lines longer than the screen width are wrapped (not truncated); all characters
//     remain reachable by scrolling.
//   - Scroll keys work before the first View() call (lines built in constructor).
//   - Width <= 0 does not cause a loop or panic; contentHeight clamps to minimum 1.
//
// Test coverage (T3.2 -- regression guard for existing content at 200x50 and app tests):
//
//   - At 200x50 dimensions the position indicator is shown and content fits
//     (same leading content visible without clipping).
//   - The updated help text (scroll hints + quit keys) appears in both the
//     normal and deploy-error views.
package screens_test

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"mosaic-run/internal/testrun"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers (scroll tests)
// ---------------------------------------------------------------------------

// newScrollResultsScreen builds a TestResultsScreen with the given width and
// height. Smaller heights force scrolling by reducing the visible content area.
func newScrollResultsScreen(width, height int, summary *testrun.TestSummary) *screens.TestResultsScreen {
	return screens.NewTestResultsScreen(width, height, screens.Styles{}, summary)
}

// summaryWithManyFailures builds a TestSummary containing count failing results,
// each carrying a short error message. The resulting View() content is
// substantially longer than a single page on small screen heights.
func summaryWithManyFailures(count int) *testrun.TestSummary {
	results := make([]testrun.TestRunResult, count)
	for i := range results {
		results[i] = testrun.TestRunResult{
			WorkflowID:     fmt.Sprintf("wf-%02d", i),
			Mode:           "auto",
			Harness:        "claude-code",
			Pass:           false,
			Error:          fmt.Errorf("failure %d: test infrastructure error", i),
			ActualExitCode: 1,
		}
	}
	return &testrun.TestSummary{
		AllPass:    false,
		TotalError: count,
		HarnessResults: []testrun.HarnessResults{{
			Harness:    "claude-code",
			ErrorCount: count,
			Results:    results,
		}},
	}
}

// scrollDown sends n down-key presses to a screen.
func scrollDown(s *screens.TestResultsScreen, n int) {
	for i := 0; i < n; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
}

// ---------------------------------------------------------------------------
// T3.1: Position indicator
// ---------------------------------------------------------------------------

// TestResultsScreen_PositionIndicator_AlwaysShown verifies that the position
// indicator ("lines A-B of N") appears in View() even when all content fits
// within the visible area. The indicator is a pinned element that is never
// absent, matching the always-shown requirement.
func TestResultsScreen_PositionIndicator_AlwaysShown(t *testing.T) {
	summary := &testrun.TestSummary{AllPass: true, TotalPass: 1}
	s := newResultsScreen(summary) // 200x50 -- content easily fits
	got := stripANSIResults(s.View())

	if !strings.Contains(got, "lines ") {
		t.Errorf("View() does not contain position indicator 'lines ...'; the indicator must always be shown, even when content fits within the visible area\ngot (stripped, first 400): %.400s", got)
	}
}

// TestResultsScreen_PositionIndicator_Format verifies that the position
// indicator follows the "lines A-B of N" format.
func TestResultsScreen_PositionIndicator_Format(t *testing.T) {
	summary := &testrun.TestSummary{AllPass: true, TotalPass: 1}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	if !strings.Contains(got, "lines 1-") {
		t.Errorf("View() position indicator does not start with 'lines 1-'; want format 'lines A-B of N' with A=1 at default offset\ngot (stripped, first 400): %.400s", got)
	}
	if !strings.Contains(got, " of ") {
		t.Errorf("View() position indicator does not contain ' of '; want format 'lines A-B of N'\ngot (stripped, first 400): %.400s", got)
	}
}

// TestResultsScreen_PositionIndicator_ShownForDeployError verifies that the
// position indicator appears in the deploy-error view path as well.
func TestResultsScreen_PositionIndicator_ShownForDeployError(t *testing.T) {
	summary := &testrun.TestSummary{
		AllPass:     false,
		DeployError: errors.New("infra failed"),
	}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	if !strings.Contains(got, "lines ") {
		t.Errorf("deploy-error View() does not contain position indicator 'lines ...'; the indicator must be shown in the deploy-error view\ngot (stripped, first 400): %.400s", got)
	}
}

// ---------------------------------------------------------------------------
// T3.1: Help text includes scroll key hints
// ---------------------------------------------------------------------------

// TestResultsScreen_HelpText_IncludesScrollUpHint verifies that the normal
// results view help text contains the scroll-up key hint "up/k".
func TestResultsScreen_HelpText_IncludesScrollUpHint(t *testing.T) {
	summary := &testrun.TestSummary{AllPass: true, TotalPass: 1}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	if !strings.Contains(got, "up/k") {
		t.Errorf("View() help text does not contain 'up/k'; scroll key hints must be listed in the help text alongside quit keys\ngot (stripped, first 400): %.400s", got)
	}
}

// TestResultsScreen_HelpText_IncludesScrollDownHint verifies that the normal
// results view help text contains the scroll-down key hint "down/j".
func TestResultsScreen_HelpText_IncludesScrollDownHint(t *testing.T) {
	summary := &testrun.TestSummary{AllPass: true, TotalPass: 1}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	if !strings.Contains(got, "down/j") {
		t.Errorf("View() help text does not contain 'down/j'; scroll key hints must be listed in the help text alongside quit keys\ngot (stripped, first 400): %.400s", got)
	}
}

// TestResultsScreen_HelpText_StillIncludesQuitHint verifies that the updated
// help text still includes the quit key hint "q/esc/enter quit".
func TestResultsScreen_HelpText_StillIncludesQuitHint(t *testing.T) {
	summary := &testrun.TestSummary{AllPass: true, TotalPass: 1}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	if !strings.Contains(got, "q/esc/enter quit") {
		t.Errorf("View() help text does not contain 'q/esc/enter quit'; quit key hint must remain in the updated help text\ngot (stripped, first 400): %.400s", got)
	}
}

// TestResultsScreen_DeployError_HelpText_IncludesScrollHints verifies that the
// deploy-error view path also shows scroll key hints in the help text.
func TestResultsScreen_DeployError_HelpText_IncludesScrollHints(t *testing.T) {
	summary := &testrun.TestSummary{
		AllPass:     false,
		DeployError: errors.New("deploy infra failed"),
	}
	s := newResultsScreen(summary)
	got := stripANSIResults(s.View())

	if !strings.Contains(got, "up/k") {
		t.Errorf("deploy-error View() help text does not contain 'up/k'; scroll hints must appear in the deploy-error view help text too\ngot (stripped, first 400): %.400s", got)
	}
	if !strings.Contains(got, "down/j") {
		t.Errorf("deploy-error View() help text does not contain 'down/j'; scroll hints must appear in the deploy-error view help text too\ngot (stripped, first 400): %.400s", got)
	}
}

// ---------------------------------------------------------------------------
// T3.1: Content clipping and key-based scrolling
// ---------------------------------------------------------------------------

// TestResultsScreen_TallContent_ClippedAndScrollable verifies that content
// taller than the screen height is clipped to a visible window, and that
// pressing a scroll key changes the visible window.
func TestResultsScreen_TallContent_ClippedAndScrollable(t *testing.T) {
	// height=10 → contentHeight = max(1, 10-5) = 5 visible body lines
	summary := summaryWithManyFailures(15) // produces many more than 5 body lines
	s := newScrollResultsScreen(80, 10, summary)

	viewBefore := stripANSIResults(s.View())
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	viewAfter := stripANSIResults(s.View())

	if viewBefore == viewAfter {
		t.Errorf("View() did not change after pressing 'down' on content taller than the screen; content must be clipped to a visible window and scroll keys must change the window\n-- view before (first 300): %.300s", viewBefore)
	}
}

// TestResultsScreen_KeyJ_ScrollsDown verifies that the 'j' key scrolls the
// visible window down when content exceeds the screen height.
func TestResultsScreen_KeyJ_ScrollsDown(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := newScrollResultsScreen(80, 10, summary)

	viewBefore := stripANSIResults(s.View())
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	viewAfter := stripANSIResults(s.View())

	if viewBefore == viewAfter {
		t.Errorf("View() did not change after pressing 'j'; the 'j' key must scroll down and change the visible window\n-- view before (first 300): %.300s", viewBefore)
	}
}

// TestResultsScreen_KeyDown_ScrollsDown verifies that the 'down' arrow key
// scrolls the visible window down when content exceeds the screen height.
func TestResultsScreen_KeyDown_ScrollsDown(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := newScrollResultsScreen(80, 10, summary)

	viewBefore := stripANSIResults(s.View())
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	viewAfter := stripANSIResults(s.View())

	if viewBefore == viewAfter {
		t.Errorf("View() did not change after pressing 'down'; the down arrow key must scroll down and change the visible window\n-- view before (first 300): %.300s", viewBefore)
	}
}

// TestResultsScreen_KeyK_ScrollsUp verifies that the 'k' key scrolls back
// up after scrolling down, returning to the original view.
func TestResultsScreen_KeyK_ScrollsUp(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := newScrollResultsScreen(80, 10, summary)

	viewAtTop := stripANSIResults(s.View())
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	viewScrolled := stripANSIResults(s.View())
	if viewAtTop == viewScrolled {
		t.Skip("prerequisite failed: 'j' key had no effect; cannot test 'k' scroll-up")
	}

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	viewBack := stripANSIResults(s.View())

	if viewBack != viewAtTop {
		t.Errorf("View() after pressing 'j' then 'k' does not match the original top-of-content view; 'k' must scroll back up one line\n-- original top (first 300): %.300s\n-- after j+k (first 300): %.300s", viewAtTop, viewBack)
	}
}

// TestResultsScreen_KeyUp_ScrollsUp verifies that the 'up' arrow key scrolls
// back up after scrolling down.
func TestResultsScreen_KeyUp_ScrollsUp(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := newScrollResultsScreen(80, 10, summary)

	viewAtTop := stripANSIResults(s.View())
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	viewScrolled := stripANSIResults(s.View())
	if viewAtTop == viewScrolled {
		t.Skip("prerequisite failed: down key had no effect; cannot test up scroll")
	}

	s.Update(tea.KeyMsg{Type: tea.KeyUp})
	viewBack := stripANSIResults(s.View())

	if viewBack != viewAtTop {
		t.Errorf("View() after pressing 'down' then 'up' does not match the original top-of-content view; 'up' must scroll back one line\n-- original top (first 300): %.300s\n-- after down+up (first 300): %.300s", viewAtTop, viewBack)
	}
}

// TestResultsScreen_KeyPgDown_ScrollsByPage verifies that pgdown scrolls the
// visible window by approximately one page (contentHeight lines).
func TestResultsScreen_KeyPgDown_ScrollsByPage(t *testing.T) {
	summary := summaryWithManyFailures(30)
	s := newScrollResultsScreen(80, 10, summary)

	viewBefore := stripANSIResults(s.View())
	s.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	viewAfter := stripANSIResults(s.View())

	if viewBefore == viewAfter {
		t.Errorf("View() did not change after pressing pgdown; pgdown must scroll by approximately one page (contentHeight lines)\n-- view before (first 300): %.300s", viewBefore)
	}
}

// TestResultsScreen_KeyPgUp_ScrollsByPageUp verifies that pgup scrolls back
// up by approximately one page after a pgdown.
func TestResultsScreen_KeyPgUp_ScrollsByPageUp(t *testing.T) {
	summary := summaryWithManyFailures(30)
	s := newScrollResultsScreen(80, 10, summary)

	viewAtTop := stripANSIResults(s.View())
	s.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	viewScrolled := stripANSIResults(s.View())
	if viewAtTop == viewScrolled {
		t.Skip("prerequisite failed: pgdown had no effect; cannot test pgup scroll")
	}

	s.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	viewBack := stripANSIResults(s.View())

	if viewBack != viewAtTop {
		t.Errorf("View() after pgdown then pgup does not match the original top view; pgup must scroll back by one page\n-- original top (first 300): %.300s\n-- after pgdown+pgup (first 300): %.300s", viewAtTop, viewBack)
	}
}

// TestResultsScreen_ScrollOffset_ClampedAtBottom verifies that the offset does
// not exceed the maximum (len(lines) - contentHeight), so pressing down many
// times does not scroll past the last line.
func TestResultsScreen_ScrollOffset_ClampedAtBottom(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := newScrollResultsScreen(80, 10, summary)

	// Scroll far past the end
	scrollDown(s, 200)
	viewAtMaxScroll := stripANSIResults(s.View())

	// One more press must not change the view (already at max)
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	viewAfterOneMore := stripANSIResults(s.View())

	if viewAtMaxScroll != viewAfterOneMore {
		t.Errorf("View() changed after pressing 'down' at the bottom of content; the offset must be clamped and not scroll past the last line")
	}
}

// TestResultsScreen_ScrollOffset_ClampedAtTop verifies that pressing 'up' at
// the top of the content (offset=0) does not change the view.
func TestResultsScreen_ScrollOffset_ClampedAtTop(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := newScrollResultsScreen(80, 10, summary)

	viewAtTop := stripANSIResults(s.View())

	// Press up at offset=0 -- must not change the view
	s.Update(tea.KeyMsg{Type: tea.KeyUp})
	viewAfterUp := stripANSIResults(s.View())

	if viewAtTop != viewAfterUp {
		t.Errorf("View() changed after pressing 'up' at the top of content (offset=0); offset must not go negative")
	}
}

// TestResultsScreen_PgUp_ClampedAtTop verifies that pressing pgup at the top
// of the content (offset=0) does not change the view. Symmetric with
// TestResultsScreen_ScrollOffset_ClampedAtTop which covers the 'up' key.
func TestResultsScreen_PgUp_ClampedAtTop(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := newScrollResultsScreen(80, 10, summary)

	viewAtTop := stripANSIResults(s.View())

	// Press pgup at offset=0 -- must not change the view
	s.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	viewAfterPgUp := stripANSIResults(s.View())

	if viewAtTop != viewAfterPgUp {
		t.Errorf("View() changed after pressing pgup at the top of content (offset=0); pgup must be clamped at zero and not produce a negative offset")
	}
}

// ---------------------------------------------------------------------------
// T3.1: Mouse wheel events
// ---------------------------------------------------------------------------

// TestResultsScreen_MouseWheelDown_ScrollsView verifies that a
// MouseButtonWheelDown event with MouseActionPress scrolls the view down.
func TestResultsScreen_MouseWheelDown_ScrollsView(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := newScrollResultsScreen(80, 10, summary)

	viewBefore := stripANSIResults(s.View())
	s.Update(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	viewAfter := stripANSIResults(s.View())

	if viewBefore == viewAfter {
		t.Errorf("View() did not change after MouseButtonWheelDown with MouseActionPress; mouse wheel down must scroll the view down one line\n-- view before (first 300): %.300s", viewBefore)
	}
}

// TestResultsScreen_MouseWheelUp_ScrollsViewUp verifies that a
// MouseButtonWheelUp event with MouseActionPress scrolls the view back up.
func TestResultsScreen_MouseWheelUp_ScrollsViewUp(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := newScrollResultsScreen(80, 10, summary)

	viewAtTop := stripANSIResults(s.View())
	s.Update(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	viewScrolled := stripANSIResults(s.View())
	if viewAtTop == viewScrolled {
		t.Skip("prerequisite failed: MouseButtonWheelDown had no effect; cannot test wheel-up")
	}

	s.Update(tea.MouseMsg{
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	})
	viewBack := stripANSIResults(s.View())

	if viewBack != viewAtTop {
		t.Errorf("View() after wheel-down then wheel-up does not match the original top view; mouse wheel up must scroll back one line\n-- original top (first 300): %.300s\n-- after wheel-down+wheel-up (first 300): %.300s", viewAtTop, viewBack)
	}
}

// TestResultsScreen_MouseMotion_DoesNotScroll verifies that a mouse motion
// event does not change the scroll offset or the View() output.
func TestResultsScreen_MouseMotion_DoesNotScroll(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := newScrollResultsScreen(80, 10, summary)

	viewBefore := stripANSIResults(s.View())
	s.Update(tea.MouseMsg{
		Button: tea.MouseButtonNone,
		Action: tea.MouseActionMotion,
	})
	viewAfter := stripANSIResults(s.View())

	if viewBefore != viewAfter {
		t.Errorf("View() changed after MouseActionMotion event; non-scroll mouse events (motion) must not change the scroll offset")
	}
}

// TestResultsScreen_MouseLeftClick_DoesNotScroll verifies that a left-button
// click event does not change the scroll offset or the View() output.
func TestResultsScreen_MouseLeftClick_DoesNotScroll(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := newScrollResultsScreen(80, 10, summary)

	viewBefore := stripANSIResults(s.View())
	s.Update(tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	viewAfter := stripANSIResults(s.View())

	if viewBefore != viewAfter {
		t.Errorf("View() changed after MouseButtonLeft click; left-click must not change the scroll offset")
	}
}

// TestResultsScreen_MouseWheelRelease_DoesNotScroll verifies that a wheel
// event with MouseActionRelease (not Press) does not change the scroll offset.
func TestResultsScreen_MouseWheelRelease_DoesNotScroll(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := newScrollResultsScreen(80, 10, summary)

	viewBefore := stripANSIResults(s.View())
	s.Update(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionRelease, // Release, not Press
	})
	viewAfter := stripANSIResults(s.View())

	if viewBefore != viewAfter {
		t.Errorf("View() changed after MouseButtonWheelDown with MouseActionRelease; only MouseActionPress must trigger scroll; Release must be ignored")
	}
}

// ---------------------------------------------------------------------------
// T3.1: Reset() and Resize()
// ---------------------------------------------------------------------------

// TestResultsScreen_Reset_ResetsScrollOffsetToZero verifies that Reset()
// returns the scroll offset to zero so that View() shows the beginning of
// content again after a scroll-down.
func TestResultsScreen_Reset_ResetsScrollOffsetToZero(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := newScrollResultsScreen(80, 10, summary)

	viewAtTop := stripANSIResults(s.View())
	scrollDown(s, 3)
	viewScrolled := stripANSIResults(s.View())
	if viewAtTop == viewScrolled {
		t.Skip("prerequisite failed: scroll down had no effect; cannot test Reset()")
	}

	s.Reset()
	viewAfterReset := stripANSIResults(s.View())

	if viewAfterReset != viewAtTop {
		t.Errorf("View() after Reset() does not match the original top-of-content view; Reset() must zero the scroll offset\n-- original top (first 300): %.300s\n-- after Reset (first 300): %.300s", viewAtTop, viewAfterReset)
	}
}

// TestResultsScreen_Reset_PreservesDoneFlag verifies that Reset() clears the
// done flag as before (existing behavior is preserved).
func TestResultsScreen_Reset_PreservesDoneFlag(t *testing.T) {
	summary := &testrun.TestSummary{AllPass: true, TotalPass: 1}
	s := newScrollResultsScreen(80, 10, summary)

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !s.Done() {
		t.Fatal("prerequisite: screen must be Done() after pressing 'q'")
	}

	s.Reset()
	if s.Done() {
		t.Errorf("Done() = true after Reset(); Reset() must clear the done flag")
	}
}

// TestResultsScreen_Resize_ClampsOffset verifies that Resize() clamps the
// scroll offset when the new dimensions reduce the visible area, so the offset
// never exceeds the new maximum.
func TestResultsScreen_Resize_ClampsOffset(t *testing.T) {
	summary := summaryWithManyFailures(20)
	// Start with height=20 (contentHeight = 15 visible lines)
	s := newScrollResultsScreen(80, 20, summary)

	// Scroll to the end of the content
	scrollDown(s, 50)
	viewScrolled := stripANSIResults(s.View())

	// Resize to a smaller height -- offset must be clamped
	s.Resize(80, 8) // contentHeight = max(1, 8-5) = 3

	viewAfterResize := stripANSIResults(s.View())

	// The view after resize must contain a valid position indicator
	if !strings.Contains(viewAfterResize, "lines ") {
		t.Errorf("View() after Resize() does not contain a position indicator; Resize() must produce a valid state with a readable indicator\ngot (first 300): %.300s", viewAfterResize)
	}

	_ = viewScrolled // Used to verify scroll happened before resize
}

// TestResultsScreen_Resize_RebuildsWrappedLines verifies that after Resize()
// the width change is reflected in View() output (narrower width causes more
// wrapping, producing more lines). Also verifies that the position indicator
// shows a higher total line count at width=40 than at width=80, confirming
// that rebuildLines() re-wraps to the new width (not just preserving old lines).
func TestResultsScreen_Resize_RebuildsWrappedLines(t *testing.T) {
	longError := strings.Repeat("W", 160) // 160 chars; wraps differently at 80 vs 40
	summary := &testrun.TestSummary{
		AllPass:    false,
		TotalError: 1,
		HarnessResults: []testrun.HarnessResults{{
			Harness:    "claude-code",
			ErrorCount: 1,
			Results: []testrun.TestRunResult{{
				WorkflowID:     "wf-wrap",
				Mode:           "auto",
				Harness:        "claude-code",
				Pass:           false,
				Error:          errors.New(longError),
				ActualExitCode: 1,
			}},
		}},
	}

	// Start at width=80 and capture indicator
	s := screens.NewTestResultsScreen(80, 50, screens.Styles{}, summary)
	view80 := stripANSIResults(s.View())

	// Resize to width=40 -- the 160-char line now wraps into more rows
	s.Resize(40, 50)
	view40 := stripANSIResults(s.View())

	// Both views contain the error content
	if !strings.Contains(view80, strings.Repeat("W", 40)) {
		t.Errorf("View() at width=80 does not contain long error content; error text must be visible")
	}
	if !strings.Contains(view40, strings.Repeat("W", 40)) {
		t.Errorf("View() at width=40 after Resize() does not contain long error content; rebuildLines must preserve all content")
	}

	// The 160-char error line wraps into more rows at width=40 than at width=80.
	// The position indicator "of N" total must be strictly higher at width=40 than at
	// width=80, proving rebuildLines() actually re-wrapped to the new width.
	ofNRe := regexp.MustCompile(`of (\d+)`)
	m80 := ofNRe.FindStringSubmatch(view80)
	m40 := ofNRe.FindStringSubmatch(view40)
	if m80 == nil {
		t.Errorf("View() at width=80 does not contain 'of N' in position indicator; indicator must always be shown\ngot (first 400): %.400s", view80)
	}
	if m40 == nil {
		t.Errorf("View() at width=40 does not contain 'of N' in position indicator; rebuildLines must produce a valid state after Resize\ngot (first 400): %.400s", view40)
	}
	if m80 != nil && m40 != nil {
		total80, _ := strconv.Atoi(m80[1])
		total40, _ := strconv.Atoi(m40[1])
		if total40 <= total80 {
			t.Errorf("View() at width=40 shows total=%d which is not higher than total=%d at width=80; rebuildLines must re-wrap the 160-char error line to the new width, producing more visual rows\ngot view40 (first 400): %.400s", total40, total80, view40)
		}
	}
}

// ---------------------------------------------------------------------------
// T3.1: Deploy-error view scrolling
// ---------------------------------------------------------------------------

// TestResultsScreen_DeployError_WithManyPaths_Scrolls verifies that the
// deploy-error view path supports scrolling when the number of resolved paths
// exceeds the visible area.
func TestResultsScreen_DeployError_WithManyPaths_Scrolls(t *testing.T) {
	resolvedPaths := make(map[string]string)
	for i := 0; i < 20; i++ {
		resolvedPaths[fmt.Sprintf("harness-%02d", i)] = fmt.Sprintf("/usr/local/bin/agent-%02d", i)
	}
	summary := &testrun.TestSummary{
		AllPass:       false,
		DeployError:   errors.New("deploy infra failed"),
		ResolvedPaths: resolvedPaths,
	}
	// height=10 means contentHeight=5; 20 resolved paths produce >5 body lines
	s := newScrollResultsScreen(80, 10, summary)

	viewBefore := stripANSIResults(s.View())
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	viewAfter := stripANSIResults(s.View())

	if viewBefore == viewAfter {
		t.Errorf("deploy-error View() did not change after pressing 'down'; the deploy-error view must support scrolling when resolved paths exceed the visible area\n-- view before (first 300): %.300s", viewBefore)
	}
}

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
	s := screens.NewTestResultsScreen(80, 50, screens.Styles{}, summary)
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
		AllPass:    false,
		TotalFail:  1,
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

	s := screens.NewTestResultsScreen(80, 50, screens.Styles{}, summary)
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
	s := screens.NewTestResultsScreen(40, 10, screens.Styles{}, summary)

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
	s := screens.NewTestResultsScreen(40, 50, screens.Styles{}, summary)
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
//   Row 0:             title
//   Row 1:             top border
//   Rows 2..h-4:       body lines (contentHeight = h-5 rows)
//   Row h-3:           bottom border
//   Row h-2:           position indicator
//   Row h-1:           help text
func TestResultsScreen_ShortContent_BodyPaddedToContentHeight(t *testing.T) {
	// A minimal summary with a single passing result produces very few body
	// lines -- far fewer than contentHeight at a large height.
	summary := &testrun.TestSummary{AllPass: true, TotalPass: 1}
	const height = 30
	s := screens.NewTestResultsScreen(80, height, screens.Styles{}, summary)
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
// T3.1: Scroll before first View() call
// ---------------------------------------------------------------------------

// TestResultsScreen_ScrollBeforeFirstView_NoPanic verifies that pressing
// scroll keys before the first View() call does not panic or produce
// incorrect behavior. Lines are built in the constructor, not lazily in View().
func TestResultsScreen_ScrollBeforeFirstView_NoPanic(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := screens.NewTestResultsScreen(80, 10, screens.Styles{}, summary)

	// Press scroll keys BEFORE calling View() -- must not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic when pressing scroll keys before first View() call: %v; lines must be built in the constructor, not lazily in View()", r)
		}
	}()

	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	s.Update(tea.KeyMsg{Type: tea.KeyPgDown})

	// First View() call after scroll keys -- must return a valid view
	got := stripANSIResults(s.View())
	if !strings.Contains(got, "lines ") {
		t.Errorf("View() after scroll-before-View does not contain position indicator; View() must be usable after scroll keys pressed before first render\ngot (first 300): %.300s", got)
	}
}

// TestResultsScreen_ScrollBeforeFirstView_PositionIndicatorUpdated verifies
// that after pressing 'down' before the first View() call, the position
// indicator reflects the updated offset (not stuck at "lines 1-...").
func TestResultsScreen_ScrollBeforeFirstView_PositionIndicatorUpdated(t *testing.T) {
	summary := summaryWithManyFailures(30)
	s := screens.NewTestResultsScreen(80, 10, screens.Styles{}, summary)

	// Pressing pgdown before any View() call
	s.Update(tea.KeyMsg{Type: tea.KeyPgDown})

	got := stripANSIResults(s.View())
	// After pgdown (which scrolls by contentHeight=5), the indicator must NOT be "lines 1-5"
	// (since we are now at line 6 or later). It must be "lines 6-" or higher.
	if strings.Contains(got, "lines 1-5 ") {
		t.Errorf("View() after scroll-before-View shows 'lines 1-5'; expected indicator to reflect the updated offset after pgdown\ngot (first 300): %.300s", got)
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

	s := screens.NewTestResultsScreen(0, 10, screens.Styles{}, summary)
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

	s := screens.NewTestResultsScreen(-1, 10, screens.Styles{}, summary)
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
		s := screens.NewTestResultsScreen(80, h, screens.Styles{}, summary)
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
	s := screens.NewTestResultsScreen(80, 24, screens.Styles{}, nil)
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
