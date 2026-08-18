package widgets_test

// detail_test.go verifies the DetailPane widget's wrapping behaviour and
// scroll math. The tests below are the RED-phase specification for the
// intended behaviour; they compile against the current implementation but
// fail at runtime, proving the bugs described in the plan.
//
// Covered:
//   - Content longer than the pane width renders with every line fitting the width.
//   - Scrolling can reach the final wrapped line of a long single-line body.
//   - Resizing to a narrower width preserves a still-valid scroll offset rather
//     than silently resetting it to the top.
//   - A pane constructed with width=0 handles all operations without panicking.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"mosaic-common/tui/widgets"
)

// newCommonDetailPane creates a DetailPane for tests using the given height
// and width. DefaultDetailPaneStyles() produces output without ANSI escape
// codes, making substring and width assertions on View() reliable.
func newCommonDetailPane(height, width int) *widgets.DetailPane {
	return widgets.NewDetailPane(height, width, widgets.DefaultDetailPaneStyles())
}

// ---------------------------------------------------------------------------
// Wrapping: each rendered line fits the pane width
// ---------------------------------------------------------------------------

// TestDetailPane_Wrapping_EachRenderedLineFitsWidth verifies that when a
// pane is given a positive width, lipgloss wraps the body so every line in
// the rendered output is at most that many cells wide. This is a guard
// confirming that the Width style is applied in View().
func TestDetailPane_Wrapping_EachRenderedLineFitsWidth(t *testing.T) {
	const width = 20
	pane := newCommonDetailPane(10, width)
	body := strings.Repeat("X", 80) // 80-char single line, much wider than width
	pane.SetContent("", body)

	view := pane.View()
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line %d is %d cells wide, want <= %d: %q", i, w, width, line)
		}
	}
}

// ---------------------------------------------------------------------------
// Wrapping + scroll: scrolling reaches the last wrapped visual line
// ---------------------------------------------------------------------------

// TestDetailPane_Wrapping_ScrollCanReachLastWrappedLine verifies that when a
// single long body line is wrapped by the pane into multiple visual lines,
// ScrollDown can advance far enough to bring the final visual line into the
// visible window.
//
// RED-phase failure: totalLines() counts raw content lines (1 for a
// single-line body), so ScrollDown calculates max-offset = 1-height and
// never advances, leaving the last wrapped segment unreachable.
func TestDetailPane_Wrapping_ScrollCanReachLastWrappedLine(t *testing.T) {
	const width = 10
	// Words that word-wrap into 3+ visual lines at width=10.
	// "aaa bbb" fits in 10, "ccc" next, "TERMINUS" last — 3 visual lines
	// from one raw content line.
	pane := newCommonDetailPane(1, width)
	pane.SetContent("", "aaa bbb ccc TERMINUS")

	// Attempt enough scrolls to reach the last wrapped line.
	for i := 0; i < 15; i++ {
		pane.ScrollDown()
	}

	view := pane.View()
	if !strings.Contains(view, "TERMINUS") {
		t.Errorf(
			"after scrolling to the bottom, the pane does not show the last wrapped segment %q;\n"+
				"ScrollDown must account for wrapped visual lines, not raw content lines",
			"TERMINUS",
		)
	}
}

// ---------------------------------------------------------------------------
// Resize: scroll offset is preserved, not blindly reset to zero
// ---------------------------------------------------------------------------

// TestDetailPane_Resize_NarrowerWidth_PreservesScrollPosition verifies that
// when the pane is scrolled to the bottom and then resized to a narrower
// width (where the current offset remains valid), the scroll offset is
// preserved rather than being blindly reset to zero.
//
// RED-phase failure: Resize resets the offset to 0 whenever
// offset >= totalLines()-height, which is always true when the pane is
// scrolled to the bottom — even though the same offset is still valid
// in the new layout.
func TestDetailPane_Resize_NarrowerWidth_PreservesScrollPosition(t *testing.T) {
	// Five short lines that do not wrap even at width=20, so the raw and
	// visual line counts are the same before and after the resize.
	const body = "Line-1\nLine-2\nLine-3\nLine-4\nLine-5"
	pane := newCommonDetailPane(2, 80)
	pane.SetContent("", body)

	// Scroll to show Line-4 and Line-5: offset = 3 for 5 raw lines, height 2.
	for i := 0; i < 3; i++ {
		pane.ScrollDown()
	}
	viewBeforeResize := pane.View()
	if !strings.Contains(viewBeforeResize, "Line-4") {
		t.Fatalf("precondition: expected Line-4 to be visible after scrolling; got:\n%s", viewBeforeResize)
	}

	// Resize to a narrower width. The content is short enough that no new
	// wrapping occurs, so offset=3 remains valid (totalLines()-height = 3).
	pane.Resize(2, 20)
	viewAfterResize := pane.View()

	// With the fix the offset is preserved → Line-4 is still visible.
	// With the bug (offset >= totalLines()-height resets to 0) → Line-1 appears.
	if !strings.Contains(viewAfterResize, "Line-4") {
		t.Errorf(
			"after resize, scroll position was lost; view does not show Line-4 which was visible before resize\n"+
				"before resize:\n%s\nafter resize:\n%s",
			viewBeforeResize, viewAfterResize,
		)
	}
}

// ---------------------------------------------------------------------------
// Width zero: no panic, no division by zero
// ---------------------------------------------------------------------------

// TestDetailPane_WidthZero_SafeForAllOperations verifies that a pane
// constructed with width=0 does not panic on any operation. Width zero is
// the existing no-wrap mode; it must continue to work safely after the
// scroll-math improvements to the positive-width path.
func TestDetailPane_WidthZero_SafeForAllOperations(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DetailPane with width=0 panicked: %v", r)
		}
	}()

	pane := newCommonDetailPane(5, 0)
	pane.SetContent("Title", "A very long body line that would exceed any normal terminal width but must not panic with width=0.")
	_ = pane.View()
	pane.ScrollDown()
	pane.ScrollUp()
	pane.Resize(5, 0)
	pane.Resize(3, 0)
	_ = pane.View()
}
