package tui

// stop_recovery_test.go verifies the TUI's stop recovery behaviour when the
// routing consultant returns a stop instruction (RunStoppedByConsultant).
//
// Tests are in package tui (internal) so they can access the unexported rootModel
// fields (screen, stopScreen) and the unexported screenStop constant.
//
// TDD RED: all tests in this file are currently failing because:
//   1. The rootModel.Update handler for runDoneMsg always routes to screenDone,
//      regardless of RunStoppedByConsultant status.
//   2. There is no wiring between the stop screen's choice and a retry or
//      manual-dispatch action.
// After Stage 8 implementation (I8.3, I8.4), these tests become GREEN.

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newStopRecoveryModel creates a rootModel with a stubNavSession that returns
// RunStoppedByConsultant when started. The model starts at screenSetupFile.
func newStopRecoveryModel(stopReason string) *rootModel {
	sess := &stubNavSession{
		outcome: domain.RunOutcome{
			Status:     domain.RunStoppedByConsultant,
			StopReason: stopReason,
		},
	}
	return newRootModel(context.Background(), sess, Options{
		Theme: tuicommon.DefaultTheme(),
	})
}

// newStopScreen creates a StopScreen for direct screen-level tests.
func newStopScreen(reason string) *screens.StopScreen {
	return screens.NewStopScreen(reason, 80, 24, screens.Styles{})
}

// ---------------------------------------------------------------------------
// T8.3 — StopScreen unit tests (screen package level)
// ---------------------------------------------------------------------------

// TestStopScreen_ViewContainsStopReason verifies that the stop recovery screen's
// View includes the reason string provided at construction time.
func TestStopScreen_ViewContainsStopReason(t *testing.T) {
	const reason = "The orchestrator decided to stop: quota exceeded"
	s := newStopScreen(reason)
	view := s.View()
	if !containsStr(view, reason) {
		t.Errorf("stop screen view does not contain stop reason %q:\n%s", reason, view)
	}
}

// TestStopScreen_ViewOffersRetryOption verifies that the stop recovery screen
// presents a "Retry" option.
func TestStopScreen_ViewOffersRetryOption(t *testing.T) {
	s := newStopScreen("some reason")
	view := s.View()
	if !containsAny(view, "Retry", "retry") {
		t.Errorf("stop screen view does not contain a retry option:\n%s", view)
	}
}

// TestStopScreen_ViewOffersManualDispatchOption verifies that the stop recovery
// screen presents a "Manual dispatch" option.
func TestStopScreen_ViewOffersManualDispatchOption(t *testing.T) {
	s := newStopScreen("some reason")
	view := s.View()
	if !containsAny(view, "Manual dispatch", "manual dispatch", "Manual Dispatch") {
		t.Errorf("stop screen view does not contain a manual dispatch option:\n%s", view)
	}
}

// TestStopScreen_SelectRetry_ReportsRetryChoice verifies that pressing Enter
// with the cursor on "Retry" (position 0) sets Done() and Choice() == StopChoiceRetry.
func TestStopScreen_SelectRetry_ReportsRetryChoice(t *testing.T) {
	s := newStopScreen("network timeout")

	// Cursor starts at 0 (Retry); press Enter to confirm.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter on Retry; want true")
	}
	if s.Choice() != screens.StopChoiceRetry {
		t.Errorf("Choice() = %q, want %q", s.Choice(), screens.StopChoiceRetry)
	}
}

// TestStopScreen_SelectManualDispatch_ReportsManualDispatchChoice verifies that
// pressing Down and then Enter selects "Manual dispatch" and sets
// Choice() == StopChoiceManualDispatch.
func TestStopScreen_SelectManualDispatch_ReportsManualDispatchChoice(t *testing.T) {
	s := newStopScreen("orchestrator failed")

	// Move cursor to Manual dispatch (position 1) and confirm.
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after selecting Manual dispatch; want true")
	}
	if s.Choice() != screens.StopChoiceManualDispatch {
		t.Errorf("Choice() = %q, want %q", s.Choice(), screens.StopChoiceManualDispatch)
	}
}

// TestStopScreen_EscSetsBack verifies that pressing Esc on the stop screen
// sets Back() without setting Done().
func TestStopScreen_EscSetsBack(t *testing.T) {
	s := newStopScreen("some reason")

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("Back() = false after Esc; want true")
	}
	if s.Done() {
		t.Error("Done() = true after Esc; must not be done — user dismissed without a choice")
	}
}

// TestStopScreen_CursorDoesNotWrapBelowManualDispatch verifies that pressing
// Down past "Manual dispatch" (the last option) does not wrap or move the cursor
// further. After N down-presses, Enter must still report StopChoiceManualDispatch.
func TestStopScreen_CursorDoesNotWrapBelowManualDispatch(t *testing.T) {
	s := newStopScreen("some reason")

	// Over-press Down to confirm cursor clamps at the last option.
	for i := 0; i < 5; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if s.Choice() != screens.StopChoiceManualDispatch {
		t.Errorf("Choice() = %q after over-pressing Down; want %q (cursor must clamp at last option)",
			s.Choice(), screens.StopChoiceManualDispatch)
	}
}

// ---------------------------------------------------------------------------
// T8.3 — rootModel-level stop recovery tests
// ---------------------------------------------------------------------------

// TestStopRecovery_RunStoppedByConsultant_ShowsStopScreen verifies that when
// the session returns RunStoppedByConsultant, the model transitions to
// screenStop (not screenDone) and the stopScreen field is populated.
//
// RED: the runDoneMsg handler always routes to screenDone regardless of status.
func TestStopRecovery_RunStoppedByConsultant_ShowsStopScreen(t *testing.T) {
	m := newTestModel()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:     domain.RunStoppedByConsultant,
		StopReason: "The orchestrator decided to stop",
	}})

	if m.screen != screenStop {
		t.Errorf("screen = %v after RunStoppedByConsultant, want screenStop (%v); "+
			"a consultant stop must show the stop recovery screen, not the done screen",
			m.screen, screenStop)
	}
	if m.stopScreen == nil {
		t.Error("stopScreen = nil; must be constructed when run stops with RunStoppedByConsultant")
	}
}

// TestStopRecovery_OtherStopStatuses_ShowDoneScreen verifies that stop statuses
// other than RunStoppedByConsultant (e.g. RunStopped from graceful stop) still
// route to the done screen, not the stop recovery screen.
func TestStopRecovery_OtherStopStatuses_ShowDoneScreen(t *testing.T) {
	m := newTestModel()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:  domain.RunStopped,
		Message: "gracefully stopped",
	}})

	if m.screen != screenDone {
		t.Errorf("screen = %v after RunStopped, want screenDone (%v); "+
			"only RunStoppedByConsultant should route to the stop recovery screen",
			m.screen, screenDone)
	}
}

// TestStopRecovery_StopScreen_ViewContainsStopReason verifies that the stop
// recovery screen shown by the model includes the StopReason from the outcome.
//
// RED: the model shows screenDone instead of screenStop; the view comes from
// the DoneScreen which does not include the StopReason.
func TestStopRecovery_StopScreen_ViewContainsStopReason(t *testing.T) {
	const stopReason = "Orchestrator quota exhausted; try again later"
	m := newTestModel()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:     domain.RunStoppedByConsultant,
		StopReason: stopReason,
	}})

	if m.screen != screenStop {
		t.Fatalf("screen = %v, want screenStop; cannot verify stop reason without the correct screen", m.screen)
	}

	view := m.View()
	if !containsStr(view, stopReason) {
		t.Errorf("stop recovery screen view does not contain StopReason %q:\n%s", stopReason, view)
	}
}

// TestStopRecovery_RetryChoice_ReturnsToProgressScreen verifies that after the
// user selects "Retry" on the stop screen, the model transitions away from
// screenStop and re-attempts the run — observable as the model reaching
// screenProgress (or re-starting the session).
//
// RED: updateStop does not wire the retry action into the session.
func TestStopRecovery_RetryChoice_ReturnsToProgressScreen(t *testing.T) {
	const stopReason = "routing failed"
	m := newTestModel()

	// Transition to the stop screen.
	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:     domain.RunStoppedByConsultant,
		StopReason: stopReason,
	}})

	if m.screen != screenStop {
		t.Fatalf("precondition: screen = %v, want screenStop", m.screen)
	}

	// Press Enter to select Retry (cursor is at 0 = Retry by default).
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// After confirming Retry, the model must not stay on screenStop.
	if m.screen == screenStop {
		t.Error("screen = screenStop after selecting Retry; the model must transition away from " +
			"the stop recovery screen when the user chooses to retry")
	}
}

// ---------------------------------------------------------------------------
// T8.3 — Behavioral differentiation: Retry vs Manual Dispatch
// ---------------------------------------------------------------------------

// capturingDispatchSession records every domain.RunConfig passed to Start,
// enabling tests to assert what reaches the session boundary after stop recovery.
type capturingDispatchSession struct {
	configs []domain.RunConfig
	outcome domain.RunOutcome
}

func (s *capturingDispatchSession) Start(_ context.Context, cfg domain.RunConfig) (domain.RunOutcome, error) {
	s.configs = append(s.configs, cfg)
	return s.outcome, nil
}

// newCapturingStopModel creates a rootModel whose session records every RunConfig.
func newCapturingStopModel(cap *capturingDispatchSession) *rootModel {
	return newRootModel(context.Background(), cap, Options{
		Theme: tuicommon.DefaultTheme(),
	})
}

// TestStopRecovery_ManualDispatch_SetsManualDispatchSignalInRunConfig verifies that
// when the user selects Manual Dispatch from the stop recovery screen, the RunConfig
// delivered to session.Start carries ManualDispatch==true, so the session layer
// can use ManualResolver for the first routing decision instead of the configured
// RoutingConsultant.
//
// RED: updateStop calls startSession() identically for both Retry and Manual
// Dispatch. The RunConfig constructed by startSession() never sets ManualDispatch,
// so the session layer has no mechanism to distinguish the two choices.
func TestStopRecovery_ManualDispatch_SetsManualDispatchSignalInRunConfig(t *testing.T) {
	cap := &capturingDispatchSession{
		outcome: domain.RunOutcome{Status: domain.RunCompleted},
	}
	m := newCapturingStopModel(cap)

	// Transition to the stop screen.
	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:     domain.RunStoppedByConsultant,
		StopReason: "consultant stopped",
	}})
	if m.screen != screenStop {
		t.Fatalf("precondition: screen = %v, want screenStop", m.screen)
	}

	// Choose Manual Dispatch (cursor 1) and confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Execute startSession() directly to synchronously call sess.Start and
	// capture the RunConfig it receives. (In production the Cmd returned by
	// updateStop runs this via the tea.Program goroutine; here we call it
	// directly so the test can inspect the config without spinning up a program.)
	startCmd := m.startSession()
	startCmd()

	if len(cap.configs) == 0 {
		t.Fatal("session.Start was not called after choosing Manual Dispatch; " +
			"startSession() must invoke sess.Start")
	}
	cfg := cap.configs[len(cap.configs)-1]

	// The session layer needs ManualDispatch==true to know it must use
	// ManualResolver for the first routing decision of this restart.
	if !cfg.ManualDispatch {
		t.Error("RunConfig.ManualDispatch = false after choosing Manual Dispatch; " +
			"updateStop must set the ManualDispatch signal in the config (or equivalent " +
			"mechanism) so the session uses ManualResolver for the first routing decision, " +
			"not the configured RoutingConsultant")
	}
}

// TestStopRecovery_Retry_DoesNotSetManualDispatchSignal verifies that when the user
// selects Retry from the stop recovery screen, the RunConfig delivered to
// session.Start has ManualDispatch==false. Retry re-invokes the configured
// RoutingConsultant unchanged; it must not activate the ManualResolver path.
//
// This test is expected to pass with the current (defective) implementation because
// ManualDispatch is never set. It becomes a meaningful regression guard once the
// Manual Dispatch path is fixed: the fix must not inadvertently set ManualDispatch
// for the Retry path.
func TestStopRecovery_Retry_DoesNotSetManualDispatchSignal(t *testing.T) {
	cap := &capturingDispatchSession{
		outcome: domain.RunOutcome{Status: domain.RunCompleted},
	}
	m := newCapturingStopModel(cap)

	// Transition to the stop screen.
	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:     domain.RunStoppedByConsultant,
		StopReason: "consultant stopped",
	}})
	if m.screen != screenStop {
		t.Fatalf("precondition: screen = %v, want screenStop", m.screen)
	}

	// Choose Retry (cursor 0) and confirm — no Down press needed.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Execute startSession() directly to capture the RunConfig.
	startCmd := m.startSession()
	startCmd()

	if len(cap.configs) == 0 {
		t.Fatal("session.Start was not called after choosing Retry; " +
			"startSession() must invoke sess.Start")
	}
	cfg := cap.configs[len(cap.configs)-1]

	if cfg.ManualDispatch {
		t.Error("RunConfig.ManualDispatch = true after choosing Retry; " +
			"Retry must not activate the ManualResolver path — only Manual Dispatch does")
	}
}

// TestStopRecovery_ManualDispatchChoice_TransitionsToDeviationOrProgress verifies
// that selecting "Manual dispatch" on the stop screen transitions the model to an
// interactive resolver screen (or back to progress after the decision is made).
// The exact intermediate screen depends on the implementation (deviation screen or
// question overlay), but it must not remain on screenStop.
//
// RED: updateStop does not wire the manual dispatch action.
func TestStopRecovery_ManualDispatchChoice_TransitionsAwayFromStopScreen(t *testing.T) {
	m := newTestModel()

	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:     domain.RunStoppedByConsultant,
		StopReason: "consultant stopped",
	}})

	if m.screen != screenStop {
		t.Fatalf("precondition: screen = %v, want screenStop", m.screen)
	}

	// Move cursor to Manual dispatch (position 1) and confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// After confirming Manual dispatch, the model must not remain on screenStop.
	if m.screen == screenStop {
		t.Error("screen = screenStop after selecting Manual dispatch; the model must transition " +
			"to an interactive resolver (deviation screen or question overlay)")
	}
}
