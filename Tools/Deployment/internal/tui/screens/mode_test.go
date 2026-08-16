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

// TestModeScreen_Enter_SelectsDeployWorkspaceAsFirstMode verifies that the first item in the
// list is "deploy workspace" and that pressing Enter without navigating selects that mode.
func TestModeScreen_Enter_SelectsDeployWorkspaceAsFirstMode(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act
	s.Update(modeEnterKey())

	// Assert
	if s.SelectedMode() != domain.ModeDeployWorkspace {
		t.Errorf("SelectedMode() = %q after Enter without navigation; want %q (deploy-workspace must be first)",
			s.SelectedMode(), domain.ModeDeployWorkspace)
	}
}

// TestModeScreen_Down_Then_Enter_SelectsUpdateWorkspaceMode verifies that navigating down to
// the second item and pressing Enter selects "update-workspace" mode, confirming that
// navigation changes which mode is returned by SelectedMode().
func TestModeScreen_Down_Then_Enter_SelectsUpdateWorkspaceMode(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act
	s.Update(modeDownKey()) // cursor on "update workspace"
	s.Update(modeEnterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after navigating and pressing Enter; want true")
	}
	if s.SelectedMode() != domain.ModeUpdateWorkspace {
		t.Errorf("SelectedMode() = %q after Down+Enter; want %q (update-workspace must be the second entry)",
			s.SelectedMode(), domain.ModeUpdateWorkspace)
	}
}

// TestModeScreen_Up_AfterDown_ReselectsDeployWorkspace verifies that pressing Up after Down
// returns the cursor to the first item so DeployWorkspace is selected on Enter.
func TestModeScreen_Up_AfterDown_ReselectsDeployWorkspace(t *testing.T) {
	// Arrange
	s := newModeScreen()
	s.Update(modeDownKey()) // move to update-workspace

	// Act
	s.Update(modeUpKey())    // move back to deploy-workspace
	s.Update(modeEnterKey())

	// Assert
	if s.SelectedMode() != domain.ModeDeployWorkspace {
		t.Errorf("SelectedMode() = %q after Down+Up+Enter; want %q (Up must restore previous item)",
			s.SelectedMode(), domain.ModeDeployWorkspace)
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
// View content — renamed labels
// ---------------------------------------------------------------------------

// TestModeScreen_View_ShowsDeployWorkspaceAndUpdateWorkspaceLabels verifies that the
// rendered view presents both the "Deploy workspace" and "Update workspace" mode labels
// so that users can see and choose the primary deployment operations. The old labels
// "Deploy new" and "Update existing" must not appear; the renamed labels replace them.
func TestModeScreen_View_ShowsDeployWorkspaceAndUpdateWorkspaceLabels(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act
	view := s.View()

	// Assert — new labels must be visible; use collapseWhitespace to tolerate lipgloss wrapping.
	collapsed := collapseWhitespace(view)
	if !strings.Contains(collapsed, "Deploy workspace") {
		t.Errorf("view does not show 'Deploy workspace' label; the rename must update the label text:\n%s", view)
	}
	if !strings.Contains(collapsed, "Update workspace") {
		t.Errorf("view does not show 'Update workspace' label; the rename must update the label text:\n%s", view)
	}
}

// TestModeScreen_View_ShowsAllFiveSurvivingModeLabels verifies that the rendered view
// presents all five surviving mode labels so every available operation is discoverable
// from the mode selection screen.
func TestModeScreen_View_ShowsAllFiveSurvivingModeLabels(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act
	view := s.View()

	// Assert — all five surviving mode labels must be visible.
	collapsed := collapseWhitespace(view)
	wantLabels := []string{
		"Deploy workspace",
		"Update workspace",
		"Update workflows",
		"Promote to generic",
		"Transform harness",
	}
	for _, label := range wantLabels {
		if !strings.Contains(collapsed, label) {
			t.Errorf("view does not show label %q; the mode list must include all five surviving modes:\n%s",
				label, view)
		}
	}
}

// ---------------------------------------------------------------------------
// Third mode — update-workflows
// ---------------------------------------------------------------------------

// TestModeScreen_DownDown_Enter_SelectsUpdateWorkflowsMode verifies that pressing Down twice
// and then Enter selects the update-workflows mode, which is the third entry in the mode list.
func TestModeScreen_DownDown_Enter_SelectsUpdateWorkflowsMode(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act — navigate to the third item and confirm.
	s.Update(modeDownKey()) // deploy-workspace → update-workspace
	s.Update(modeDownKey()) // update-workspace → update-workflows
	s.Update(modeEnterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after Down+Down+Enter; want true (third item must be selectable)")
	}
	if s.SelectedMode() != domain.ModeUpdateWorkflows {
		t.Errorf("SelectedMode() = %q after Down+Down+Enter; want %q (update-workflows must be the third list entry)",
			s.SelectedMode(), domain.ModeUpdateWorkflows)
	}
}

// TestModeScreen_UpdateWorkflows_YieldsCorrectRunMode verifies that when the update-workflows
// mode is selected, SelectedMode() returns exactly domain.ModeUpdateWorkflows. The string
// value must match the constant so both frontends produce the same mode value.
func TestModeScreen_UpdateWorkflows_YieldsCorrectRunMode(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act — navigate to update-workflows (third item) and confirm.
	s.Update(modeDownKey())
	s.Update(modeDownKey())
	s.Update(modeEnterKey())

	// Assert — the concrete string value must equal the domain constant, not just be non-empty.
	got := s.SelectedMode()
	want := domain.ModeUpdateWorkflows
	if got != want {
		t.Errorf("SelectedMode() = %q; want %q — the mode value must match the domain constant exactly", got, want)
	}
}

// TestModeScreen_UpdateWorkflows_DetailMentionsOrchestratorAndRemoval verifies that after
// navigating to the update-workflows entry, the detail text describes the key behaviour: only
// the orchestrator is rewritten, and unselected workflows are removed. Users must be able to
// understand the destructive nature of the operation from the detail pane alone.
func TestModeScreen_UpdateWorkflows_DetailMentionsOrchestratorAndRemoval(t *testing.T) {
	// Arrange — navigate to the third item so its detail text is shown.
	s := newModeScreen()
	s.Update(modeDownKey()) // deploy-workspace → update-workspace
	s.Update(modeDownKey()) // update-workspace → update-workflows

	// Act
	view := s.View()

	// Assert — the detail pane (shown in the right column) must mention both concepts.
	collapsed := collapseWhitespace(view)
	if !strings.Contains(collapsed, "orchestrator") {
		t.Errorf("view after navigating to update-workflows does not mention 'orchestrator' in the detail text; "+
			"the user must understand that only the orchestrator file is affected:\n%s", view)
	}
	if !strings.Contains(collapsed, "removed") && !strings.Contains(collapsed, "remove") {
		t.Errorf("view after navigating to update-workflows does not mention workflow removal in the detail text; "+
			"the user must be warned that unselected workflows are removed:\n%s", view)
	}
}

// TestModeScreen_ThirdEntry_DoesNotConfirmOnSecondDown verifies that the second Down press
// does not confirm the selection prematurely — it moves to the third item. Only Enter
// confirms. This guards against a bug where a list wraps or stops at index 1.
func TestModeScreen_ThirdEntry_DoesNotConfirmOnSecondDown(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act — press Down twice (moves to third item); do NOT press Enter.
	s.Update(modeDownKey()) // first Down
	s.Update(modeDownKey()) // second Down

	// Assert — Done must still be false; only Enter can confirm.
	if s.Done() {
		t.Error("Done() = true after two Down presses without Enter; Down must navigate, not confirm")
	}
	if s.Back() {
		t.Error("Back() = true after two Down presses; Down must not trigger back navigation")
	}
}

// ---------------------------------------------------------------------------
// All mode items — ID / RunMode correspondence invariant
// ---------------------------------------------------------------------------

// TestModeScreen_AllItems_IDMatchesDeclaredRunMode navigates to each position in the
// mode list and verifies that SelectedMode() returns exactly the expected domain.RunMode
// constant. This invariant ensures every modeItems entry's ID equals the string value of
// the matching RunMode constant, which ModeScreen.SelectedMode() relies on to map the
// selected list item back to a well-typed RunMode.
//
// Expected list order:
//   Position 0: ModeDeployWorkspace  ("deploy-workspace")
//   Position 1: ModeUpdateWorkspace  ("update-workspace")
//   Position 2: ModeUpdateWorkflows  ("update-workflows")
//   Position 3: ModeDeployAgents     ("deploy-agents")
//   Position 4: ModeDeployHooks      ("deploy-hooks")
//   Position 5: ModePromoteToGeneric ("promote-to-generic")
//   Position 6: ModeTransformHarness ("transform-harness")
func TestModeScreen_AllItems_IDMatchesDeclaredRunMode(t *testing.T) {
	positions := []struct {
		name        string
		downPresses int
		wantMode    domain.RunMode
	}{
		{"position 0 (deploy-workspace)", 0, domain.ModeDeployWorkspace},
		{"position 1 (update-workspace)", 1, domain.ModeUpdateWorkspace},
		{"position 2 (update-workflows)", 2, domain.ModeUpdateWorkflows},
		{"position 3 (deploy-agents)", 3, domain.ModeDeployAgents},
		{"position 4 (deploy-hooks)", 4, domain.ModeDeployHooks},
		{"position 5 (promote-to-generic)", 5, domain.ModePromoteToGeneric},
		{"position 6 (transform-harness)", 6, domain.ModeTransformHarness},
	}

	for _, tc := range positions {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Arrange — fresh screen for each position so prior navigation does not carry over.
			s := newModeScreen()

			// Act — navigate to the target position and confirm.
			for i := 0; i < tc.downPresses; i++ {
				s.Update(modeDownKey())
			}
			s.Update(modeEnterKey())

			// Assert
			if !s.Done() {
				t.Fatalf("Done() = false after navigating to %s and pressing Enter", tc.name)
			}
			if got := s.SelectedMode(); got != tc.wantMode {
				t.Errorf("SelectedMode() at %s = %q, want %q; "+
					"every modeItems entry's ID must equal string(matching RunMode constant)",
					tc.name, got, tc.wantMode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Keyboard-only operability
// ---------------------------------------------------------------------------

// TestModeScreen_KeyboardOnly_CanSelectDeployWorkspace verifies that the deploy-workspace mode
// is reachable using the keyboard alone: press Enter without any navigation to select the
// default first item.
func TestModeScreen_KeyboardOnly_CanSelectDeployWorkspace(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act — keyboard-only: Enter selects the current item
	s.Update(modeEnterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after keyboard Enter; deploy-workspace must be selectable by keyboard alone")
	}
	if s.SelectedMode() != domain.ModeDeployWorkspace {
		t.Errorf("SelectedMode() = %q; want %q", s.SelectedMode(), domain.ModeDeployWorkspace)
	}
}

// TestModeScreen_KeyboardOnly_CanSelectUpdateWorkspace verifies that the update-workspace mode
// is reachable using the keyboard alone: navigate down with the arrow key and confirm with Enter.
// No mouse event is needed.
func TestModeScreen_KeyboardOnly_CanSelectUpdateWorkspace(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act — keyboard-only: Down arrow then Enter
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if !s.Done() {
		t.Error("Done() = false after keyboard Down+Enter; update-workspace mode must be reachable by keyboard alone")
	}
	if s.SelectedMode() != domain.ModeUpdateWorkspace {
		t.Errorf("SelectedMode() = %q; want %q", s.SelectedMode(), domain.ModeUpdateWorkspace)
	}
}

// TestModeScreen_KeyboardOnly_CanSelectUpdateWorkflows verifies that the update-workflows mode
// is reachable using the keyboard alone: navigate down twice with arrow keys and confirm
// with Enter. No mouse event is required.
func TestModeScreen_KeyboardOnly_CanSelectUpdateWorkflows(t *testing.T) {
	// Arrange
	s := newModeScreen()

	// Act — keyboard-only: Down+Down+Enter
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if !s.Done() {
		t.Error("Done() = false after keyboard Down+Down+Enter; update-workflows must be reachable by keyboard alone")
	}
	if s.SelectedMode() != domain.ModeUpdateWorkflows {
		t.Errorf("SelectedMode() = %q; want %q", s.SelectedMode(), domain.ModeUpdateWorkflows)
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
	if s.SelectedMode() != domain.ModeUpdateWorkspace {
		t.Errorf("SelectedMode() = %q after 'j'+Enter; want %q", s.SelectedMode(), domain.ModeUpdateWorkspace)
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
