package tui

// flow_test.go verifies the TUI's progress screen behaviour: that it updates on step
// completion notices, and that the done screen shows the correct outcome on completion.
//
// These tests use the model/update cycle with no real terminal.

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Stub session for flow tests
// ---------------------------------------------------------------------------

type stubFlowSession struct {
	outcome domain.RunOutcome
	err     error
}

func (s *stubFlowSession) Start(_ context.Context, _ domain.RunConfig) (domain.RunOutcome, error) {
	return s.outcome, s.err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newFlowModel(outcome domain.RunOutcome) *rootModel {
	sess := &stubFlowSession{outcome: outcome}
	return newRootModel(context.Background(), sess, Options{
		Theme: tuicommon.DefaultTheme(),
	})
}

// ---------------------------------------------------------------------------
// Progress screen updates
// ---------------------------------------------------------------------------

// TestFlow_ProgressScreen_UpdatesOnStepCompletion verifies that the progress screen
// reflects step completion notices sent from the session goroutine.
func TestFlow_ProgressScreen_UpdatesOnStepCompletion(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunCompleted})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	// Simulate a step starting (row appended).
	m.progressScreen.AppendRow(progressRowForTest("planning-agent#1", "PLANNING", ""))

	// View must contain the agent identifier.
	view := m.progressScreen.View()
	if !containsStr(view, "planning-agent#1") {
		t.Errorf("progress screen does not show step identifier:\n%s", view)
	}

	// Simulate the session sending a step completion notice.
	m.Update(questionMsg{
		kind: questionNotice,
		notice: interaction.Notice{
			Level:   interaction.NoticeInfo,
			Title:   "planning-agent#1",
			Message: "phase=PLANNING stage=\"\" status=SUCCESS",
		},
	})

	// The row should now be marked complete with SUCCESS status.
	view2 := m.progressScreen.View()
	if !containsStr(view2, "planning-agent#1") {
		t.Errorf("progress screen lost step after completion notice:\n%s", view2)
	}
	if !containsStr(view2, "SUCCESS") {
		t.Errorf("progress screen does not show SUCCESS status after completion notice:\n%s", view2)
	}
}

// TestFlow_ProgressScreen_ElapsedTimeAppearsInView verifies that the progress screen
// renders some time-related text.
func TestFlow_ProgressScreen_ElapsedTimeAppearsInView(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunCompleted})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	view := m.View()
	if !containsAny(view, "elapsed", "Running") {
		t.Errorf("progress screen does not contain elapsed time or 'Running' text:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Completion outcomes
// ---------------------------------------------------------------------------

// TestFlow_CompletedOutcome_ShowsSuccessOnDoneScreen verifies that a RunCompleted outcome
// results in the done screen showing a success indication.
func TestFlow_CompletedOutcome_ShowsSuccessOnDoneScreen(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunCompleted, Message: "run completed successfully"})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "run completed successfully"}})

	if m.screen != screenDone {
		t.Fatalf("screen = %v, want screenDone", m.screen)
	}
	view := m.View()
	if !containsAny(view, "Completed", "completed", "success", "Success") {
		t.Errorf("done screen does not show success indicator:\n%s", view)
	}
}

// TestFlow_StoppedOutcome_ShowsStoppedOnDoneScreen verifies that a RunStopped outcome is
// reflected on the done screen.
func TestFlow_StoppedOutcome_ShowsStoppedOnDoneScreen(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunStopped})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped"}})

	if m.screen != screenDone {
		t.Fatalf("screen = %v, want screenDone", m.screen)
	}
	view := m.View()
	if !containsAny(view, "Stopped", "stopped", "resumable") {
		t.Errorf("done screen does not show stopped indicator:\n%s", view)
	}
}

// TestFlow_RefusedOutcome_ShowsRefusedOnDoneScreen verifies that a RunRefused outcome is
// reflected on the done screen.
func TestFlow_RefusedOutcome_ShowsRefusedOnDoneScreen(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunRefused})

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunRefused, Message: "refused"}})

	if m.screen != screenDone {
		t.Fatalf("screen = %v, want screenDone", m.screen)
	}
	view := m.View()
	if !containsAny(view, "Refused", "refused") {
		t.Errorf("done screen does not show refused indicator:\n%s", view)
	}
}

// TestFlow_FailedOutcome_ShowsFailedOnDoneScreen verifies that a RunFailed outcome
// results in the done screen showing a failure indication.
func TestFlow_FailedOutcome_ShowsFailedOnDoneScreen(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunFailed})

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunFailed, Message: "unexpected error"}})

	if m.screen != screenDone {
		t.Fatalf("screen = %v, want screenDone", m.screen)
	}
	view := m.View()
	if !containsAny(view, "Failed", "failed") {
		t.Errorf("done screen does not show failure indicator for RunFailed:\n%s", view)
	}
}

// TestFlow_DeviationUnresolvedOutcome_ShowsDeviationUnresolvedOnDoneScreen verifies that
// a RunDeviationUnresolved outcome results in the done screen showing deviation-specific text.
func TestFlow_DeviationUnresolvedOutcome_ShowsDeviationUnresolvedOnDoneScreen(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunDeviationUnresolved})

	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:  domain.RunDeviationUnresolved,
		Message: "deviation could not be resolved",
	}})

	if m.screen != screenDone {
		t.Fatalf("screen = %v, want screenDone", m.screen)
	}
	view := m.View()
	if !containsAny(view, "Deviation", "deviation", "Unresolved", "unresolved") {
		t.Errorf("done screen does not show deviation-unresolved indicator:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Ctrl+C during progress
// ---------------------------------------------------------------------------

// TestFlow_CtrlC_DuringProgress_CancelsAndQuits verifies that ctrl+c during execution
// cancels the context (which stops the session) and issues a quit command.
func TestFlow_CtrlC_DuringProgress_CancelsAndQuits(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunCompleted})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if m.ctx.Err() == nil {
		t.Error("ctx.Err() = nil after ctrl+c during progress; context must be cancelled")
	}
	if cmd == nil {
		t.Error("cmd = nil after ctrl+c during progress; want tea.Quit (non-nil)")
	}
}

// ---------------------------------------------------------------------------
// Run setup sequence
// ---------------------------------------------------------------------------

// TestFlow_SetupSequence_StartsOnHarnessScreen verifies that a fresh model starts on the
// harness selection screen, which is the first step of the setup sequence.
func TestFlow_SetupSequence_StartsOnHarnessScreen(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunCompleted})
	if m.screen != screenSetupHarness {
		t.Errorf("fresh model screen = %v, want screenSetupHarness (%v)", m.screen, screenSetupHarness)
	}
}

// TestFlow_SetupSequence_RequiredFieldsTracked verifies that the rootModel tracks the
// selections struct for all required run configuration fields.
func TestFlow_SetupSequence_RequiredFieldsTracked(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunCompleted})

	// Verify selections are zero-valued initially (no pre-fills).
	if m.selections.orchestratorFile != "" {
		t.Errorf("orchestratorFile = %q initially, want empty", m.selections.orchestratorFile)
	}
	if m.selections.workflowID != "" {
		t.Errorf("workflowID = %q initially, want empty", m.selections.workflowID)
	}
	if m.selections.task != "" {
		t.Errorf("task = %q initially, want empty", m.selections.task)
	}
}

// ---------------------------------------------------------------------------
// AppendRow via start-notice
// ---------------------------------------------------------------------------

// TestFlow_RunningNotice_AppendRowViaStartNotice verifies that a session start-notice
// with status=running causes the progress screen to append a new row via AppendRow.
// This exercises the handleQuestionMsg path added during the review-fix phase that
// distinguishes status=running (start) from any other status (completion).
func TestFlow_RunningNotice_AppendRowViaStartNotice(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunCompleted})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	// Before any notice the progress screen has no rows.
	view := m.progressScreen.View()
	if containsStr(view, "start-agent#1") {
		t.Fatal("precondition: progress screen already contains agent before notice was sent")
	}

	// Send a start-notice with status=running — mirrors what session.go sends before Harness.Invoke.
	m.Update(questionMsg{
		kind: questionNotice,
		notice: interaction.Notice{
			Level:   interaction.NoticeInfo,
			Title:   "start-agent#1",
			Message: `phase=PLANNING stage="" status=running`,
		},
	})

	view = m.progressScreen.View()
	if !containsStr(view, "start-agent#1") {
		t.Errorf("progress screen does not show agent after running-notice; AppendRow was not called:\n%s", view)
	}
}

// TestFlow_RunningNoticeThenCompletion_RowMarkedComplete verifies that a start-notice
// (status=running) followed by a completion-notice (status=SUCCESS) leaves the row
// visible and marked with the final status in the progress screen.
func TestFlow_RunningNoticeThenCompletion_RowMarkedComplete(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunCompleted})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	agentID := "sequence-agent#1"

	// Start-notice: AppendRow.
	m.Update(questionMsg{
		kind: questionNotice,
		notice: interaction.Notice{
			Level:   interaction.NoticeInfo,
			Title:   agentID,
			Message: `phase=PLANNING stage="" status=running`,
		},
	})

	// Completion-notice: CompleteRow.
	m.Update(questionMsg{
		kind: questionNotice,
		notice: interaction.Notice{
			Level:   interaction.NoticeInfo,
			Title:   agentID,
			Message: `phase=PLANNING stage="" status=SUCCESS`,
		},
	})

	view := m.progressScreen.View()
	if !containsStr(view, agentID) {
		t.Errorf("progress screen lost agent identifier after completion notice:\n%s", view)
	}
	// After CompleteRow the row status is SUCCESS; the view should contain "SUCCESS".
	if !containsStr(view, "SUCCESS") {
		t.Errorf("progress screen does not show SUCCESS status after completion notice:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// ConfigScreen infra-class step
// ---------------------------------------------------------------------------

// TestFlow_ConfigScreen_InfraClassStep_PopulatesInfraClassSelections verifies that when
// the ConfigScreen is given two same-class checkpoint agents via SetDeclaredAgents, the
// configStepInfraClass step is shown and the user's selection is captured in
// ConfigSelection.InfraClassSelections.
//
// In RED: SetDeclaredAgents stores the agents but advance() skips configStepInfraClass
// entirely (the implementation task I7.2 has not been done). After driving through all
// existing steps the ConfigScreen reaches Done() with InfraClassSelections == nil. The
// assertion that InfraClassSelections["checkpoint"] == "checkpoint-manager-git" fails.
//
// In GREEN: advance() after configStepCheckpoints transitions to configStepInfraClass
// when multiple same-class agents are declared; the user selects an agent and
// InfraClassSelections is populated before Done() is set.
func TestFlow_ConfigScreen_InfraClassStep_PopulatesInfraClassSelections(t *testing.T) {
	tuiTheme := tuicommon.DefaultTheme()
	style := stylesFromTheme(tuiTheme)
	s := screens.NewConfigScreen(80, 24, style)

	// Inject two same-class checkpoint agents. The step should be shown so the user
	// can select which one to use for this run.
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
		{Name: "checkpoint-manager-alt", Class: "checkpoint"},
	})

	// Drive through: mode (Down+Enter to select first option, orchestrated), harness
	// (enter → claude-code, always goes to timeout), timeout (type+enter), versionDrift
	// (enter), checkpoints (enter). Mode is now the first step and requires an explicit
	// cursor movement before Enter (no preselection per AC8.3).
	// After checkpoints: manual-resolution (always shown), then the infra-class step.
	s.Update(tea.KeyMsg{Type: tea.KeyDown})  // move cursor to first mode option
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm mode (orchestrated)
	driveConfigStepEnter(s)                  // harness → must advance to timeout step (not version drift)

	// After harness Enter the screen must show the invocation-timeout entry step.
	// This assertion fails until the fake-harness option is removed so that
	// advance() always proceeds to configStepHarnessTimeout.
	if !containsAny(s.View(), "timeout", "Timeout", "Invocation", "invocation") {
		t.Fatalf("after harness Enter, expected invocation-timeout step; "+
			"fake harness may still be skipping timeout:\n%s", s.View())
	}

	driveConfigStepTimeout(s) // timeout → version drift
	driveConfigStepEnter(s)   // version drift
	driveConfigStepEnter(s)   // checkpoints

	// The always-shown manual-resolution step follows checkpoints. Drive it with
	// the default selection (disabled = cursor 0, per ContractsDesign.md).
	if !s.Done() {
		driveConfigStepEnter(s) // manual-resolution: accept default (disabled)
	}

	// If the infra-class step is shown (two checkpoint agents declared), drive it
	// by selecting option 0 (checkpoint-manager-git) and confirming.
	if !s.Done() {
		driveConfigStepEnter(s) // infra-class: select first option
	}

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving through all expected steps")
	}

	sel := s.Selection()
	if sel.InfraClassSelections == nil {
		t.Error("InfraClassSelections is nil: want non-nil map populated by configStepInfraClass when multiple same-class agents are declared")
		return
	}
	if sel.InfraClassSelections["checkpoint"] != "checkpoint-manager-git" {
		t.Errorf("InfraClassSelections[checkpoint] = %q, want %q (first option selected)",
			sel.InfraClassSelections["checkpoint"], "checkpoint-manager-git")
	}
}

// TestFlow_ConfigScreen_InfraClassStep_SkippedWhenSingleAgentPerClass verifies that
// when only one agent of each gated class is declared, the configStepInfraClass step
// is skipped entirely — the ConfigScreen reaches Done() after the existing steps
// without showing an extra prompt.
//
// This test passes in both RED and GREEN: in RED the infra-class step is not shown
// (the implementation is absent), and in GREEN the implementation also skips it for
// single-agent classes. It serves as a regression guard: if a future change incorrectly
// adds the infra step for single-agent classes, Done() would not be set after the
// expected number of key presses.
func TestFlow_ConfigScreen_InfraClassStep_SkippedWhenSingleAgentPerClass(t *testing.T) {
	tuiTheme := tuicommon.DefaultTheme()
	style := stylesFromTheme(tuiTheme)
	s := screens.NewConfigScreen(80, 24, style)

	// Inject a single checkpoint agent. No selection prompt should be shown.
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
	})

	// Drive through: mode (Down+Enter to select first option, orchestrated), harness
	// (→ claude-code, timeout step always present), timeout (type+enter), versionDrift,
	// checkpoints, manual-resolution (always shown). Done() should be true after
	// manual-resolution — no infra-class step for single-agent classes.
	s.Update(tea.KeyMsg{Type: tea.KeyDown})  // move cursor to first mode option
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm mode (orchestrated)
	driveConfigStepEnter(s)                  // harness → must advance to timeout step (not version drift)

	// After harness Enter the screen must show the invocation-timeout entry step.
	// This assertion fails until the fake-harness option is removed so that
	// advance() always proceeds to configStepHarnessTimeout.
	if !containsAny(s.View(), "timeout", "Timeout", "Invocation", "invocation") {
		t.Fatalf("after harness Enter, expected invocation-timeout step; "+
			"fake harness may still be skipping timeout:\n%s", s.View())
	}

	driveConfigStepTimeout(s) // timeout → version drift
	driveConfigStepEnter(s)   // version drift
	driveConfigStepEnter(s)   // checkpoints
	driveConfigStepEnter(s)   // manual-resolution (always shown; accept default disabled)

	if !s.Done() {
		t.Error("ConfigScreen did not reach Done() after the standard steps; configStepInfraClass must be skipped when only one agent per gated class is declared")
	}

	sel := s.Selection()
	// InfraClassSelections should be nil when there is nothing to select.
	if sel.InfraClassSelections != nil {
		t.Errorf("InfraClassSelections = %v, want nil when single agent per class (auto-selected, no user prompt needed)",
			sel.InfraClassSelections)
	}
}

// driveConfigStepEnter sends one Enter key press to the ConfigScreen.
func driveConfigStepEnter(s *screens.ConfigScreen) {
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
}

// driveConfigStepTimeout types a valid invocation timeout ("30m") into the
// ConfigScreen and confirms it.  Must be called when the ConfigScreen is at
// the invocation-timeout step (configStepHarnessTimeout).
func driveConfigStepTimeout(s *screens.ConfigScreen) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func progressRowForTest(agentInstance, phase, stage string) progressRow {
	return progressRow{
		AgentInstance: agentInstance,
		Phase:         phase,
		Stage:         stage,
	}
}

// progressRow is an alias to screens.ProgressRow for use in tests.
type progressRow = screens.ProgressRow
