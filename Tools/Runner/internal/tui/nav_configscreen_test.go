package tui

// nav_configscreen_test.go verifies the ConfigScreen's step sequence: the
// six-prompt orchestrated-mode flow, the harness step's real (non-fake)
// options, and Esc backward navigation across the harness/timeout/version-drift
// steps.
//
// Tests are in package tui (internal) because rootModel and screen constants are
// unexported. Tests drive the model, and the ConfigScreen directly, through the
// Bubble Tea update cycle with no real terminal attached.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/tui/screens/runconfig"
)

// ---------------------------------------------------------------------------
// ConfigScreen: ExistingArtifact step removed; fake harness removed
// ---------------------------------------------------------------------------

// TestConfigScreen_PromptsCount verifies that the ConfigScreen presents exactly six
// prompts in orchestrated mode: execution mode, harness selection, invocation timeout,
// version drift, checkpoints, and manual resolution.  The timeout step is always
// present now that the fake harness has been removed from the harness step.
func TestConfigScreen_PromptsCount(t *testing.T) {
	m := newTestModel()
	m.screen = screenSetupConfig

	// Step 1: select execution mode. The mode step requires an explicit choice;
	// pressing Enter with no cursor movement is rejected (no preselection).
	// Press Down to move the cursor to the first option (orchestrated), then Enter to confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})  // move cursor to orchestrated (first option)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select mode → advance to harness
	// Step 2: accept harness selection — must advance to the timeout step, not version drift.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// After deviation + harness Enters, the config screen must be showing the
	// invocation-timeout entry step.  This assertion fails until the fake-harness
	// option is removed and advance() always proceeds to configStepHarnessTimeout.
	configView := m.configScreen.View()
	if !containsAny(configView, "timeout", "Timeout", "Invocation", "invocation") {
		t.Fatalf("after deviation+harness Enters, expected invocation-timeout step; "+
			"fake harness may still be skipping timeout:\n%s", configView)
	}

	// Step 3: enter a valid invocation timeout and confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Steps 4–6: version drift, checkpoints, and manual resolution.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // version drift
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // checkpoints → manual-resolution
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // manual-resolution: disabled (default, cursor 0) → progress

	if m.screen != screenProgress {
		t.Errorf("screen = %v after six config steps, want screenProgress (%v)", m.screen, screenProgress)
	}
}

// ---------------------------------------------------------------------------
// ConfigScreen harness step: single option, no fake/scripted text (T2.1, T2.2)
// ---------------------------------------------------------------------------

// TestConfigScreen_HarnessStep_NoFakeOrScriptedText verifies that the harness
// step's rendered output contains no reference to the fake or scripted harness.
func TestConfigScreen_HarnessStep_NoFakeOrScriptedText(t *testing.T) {
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := runconfig.NewConfigScreen(80, 24, style)

	// Harness is now the first step; no advance needed.
	view := s.View()
	if containsAny(view, "fake", "Fake", "scripted", "Scripted") {
		t.Errorf("harness step view contains fake/scripted harness reference; want no such mention:\n%s", view)
	}
}

// TestConfigScreen_HarnessStep_ContainsRealHarnessOption verifies that the harness
// step shows the real harness option.
func TestConfigScreen_HarnessStep_ContainsRealHarnessOption(t *testing.T) {
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := runconfig.NewConfigScreen(80, 24, style)

	// Mode is the first step; advance past it by selecting orchestrated (Down + Enter).
	s.Update(tea.KeyMsg{Type: tea.KeyDown})  // move cursor to first mode option
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm mode selection
	view := s.View()
	// In pre-I8.1 RED state the mode step is not first: Down moved the harness cursor
	// to position 1 ("opencode") and Enter confirmed it, advancing to the timeout step.
	// The view therefore shows the timeout prompt, not harness labels. The correct RED
	// failure for this test is "harness renders a single hardcoded option, not
	// CLISelections() labels" (I4.6 missing) — but the test never reaches the harness
	// step. Skip until I8.1 wires mode as the first step.
	if containsAny(view, "timeout", "Timeout", "Invocation", "invocation") {
		t.Fatalf("I8.1 not yet implemented: mode step not first; Down+Enter advances harness step instead of mode step and lands at timeout. Do not ship I4.6 verification until I8.1 is complete.")
	}
	if !containsAny(view, "Claude Code CLI", "Claude Code", "claude-code") {
		t.Errorf("harness step view does not contain real harness option:\n%s", view)
	}
}

// TestConfigScreen_HarnessStep_CursorMovesBetweenAcceptedHarnesses verifies that
// pressing Down on the harness step moves the cursor between the accepted
// harnesses. The step now renders one option per accepted harness (AC4.6)
// rather than a single pinned option, so with more than one accepted harness
// the cursor — and the rendered view — must change.
func TestConfigScreen_HarnessStep_CursorMovesBetweenAcceptedHarnesses(t *testing.T) {
	sels := harness.CLISelections()
	if len(sels) < 2 {
		t.Fatalf("test fixture assumption violated: want at least 2 CLI-backed harnesses to observe cursor movement, got %d", len(sels))
	}

	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := runconfig.NewConfigScreen(80, 24, style)

	// Harness is now the first step; no advance needed.
	viewBefore := s.View()
	s.Update(tea.KeyMsg{Type: tea.KeyDown}) // move cursor to the next accepted harness
	viewAfter := s.View()

	if viewBefore == viewAfter {
		t.Errorf("pressing Down on the harness step did not change the view; cursor must move between "+
			"the %d accepted harnesses:\n%s", len(sels), viewBefore)
	}
}

// TestConfigScreen_HarnessStep_AdvancesToTimeoutStep verifies that confirming the
// harness step transitions to the invocation-timeout entry step.
func TestConfigScreen_HarnessStep_AdvancesToTimeoutStep(t *testing.T) {
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := runconfig.NewConfigScreen(80, 24, style)

	s.Update(tea.KeyMsg{Type: tea.KeyDown})  // mode: move cursor to first option (orchestrated)
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode: confirm selection
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → must go to timeout

	view := s.View()
	if !containsAny(view, "timeout", "Timeout", "Invocation", "invocation") {
		t.Errorf("after confirming harness step, expected invocation-timeout step; "+
			"fake harness may still be routing to version drift instead:\n%s", view)
	}
	if containsAny(view, "version drift", "Version drift", "drift") {
		t.Errorf("after confirming harness step, view shows version-drift content instead of timeout step:\n%s", view)
	}
}

// TestConfigScreen_HarnessStep_SelectionIsAlwaysClaudeCode verifies that after
// driving through the full config screen, Selection().Harness is "claude-code".
func TestConfigScreen_HarnessStep_SelectionIsAlwaysClaudeCode(t *testing.T) {
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := runconfig.NewConfigScreen(80, 24, style)

	s.Update(tea.KeyMsg{Type: tea.KeyDown})  // mode: move cursor to first option (orchestrated)
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode: confirm selection
	// In pre-I8.1 RED state the mode step is not first: Down moved the harness cursor to
	// position 1 ("opencode") and Enter confirmed it, advancing to the timeout step.
	// All subsequent interactions drive the remaining steps and the wizard completes with
	// sel.Harness = "opencode" — which makes the test fail for I8.1 reasons (wrong harness
	// selected accidentally) rather than I4.6 reasons (harness step always assigns
	// "claude-code" regardless of cursor). Skip until I8.1 wires mode as the first step.
	{
		view := s.View()
		if containsAny(view, "timeout", "Timeout", "Invocation", "invocation") {
			t.Fatalf("I8.1 not yet implemented: mode step not first; Down+Enter advances harness step instead of mode step and lands at timeout. Do not ship I4.6 verification until I8.1 is complete.")
		}
	}
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → timeout
	// Confirm a valid invocation timeout.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // timeout → version drift
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // version drift → checkpoints
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // checkpoints → manual resolution
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // manual resolution → done

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving through all steps; cannot verify Selection()")
	}
	sel := s.Selection()
	if sel.Harness != "claude-code" {
		t.Errorf("Selection().Harness = %q, want %q; the harness step must always produce claude-code",
			sel.Harness, "claude-code")
	}
}

// ---------------------------------------------------------------------------
// ConfigScreen Esc backward navigation across harness/timeout/version-drift (T2.4)
// ---------------------------------------------------------------------------

// TestConfigScreen_EscBackNavigation_VersionDriftToTimeoutStep verifies that pressing
// Esc from the version-drift step returns to the invocation-timeout step, not the
// harness step.  This exercises the removal of the special-case Esc branch that
// existed only because the timeout step was skipped for the fake harness.
func TestConfigScreen_EscBackNavigation_VersionDriftToTimeoutStep(t *testing.T) {
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := runconfig.NewConfigScreen(80, 24, style)

	// Mode is the first step; advance past it before reaching harness.
	s.Update(tea.KeyMsg{Type: tea.KeyDown})  // mode: move cursor to first option (orchestrated)
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode: confirm selection
	// Confirm harness — must go to timeout step, not version drift.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := s.View()
	if !containsAny(view, "timeout", "Timeout", "Invocation", "invocation") {
		t.Fatalf("precondition: after harness Enter, expected timeout step; "+
			"fake harness skipping may still be present:\n%s", view)
	}

	// Type a valid timeout and advance to version drift.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // timeout → version drift

	// Esc from version drift must return to the timeout step, not harness.
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	view = s.View()
	if !containsAny(view, "timeout", "Timeout", "Invocation", "invocation") {
		t.Errorf("Esc from version-drift did not return to timeout step; "+
			"the special-case harness branch may still be routing back to harness:\n%s", view)
	}
}

// TestConfigScreen_EscBackNavigation_TimeoutToHarnessStep verifies that pressing
// Esc from the invocation-timeout step returns to the harness step.
func TestConfigScreen_EscBackNavigation_TimeoutToHarnessStep(t *testing.T) {
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := runconfig.NewConfigScreen(80, 24, style)

	s.Update(tea.KeyMsg{Type: tea.KeyDown})  // mode: move cursor to first option (orchestrated)
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode: confirm selection
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → timeout

	view := s.View()
	if !containsAny(view, "timeout", "Timeout", "Invocation", "invocation") {
		t.Fatalf("precondition: expected timeout step after harness Enter; view:\n%s", view)
	}

	// Esc from timeout → harness.
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	view = s.View()
	if !containsAny(view, "Harness", "harness", "adapter", "Adapter") {
		t.Errorf("Esc from timeout step did not return to harness step; view:\n%s", view)
	}
	if s.Back() {
		t.Error("Back() = true after Esc from timeout to harness; must not exit the config screen")
	}
}
