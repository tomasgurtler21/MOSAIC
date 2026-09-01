package tui

// versiondrift_wiring_test.go covers the version-drift prompt as the user
// actually meets it: through the rootModel's own construction and navigation,
// not through a ConfigScreen a test wired up by hand.
//
// The screens package already tests ConfigScreen's skip logic in isolation.
// Those tests call SetIsNewRun and SetVersionDriftInfo themselves, so they stay
// green even if nothing in the running application ever calls them. The tests
// here close that gap: they drive the real forward path (workflow selection ->
// task -> seed -> configuration) and assert on what the configuration screen
// renders, so a ConfigScreen that never receives the run mode or the workflow
// version fails them.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens"
)

// versionDriftPrompt is the prompt text the configuration screen renders when it
// asks the user whether to tolerate a workflow version mismatch.
const versionDriftPrompt = "Allow workflow version drift"

// driveModelToConfigScreen puts the model on the workflow-selection screen with a
// single workflow region, selects it, enters a task description, and confirms the
// seed screen when the run mode shows it. It returns with the model on the
// configuration screen.
func driveModelToConfigScreen(t *testing.T, m *rootModel, wf domain.WorkflowRegion) {
	t.Helper()

	style := stylesFromTheme(m.theme)
	workflows := []domain.WorkflowRegion{wf}
	m.workflows = workflows
	m.workflowScreen = screens.NewWorkflowSelectScreen(workflows, m.width, m.height, style)
	m.screen = screenSetupWorkflow

	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select the workflow
	if m.screen != screenSetupTask {
		t.Fatalf("after workflow selection: screen = %v, want screenSetupTask", m.screen)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm task
	if m.screen == screenSetupSeedInput {
		m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // blank seed is a legal confirmation
	}
	if m.screen != screenSetupConfig {
		t.Fatalf("after task/seed entry: screen = %v, want screenSetupConfig", m.screen)
	}
}

// driveConfigScreenPastTimeout answers the mode, harness, and invocation-timeout
// prompts, leaving the configuration screen on whatever step follows the timeout.
func driveConfigScreenPastTimeout(m *rootModel) {
	m.Update(tea.KeyMsg{Type: tea.KeyDown})  // move cursor to the first mode option
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm mode -> harness
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm harness -> timeout
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm timeout
}

// TestSetupFlow_NewRun_NeverPromptsForVersionDrift verifies that a new run driven
// through the model's own setup sequence never sees the version-drift prompt.
//
// A new run has no previously recorded workflow version, so there is nothing for
// the version to have drifted from and the question is meaningless. This is the
// end-to-end statement of that rule: it holds only if the ConfigScreen the model
// constructs is told which run mode it is in.
func TestSetupFlow_NewRun_NeverPromptsForVersionDrift(t *testing.T) {
	// Arrange
	m := newTestModelNewRun()
	wf := domain.WorkflowRegion{Info: domain.WorkflowInfo{ID: "test-workflow", Version: "1.0"}}
	driveModelToConfigScreen(t, m, wf)

	// Act
	driveConfigScreenPastTimeout(m)

	// Assert
	if view := m.configScreen.View(); containsStr(view, versionDriftPrompt) {
		t.Errorf("the setup sequence asked a new run to allow workflow version drift; "+
			"a new run has no recorded version, so the prompt must never appear. View:\n%s", view)
	}
}

// TestSetupFlow_ResumedRun_WorkflowVersionUnmatchedByRecord_PromptsForVersionDrift verifies
// that a resumed run whose recorded version does not match the version declared by the
// selected workflow is asked to allow the drift.
//
// The run in this test has no recorded workflow version while the workflow declares
// "1.0". One value present and one absent counts as a mismatch, deliberately, because
// treating an unknown recorded version as agreement would let a silently changed
// workflow resume unchallenged.
//
// This is the counterpart to the new-run test: it fails if the model never passes the
// selected workflow's version to the configuration screen, because two absent versions
// compare as matching and the prompt is skipped.
//
// It reaches configuration the way a resumed run actually does -- by answering the
// harness question and being carried past the workflow and task screens -- rather than
// through the new run's longer path. That matters here more than anywhere else: version
// drift is only meaningful for a resumed run, and the run mode and both versions were
// historically handed to the configuration screen from inside the workflow screen's
// completion branch, which is precisely the branch a resumed run no longer enters. If
// that wiring is left behind when the skip is added, this test is the one that notices.
func TestSetupFlow_ResumedRun_WorkflowVersionUnmatchedByRecord_PromptsForVersionDrift(t *testing.T) {
	// Arrange
	m := newResumedRunModel(t)
	sendKey(m, tea.KeyEnter) // answer the harness question
	if m.screen != screenSetupConfig {
		t.Fatalf("after the harness question on a resumed run: screen = %v, want screenSetupConfig", m.screen)
	}

	// Act
	driveConfigScreenPastTimeout(m)

	// Assert
	if view := m.configScreen.View(); !containsStr(view, versionDriftPrompt) {
		t.Errorf("the setup sequence did not ask a resumed run about a workflow version it has "+
			"no matching record of; the run mode and the current workflow's version must reach "+
			"the configuration screen on the resume path, not only on the new-run path. View:\n%s", view)
	}
}
