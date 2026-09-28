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
package devtest_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/testrun"
	"mosaic-run/internal/tui/screens/devtest"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers (scroll tests)
// ---------------------------------------------------------------------------

// newScrollResultsScreen builds a TestResultsScreen with the given width and
// height. Smaller heights force scrolling by reducing the visible content area.
func newScrollResultsScreen(width, height int, summary *testrun.TestSummary) *devtest.TestResultsScreen {
	return devtest.NewTestResultsScreen(width, height, screens.Styles{}, summary)
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
func scrollDown(s *devtest.TestResultsScreen, n int) {
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
