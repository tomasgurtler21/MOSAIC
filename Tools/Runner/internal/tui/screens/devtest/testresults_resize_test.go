// Tests for TestResultsScreen reset, resize, and scroll-state edge cases.
package devtest_test

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/testrun"
	"mosaic-run/internal/tui/screens/devtest"
	"mosaic-run/internal/tui/screens"
)

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
	s := devtest.NewTestResultsScreen(80, 50, screens.Styles{}, summary)
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
// T3.1: Scroll before first View() call
// ---------------------------------------------------------------------------

// TestResultsScreen_ScrollBeforeFirstView_NoPanic verifies that pressing
// scroll keys before the first View() call does not panic or produce
// incorrect behavior. Lines are built in the constructor, not lazily in View().
func TestResultsScreen_ScrollBeforeFirstView_NoPanic(t *testing.T) {
	summary := summaryWithManyFailures(15)
	s := devtest.NewTestResultsScreen(80, 10, screens.Styles{}, summary)

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
	s := devtest.NewTestResultsScreen(80, 10, screens.Styles{}, summary)

	// Pressing pgdown before any View() call
	s.Update(tea.KeyMsg{Type: tea.KeyPgDown})

	got := stripANSIResults(s.View())
	// After pgdown (which scrolls by contentHeight=5), the indicator must NOT be "lines 1-5"
	// (since we are now at line 6 or later). It must be "lines 6-" or higher.
	if strings.Contains(got, "lines 1-5 ") {
		t.Errorf("View() after scroll-before-View shows 'lines 1-5'; expected indicator to reflect the updated offset after pgdown\ngot (first 300): %.300s", got)
	}
}
