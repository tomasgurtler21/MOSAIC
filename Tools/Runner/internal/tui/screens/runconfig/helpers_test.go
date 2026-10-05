package runconfig

// Shared helpers for the screen tests: temp-file creation and ConfigScreen driving.

import (
	"os"
	"strings"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/tui/screens"
)

// newTestTempFile creates a temporary directory and a file named "orch.md" inside it.
// It returns the absolute path to the file. The directory is cleaned up automatically
// by the test runner via t.Cleanup.
func newTestTempFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "orch.md")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("setup: could not create temp file: %v", err)
	}
	f.Close()
	return path
}

// pressKey sends a single key press to the screen's Update method.
func pressKey(s *ConfigScreen, keyType tea.KeyType) tea.Cmd {
	return s.Update(tea.KeyMsg{Type: keyType})
}

// newConfigScreenAtHarnessStep creates a ConfigScreen positioned at the
// harness step by driving past the mode step (which is now the first step).
// It selects the first mode option (orchestrated) so harness is the current step.
func newConfigScreenAtHarnessStep() *ConfigScreen {
	s := NewConfigScreen(80, 24, screens.Styles{})
	driveConfigScreenModeSelect(s, 0) // advance past mode step; harness is now current
	return s
}

// advanceConfigScreenTimeout types a valid timeout ("30m") into the ConfigScreen
// and confirms it. Must be called when the screen is at configStepHarnessTimeout.
func advanceConfigScreenTimeout(s *ConfigScreen) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	pressKey(s, tea.KeyEnter)
}

// driveConfigScreenPastPreModeSteps is a no-op in Stage 8: in GREEN the mode
// step is the first step and there are no steps before it. It exists here so
// that downstream helpers can insert steps before mode if a future stage adds
// them without breaking these tests.
func driveConfigScreenPastPreModeSteps(_ *ConfigScreen) {}

// driveConfigScreenModeSelect drives the mode step by moving the cursor to the
// given index (0=orchestrated, 1=auto, 2=auto-review) and pressing Enter.
// The mode step starts with no option selected (AC8.3): Enter alone is rejected.
// One Down press moves the cursor to the first option (orchestrated), so idx=0
// requires one Down press, idx=1 requires two, and so on.
func driveConfigScreenModeSelect(s *ConfigScreen, idx int) {
	// The mode step starts with no item selected. One Down press is needed
	// to reach the first item (idx 0); each additional Down moves one further.
	for i := 0; i <= idx; i++ {
		pressKey(s, tea.KeyDown)
	}
	pressKey(s, tea.KeyEnter)
}

// driveConfigScreenPastModeAndHarness drives through mode selection, harness,
// and the timeout step to leave the screen at configStepVersionDrift.
// The mode option at index 0 (orchestrated) is selected.
func driveConfigScreenPastModeAndHarness(s *ConfigScreen) {
	driveConfigScreenModeSelect(s, 0) // select first mode (orchestrated)
	pressKey(s, tea.KeyEnter)         // harness
	advanceConfigScreenTimeout(s)     // timeout → version drift
}

// harness, timeout, version drift, and checkpoints so that the next step is
// configStepPreConsult. Callers must pass a screen with no declared agents (or
// with a commit agent already handled) and assert s.step == configStepPreConsult
// afterwards if the precondition matters for their scenario.
func driveConfigScreenToPreConsultStep(s *ConfigScreen) {
	driveConfigScreenModeSelect(s, 1) // auto — pre-consult is shown for this mode
	pressKey(s, tea.KeyEnter)         // harness
	advanceConfigScreenTimeout(s)     // timeout
	pressKey(s, tea.KeyEnter)         // version drift
	pressKey(s, tea.KeyEnter)         // checkpoints (no commit agent → lands on pre-consult)
}

// containsSubstr is a package-level helper for string containment checks in
// Stage 8 tests, following the same pattern as the tui-level containsStr.
func containsSubstr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// driveConfigScreenVersionDriftChoice selects an option on the version-drift
// step and confirms it. idx 0 is "Yes" (allow drift), idx 1 is "No" (refuse).
// The step is entered with the cursor at 0, so idx 0 needs no movement.
func driveConfigScreenVersionDriftChoice(s *ConfigScreen, idx int) {
	for i := 0; i < idx; i++ {
		pressKey(s, tea.KeyDown)
	}
	pressKey(s, tea.KeyEnter)
}

// acceptReviewLoopLimitIfAsked accepts the suggested review loop limit when the
// wizard is showing that prompt (new runs ask it as the last prompt), so flows
// driven to completion do not depend on it.
func acceptReviewLoopLimitIfAsked(s *ConfigScreen) {
	if !s.Done() && containsSubstr(strings.ToLower(s.View()), "review loop limit") {
		pressKey(s, tea.KeyEnter)
	}
}
