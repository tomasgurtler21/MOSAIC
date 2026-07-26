package screens_test

// mode_test.go verifies the ModeScreen at the screen level: keyboard navigation, selection
// (Done() / SelectedMode()), back navigation (Back()), Reset(), and keyboard-only operability.
//
// All tests drive the screen through the model/update cycle. No real terminal is required.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func modeEnterKey() tea.Msg { return tea.KeyMsg{Type: tea.KeyEnter} }
func modeEscKey() tea.Msg   { return tea.KeyMsg{Type: tea.KeyEsc} }
func modeDownKey() tea.Msg  { return tea.KeyMsg{Type: tea.KeyDown} }
func modeUpKey() tea.Msg    { return tea.KeyMsg{Type: tea.KeyUp} }

func newModeScreen() *screens.ModeScreen {
	return screens.NewModeScreen(80, 24, plainStyles())
}

// ---------------------------------------------------------------------------
// Initial state
// ---------------------------------------------------------------------------

// TestModeScreen_InitialState_DoneIsFalse verifies that a freshly created ModeScreen has not
// been confirmed — the user has not yet chosen a mode.
func TestModeScreen_InitialState_DoneIsFalse(t *testing.T) {
	// Arrange / Act
	s := newModeScreen()

	// Assert
	if s.Done() {
		t.Error("Done() = true on a new ModeScreen; want false (no mode selected yet)")
	}
}

// TestModeScreen_InitialState_BackIsFalse verifies that a freshly created ModeScreen is not
// in the back-navigation state.
func TestModeScreen_InitialState_BackIsFalse(t *testing.T) {
	// Arrange / Act
	s := newModeScreen()

	// Assert
	if s.Back() {
		t.Error("Back() = true on a new ModeScreen; want false (Esc has not been pressed)")
	}
}

// ---------------------------------------------------------------------------
// Selection — Enter confirms
// ---------------------------------------------------------------------------

// TestModeScreen_Enter_SetsDone verifies that pressing Enter on the first item sets Done()
// so the root model knows the user has confirmed a mode and can advance to the next screen.
func TestModeScreen_Enter_SetsDone(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act
	s.Update(modeEnterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after Enter; want true (mode must be confirmed on Enter)")
	}
}

// TestModeScreen_Enter_SelectsDeployNewAsFirstMode verifies that the first item in the list
// is "deploy new" and that pressing Enter without navigating selects that mode.
func TestModeScreen_Enter_SelectsDeployNewAsFirstMode(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act
	s.Update(modeEnterKey())

	// Assert
	if s.SelectedMode() != domain.ModeDeployNew {
		t.Errorf("SelectedMode() = %q after Enter without navigation; want %q", s.SelectedMode(), domain.ModeDeployNew)
	}
}

// TestModeScreen_Down_Then_Enter_SelectsUpdateMode verifies that navigating down to the
// second item and pressing Enter selects "update" mode, confirming that navigation changes
// which mode is returned by SelectedMode().
func TestModeScreen_Down_Then_Enter_SelectsUpdateMode(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act
	s.Update(modeDownKey()) // cursor on "update existing"
	s.Update(modeEnterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after navigating and pressing Enter; want true")
	}
	if s.SelectedMode() != domain.ModeUpdate {
		t.Errorf("SelectedMode() = %q after Down+Enter; want %q", s.SelectedMode(), domain.ModeUpdate)
	}
}

// TestModeScreen_Up_AfterDown_ReselectsDeployNew verifies that pressing Up after Down returns
// the cursor to the first item so DeployNew is selected on Enter.
func TestModeScreen_Up_AfterDown_ReselectsDeployNew(t *testing.T) {
	// Arrange
	s := newModeScreen()
	s.Update(modeDownKey()) // move to update

	// Act
	s.Update(modeUpKey())    // move back to deploy-new
	s.Update(modeEnterKey())

	// Assert
	if s.SelectedMode() != domain.ModeDeployNew {
		t.Errorf("SelectedMode() = %q after Down+Up+Enter; want %q (Up must restore previous item)",
			s.SelectedMode(), domain.ModeDeployNew)
	}
}

// TestModeScreen_Enter_DoesNotSetBack verifies that Enter and Back are mutually exclusive:
// confirming a mode must not also trigger back navigation.
func TestModeScreen_Enter_DoesNotSetBack(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act
	s.Update(modeEnterKey())

	// Assert
	if s.Back() {
		t.Error("Back() = true after Enter; want false (confirm and back must be mutually exclusive)")
	}
}

// ---------------------------------------------------------------------------
// Back navigation — Esc
// ---------------------------------------------------------------------------

// TestModeScreen_Esc_SetsBack verifies that pressing Esc signals back navigation so the root
// model can return to the harness selection screen while preserving the harness selection.
func TestModeScreen_Esc_SetsBack(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act
	s.Update(modeEscKey())

	// Assert
	if !s.Back() {
		t.Error("Back() = false after Esc; want true")
	}
}

// TestModeScreen_Esc_DoesNotSetDone verifies that Esc and Done are mutually exclusive.
func TestModeScreen_Esc_DoesNotSetDone(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act
	s.Update(modeEscKey())

	// Assert
	if s.Done() {
		t.Error("Done() = true after Esc; want false (Esc must not confirm a mode)")
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestModeScreen_Reset_ClearsDoneFlag verifies that Reset clears the Done flag so the root
// model can reuse the screen when the user navigates back to it.
func TestModeScreen_Reset_ClearsDoneFlag(t *testing.T) {
	// Arrange
	s := newModeScreen()
	s.Update(modeEnterKey())
	if !s.Done() {
		t.Fatal("precondition: Done() must be true before Reset")
	}

	// Act
	s.Reset()

	// Assert
	if s.Done() {
		t.Error("Done() = true after Reset; want false")
	}
}

// TestModeScreen_Reset_ClearsBackFlag verifies that Reset clears the Back flag so the screen
// is fully idle after the root model processes a back navigation and then reactivates it.
func TestModeScreen_Reset_ClearsBackFlag(t *testing.T) {
	// Arrange
	s := newModeScreen()
	s.Update(modeEscKey())
	if !s.Back() {
		t.Fatal("precondition: Back() must be true before Reset")
	}

	// Act
	s.Reset()

	// Assert
	if s.Back() {
		t.Error("Back() = true after Reset; want false")
	}
}

// ---------------------------------------------------------------------------
// View content
// ---------------------------------------------------------------------------

// TestModeScreen_View_ShowsBothModes verifies that the rendered view presents both run modes
// so the user can see and choose between them.
func TestModeScreen_View_ShowsBothModes(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act
	view := s.View()

	// Assert — both modes must be visible; use collapseWhitespace to tolerate lipgloss wrapping.
	collapsed := collapseWhitespace(view)
	if !strings.Contains(collapsed, "Deploy new") {
		t.Errorf("view does not mention 'Deploy new':\n%s", view)
	}
	if !strings.Contains(collapsed, "Update existing") {
		t.Errorf("view does not mention 'Update existing':\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Keyboard-only operability
// ---------------------------------------------------------------------------

// TestModeScreen_KeyboardOnly_CanSelectDeployNew verifies that the deploy-new mode is
// reachable using the keyboard alone: press Enter without any navigation to select the
// default first item.
func TestModeScreen_KeyboardOnly_CanSelectDeployNew(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act — keyboard-only: Enter selects the current item
	s.Update(modeEnterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after keyboard Enter; deploy-new must be selectable by keyboard alone")
	}
	if s.SelectedMode() != domain.ModeDeployNew {
		t.Errorf("SelectedMode() = %q; want %q", s.SelectedMode(), domain.ModeDeployNew)
	}
}

// TestModeScreen_KeyboardOnly_CanSelectUpdate verifies that the update mode is reachable
// using the keyboard alone: navigate down with the arrow key and confirm with Enter.
// No mouse event is needed.
func TestModeScreen_KeyboardOnly_CanSelectUpdate(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act — keyboard-only: Down arrow then Enter
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if !s.Done() {
		t.Error("Done() = false after keyboard Down+Enter; update mode must be reachable by keyboard alone")
	}
	if s.SelectedMode() != domain.ModeUpdate {
		t.Errorf("SelectedMode() = %q; want %q", s.SelectedMode(), domain.ModeUpdate)
	}
}

// TestModeScreen_KeyboardOnly_VimKeysNavigate verifies that the vim-style j/k navigation
// keys work on the mode screen, providing an alternative keyboard path.
func TestModeScreen_KeyboardOnly_VimKeysNavigate(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act — navigate with 'j' (vim-style down), then confirm
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	s.Update(modeEnterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after vim-key navigation; 'j'/'k' must work on the mode screen")
	}
	if s.SelectedMode() != domain.ModeUpdate {
		t.Errorf("SelectedMode() = %q after 'j'+Enter; want %q", s.SelectedMode(), domain.ModeUpdate)
	}
}

// TestModeScreen_KeyboardOnly_EscNavigatesBack verifies that the Esc key alone triggers back
// navigation on the mode screen, so the user can return to harness selection without a mouse.
func TestModeScreen_KeyboardOnly_EscNavigatesBack(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act
	s.Update(modeEscKey())

	// Assert
	if !s.Back() {
		t.Error("Back() = false after keyboard Esc; mode screen must be fully navigable by keyboard")
	}
}
