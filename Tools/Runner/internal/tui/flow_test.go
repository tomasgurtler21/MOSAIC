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

// TestFlow_SetupSequence_StartsOnFileScreen verifies that a fresh model starts on the
// orchestrator file entry screen, which is the first step of the setup sequence.
func TestFlow_SetupSequence_StartsOnFileScreen(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunCompleted})
	if m.screen != screenSetupFile {
		t.Errorf("fresh model screen = %v, want screenSetupFile (%v)", m.screen, screenSetupFile)
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
