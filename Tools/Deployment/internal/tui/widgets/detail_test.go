package widgets_test

// detail_test.go verifies the DetailPane widget: content display, scroll behaviour,
// and resize. All tests drive the widget through SetContent/ScrollDown/ScrollUp/View with
// no real terminal. DefaultDetailPaneStyles() produces a plain-text style set.

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/tui/widgets"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newDetailPane creates a DetailPane with the given height and a fixed width of 80. The
// plain styles from DefaultDetailPaneStyles() produce output without ANSI escape codes,
// making substring assertions on View() reliable.
func newDetailPane(height int) *widgets.DetailPane {
	return widgets.NewDetailPane(height, 80, widgets.DefaultDetailPaneStyles())
}

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

// TestDetailPane_View_WhenEmpty_ShowsPlaceholder verifies that a newly created pane with no
// content shows a placeholder string so the user knows the pane is present but nothing is
// selected yet.
func TestDetailPane_View_WhenEmpty_ShowsPlaceholder(t *testing.T) {
	// Arrange
	pane := newDetailPane(5)

	// Act
	view := pane.View()

	// Assert
	if !strings.Contains(view, "nothing selected") {
		t.Errorf("View() for an empty pane does not contain the placeholder text:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Content display
// ---------------------------------------------------------------------------

// TestDetailPane_SetContent_ShowsTitleInView verifies that after SetContent the title text
// appears in the rendered view.
func TestDetailPane_SetContent_ShowsTitleInView(t *testing.T) {
	// Arrange
	pane := newDetailPane(5)

	// Act
	pane.SetContent("My Title", "")
	view := pane.View()

	// Assert
	if !strings.Contains(view, "My Title") {
		t.Errorf("View() does not contain the title after SetContent:\n%s", view)
	}
}

// TestDetailPane_SetContent_ShowsBodyInView verifies that after SetContent the body text
// appears in the rendered view.
func TestDetailPane_SetContent_ShowsBodyInView(t *testing.T) {
	// Arrange
	pane := newDetailPane(5)

	// Act
	pane.SetContent("", "Body text here")
	view := pane.View()

	// Assert
	if !strings.Contains(view, "Body text here") {
		t.Errorf("View() does not contain the body after SetContent:\n%s", view)
	}
}

// TestDetailPane_SetContent_ShowsBothTitleAndBody verifies that when both title and body are
// provided they both appear in the rendered view.
func TestDetailPane_SetContent_ShowsBothTitleAndBody(t *testing.T) {
	// Arrange
	pane := newDetailPane(5)

	// Act
	pane.SetContent("Panel Title", "Panel body")
	view := pane.View()

	// Assert
	if !strings.Contains(view, "Panel Title") {
		t.Errorf("View() does not contain the title:\n%s", view)
	}
	if !strings.Contains(view, "Panel body") {
		t.Errorf("View() does not contain the body:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Scroll behaviour
// ---------------------------------------------------------------------------

// TestDetailPane_ScrollDown_ChangesVisibleContent verifies that after ScrollDown the pane
// shows content that was previously below the visible area and no longer shows content that
// was previously at the top. This confirms the scroll offset is advancing.
func TestDetailPane_ScrollDown_ChangesVisibleContent(t *testing.T) {
	// Arrange — two-line content in a one-line pane; only one line visible at a time.
	pane := newDetailPane(1)
	pane.SetContent("First line", "Second line")

	initialView := pane.View()
	if !strings.Contains(initialView, "First line") {
		t.Fatalf("precondition: initial view must contain 'First line'; got:\n%s", initialView)
	}

	// Act
	pane.ScrollDown()
	scrolledView := pane.View()

	// Assert — after scrolling down, the first line should no longer be visible and the
	// second line should be visible.
	if strings.Contains(scrolledView, "First line") {
		t.Errorf("View() still shows 'First line' after ScrollDown; offset was not advanced:\n%s", scrolledView)
	}
	if !strings.Contains(scrolledView, "Second line") {
		t.Errorf("View() does not show 'Second line' after ScrollDown:\n%s", scrolledView)
	}
}

// TestDetailPane_ScrollUp_AfterScrollDown_RestoresPreviousContent verifies that ScrollUp
// reverses a previous ScrollDown, returning the view to the line that was visible before.
func TestDetailPane_ScrollUp_AfterScrollDown_RestoresPreviousContent(t *testing.T) {
	// Arrange
	pane := newDetailPane(1)
	pane.SetContent("First line", "Second line")
	pane.ScrollDown() // now showing second line

	// Act
	pane.ScrollUp()
	view := pane.View()

	// Assert — first line must be visible again.
	if !strings.Contains(view, "First line") {
		t.Errorf("View() does not show 'First line' after ScrollUp following ScrollDown:\n%s", view)
	}
}

// TestDetailPane_ScrollDown_AtBottom_DoesNotScrollBeyondContent verifies that ScrollDown has
// no effect when the pane is already showing the last line. The view must remain stable and
// not wrap around or panic.
func TestDetailPane_ScrollDown_AtBottom_DoesNotScrollBeyondContent(t *testing.T) {
	// Arrange — height equals total content lines so everything is already visible.
	// A pane of height 2 with one title and one body line is fully visible from the start;
	// ScrollDown should have no effect.
	pane := newDetailPane(2)
	pane.SetContent("Line A", "Line B")

	viewBeforeScroll := pane.View()

	// Act — attempt to scroll beyond the visible boundary
	pane.ScrollDown()
	pane.ScrollDown()
	viewAfterScroll := pane.View()

	// Assert — the view must be identical: cannot scroll past the bottom.
	if viewBeforeScroll != viewAfterScroll {
		t.Errorf("View() changed after ScrollDown at bottom; scrolling must be clamped:\nbefore:\n%s\nafter:\n%s",
			viewBeforeScroll, viewAfterScroll)
	}
}

// TestDetailPane_ScrollUp_AtTop_DoesNotScrollBelowZero verifies that ScrollUp has no effect
// when the pane is already at the top. This guards against an offset going negative.
func TestDetailPane_ScrollUp_AtTop_DoesNotScrollBelowZero(t *testing.T) {
	// Arrange — pane with content, no prior scroll
	pane := newDetailPane(5)
	pane.SetContent("Title", "Body text")

	viewBeforeScroll := pane.View()

	// Act
	pane.ScrollUp()
	viewAfterScroll := pane.View()

	// Assert — view must be unchanged; scrolling up from the top is a no-op.
	if viewBeforeScroll != viewAfterScroll {
		t.Errorf("View() changed after ScrollUp at top; offset must never go below zero:\nbefore:\n%s\nafter:\n%s",
			viewBeforeScroll, viewAfterScroll)
	}
}

// TestDetailPane_SetContent_ResetsScrollOffset verifies that calling SetContent always
// resets the scroll position to the top, so new content is displayed from the beginning
// regardless of where the previous content was scrolled to.
func TestDetailPane_SetContent_ResetsScrollOffset(t *testing.T) {
	// Arrange — scroll the pane down so it shows the second line
	pane := newDetailPane(1)
	pane.SetContent("Original first", "Original second")
	pane.ScrollDown()
	scrolledView := pane.View()
	if !strings.Contains(scrolledView, "Original second") {
		t.Fatalf("precondition: pane must be scrolled to show 'Original second'; got:\n%s", scrolledView)
	}

	// Act — replace the content; this should reset the scroll to the top.
	pane.SetContent("New first", "New second")
	view := pane.View()

	// Assert — the new content must start from the top (first line visible, not the second).
	if !strings.Contains(view, "New first") {
		t.Errorf("View() does not show the first line of new content; SetContent must reset the scroll offset:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Resize
// ---------------------------------------------------------------------------

// TestDetailPane_Resize_UpdatesDimensionsWithoutPanicking verifies that Resize can be called
// with new dimensions and the pane still renders correctly. This is a smoke test: if Resize
// silently corrupts state, subsequent View() calls would panic or produce garbled output.
func TestDetailPane_Resize_UpdatesDimensionsWithoutPanicking(t *testing.T) {
	// Arrange
	pane := newDetailPane(5)
	pane.SetContent("Title text", "Body text that will be re-rendered")

	// Act — resize to both larger and smaller dimensions; must not panic.
	pane.Resize(10, 120)
	view := pane.View()

	// Assert — content must still be present after resize.
	if !strings.Contains(view, "Title text") {
		t.Errorf("View() does not contain title after Resize:\n%s", view)
	}
	if !strings.Contains(view, "Body text") {
		t.Errorf("View() does not contain body after Resize:\n%s", view)
	}
}

// TestDetailPane_Resize_ClampsOffsetWhenHeightIncreasesAboveContent verifies that when the
// pane is scrolled and then resized to a height that accommodates all content, the offset is
// reset to zero so the content is shown from the top.
func TestDetailPane_Resize_ClampsOffsetWhenHeightIncreasesAboveContent(t *testing.T) {
	// Arrange — scroll down to show the second line
	pane := newDetailPane(1)
	pane.SetContent("Alpha", "Beta")
	pane.ScrollDown() // offset = 1, showing "Beta"

	scrolledView := pane.View()
	if !strings.Contains(scrolledView, "Beta") {
		t.Fatalf("precondition: pane must show 'Beta' after ScrollDown; got:\n%s", scrolledView)
	}

	// Act — resize to height=2, which is enough to show both lines; the offset should be reset.
	pane.Resize(2, 80)
	view := pane.View()

	// Assert — "Alpha" (first line) must now be visible because the offset was clamped.
	if !strings.Contains(view, "Alpha") {
		t.Errorf("View() does not show 'Alpha' after resize that accommodates all content; offset must have been reset:\n%s", view)
	}
}
