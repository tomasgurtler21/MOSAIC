package runconfig

// config_preconsult_test.go verifies the ConfigScreen pre-consultation step and its navigation.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Stage 8 — T8.2: Backward navigation through conditional steps
//
// Conditional steps must be skipped in BOTH navigation directions. When a step
// is not applicable, pressing Esc from the step after it must jump over it and
// land on the step before it.
// ---------------------------------------------------------------------------

// TestConfigScreen_BackNav_CommitBranchSkipped_WhenCommitsNotApplicable verifies
// that pressing Esc from the manual-resolution step lands on the pre-consult
// step (or checkpoints, if pre-consult is also not applicable) when no commit
// agent is declared — commits and commit-branch steps are skipped in both
// navigation directions.
//
// RED: configStepManualResolution is not yet wired into advance(); the test
// cannot reach it to press Esc.
func TestConfigScreen_BackNav_CommitBranchSkipped_WhenCommitsNotApplicable(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil) // no commit agent

	// Drive forward: mode (orchestrated), harness, timeout, version drift, checkpoints,
	// manual resolution (if shown). Since no commit agent is declared, commits and
	// commit-branch steps must be skipped. Pre-consult is also skipped (orchestrated mode).
	driveConfigScreenModeSelect(s, 0)
	pressKey(s, tea.KeyEnter) // harness
	advanceConfigScreenTimeout(s)
	pressKey(s, tea.KeyEnter) // version drift
	pressKey(s, tea.KeyEnter) // checkpoints

	// After checkpoints without a commit agent and with orchestrated mode,
	// we should be at configStepManualResolution (always shown).
	if s.step != configStepManualResolution {
		t.Fatalf("step = %v after checkpoints (no commit, orchestrated mode), want configStepManualResolution (%v); "+
			"cannot test backward navigation without reaching that step", s.step, configStepManualResolution)
	}

	// Press Esc from manual-resolution — must go back to checkpoints (commits and
	// commit-branch are skipped, pre-consult is skipped for orchestrated mode).
	pressKey(s, tea.KeyEsc)
	if s.step != configStepCheckpoints {
		t.Errorf("Esc from manual-resolution with no commit agent and orchestrated mode: "+
			"step = %v, want configStepCheckpoints (%v); "+
			"skipped conditional steps must not appear in backward navigation",
			s.step, configStepCheckpoints)
	}
}

// TestConfigScreen_BackNav_PreConsultSkipped_WhenModeOrchestrated verifies that
// pressing Esc from the manual-resolution step skips the pre-consultation step
// (not applicable for orchestrated mode) when going backward.
//
// RED: configStepManualResolution is never reached.
func TestConfigScreen_BackNav_PreConsultSkipped_WhenModeOrchestrated(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)
	pressKey(s, tea.KeyEnter) // version drift
	pressKey(s, tea.KeyEnter) // checkpoints

	if s.step != configStepManualResolution {
		t.Fatalf("step = %v, want configStepManualResolution; cannot test backward skip", s.step)
	}

	// Esc must skip pre-consult (not applicable for orchestrated) and land on checkpoints.
	pressKey(s, tea.KeyEsc)
	if s.step == configStepPreConsult {
		t.Error("Esc from manual-resolution (orchestrated mode) landed on configStepPreConsult; " +
			"the pre-consult step must be skipped in backward navigation when mode is orchestrated")
	}
}

// TestConfigScreen_BackNav_PreConsultStepReachable_WhenModeAuto verifies that
// pressing Esc from the manual-resolution step DOES land on the pre-consult
// step when mode is auto (the step is applicable).
//
// RED: neither manual-resolution nor pre-consult steps are reachable.
func TestConfigScreen_BackNav_PreConsultStepReachable_WhenModeAuto(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 1) // auto
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)
	pressKey(s, tea.KeyEnter) // version drift
	pressKey(s, tea.KeyEnter) // checkpoints

	// Skip to manual-resolution: need to advance through pre-consult.
	if s.step == configStepPreConsult {
		pressKey(s, tea.KeyEnter) // accept pre-consult default (disabled)
	}

	if s.step != configStepManualResolution {
		t.Fatalf("step = %v, want configStepManualResolution; cannot test backward navigation", s.step)
	}

	// Esc must go back to pre-consult (applicable for auto mode).
	pressKey(s, tea.KeyEsc)
	if s.step != configStepPreConsult {
		t.Errorf("Esc from manual-resolution (mode=auto) landed on step %v, want configStepPreConsult (%v); "+
			"Esc must go back to the pre-consult step when mode is auto",
			s.step, configStepPreConsult)
	}
}

// ---------------------------------------------------------------------------
// Pre-consultation step cursor default (TDD RED)
//
// The pre-consultation step must arrive with its cursor on "yes" (cursor index 1)
// on every path that reaches it — forward navigation from checkpoints or
// commit-branch, and backward navigation from manual-resolution. Pressing Enter
// without moving the cursor must therefore yield PreConsultation == true.
//
// Currently the step always arrives with cursor 0 ("no"), so all of the tests
// below are RED until the cursor default is updated in the implementation.
// ---------------------------------------------------------------------------

// driveConfigScreenToPreConsultStep drives a ConfigScreen through mode (auto),
// TestConfigScreen_PreConsultStep_CursorDefaultsToYes_ForwardEntry verifies that
// when the wizard arrives at the pre-consultation step via forward navigation
// (mode=auto, no commit agent), the cursor is pre-set to "yes" (cursor index 1)
// so the user can confirm the default without navigating.
//
// RED: the step currently resets cursor to 0 ("no") on entry from checkpoints.
func TestConfigScreen_PreConsultStep_CursorDefaultsToYes_ForwardEntry(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenToPreConsultStep(s)

	if s.step != configStepPreConsult {
		t.Fatalf("step = %v after driving to pre-consult step, want configStepPreConsult (%v); "+
			"cannot verify cursor without reaching the step", s.step, configStepPreConsult)
	}
	if s.cursor != 1 {
		t.Errorf("cursor = %d on entry to pre-consult step, want 1 (yes); "+
			"the pre-consultation step must pre-select yes so automated runs are safe by default",
			s.cursor)
	}
}

// TestConfigScreen_PreConsultStep_ConfirmWithoutMoving_EnablesPreConsultation verifies
// that pressing Enter without moving the cursor at the pre-consultation step yields
// Settings.PreConsultation == true. This is the critical behaviour: the user must
// not have to navigate to enable pre-consultation — confirming the default suffices.
//
// RED: cursor defaults to 0 ("no"), so confirming without moving sets PreConsultation = false.
func TestConfigScreen_PreConsultStep_ConfirmWithoutMoving_EnablesPreConsultation(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenToPreConsultStep(s)

	if s.step != configStepPreConsult {
		t.Fatalf("step = %v, want configStepPreConsult; cannot test confirm-without-move", s.step)
	}

	// Confirm without any cursor movement — this must commit the pre-selected "yes".
	pressKey(s, tea.KeyEnter)

	if s.step == configStepPreConsult {
		t.Fatal("step did not advance after pressing Enter on the pre-consult step")
	}
	// Drive to completion so we can read the final Settings.
	pressKey(s, tea.KeyEnter) // manual resolution (default disabled)

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving all remaining steps")
	}
	if !s.Selection().Settings.PreConsultation {
		t.Error("Settings.PreConsultation = false after confirming pre-consult step without moving cursor; " +
			"confirming the default must yield PreConsultation = true")
	}
}

// TestConfigScreen_PreConsultStep_CursorDefaultsToYes_BackNavigation verifies that
// when the user presses Esc from the manual-resolution step in auto mode (going
// back to the pre-consultation step), the cursor is again pre-set to "yes"
// (cursor index 1). Back-navigation must not reset the cursor to "no".
//
// RED: prevStepAndCursor returns cursor 0 for every back-navigation path into
// the pre-consult step.
func TestConfigScreen_PreConsultStep_CursorDefaultsToYes_BackNavigation(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenToPreConsultStep(s)

	if s.step != configStepPreConsult {
		t.Fatalf("step = %v, want configStepPreConsult; cannot test back-navigation cursor", s.step)
	}

	// Advance through pre-consult (confirm default) to reach manual-resolution.
	pressKey(s, tea.KeyEnter)

	if s.step != configStepManualResolution {
		t.Fatalf("step = %v after confirming pre-consult, want configStepManualResolution; "+
			"cannot press Esc to go back", s.step)
	}

	// Press Esc to go back to pre-consult.
	pressKey(s, tea.KeyEsc)

	if s.step != configStepPreConsult {
		t.Fatalf("step = %v after Esc from manual-resolution (mode=auto), want configStepPreConsult (%v)",
			s.step, configStepPreConsult)
	}
	if s.cursor != 1 {
		t.Errorf("cursor = %d after back-navigation to pre-consult step, want 1 (yes); "+
			"back-navigation must restore the yes pre-selection, not reset to no",
			s.cursor)
	}
}

// TestConfigScreen_PreConsultStep_CursorDefaultsToYes_ForwardEntryViaCommitBranch verifies
// that when commits are enabled (so the flow goes commits → commit-branch → pre-consult),
// the pre-consultation step still arrives with cursor index 1 ("yes"). This covers the
// commit-branch → pre-consult forward path independently of the checkpoints → pre-consult path.
//
// RED: the commit-branch advance also resets cursor to 0 before moving to the next step.
func TestConfigScreen_PreConsultStep_CursorDefaultsToYes_ForwardEntryViaCommitBranch(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	// Declare a commit agent so the commits step appears.
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 1) // auto
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints
	// Commits step: move cursor to enabled (cursor 1) and confirm.
	pressKey(s, tea.KeyDown)  // cursor → 1 (enabled)
	pressKey(s, tea.KeyEnter) // commit-branch step now
	// Commit-branch step: confirm the default (mosaic-owned).
	pressKey(s, tea.KeyEnter) // → pre-consult step

	if s.step != configStepPreConsult {
		t.Fatalf("step = %v after commit-branch, want configStepPreConsult (%v); "+
			"cannot verify cursor without reaching the step", s.step, configStepPreConsult)
	}
	if s.cursor != 1 {
		t.Errorf("cursor = %d on entry to pre-consult step via commit-branch path, want 1 (yes); "+
			"the yes pre-selection must apply regardless of which forward path reaches the step",
			s.cursor)
	}
}

