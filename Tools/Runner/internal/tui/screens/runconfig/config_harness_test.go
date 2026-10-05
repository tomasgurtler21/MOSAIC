package runconfig

// config_harness_test.go verifies the ConfigScreen harness selection step.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/harness"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// ConfigScreen — harness step (T4.5)
//
// These tests exercise ConfigScreen's harness configuration step, which
// currently renders exactly one hard-coded option ("Claude Code CLI") and
// assigns the literal "claude-code" regardless of cursor position
// (ConfigSelection.Harness's own doc comment states as much). They are RED
// until the step is reworked (I4.6) to enumerate harness.CLISelections() and
// assign the identity the cursor actually rests on.
//
// Tests are in package screens (not screens_test) so they can inspect
// ConfigScreen's unexported step/cursor fields directly, the same access
// pattern validateOrchestratorFile testing above relies on.
// ---------------------------------------------------------------------------

// TestConfigScreen_HarnessStep_OffersOneOptionPerAcceptedHarness verifies
// that the rendered view lists exactly one option per
// harness.CLISelections() entry, using each entry's Label — not a single
// hard-coded "Claude Code CLI" line.
func TestConfigScreen_HarnessStep_OffersOneOptionPerAcceptedHarness(t *testing.T) {
	s := newConfigScreenAtHarnessStep()
	// In pre-I8.1 RED state the mode step is not first, so driveConfigScreenModeSelect
	// inside the helper accidentally advances the harness step (selecting the second
	// harness option) and lands at the timeout step. Skip until I8.1 makes mode first.
	if s.step != configStepHarness {
		t.Fatalf("I8.1 not yet implemented: mode step not first; harness helper positions at timeout step, not harness step. Do not ship I4.6 verification until I8.1 is complete.")
	}
	view := s.View()

	sels := harness.CLISelections()
	if len(sels) < 2 {
		t.Fatalf("test fixture assumption violated: want at least 2 CLI-backed selections to distinguish from the old single-option render, got %d", len(sels))
	}
	for _, sel := range sels {
		if !strings.Contains(view, sel.Label) {
			t.Errorf("harness step view does not contain label %q for accepted harness %q; view:\n%s", sel.Label, sel.ID, view)
		}
	}
}

// TestConfigScreen_HarnessStep_SelectionAssignsCursorIdentity verifies that
// pressing Enter on the harness step assigns the identity the cursor
// actually rests on (harness.CLISelections()[cursor].ID), not a fixed
// literal. It moves the cursor down once, so unless there are at least 2
// CLI-backed selections, cursor movement would be clamped and this test
// would not distinguish the fixed-literal bug from a correct implementation.
func TestConfigScreen_HarnessStep_SelectionAssignsCursorIdentity(t *testing.T) {
	sels := harness.CLISelections()
	if len(sels) < 2 {
		t.Fatalf("test fixture assumption violated: want at least 2 CLI-backed selections, got %d", len(sels))
	}

	s := newConfigScreenAtHarnessStep()
	// In pre-I8.1 RED state the mode step is not first, so driveConfigScreenModeSelect
	// inside the helper accidentally selects a second harness option (position 1) and
	// lands at the timeout step. Without this guard the test would pass coincidentally
	// (the accidentally-selected harness ID matches sels[1].ID), masking the missing
	// I4.6 implementation. Skip until I8.1 makes mode the first step.
	if s.step != configStepHarness {
		t.Fatalf("I8.1 not yet implemented: mode step not first; harness helper positions at timeout step; this test cannot be verified until the mode step is the first step. Do not ship I4.6 verification until I8.1 is complete.")
	}
	pressKey(s, tea.KeyDown) // move cursor to the second entry
	pressKey(s, tea.KeyEnter)

	want := sels[1].ID
	if s.sel.Harness != want {
		t.Errorf("sel.Harness = %q, want %q (the identity the cursor rested on, not a fixed literal)", s.sel.Harness, want)
	}
}

// TestConfigScreen_HarnessStep_FirstOptionSelection verifies the cursor-0
// case explicitly: selecting without moving the cursor assigns
// CLISelections()[0].ID.
func TestConfigScreen_HarnessStep_FirstOptionSelection(t *testing.T) {
	sels := harness.CLISelections()
	if len(sels) == 0 {
		t.Fatalf("test fixture assumption violated: want at least 1 CLI-backed selection, got 0")
	}

	s := newConfigScreenAtHarnessStep()
	// In pre-I8.1 RED state the helper positions at the timeout step, not the harness
	// step; pressing Enter would advance the timeout step, not confirm a harness option.
	// Skip until I8.1 makes mode the first step.
	if s.step != configStepHarness {
		t.Fatalf("I8.1 not yet implemented: mode step not first; harness helper positions at timeout step, not harness step. Do not ship I4.6 verification until I8.1 is complete.")
	}
	pressKey(s, tea.KeyEnter)

	want := sels[0].ID
	if s.sel.Harness != want {
		t.Errorf("sel.Harness = %q, want %q", s.sel.Harness, want)
	}
}

// TestConfigScreen_HarnessStep_CursorBoundsMatchAcceptedSetLength verifies
// that cursor movement is bounded by the number of accepted CLI-backed
// harnesses rather than a hard-coded single option: pressing down
// (len(sels)-1) times should reach the last entry, and one more press must
// not move the cursor further.
func TestConfigScreen_HarnessStep_CursorBoundsMatchAcceptedSetLength(t *testing.T) {
	sels := harness.CLISelections()
	if len(sels) < 2 {
		t.Fatalf("test fixture assumption violated: want at least 2 CLI-backed selections, got %d", len(sels))
	}

	s := newConfigScreenAtHarnessStep()
	// In pre-I8.1 RED state the helper positions at the timeout step, not the harness
	// step. Skip until I8.1 makes mode the first step.
	if s.step != configStepHarness {
		t.Fatalf("I8.1 not yet implemented: mode step not first; harness helper positions at timeout step, not harness step. Do not ship I4.6 verification until I8.1 is complete.")
	}
	for i := 0; i < len(sels)+2; i++ { // deliberately over-press
		pressKey(s, tea.KeyDown)
	}
	pressKey(s, tea.KeyEnter)

	want := sels[len(sels)-1].ID
	if s.sel.Harness != want {
		t.Errorf("sel.Harness = %q after over-pressing down, want %q (cursor must clamp at the last accepted entry)", s.sel.Harness, want)
	}
}

// TestConfigScreen_HarnessStep_EscSetsBack verifies that pressing Esc on the
// mode step (the first step after I8.1 reorders the wizard) sets the screen's
// Back flag, signalling the caller to navigate away from the configuration
// screen entirely. The mode step replaces harness as the entry point.
func TestConfigScreen_HarnessStep_EscSetsBack(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	// Mode step is the first step; no advance needed.
	pressKey(s, tea.KeyEsc)

	if !s.Back() {
		t.Error("Back() = false; Esc on the mode step (the first step) must set the screen's Back flag")
	}
}

// TestConfigScreen_HarnessStep_EnterAdvancesToTimeoutStep verifies that
// pressing Enter on the harness step still advances to the harness-timeout
// step, preserving navigation behaviour once the step enumerates multiple
// options.
func TestConfigScreen_HarnessStep_EnterAdvancesToTimeoutStep(t *testing.T) {
	s := newConfigScreenAtHarnessStep()
	pressKey(s, tea.KeyEnter)

	if s.step != configStepHarnessTimeout {
		t.Errorf("step = %v after Enter on harness step, want configStepHarnessTimeout", s.step)
	}
}

