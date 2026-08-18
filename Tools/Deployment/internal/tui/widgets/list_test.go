package widgets_test

// list_test.go verifies the keyboard-driven behaviour of the List widget: cursor movement,
// disabled-item skipping, viewport scrolling, selection, back navigation, and reset.
//
// All tests drive the widget through the Update/View cycle with no real terminal. The widget
// is deliberately style-agnostic; DefaultListStyles() produces a plain-text style set.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/tui/widgets"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// threeItems returns [enabled "a", disabled "b", enabled "c"] — a canonical fixture.
func threeItems() []widgets.ListItem {
	return []widgets.ListItem{
		{ID: "a", Label: "Alpha"},
		{ID: "b", Label: "Beta", Disabled: true, DisabledReason: "not available"},
		{ID: "c", Label: "Gamma"},
	}
}

func downKey() tea.Msg { return tea.KeyMsg{Type: tea.KeyDown} }
func upKey() tea.Msg   { return tea.KeyMsg{Type: tea.KeyUp} }
func enterKey() tea.Msg { return tea.KeyMsg{Type: tea.KeyEnter} }
func escKey() tea.Msg   { return tea.KeyMsg{Type: tea.KeyEsc} }
func runeKey(r rune) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// ---------------------------------------------------------------------------
// Initial cursor placement
// ---------------------------------------------------------------------------

// TestList_NewList_CursorStartsOnFirstEnabledItem verifies that when all items are created
// and the first item is disabled, the initial cursor position is the first enabled item.
func TestList_NewList_CursorStartsOnFirstEnabledItem(t *testing.T) {
	// Arrange
	items := []widgets.ListItem{
		{ID: "a", Label: "Alpha", Disabled: true},
		{ID: "b", Label: "Beta"},
		{ID: "c", Label: "Gamma"},
	}

	// Act
	l := widgets.NewList(items, 10, 80, widgets.DefaultListStyles())

	// Assert
	if l.SelectedID() != "b" {
		t.Errorf("initial cursor = %q, want %q (first enabled item)", l.SelectedID(), "b")
	}
}

// ---------------------------------------------------------------------------
// Cursor movement — arrow keys
// ---------------------------------------------------------------------------

// TestList_MoveDown_ArrowKey_AdvancesCursorSkippingDisabled verifies that the down arrow
// moves the cursor forward and skips any disabled items in between.
func TestList_MoveDown_ArrowKey_AdvancesCursorSkippingDisabled(t *testing.T) {
	// Arrange
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())
	// cursor starts on "a"

	// Act — press down once; "b" is disabled so cursor must land on "c"
	l.Update(downKey())

	// Assert
	if l.SelectedID() != "c" {
		t.Errorf("cursor = %q, want %q (skipped disabled item)", l.SelectedID(), "c")
	}
}

// TestList_MoveUp_ArrowKey_RetractsCursorSkippingDisabled verifies that the up arrow moves
// the cursor backward, skipping any disabled items in between.
func TestList_MoveUp_ArrowKey_RetractsCursorSkippingDisabled(t *testing.T) {
	// Arrange
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())
	l.Update(downKey()) // cursor on "c"

	// Act — press up; "b" is disabled so cursor must land back on "a"
	l.Update(upKey())

	// Assert
	if l.SelectedID() != "a" {
		t.Errorf("cursor = %q, want %q (skipped disabled item on the way up)", l.SelectedID(), "a")
	}
}

// TestList_MoveDown_AtLastItem_DoesNotWrap verifies that pressing down at the bottom of the
// list does not wrap around to the top. Wrap-around would be confusing for a selection list
// because it suggests the choice is circular when it is not.
func TestList_MoveDown_AtLastItem_DoesNotWrap(t *testing.T) {
	// Arrange
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())
	l.Update(downKey()) // cursor on "c" (last enabled item)

	// Act
	l.Update(downKey())

	// Assert
	if l.SelectedID() != "c" {
		t.Errorf("cursor = %q, want %q (must not wrap at list end)", l.SelectedID(), "c")
	}
}

// TestList_MoveUp_AtFirstItem_DoesNotWrap verifies that pressing up at the top does not wrap.
func TestList_MoveUp_AtFirstItem_DoesNotWrap(t *testing.T) {
	// Arrange — cursor starts on "a"
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())

	// Act
	l.Update(upKey())

	// Assert
	if l.SelectedID() != "a" {
		t.Errorf("cursor = %q, want %q (must not wrap at list start)", l.SelectedID(), "a")
	}
}

// ---------------------------------------------------------------------------
// Cursor movement — vim keys (j/k)
// ---------------------------------------------------------------------------

// TestList_MoveDown_JKey_AdvancesCursor verifies that the 'j' key (vim-style) moves the
// cursor down, providing a keyboard-only alternative to the arrow keys.
func TestList_MoveDown_JKey_AdvancesCursor(t *testing.T) {
	// Arrange
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())

	// Act
	l.Update(runeKey('j'))

	// Assert — skips disabled "b"
	if l.SelectedID() != "c" {
		t.Errorf("cursor = %q, want %q after 'j' key", l.SelectedID(), "c")
	}
}

// TestList_MoveUp_KKey_RetractsCursor verifies that the 'k' key (vim-style) moves the
// cursor up, providing a keyboard-only alternative to the arrow keys.
func TestList_MoveUp_KKey_RetractsCursor(t *testing.T) {
	// Arrange
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())
	l.Update(runeKey('j')) // cursor on "c"

	// Act
	l.Update(runeKey('k'))

	// Assert — skips disabled "b"
	if l.SelectedID() != "a" {
		t.Errorf("cursor = %q, want %q after 'k' key", l.SelectedID(), "a")
	}
}

// ---------------------------------------------------------------------------
// Selection and back
// ---------------------------------------------------------------------------

// TestList_Enter_OnEnabledItem_SetsDone verifies that pressing Enter on a selectable item
// sets Done() and makes the item's ID available via SelectedID().
func TestList_Enter_OnEnabledItem_SetsDone(t *testing.T) {
	// Arrange
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())
	// cursor on "a" (first enabled)

	// Act
	l.Update(enterKey())

	// Assert
	if !l.Done() {
		t.Error("Done() = false after Enter on enabled item; want true")
	}
	if l.SelectedID() != "a" {
		t.Errorf("SelectedID() = %q, want %q", l.SelectedID(), "a")
	}
}

// TestList_Enter_WhenAllItemsDisabled_DoesNotSetDone verifies that Enter has no effect when
// the item under the cursor is disabled, preventing accidental selection of unavailable items.
func TestList_Enter_WhenAllItemsDisabled_DoesNotSetDone(t *testing.T) {
	// Arrange — only disabled items
	items := []widgets.ListItem{
		{ID: "a", Label: "Alpha", Disabled: true, DisabledReason: "unavailable"},
	}
	l := widgets.NewList(items, 10, 80, widgets.DefaultListStyles())

	// Act
	l.Update(enterKey())

	// Assert
	if l.Done() {
		t.Error("Done() = true after Enter on disabled item; the user must not be able to select an unavailable item")
	}
}

// TestList_Esc_SetsBack verifies that pressing Esc signals back navigation so the root model
// can return to the previous screen without any terminal interaction.
func TestList_Esc_SetsBack(t *testing.T) {
	// Arrange
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())

	// Act
	l.Update(escKey())

	// Assert
	if !l.Back() {
		t.Error("Back() = false after Esc; want true")
	}
}

// TestList_Back_DoesNotSetDone verifies that Esc and Done are mutually exclusive: pressing
// Esc must not accidentally confirm a selection.
func TestList_Back_DoesNotSetDone(t *testing.T) {
	// Arrange
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())

	// Act
	l.Update(escKey())

	// Assert
	if l.Done() {
		t.Error("Done() = true after Esc; want false (Esc and Done must be mutually exclusive)")
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestList_Reset_ClearsDoneFlag verifies that Reset clears the Done flag, allowing the same
// list to be reused after a selection (as the root model does on back navigation).
func TestList_Reset_ClearsDoneFlag(t *testing.T) {
	// Arrange
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())
	l.Update(enterKey())
	if !l.Done() {
		t.Fatal("precondition: Done() must be true before testing Reset")
	}

	// Act
	l.Reset()

	// Assert
	if l.Done() {
		t.Error("Done() = true after Reset; want false")
	}
}

// TestList_Reset_ClearsBackFlag verifies that Reset clears the Back flag so the list can
// re-enter the active screen after the user navigates away and returns.
func TestList_Reset_ClearsBackFlag(t *testing.T) {
	// Arrange
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())
	l.Update(escKey())
	if !l.Back() {
		t.Fatal("precondition: Back() must be true before testing Reset")
	}

	// Act
	l.Reset()

	// Assert
	if l.Back() {
		t.Error("Back() = true after Reset; want false")
	}
}

// TestList_Reset_PreservesSelection verifies that Reset does not move the cursor. Prior valid
// input (the harness or mode the user had selected) is preserved when they navigate back.
func TestList_Reset_PreservesSelection(t *testing.T) {
	// Arrange
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())
	l.Update(downKey()) // cursor on "c"
	l.Update(enterKey())

	// Act
	l.Reset()

	// Assert
	if l.SelectedID() != "c" {
		t.Errorf("SelectedID() = %q after Reset; want %q (prior selection must be preserved)", l.SelectedID(), "c")
	}
}

// ---------------------------------------------------------------------------
// SetCursor
// ---------------------------------------------------------------------------

// TestList_SetCursor_MovesToNamedItem verifies that SetCursor places the cursor on the item
// with the matching ID without requiring keyboard navigation.
func TestList_SetCursor_MovesToNamedItem(t *testing.T) {
	// Arrange
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())

	// Act
	l.SetCursor("c")

	// Assert
	if l.SelectedID() != "c" {
		t.Errorf("SelectedID() = %q, want %q after SetCursor", l.SelectedID(), "c")
	}
}

// TestList_SetCursor_UnknownID_DoesNotMoveCursor verifies that SetCursor with an unknown ID
// leaves the cursor in its current position rather than jumping to an undefined location.
func TestList_SetCursor_UnknownID_DoesNotMoveCursor(t *testing.T) {
	// Arrange
	l := widgets.NewList(threeItems(), 10, 80, widgets.DefaultListStyles())
	initial := l.SelectedID()

	// Act
	l.SetCursor("nonexistent")

	// Assert
	if l.SelectedID() != initial {
		t.Errorf("cursor moved to %q after SetCursor with unknown ID; want %q (unchanged)", l.SelectedID(), initial)
	}
}

// ---------------------------------------------------------------------------
// Viewport scrolling
// ---------------------------------------------------------------------------

// TestList_Viewport_ScrollsWhenCursorMovesBelow verifies that the viewport adjusts to keep
// the cursor row visible when it moves beyond the visible area. This is essential for
// keyboard-only operability: if items scroll off-screen, the user cannot navigate to them.
func TestList_Viewport_ScrollsWhenCursorMovesBelow(t *testing.T) {
	// Arrange — four enabled items, viewport height = 2
	items := []widgets.ListItem{
		{ID: "a", Label: "Alpha"},
		{ID: "b", Label: "Beta"},
		{ID: "c", Label: "Gamma"},
		{ID: "d", Label: "Delta"},
	}
	l := widgets.NewList(items, 2, 80, widgets.DefaultListStyles())

	// Act — move down three times so cursor reaches "d" (below initial viewport)
	l.Update(downKey())
	l.Update(downKey())
	l.Update(downKey())

	// Assert — the rendered view must show the current item
	view := l.View()
	if !strings.Contains(view, "Delta") {
		t.Errorf("view does not contain cursor item after scroll:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Empty list
// ---------------------------------------------------------------------------

// TestList_EmptyList_View_ShowsNoItemsMessage verifies that an empty list renders a
// meaningful "(no items)" message rather than blank output, so the user understands why
// no selection is possible.
func TestList_EmptyList_View_ShowsNoItemsMessage(t *testing.T) {
	// Arrange
	l := widgets.NewList(nil, 10, 80, widgets.DefaultListStyles())

	// Act
	view := l.View()

	// Assert
	if !strings.Contains(view, "no items") {
		t.Errorf("empty list view does not contain 'no items': %q", view)
	}
}
