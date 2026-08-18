package screens_test

// keybinding_nonregression_test.go guards against accidental breakage of the shared
// widgets.List and widgets.MultiSelect key bindings as a consequence of the Stage 6
// changes to workflows.go. The Workflow Browser intercepts keys before delegating to
// the composed widget; any mistake in the interception layer could inadvertently change
// the widget's exported bindings and affect other screens.
//
// These tests drive each flat multi-select screen (UtilityAgentScreen, HookScreen) and
// each single-select list screen (HarnessScreen, ModeScreen) through Enter and Esc,
// asserting that their existing confirm-on-Enter and go-back-on-Esc semantics are intact.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Flat multi-select screens: UtilityAgentScreen
// ---------------------------------------------------------------------------

// TestNonRegression_UtilityAgentScreen_Enter_StillConfirms verifies that the
// UtilityAgentScreen still confirms the selection on Enter after Stage 6 changes.
// UtilityAgentScreen uses widgets.MultiSelect, whose Enter-confirms binding must not
// have been modified.
func TestNonRegression_UtilityAgentScreen_Enter_StillConfirms(t *testing.T) {
	s := screens.NewUtilityAgentScreen(utilityAgents, 80, 24, plainStyles(), "")

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("UtilityAgentScreen: Done() = false after Enter; widgets.MultiSelect Enter-confirms binding must be intact after Stage 6 changes")
	}
}

// TestNonRegression_UtilityAgentScreen_Esc_StillGoesBack verifies that the
// UtilityAgentScreen still signals Back() on Esc after Stage 6 changes.
func TestNonRegression_UtilityAgentScreen_Esc_StillGoesBack(t *testing.T) {
	s := screens.NewUtilityAgentScreen(utilityAgents, 80, 24, plainStyles(), "")

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("UtilityAgentScreen: Back() = false after Esc; widgets.MultiSelect Esc-goes-back binding must be intact after Stage 6 changes")
	}
	if s.Done() {
		t.Error("UtilityAgentScreen: Done() = true after Esc; Back and Done must be mutually exclusive")
	}
}

// TestNonRegression_UtilityAgentScreen_Tab_DoesNotConfirm verifies that Tab does not
// confirm the UtilityAgentScreen. This is the cross-screen invariant: no screen may bind
// Tab as a confirm key.
func TestNonRegression_UtilityAgentScreen_Tab_DoesNotConfirm(t *testing.T) {
	s := screens.NewUtilityAgentScreen(utilityAgents, 80, 24, plainStyles(), "")

	s.Update(tea.KeyMsg{Type: tea.KeyTab})

	if s.Done() {
		t.Error("UtilityAgentScreen: Done() = true after Tab; no screen may bind Tab as a confirm key (cross-screen invariant)")
	}
}

// ---------------------------------------------------------------------------
// Flat multi-select screens: HookScreen
// ---------------------------------------------------------------------------

// TestNonRegression_HookScreen_Enter_StillConfirms verifies that the HookScreen still
// confirms the selection on Enter after Stage 6 changes.
func TestNonRegression_HookScreen_Enter_StillConfirms(t *testing.T) {
	s := screens.NewHookScreen(hookBundles, true, 80, 24, plainStyles(), "")

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("HookScreen: Done() = false after Enter; widgets.MultiSelect Enter-confirms binding must be intact after Stage 6 changes")
	}
}

// TestNonRegression_HookScreen_Esc_StillGoesBack verifies that the HookScreen still
// signals Back() on Esc after Stage 6 changes.
func TestNonRegression_HookScreen_Esc_StillGoesBack(t *testing.T) {
	s := screens.NewHookScreen(hookBundles, true, 80, 24, plainStyles(), "")

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("HookScreen: Back() = false after Esc; widgets.MultiSelect Esc-goes-back binding must be intact after Stage 6 changes")
	}
	if s.Done() {
		t.Error("HookScreen: Done() = true after Esc; Back and Done must be mutually exclusive")
	}
}

// TestNonRegression_HookScreen_Tab_DoesNotConfirm verifies that Tab does not confirm the
// HookScreen selection.
func TestNonRegression_HookScreen_Tab_DoesNotConfirm(t *testing.T) {
	s := screens.NewHookScreen(hookBundles, true, 80, 24, plainStyles(), "")

	s.Update(tea.KeyMsg{Type: tea.KeyTab})

	if s.Done() {
		t.Error("HookScreen: Done() = true after Tab; no screen may bind Tab as a confirm key (cross-screen invariant)")
	}
}

// ---------------------------------------------------------------------------
// Single-select list screens: HarnessScreen
// ---------------------------------------------------------------------------

// TestNonRegression_HarnessScreen_Enter_StillConfirms verifies that the HarnessScreen still
// confirms the harness selection on Enter after Stage 6 changes. HarnessScreen uses
// widgets.List, whose Enter-confirms binding must not have been modified.
func TestNonRegression_HarnessScreen_Enter_StillConfirms(t *testing.T) {
	harnesses := []domain.HarnessRef{builtinHarness, descriptorHarness}
	s := screens.NewHarnessScreen(harnesses, 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("HarnessScreen: Done() = false after Enter; widgets.List Enter-confirms binding must be intact after Stage 6 changes")
	}
}

// TestNonRegression_HarnessScreen_Esc_StillGoesBack verifies that the HarnessScreen still
// signals Back() on Esc after Stage 6 changes.
func TestNonRegression_HarnessScreen_Esc_StillGoesBack(t *testing.T) {
	harnesses := []domain.HarnessRef{builtinHarness, descriptorHarness}
	s := screens.NewHarnessScreen(harnesses, 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("HarnessScreen: Back() = false after Esc; widgets.List Esc-goes-back binding must be intact after Stage 6 changes")
	}
	if s.Done() {
		t.Error("HarnessScreen: Done() = true after Esc; Back and Done must be mutually exclusive")
	}
}

// TestNonRegression_HarnessScreen_Tab_DoesNotConfirm verifies that Tab does not confirm the
// HarnessScreen selection.
func TestNonRegression_HarnessScreen_Tab_DoesNotConfirm(t *testing.T) {
	harnesses := []domain.HarnessRef{builtinHarness, descriptorHarness}
	s := screens.NewHarnessScreen(harnesses, 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyTab})

	if s.Done() {
		t.Error("HarnessScreen: Done() = true after Tab; no screen may bind Tab as a confirm key (cross-screen invariant)")
	}
}

// ---------------------------------------------------------------------------
// Single-select list screens: ModeScreen
// ---------------------------------------------------------------------------

// TestNonRegression_ModeScreen_Enter_StillConfirms verifies that the ModeScreen still
// confirms the mode selection on Enter after Stage 6 changes.
func TestNonRegression_ModeScreen_Enter_StillConfirms(t *testing.T) {
	s := screens.NewModeScreen(80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("ModeScreen: Done() = false after Enter; widgets.List Enter-confirms binding must be intact after Stage 6 changes")
	}
}

// TestNonRegression_ModeScreen_Esc_StillGoesBack verifies that the ModeScreen still signals
// Back() on Esc after Stage 6 changes.
func TestNonRegression_ModeScreen_Esc_StillGoesBack(t *testing.T) {
	s := screens.NewModeScreen(80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("ModeScreen: Back() = false after Esc; widgets.List Esc-goes-back binding must be intact after Stage 6 changes")
	}
	if s.Done() {
		t.Error("ModeScreen: Done() = true after Esc; Back and Done must be mutually exclusive")
	}
}

// TestNonRegression_ModeScreen_Tab_DoesNotConfirm verifies that Tab does not confirm the
// ModeScreen selection.
func TestNonRegression_ModeScreen_Tab_DoesNotConfirm(t *testing.T) {
	s := screens.NewModeScreen(80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyTab})

	if s.Done() {
		t.Error("ModeScreen: Done() = true after Tab; no screen may bind Tab as a confirm key (cross-screen invariant)")
	}
}
