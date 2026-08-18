package screens_test

// harness_test.go verifies the HarnessScreen: keyboard navigation, selection, back
// navigation, provision-tier badge display, and the external-module indicator.

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

// plainStyles returns a Styles with zero-value lipgloss styles, producing plain text without
// any ANSI escape codes. This makes view assertions on substring content reliable.
func plainStyles() screens.Styles {
	return screens.Styles{} // zero-value lipgloss.Style renders plain text
}

func harnessEnterKey() tea.Msg { return tea.KeyMsg{Type: tea.KeyEnter} }
func harnessEscKey() tea.Msg   { return tea.KeyMsg{Type: tea.KeyEsc} }
func harnessDownKey() tea.Msg  { return tea.KeyMsg{Type: tea.KeyDown} }

// ---------------------------------------------------------------------------
// Standard harness fixtures
// ---------------------------------------------------------------------------

var builtinHarness = domain.HarnessRef{
	ID:          "claude-code",
	DisplayName: "Claude Code",
	Tier:        domain.TierBuiltin,
	Usable:      true,
}

var descriptorHarness = domain.HarnessRef{
	ID:          "custom-desc",
	DisplayName: "Custom Descriptor",
	Tier:        domain.TierDescriptor,
	Usable:      true,
}

var activeExternalHarness = domain.HarnessRef{
	ID:             "my-external",
	DisplayName:    "My External Module",
	Tier:           domain.TierExternal,
	Usable:         true,
	ExecutablePath: "/usr/local/bin/my-module",
}

var inactiveExternalHarness = domain.HarnessRef{
	ID:             "gated-external",
	DisplayName:    "Gated External",
	Tier:           domain.TierExternal,
	Usable:         false,
	UnusableReason: "requires explicit opt-in",
}

// ---------------------------------------------------------------------------
// Selection — keyboard only
// ---------------------------------------------------------------------------

// TestHarnessScreen_Enter_OnUsableHarness_SetsDone verifies that pressing Enter when a
// usable harness is highlighted sets Done() and returns the correct harness ID.
func TestHarnessScreen_Enter_OnUsableHarness_SetsDone(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{builtinHarness},
		80, 24, plainStyles(),
	)

	// Act
	s.Update(harnessEnterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after Enter on a usable harness; want true")
	}
	if s.SelectedID() != "claude-code" {
		t.Errorf("SelectedID() = %q, want %q", s.SelectedID(), "claude-code")
	}
}

// TestHarnessScreen_Enter_OnUnusableHarness_DoesNotSetDone verifies that pressing Enter on a
// disabled (unusable) harness has no effect. The user must not be able to select a harness
// that cannot be loaded.
func TestHarnessScreen_Enter_OnUnusableHarness_DoesNotSetDone(t *testing.T) {
	// Arrange — only an unusable harness
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{inactiveExternalHarness},
		80, 24, plainStyles(),
	)

	// Act
	s.Update(harnessEnterKey())

	// Assert
	if s.Done() {
		t.Error("Done() = true after Enter on an unusable harness; want false (unusable harness must not be selectable)")
	}
}

// TestHarnessScreen_Down_Then_Enter_SelectsSecondHarness verifies that keyboard navigation
// to a different item and then Enter selects that item, not the initial cursor position.
func TestHarnessScreen_Down_Then_Enter_SelectsSecondHarness(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{builtinHarness, descriptorHarness},
		80, 24, plainStyles(),
	)

	// Act
	s.Update(harnessDownKey()) // cursor on descriptorHarness
	s.Update(harnessEnterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after navigating and pressing Enter; want true")
	}
	if s.SelectedID() != "custom-desc" {
		t.Errorf("SelectedID() = %q, want %q", s.SelectedID(), "custom-desc")
	}
}

// ---------------------------------------------------------------------------
// Back navigation
// ---------------------------------------------------------------------------

// TestHarnessScreen_Esc_SetsBack verifies that pressing Esc sets Back() so the root model
// can interpret it as a quit signal (nothing to go back to from the first entry screen).
func TestHarnessScreen_Esc_SetsBack(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{builtinHarness},
		80, 24, plainStyles(),
	)

	// Act
	s.Update(harnessEscKey())

	// Assert
	if !s.Back() {
		t.Error("Back() = false after Esc; want true")
	}
}

// TestHarnessScreen_Reset_ClearsDoneAndBack verifies that Reset restores the screen to an
// idle state so the root model can reuse it when the user navigates back to it.
func TestHarnessScreen_Reset_ClearsDoneAndBack(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{builtinHarness},
		80, 24, plainStyles(),
	)
	s.Update(harnessEnterKey())
	if !s.Done() {
		t.Fatal("precondition: Done() must be true before Reset")
	}

	// Act
	s.Reset()

	// Assert
	if s.Done() {
		t.Error("Done() = true after Reset; want false")
	}
	if s.Back() {
		t.Error("Back() = true after Reset; want false")
	}
}

// ---------------------------------------------------------------------------
// Provision tier display
// ---------------------------------------------------------------------------

// TestHarnessScreen_View_ShowsBuiltinTierBadge verifies that a built-in harness is
// labelled with its provision tier so the user knows what kind of integration they are
// selecting.
func TestHarnessScreen_View_ShowsBuiltinTierBadge(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{builtinHarness},
		80, 24, plainStyles(),
	)

	// Act
	view := s.View()

	// Assert — the built-in tier badge must appear somewhere in the rendered view.
	if !strings.Contains(view, "[built-in]") {
		t.Errorf("view does not contain the built-in tier badge:\n%s", view)
	}
}

// TestHarnessScreen_View_ShowsDescriptorTierBadge verifies that a descriptor-only harness
// is labelled with the [descriptor] tier badge.
func TestHarnessScreen_View_ShowsDescriptorTierBadge(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{descriptorHarness},
		80, 24, plainStyles(),
	)

	// Act
	view := s.View()

	// Assert
	if !strings.Contains(view, "[descriptor]") {
		t.Errorf("view does not contain the descriptor tier badge:\n%s", view)
	}
}

// TestHarnessScreen_View_ShowsExternalTierBadge verifies that a harness served by an external
// module is labelled with the [external] tier badge.
func TestHarnessScreen_View_ShowsExternalTierBadge(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{activeExternalHarness},
		80, 24, plainStyles(),
	)

	// Act
	view := s.View()

	// Assert
	if !strings.Contains(view, "[external]") {
		t.Errorf("view does not contain the external tier badge:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// External-module indicator
// ---------------------------------------------------------------------------

// TestHarnessScreen_View_ShowsExternalModuleIndicator_WhenUsable verifies that an external
// harness that is usable (opted-in and loaded) displays the "[active external module]"
// indicator. This indicator is the acceptance criterion's "unmistakable" signal.
// The view is whitespace-collapsed before the check because lipgloss may wrap the label
// across multiple lines at narrow terminal widths.
func TestHarnessScreen_View_ShowsExternalModuleIndicator_WhenUsable(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{activeExternalHarness},
		80, 24, plainStyles(),
	)

	// Act
	view := collapseWhitespace(s.View())

	// Assert
	if !strings.Contains(view, "[active external module]") {
		t.Errorf("view does not contain the external-module indicator for a usable external harness:\n%s", s.View())
	}
}

// TestHarnessScreen_View_OmitsExternalModuleIndicator_WhenNotUsable verifies that the
// "[active external module]" indicator is NOT shown when an external harness is unusable
// (gated, not opted in, or failed to load). Only an active (opted-in and loaded) external
// module is "unmistakably indicated".
func TestHarnessScreen_View_OmitsExternalModuleIndicator_WhenNotUsable(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{inactiveExternalHarness},
		80, 24, plainStyles(),
	)

	// Act — collapse whitespace for reliable substring matching
	view := collapseWhitespace(s.View())

	// Assert
	if strings.Contains(view, "[active external module]") {
		t.Errorf("view contains the external-module indicator for an unusable (gated) harness; it must only appear when the module is active:\n%s", s.View())
	}
}

// TestHarnessScreen_View_OmitsExternalModuleIndicator_ForBuiltin verifies that the
// "[active external module]" indicator does not appear for built-in harnesses, where the
// tier badge alone is the correct label.
func TestHarnessScreen_View_OmitsExternalModuleIndicator_ForBuiltin(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{builtinHarness},
		80, 24, plainStyles(),
	)

	// Act — collapse whitespace for reliable substring matching
	view := collapseWhitespace(s.View())

	// Assert
	if strings.Contains(view, "[active external module]") {
		t.Errorf("view shows external-module indicator for a built-in harness; it must only appear for active external modules:\n%s", s.View())
	}
}

// TestHarnessScreen_View_MultipleHarnesses_ShowsAllTierBadges verifies that when several
// harnesses are listed, each one shows its own tier badge so the user can distinguish them.
func TestHarnessScreen_View_MultipleHarnesses_ShowsAllTierBadges(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{builtinHarness, descriptorHarness, activeExternalHarness},
		80, 24, plainStyles(),
	)

	// Act
	view := s.View()

	// Assert
	if !strings.Contains(view, "[built-in]") {
		t.Error("view does not show [built-in] badge for the builtin harness")
	}
	if !strings.Contains(view, "[descriptor]") {
		t.Error("view does not show [descriptor] badge for the descriptor harness")
	}
	if !strings.Contains(view, "[external]") {
		t.Error("view does not show [external] badge for the external harness")
	}
}

// ---------------------------------------------------------------------------
// Keyboard-only operability
// ---------------------------------------------------------------------------

// TestHarnessScreen_KeyboardOnly_FullSelectionFlow verifies that the harness selection
// screen is fully operable by keyboard alone: up/down to navigate, Enter to confirm.
// No mouse event is required at any point in the flow.
func TestHarnessScreen_KeyboardOnly_FullSelectionFlow(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{builtinHarness, descriptorHarness},
		80, 24, plainStyles(),
	)

	// Act — navigate to second item using down arrow, then confirm with Enter
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert — the screen completed using keyboard alone
	if !s.Done() {
		t.Error("Done() = false after keyboard-only navigation and confirmation; every selection must be reachable by keyboard")
	}
	if s.SelectedID() != "custom-desc" {
		t.Errorf("SelectedID() = %q, want %q", s.SelectedID(), "custom-desc")
	}
}

// TestHarnessScreen_KeyboardOnly_VimKeysWork verifies that the vim-style navigation keys
// (j/k) work on the harness screen, providing an alternative keyboard path.
func TestHarnessScreen_KeyboardOnly_VimKeysWork(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{builtinHarness, descriptorHarness},
		80, 24, plainStyles(),
	)

	// Act — navigate using 'j' (vim-style), then confirm
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if !s.Done() {
		t.Error("Done() = false after vim-style navigation; 'j'/'k' keys must be operable on the harness screen")
	}
	if s.SelectedID() != "custom-desc" {
		t.Errorf("SelectedID() = %q, want %q", s.SelectedID(), "custom-desc")
	}
}
