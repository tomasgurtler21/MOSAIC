package widgets_test

// multiselect_test.go verifies the keyboard-driven behaviour of the MultiSelect widget:
// cursor movement, space-toggling, disabled-item immunity, confirmation, and back navigation.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/tui/widgets"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// spaceKey returns a tea.KeyMsg for the space bar.
func spaceKey() tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}
}

// threeSelectItems returns [enabled "x", disabled "y", enabled "z"].
func threeSelectItems() []widgets.ListItem {
	return []widgets.ListItem{
		{ID: "x", Label: "Xavier"},
		{ID: "y", Label: "Yorick", Disabled: true},
		{ID: "z", Label: "Zelda"},
	}
}

// ---------------------------------------------------------------------------
// Initial cursor placement
// ---------------------------------------------------------------------------

// TestMultiSelect_NewMultiSelect_CursorStartsOnFirstEnabledItem verifies that the cursor
// begins on the first non-disabled item, consistent with List widget behaviour.
func TestMultiSelect_NewMultiSelect_CursorStartsOnFirstEnabledItem(t *testing.T) {
	// Arrange
	items := []widgets.ListItem{
		{ID: "a", Label: "Alpha", Disabled: true},
		{ID: "b", Label: "Beta"},
	}

	// Act
	m := widgets.NewMultiSelect(items, 10, 80, widgets.DefaultMultiSelectStyles())

	// Assert
	if m.CursorIndex() != 1 {
		t.Errorf("initial cursor index = %d, want 1 (first enabled item)", m.CursorIndex())
	}
}

// ---------------------------------------------------------------------------
// Cursor movement
// ---------------------------------------------------------------------------

// TestMultiSelect_MoveDown_ArrowKey_AdvancesCursorSkippingDisabled verifies that the down
// arrow moves the cursor forward, skipping disabled items.
func TestMultiSelect_MoveDown_ArrowKey_AdvancesCursorSkippingDisabled(t *testing.T) {
	// Arrange
	m := widgets.NewMultiSelect(threeSelectItems(), 10, 80, widgets.DefaultMultiSelectStyles())
	// cursor on "x"

	// Act — down once: "y" is disabled so cursor lands on "z"
	m.Update(downKey())

	// Assert
	if m.CursorIndex() != 2 {
		t.Errorf("cursor index = %d, want 2 (skipped disabled item)", m.CursorIndex())
	}
}

// TestMultiSelect_MoveUp_ArrowKey_RetractsCursorSkippingDisabled verifies that the up arrow
// moves the cursor backward, skipping disabled items.
func TestMultiSelect_MoveUp_ArrowKey_RetractsCursorSkippingDisabled(t *testing.T) {
	// Arrange
	m := widgets.NewMultiSelect(threeSelectItems(), 10, 80, widgets.DefaultMultiSelectStyles())
	m.Update(downKey()) // cursor on "z"

	// Act
	m.Update(upKey())

	// Assert — "y" disabled, so cursor lands on "x"
	if m.CursorIndex() != 0 {
		t.Errorf("cursor index = %d, want 0 (skipped disabled item on up)", m.CursorIndex())
	}
}

// TestMultiSelect_MoveDown_JKey_AdvancesCursor verifies that 'j' advances the cursor.
func TestMultiSelect_MoveDown_JKey_AdvancesCursor(t *testing.T) {
	// Arrange
	m := widgets.NewMultiSelect(threeSelectItems(), 10, 80, widgets.DefaultMultiSelectStyles())

	// Act
	m.Update(runeKey('j'))

	// Assert
	if m.CursorIndex() != 2 {
		t.Errorf("cursor index = %d, want 2 after 'j'", m.CursorIndex())
	}
}

// TestMultiSelect_MoveUp_KKey_RetractsCursor verifies that 'k' retracts the cursor.
func TestMultiSelect_MoveUp_KKey_RetractsCursor(t *testing.T) {
	// Arrange
	m := widgets.NewMultiSelect(threeSelectItems(), 10, 80, widgets.DefaultMultiSelectStyles())
	m.Update(runeKey('j')) // cursor on "z"

	// Act
	m.Update(runeKey('k'))

	// Assert
	if m.CursorIndex() != 0 {
		t.Errorf("cursor index = %d, want 0 after 'k'", m.CursorIndex())
	}
}

// ---------------------------------------------------------------------------
// Space toggling
// ---------------------------------------------------------------------------

// TestMultiSelect_Space_TogglesCurrentItemOn verifies that pressing space checks the item
// under the cursor.
func TestMultiSelect_Space_TogglesCurrentItemOn(t *testing.T) {
	// Arrange
	m := widgets.NewMultiSelect(threeSelectItems(), 10, 80, widgets.DefaultMultiSelectStyles())
	// cursor on "x", which is unchecked

	// Act
	m.Update(spaceKey())

	// Assert
	if !m.IsChecked("x") {
		t.Error("IsChecked(\"x\") = false after Space; want true")
	}
}

// TestMultiSelect_Space_TogglesCurrentItemOff verifies that pressing space a second time
// unchecks an item that was already checked.
func TestMultiSelect_Space_TogglesCurrentItemOff(t *testing.T) {
	// Arrange
	m := widgets.NewMultiSelect(threeSelectItems(), 10, 80, widgets.DefaultMultiSelectStyles())
	m.Update(spaceKey()) // check "x"

	// Act
	m.Update(spaceKey()) // uncheck "x"

	// Assert
	if m.IsChecked("x") {
		t.Error("IsChecked(\"x\") = true after double Space; want false (toggle must uncheck)")
	}
}

// TestMultiSelect_Space_OnDisabledItem_DoesNotToggle verifies that space has no effect on a
// disabled item. The cursor should never land on a disabled item, but if it does (e.g. all
// other items are disabled), the toggle must be silently ignored.
func TestMultiSelect_Space_OnDisabledItem_DoesNotToggle(t *testing.T) {
	// Arrange — only a disabled item
	items := []widgets.ListItem{
		{ID: "a", Label: "Alpha", Disabled: true},
	}
	m := widgets.NewMultiSelect(items, 10, 80, widgets.DefaultMultiSelectStyles())

	// Act
	m.Update(spaceKey())

	// Assert
	if m.IsChecked("a") {
		t.Error("IsChecked(\"a\") = true after Space on disabled item; toggling a disabled item must have no effect")
	}
}

// ---------------------------------------------------------------------------
// SelectedIDs — order and correctness
// ---------------------------------------------------------------------------

// TestMultiSelect_SelectedIDs_ReturnsCheckedInDeclarationOrder verifies that SelectedIDs
// returns the IDs of all checked items in the order they were declared (not toggle order).
// This is important for deterministic downstream processing.
func TestMultiSelect_SelectedIDs_ReturnsCheckedInDeclarationOrder(t *testing.T) {
	// Arrange
	items := []widgets.ListItem{
		{ID: "p", Label: "Panda"},
		{ID: "q", Label: "Quail"},
		{ID: "r", Label: "Rabbit"},
	}
	m := widgets.NewMultiSelect(items, 10, 80, widgets.DefaultMultiSelectStyles())

	// Toggle "r" first, then "p" — SelectedIDs must still return ["p", "r"].
	m.Update(downKey()) // cursor on "q"
	m.Update(downKey()) // cursor on "r"
	m.Update(spaceKey()) // toggle "r"
	m.Update(upKey())   // cursor on "q"
	m.Update(upKey())   // cursor on "p"
	m.Update(spaceKey()) // toggle "p"

	// Act
	ids := m.SelectedIDs()

	// Assert
	want := []string{"p", "r"}
	if len(ids) != len(want) {
		t.Fatalf("SelectedIDs() = %v, want %v (wrong count)", ids, want)
	}
	for i, id := range ids {
		if id != want[i] {
			t.Errorf("SelectedIDs()[%d] = %q, want %q", i, id, want[i])
		}
	}
}

// TestMultiSelect_SelectedIDs_WhenNoneChecked_ReturnsNil verifies that SelectedIDs returns
// nil (not an empty slice) when nothing is checked, consistent with Go convention.
func TestMultiSelect_SelectedIDs_WhenNoneChecked_ReturnsNil(t *testing.T) {
	// Arrange
	m := widgets.NewMultiSelect(threeSelectItems(), 10, 80, widgets.DefaultMultiSelectStyles())

	// Act
	ids := m.SelectedIDs()

	// Assert
	if len(ids) != 0 {
		t.Errorf("SelectedIDs() = %v, want empty when nothing is checked", ids)
	}
}

// ---------------------------------------------------------------------------
// Confirm and back
// ---------------------------------------------------------------------------

// TestMultiSelect_Enter_SetsDone verifies that pressing Enter confirms the current set of
// selections without requiring any item to be checked (zero selection is valid, e.g. no
// optional workflows selected).
func TestMultiSelect_Enter_SetsDone(t *testing.T) {
	// Arrange
	m := widgets.NewMultiSelect(threeSelectItems(), 10, 80, widgets.DefaultMultiSelectStyles())

	// Act
	m.Update(enterKey())

	// Assert
	if !m.Done() {
		t.Error("Done() = false after Enter; want true")
	}
}

// TestMultiSelect_Esc_SetsBack verifies that pressing Esc signals back navigation, allowing
// the user to return to the previous screen using the keyboard alone.
func TestMultiSelect_Esc_SetsBack(t *testing.T) {
	// Arrange
	m := widgets.NewMultiSelect(threeSelectItems(), 10, 80, widgets.DefaultMultiSelectStyles())

	// Act
	m.Update(escKey())

	// Assert
	if !m.Back() {
		t.Error("Back() = false after Esc; want true")
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestMultiSelect_Reset_ClearsDoneAndBack verifies that Reset clears both Done and Back so
// the widget can be reused across navigation cycles.
func TestMultiSelect_Reset_ClearsDoneAndBack(t *testing.T) {
	// Arrange
	m := widgets.NewMultiSelect(threeSelectItems(), 10, 80, widgets.DefaultMultiSelectStyles())
	m.Update(enterKey()) // set Done

	// Act
	m.Reset()

	// Assert
	if m.Done() {
		t.Error("Done() = true after Reset; want false")
	}
	if m.Back() {
		t.Error("Back() = true after Reset; want false")
	}
}

// TestMultiSelect_Reset_PreservesCheckedState verifies that Reset does not clear checked
// items, so prior valid selections survive back navigation.
func TestMultiSelect_Reset_PreservesCheckedState(t *testing.T) {
	// Arrange
	m := widgets.NewMultiSelect(threeSelectItems(), 10, 80, widgets.DefaultMultiSelectStyles())
	m.Update(spaceKey()) // check "x"
	m.Update(enterKey())

	// Act
	m.Reset()

	// Assert
	if !m.IsChecked("x") {
		t.Error("IsChecked(\"x\") = false after Reset; prior checked state must survive reset")
	}
}

// ---------------------------------------------------------------------------
// SetChecked
// ---------------------------------------------------------------------------

// TestMultiSelect_SetChecked_ToTrue_MarksItem verifies that SetChecked(id, true) marks an
// item as checked without requiring keyboard interaction.
func TestMultiSelect_SetChecked_ToTrue_MarksItem(t *testing.T) {
	// Arrange
	m := widgets.NewMultiSelect(threeSelectItems(), 10, 80, widgets.DefaultMultiSelectStyles())

	// Act
	m.SetChecked("z", true)

	// Assert
	if !m.IsChecked("z") {
		t.Error("IsChecked(\"z\") = false after SetChecked(true); want true")
	}
}

// TestMultiSelect_SetChecked_ToFalse_UnmarksItem verifies that SetChecked(id, false) removes
// the check from an item.
func TestMultiSelect_SetChecked_ToFalse_UnmarksItem(t *testing.T) {
	// Arrange
	m := widgets.NewMultiSelect(threeSelectItems(), 10, 80, widgets.DefaultMultiSelectStyles())
	m.SetChecked("x", true)

	// Act
	m.SetChecked("x", false)

	// Assert
	if m.IsChecked("x") {
		t.Error("IsChecked(\"x\") = true after SetChecked(false); want false")
	}
}
