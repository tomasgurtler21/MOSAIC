package screens_test

// hooks_test.go verifies the HookScreen across all three presentation modes:
//   - Harness supports hooks and bundles are available → multi-select list.
//   - Harness does not support hooks → clear informational message.
//   - Harness supports hooks but catalog has no bundles → empty-state message.
//   - Placeholder bundles are presented with a [placeholder] label and a warning.
//   - Keyboard-only operability on the multi-select path.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

var hookBundles = []domain.HookBundle{
	{Key: "subagent-logger", Description: "Logs subagent interactions.", Version: "1.0"},
}

var placeholderHookBundles = []domain.HookBundle{
	{Key: "experimental-hook", Description: "Experimental hook bundle.", Version: "0.1", Placeholder: true},
}

func newHookScreen(hooks []domain.HookBundle, harnessSupported bool) *screens.HookScreen {
	return screens.NewHookScreen(hooks, harnessSupported, 80, 24, plainStyles(), "")
}

// ---------------------------------------------------------------------------
// Harness supports hooks — normal path
// ---------------------------------------------------------------------------

// TestHookScreen_WithHarnessSupportAndBundles_ShowsList verifies that when the harness
// supports hooks and bundles are available, the multi-select list is shown.
func TestHookScreen_WithHarnessSupportAndBundles_ShowsList(t *testing.T) {
	s := newHookScreen(hookBundles, true)

	view := s.View()

	if !strings.Contains(view, "subagent-logger") {
		t.Errorf("view must contain 'subagent-logger'; got:\n%s", view)
	}
}

// TestHookScreen_Space_TogglesBundle verifies that Space toggles a hook bundle's selection.
func TestHookScreen_Space_TogglesBundle(t *testing.T) {
	s := newHookScreen(hookBundles, true)

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

	ids := s.SelectedIDs()
	if !containsStr(ids, "subagent-logger") {
		t.Errorf("SelectedIDs() = %v after Space; want 'subagent-logger'", ids)
	}
}

// TestHookScreen_Enter_SetsDone verifies that pressing Enter confirms the hook selection.
func TestHookScreen_Enter_SetsDone(t *testing.T) {
	s := newHookScreen(hookBundles, true)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter on a multi-select hook screen; want true")
	}
}

// TestHookScreen_Esc_SetsBack verifies that Esc goes back on the multi-select path.
func TestHookScreen_Esc_SetsBack(t *testing.T) {
	s := newHookScreen(hookBundles, true)

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("Back() = false after Esc; want true")
	}
}

// ---------------------------------------------------------------------------
// No-hooks-supported case
// ---------------------------------------------------------------------------

// TestHookScreen_HarnessNotSupported_ShowsClearMessage verifies that when the harness does
// not support hooks a clear human-readable message is shown, not an empty screen (AC21.5).
func TestHookScreen_HarnessNotSupported_ShowsClearMessage(t *testing.T) {
	s := newHookScreen(hookBundles, false /* harness does not support hooks */)

	view := s.View()

	if strings.TrimSpace(view) == "" {
		t.Error("no-hooks-supported view must not be blank")
	}
	// The message must make it clear that hooks are not supported by the harness.
	lv := strings.ToLower(view)
	if !strings.Contains(lv, "does not support hooks") && !strings.Contains(lv, "not supported") {
		t.Errorf("no-hooks-supported view must contain a clear message; got:\n%s", view)
	}
}

// TestHookScreen_HarnessNotSupported_EnterAdvances verifies that Enter advances past the
// no-hooks-supported informational screen without requiring Esc.
func TestHookScreen_HarnessNotSupported_EnterAdvances(t *testing.T) {
	s := newHookScreen(hookBundles, false)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter on no-hooks-supported screen; want true (must be able to advance)")
	}
}

// TestHookScreen_HarnessNotSupported_SelectedIDs_IsEmpty verifies that SelectedIDs() returns
// an empty slice when the harness does not support hooks.
func TestHookScreen_HarnessNotSupported_SelectedIDs_IsEmpty(t *testing.T) {
	s := newHookScreen(hookBundles, false)

	ids := s.SelectedIDs()
	if len(ids) != 0 {
		t.Errorf("SelectedIDs() = %v when harness does not support hooks; want empty slice", ids)
	}
}

// ---------------------------------------------------------------------------
// Placeholder bundle presentation
// ---------------------------------------------------------------------------

// TestHookScreen_PlaceholderBundle_LabelledInList verifies that a placeholder bundle has
// a [placeholder] suffix in the list view so the user can identify it (AC21.5).
func TestHookScreen_PlaceholderBundle_LabelledInList(t *testing.T) {
	s := newHookScreen(placeholderHookBundles, true)

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "[placeholder]") {
		t.Errorf("placeholder bundle must show [placeholder] label in the list; got:\n%s", s.View())
	}
}

// TestHookScreen_PlaceholderBundle_DetailPaneWarning verifies that the detail pane shows a
// warning message when a placeholder bundle is focused (honest presentation, AC21.5).
func TestHookScreen_PlaceholderBundle_DetailPaneWarning(t *testing.T) {
	s := newHookScreen(placeholderHookBundles, true)

	view := strings.ToLower(s.View())

	// The detail pane must contain a warning about placeholder content.
	if !strings.Contains(view, "placeholder") || !strings.Contains(view, "warning") {
		t.Errorf("detail pane must show a warning for placeholder bundles; got:\n%s", s.View())
	}
}

// TestHookScreen_PlaceholderBundle_IsSelectable verifies that placeholder bundles can still
// be selected (the user is informed, not blocked).
func TestHookScreen_PlaceholderBundle_IsSelectable(t *testing.T) {
	s := newHookScreen(placeholderHookBundles, true)

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

	ids := s.SelectedIDs()
	if !containsStr(ids, "experimental-hook") {
		t.Errorf("SelectedIDs() = %v; placeholder bundle must be selectable; want 'experimental-hook'", ids)
	}
}

// ---------------------------------------------------------------------------
// Empty-state
// ---------------------------------------------------------------------------

// TestHookScreen_EmptyBundleList_RendersMessage verifies that when the harness supports hooks
// but there are no bundles in the catalog, an informational message is shown (AC21.6).
func TestHookScreen_EmptyBundleList_RendersMessage(t *testing.T) {
	s := newHookScreen(nil, true /* harness supports hooks */)

	view := s.View()

	if strings.TrimSpace(view) == "" {
		t.Error("empty-bundle view must not be blank")
	}
	if !strings.Contains(strings.ToLower(view), "no hook") {
		t.Errorf("empty-bundle view must contain a 'no hook' message; got:\n%s", view)
	}
}

// TestHookScreen_EmptyBundleList_EnterAdvances verifies that Enter advances past the empty
// bundle screen.
func TestHookScreen_EmptyBundleList_EnterAdvances(t *testing.T) {
	s := newHookScreen(nil, true)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter on empty-bundle screen; want true")
	}
}

// ---------------------------------------------------------------------------
// Error state
// ---------------------------------------------------------------------------

// TestHookScreen_MultiSelect_MultipleBundles verifies that multiple hook bundles can be
// selected in a single session and all appear in SelectedIDs().
func TestHookScreen_MultiSelect_MultipleBundles(t *testing.T) {
	bundles := []domain.HookBundle{
		{Key: "subagent-logger", Description: "Logs subagent interactions.", Version: "1.0"},
		{Key: "cost-tracker", Description: "Tracks token costs.", Version: "1.0"},
	}
	s := newHookScreen(bundles, true)

	// Select first bundle.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	// Navigate to second bundle and select it.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

	ids := s.SelectedIDs()
	if !containsStr(ids, "subagent-logger") {
		t.Errorf("SelectedIDs() = %v; want 'subagent-logger'", ids)
	}
	if !containsStr(ids, "cost-tracker") {
		t.Errorf("SelectedIDs() = %v; want 'cost-tracker'", ids)
	}
}

// TestHookScreen_ErrorState_SelectedIDs_IsEmpty verifies that SelectedIDs() returns an
// empty slice when the screen is in the error state, regardless of what bundles were passed.
func TestHookScreen_ErrorState_SelectedIDs_IsEmpty(t *testing.T) {
	s := screens.NewHookScreen(hookBundles, true, 80, 24, plainStyles(), "catalog error")

	ids := s.SelectedIDs()
	if len(ids) != 0 {
		t.Errorf("SelectedIDs() = %v in error state; want empty slice", ids)
	}
}

// ---------------------------------------------------------------------------
// Error state
// ---------------------------------------------------------------------------

// TestHookScreen_ErrorState_RendersMessage verifies that catalog errors are surfaced.
func TestHookScreen_ErrorState_RendersMessage(t *testing.T) {
	s := screens.NewHookScreen(hookBundles, true, 80, 24, plainStyles(), "catalog error: hook file missing")

	view := s.View()

	if !strings.Contains(view, "catalog error: hook file missing") {
		t.Errorf("error-state view must contain the error message; got:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestHookScreen_Reset_ClearsDoneAndBack verifies that Reset restores idle state.
func TestHookScreen_Reset_ClearsDoneAndBack(t *testing.T) {
	s := newHookScreen(hookBundles, true)
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after Enter")
	}

	s.Reset()

	if s.Done() {
		t.Error("Done() = true after Reset; want false")
	}
	if s.Back() {
		t.Error("Back() = true after Reset; want false")
	}
}

// ---------------------------------------------------------------------------
// Keyboard-only operability
// ---------------------------------------------------------------------------

// TestHookScreen_KeyboardOnly_FullSelectionFlow verifies that hook selection is completely
// operable by keyboard alone.
func TestHookScreen_KeyboardOnly_FullSelectionFlow(t *testing.T) {
	s := newHookScreen(hookBundles, true)

	// Toggle and confirm using only keyboard.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})  // select
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})                       // confirm

	if !s.Done() {
		t.Error("Done() = false after keyboard-only hook selection; want true")
	}
	ids := s.SelectedIDs()
	if !containsStr(ids, "subagent-logger") {
		t.Errorf("SelectedIDs() = %v; want 'subagent-logger' after keyboard-only flow", ids)
	}
}

// TestHookScreen_KeyboardOnly_VimKeysWork verifies vim-style navigation keys work on the
// hook screen.
func TestHookScreen_KeyboardOnly_VimKeysWork(t *testing.T) {
	bundles := []domain.HookBundle{
		{Key: "hook-a", Description: "First hook.", Version: "1.0"},
		{Key: "hook-b", Description: "Second hook.", Version: "1.0"},
	}
	s := newHookScreen(bundles, true)

	// Navigate to second bundle using 'j', then select it.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	ids := s.SelectedIDs()
	if !containsStr(ids, "hook-b") {
		t.Errorf("SelectedIDs() = %v; 'j' key must navigate to 'hook-b'; want it selected", ids)
	}
	if containsStr(ids, "hook-a") {
		t.Errorf("SelectedIDs() = %v; 'hook-a' must not be selected", ids)
	}
}
