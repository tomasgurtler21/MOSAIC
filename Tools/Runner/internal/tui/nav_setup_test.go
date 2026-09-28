package tui

// nav_setup_test.go verifies setup-screen back-navigation (Esc across the
// workflow and task screens) and the resumed-run forward-navigation sequence
// through to the progress screen.
//
// Tests are in package tui (internal) because screenID and rootModel fields are
// unexported. Tests drive the model through the Bubble Tea model/update cycle with no
// real terminal attached.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens/setup"
)

// ---------------------------------------------------------------------------
// Setup screen back-navigation
// ---------------------------------------------------------------------------

// TestNavigation_WorkflowScreen_EscReturnsToHarnessScreen verifies that pressing Esc on the
// workflow selection screen transitions back to the harness selection screen.
//
// This is a new-run test: the workflow screen is only ever reached by a run that
// has no recorded workflow, so a resumed run has no back-navigation out of it.
func TestNavigation_WorkflowScreen_EscReturnsToHarnessScreen(t *testing.T) {
	m := newTestModelNewRun()
	style := stylesFromTheme(m.theme)
	m.workflowScreen = setup.NewWorkflowSelectScreen(
		[]domain.WorkflowRegion{{Info: domain.WorkflowInfo{ID: "wf1"}}},
		m.width, m.height, style,
	)
	m.screen = screenSetupWorkflow

	sendKey(m, tea.KeyEsc)

	if m.screen != screenSetupHarness {
		t.Errorf("screen = %v after Esc from workflow screen, want screenSetupHarness (%v)", m.screen, screenSetupHarness)
	}
}

// TestNavigation_TaskScreen_EscReturnsToWorkflowScreen verifies that pressing Esc on the
// task description screen transitions back to the workflow selection screen.
//
// This is a new-run test: both screens belong to the new-run path, and a resumed
// run is shown neither.
func TestNavigation_TaskScreen_EscReturnsToWorkflowScreen(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupTask

	sendKey(m, tea.KeyEsc)

	if m.screen != screenSetupWorkflow {
		t.Errorf("screen = %v after Esc from task screen, want screenSetupWorkflow (%v)", m.screen, screenSetupWorkflow)
	}
}

// Back-navigation out of the configuration screen is covered per run mode
// elsewhere: TestNavigation_ConfigScreen_NewRun_EscReturnsToSeedScreen in
// seed_navigation_test.go for a new run, and
// TestSetupFlow_ResumedRun_BackFromConfiguration_ReturnsToTheHarnessQuestion in
// resume_skip_test.go for a resumed one. There is no run-mode-independent
// answer to state here: the two paths pass through different screens on the way
// in and must return to different screens on the way out.

// ---------------------------------------------------------------------------
// Setup sequence: forward navigation
// ---------------------------------------------------------------------------

// TestSetupSequence_ResumedRun_ForwardNavigation_ReachesProgressScreen verifies
// the complete forward-navigation path for a resumed run:
// harness → configuration → progress screen. The workflow and task screens are
// not steps on this path; a resumed run brings both values with it.
//
// This is the resumed-run counterpart of
// TestSetupSequence_NewRun_ForwardNavigation_ReachesProgressScreen in
// seed_navigation_test.go, which walks the longer new-run path.
func TestSetupSequence_ResumedRun_ForwardNavigation_ReachesProgressScreen(t *testing.T) {
	m := newResumedRunModel(t)

	// Answer the harness question. A resumed run has nothing further to answer
	// before configuration.
	sendKey(m, tea.KeyEnter)
	if m.screen != screenSetupConfig {
		t.Fatalf("after the harness question on a resumed run: screen = %v, want screenSetupConfig", m.screen)
	}

	// Accept all configuration prompts.  The timeout step is always present
	// now that the fake harness has been removed.
	// The mode step is now first and requires an explicit selection (no preselection).
	// Press Down to move the cursor to the first option (orchestrated), then Enter to confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})  // move cursor to first mode option (orchestrated)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select mode → advance to harness
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → must advance to timeout

	// Verify we are at the timeout step (not version drift) before supplying the value.
	// This assertion fails until the fake-harness option is removed.
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
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // checkpoints → manual-resolution
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // manual-resolution: disabled (default, cursor 0) → screenProgress

	if m.screen != screenProgress {
		t.Errorf("after config completion: screen = %v, want screenProgress", m.screen)
	}
	if m.progressScreen == nil {
		t.Error("progressScreen = nil after reaching progress screen; must be constructed")
	}
	if m.selections.workflowID != domain.WorkflowID(recordedWorkflowID) {
		t.Errorf("workflowID = %q, want %q", m.selections.workflowID, recordedWorkflowID)
	}
	if m.selections.task != "" {
		t.Errorf("task = %q, want empty; a resumed run reads its task from its own artifact "+
			"and must not collect one during setup", m.selections.task)
	}
}
