package tui

// seed_navigation_test.go verifies the navigation contract for the seed-input
// screen:
//   - Task -> seed screen on forward navigation when isNewRun == true
//   - Esc from seed screen returns to task screen
//   - Seed screen -> config on Enter
//   - Esc from config first prompt returns to seed screen when isNewRun == true
//   - Seed screen is not shown at all when isNewRun == false (the resumed run's
//     forward and backward paths past it are covered in resume_skip_test.go)
//   - Re-entry of the seed screen works (Reset obligation)
//   - selections.seedInput is captured on seed-screen confirmation
//   - The full new-run forward-navigation sequence reaches the progress screen

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens/setup"
)

// ---------------------------------------------------------------------------
// T1.2 — Navigation: new-run path (isNewRun == true)
// ---------------------------------------------------------------------------

// TestNavigation_SeedInputScreen_NewRun_TaskEnterGoesToSeedScreen verifies that
// confirming the task screen transitions to the seed-input screen when isNewRun is true.
func TestNavigation_SeedInputScreen_NewRun_TaskEnterGoesToSeedScreen(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupTask

	// Type one character to satisfy the non-empty task validator, then confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenSetupSeedInput {
		t.Errorf("screen = %v after task Enter (new run), want screenSetupSeedInput (%v)",
			m.screen, screenSetupSeedInput)
	}
}

// TestNavigation_SeedInputScreen_NewRun_EscFromSeedReturnsToTask verifies that
// pressing Esc on the seed-input screen returns to the task screen.
func TestNavigation_SeedInputScreen_NewRun_EscFromSeedReturnsToTask(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupSeedInput

	sendKey(m, tea.KeyEsc)

	if m.screen != screenSetupTask {
		t.Errorf("screen = %v after Esc from seed screen, want screenSetupTask (%v)",
			m.screen, screenSetupTask)
	}
}

// TestNavigation_SeedInputScreen_NewRun_EnterFromSeedGoesToConfig verifies that
// confirming the seed-input screen (blank is a legal confirmation) transitions to config.
func TestNavigation_SeedInputScreen_NewRun_EnterFromSeedGoesToConfig(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupSeedInput

	// Press Enter without typing — blank is a legal confirmation (field is optional).
	sendKey(m, tea.KeyEnter)

	if m.screen != screenSetupConfig {
		t.Errorf("screen = %v after seed Enter, want screenSetupConfig (%v)",
			m.screen, screenSetupConfig)
	}
}

// TestNavigation_ConfigScreen_NewRun_EscReturnsToSeedScreen verifies that pressing
// Esc on the first config prompt transitions back to the seed-input screen
// when isNewRun is true (not directly to the task screen).
func TestNavigation_ConfigScreen_NewRun_EscReturnsToSeedScreen(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupConfig

	sendKey(m, tea.KeyEsc)

	if m.screen != screenSetupSeedInput {
		t.Errorf("screen = %v after Esc from config (new run), want screenSetupSeedInput (%v)",
			m.screen, screenSetupSeedInput)
	}
}

// ---------------------------------------------------------------------------
// T1.2 — Navigation: resume-run path (isNewRun == false)
// ---------------------------------------------------------------------------

// The resume path no longer passes through the task screen in either direction:
// a resumed run is asked neither which workflow to run nor what the task is, so
// there is no task screen for it to advance from or step back to. Both
// directions are covered against the resume path's real shape in
// resume_skip_test.go -- TestSetupFlow_ResumedRun_ReachesConfigurationDirectly
// forward, TestSetupFlow_ResumedRun_BackFromConfiguration_ReturnsToTheHarnessQuestion
// backward. What survives here is the statement those tests do not make: the
// seed screen is not a step on the resume path either.

// TestNavigation_SeedInputScreen_Resume_IsNotShown verifies that a resumed run
// reaching configuration never stops at the seed-input screen.
//
// Seed inputs seed a run at its creation. A resumed run was seeded when it was
// created, and offering the screen again invites input that has nowhere to go.
func TestNavigation_SeedInputScreen_Resume_IsNotShown(t *testing.T) {
	m := newResumedRunModel(t)

	sendKey(m, tea.KeyEnter) // answer the harness question

	if m.screen == screenSetupSeedInput {
		t.Errorf("the setup sequence stopped on the seed-input screen for a resumed run; " +
			"seed inputs belong to a run's creation and cannot be supplied again on resume")
	}
	if m.selections.seedInput != "" {
		t.Errorf("selections.seedInput = %q for a resumed run, want empty",
			m.selections.seedInput)
	}
}

// ---------------------------------------------------------------------------
// T1.2 — Re-entry: Reset obligation
// ---------------------------------------------------------------------------

// TestNavigation_SeedInputScreen_ReEntry_TaskToSeedToTaskToSeed verifies that the
// seed screen can be entered a second time without auto-advancing due to a stale Done
// or Back flag from the previous visit.
//
// Drive: task -> seed (Enter on task) -> task (Esc on seed) -> seed (Enter on task again).
// After the second entry the model must still be on the seed screen, not auto-advanced
// to config because of a stale Done flag that was not cleared by Reset.
func TestNavigation_SeedInputScreen_ReEntry_TaskToSeedToTaskToSeed(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupTask

	// First visit: Enter on task -> seed.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenSetupSeedInput {
		t.Fatalf("precondition: after first task Enter, screen = %v, want screenSetupSeedInput",
			m.screen)
	}

	// Esc from seed -> task.
	sendKey(m, tea.KeyEsc)
	if m.screen != screenSetupTask {
		t.Fatalf("precondition: after Esc from seed, screen = %v, want screenSetupTask", m.screen)
	}

	// Second visit: Enter on task again -> seed again.
	// Reset the taskScreen so it accepts fresh input.
	m.taskScreen.Reset()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenSetupSeedInput {
		t.Errorf("screen = %v after second task Enter, want screenSetupSeedInput (%v) — "+
			"seed screen must not auto-advance on re-entry; Reset obligation may be violated",
			m.screen, screenSetupSeedInput)
	}
}

// TestNavigation_SeedInputScreen_ReEntry_ConfigToSeedToConfig verifies that the
// seed screen, after being re-entered from the config's Esc path, does not
// immediately auto-return to task because of a stale Back flag.
//
// Drive: seed (Enter, blank) -> config -> seed (Esc from config).
// After the second entry the model must remain on the seed screen waiting for user input,
// not immediately transition back to task.
func TestNavigation_SeedInputScreen_ReEntry_ConfigToSeedToConfig(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupSeedInput

	// Forward: Enter on seed -> config.
	sendKey(m, tea.KeyEnter)
	if m.screen != screenSetupConfig {
		t.Fatalf("precondition: after seed Enter, screen = %v, want screenSetupConfig", m.screen)
	}

	// Esc from config -> back to seed.
	sendKey(m, tea.KeyEsc)

	// The model must be on the seed screen, not auto-returned to task
	// because of a stale Back flag that was not cleared by Reset.
	if m.screen != screenSetupSeedInput {
		t.Errorf("screen = %v after Esc from config, want screenSetupSeedInput (%v) — "+
			"seed screen must not auto-return on re-entry; Reset obligation may be violated",
			m.screen, screenSetupSeedInput)
	}
}

// ---------------------------------------------------------------------------
// T1.2 — selections.seedInput is captured on seed screen confirmation
// ---------------------------------------------------------------------------

// TestNavigation_SeedInputScreen_EnteredPathCapturedInSelections verifies that the
// value typed on the seed screen is stored in m.selections.seedInput when confirmed,
// so that startSession() can map it into domain.RunConfig.SeedInputs.
func TestNavigation_SeedInputScreen_EnteredPathCapturedInSelections(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupSeedInput

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/path/to/seed")})
	sendKey(m, tea.KeyEnter)

	if m.screen != screenSetupConfig {
		t.Fatalf("precondition: screen = %v after seed Enter, want screenSetupConfig", m.screen)
	}
	if m.selections.seedInput != "/path/to/seed" {
		t.Errorf("selections.seedInput = %q, want %q", m.selections.seedInput, "/path/to/seed")
	}
}

// TestNavigation_SeedInputScreen_BlankCapturedAsEmpty verifies that a blank
// confirmation stores an empty string in selections.seedInput.
func TestNavigation_SeedInputScreen_BlankCapturedAsEmpty(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupSeedInput

	sendKey(m, tea.KeyEnter)

	if m.screen != screenSetupConfig {
		t.Fatalf("precondition: screen = %v after blank seed Enter, want screenSetupConfig", m.screen)
	}
	if m.selections.seedInput != "" {
		t.Errorf("selections.seedInput = %q after blank confirmation, want empty string",
			m.selections.seedInput)
	}
}

// ---------------------------------------------------------------------------
// T1.2 — Full new-run forward navigation sequence
// ---------------------------------------------------------------------------

// TestSetupSequence_NewRun_ForwardNavigation_ReachesProgressScreen verifies the
// complete forward-navigation path for a new run:
// workflow -> task -> seed input -> config -> progress.
// This is the new-run counterpart of TestSetupSequence_ResumedRun_ForwardNavigation_ReachesProgressScreen
// in nav_setup_test.go (which tests the resumed-run path where the seed screen is skipped).
func TestSetupSequence_NewRun_ForwardNavigation_ReachesProgressScreen(t *testing.T) {
	m := newTestModelNewRun()
	style := stylesFromTheme(m.theme)

	// Bypass file selection: directly populate workflow regions and jump to the workflow
	// screen, replicating what updateSetupFile does after loading.
	testWorkflows := []domain.WorkflowRegion{
		{Info: domain.WorkflowInfo{ID: "test-workflow"}},
	}
	m.workflows = testWorkflows
	m.workflowScreen = setup.NewWorkflowSelectScreen(testWorkflows, m.width, m.height, style)
	m.screen = screenSetupWorkflow

	// Select the only workflow.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenSetupTask {
		t.Fatalf("after workflow selection: screen = %v, want screenSetupTask", m.screen)
	}

	// Type one character to satisfy the non-empty task validator, then confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenSetupSeedInput {
		t.Fatalf("after task entry (new run): screen = %v, want screenSetupSeedInput", m.screen)
	}

	// Accept the seed screen with a blank entry (field is optional).
	sendKey(m, tea.KeyEnter)
	if m.screen != screenSetupConfig {
		t.Fatalf("after seed entry: screen = %v, want screenSetupConfig", m.screen)
	}

	// Accept all configuration prompts. Mode is now the first step and requires an
	// explicit cursor movement before Enter (no preselection per AC8.3). The timeout
	// step is always present now that the fake harness has been removed. The always-shown
	// manual-resolution step follows checkpoints.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})  // move cursor to first mode option (orchestrated)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm mode
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → must advance to timeout

	// Verify we are at the timeout step (not version drift) before supplying the value.
	// This assertion fails until the fake-harness option is removed and advance() always
	// proceeds to configStepHarnessTimeout — mirroring the check in
	// TestSetupSequence_ResumedRun_ForwardNavigation_ReachesProgressScreen.
	configView := m.configScreen.View()
	if !containsAny(configView, "timeout", "Timeout", "Invocation", "invocation") {
		t.Fatalf("after harness Enter, expected invocation-timeout step; "+
			"fake harness may still be skipping timeout:\n%s", configView)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // timeout → version drift
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // version drift
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // checkpoints
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // manual-resolution (always shown; accept default disabled)

	if m.screen != screenProgress {
		t.Errorf("after config completion (new run): screen = %v, want screenProgress", m.screen)
	}
	if m.progressScreen == nil {
		t.Error("progressScreen = nil after reaching progress screen; must be constructed")
	}
	if m.selections.task != "T" {
		t.Errorf("selections.task = %q, want %q", m.selections.task, "T")
	}
}
