package tui

// restart_stopstate_test.go verifies that every session-restart path leaves the
// resumed run with a clean stop state.
//
// Two distinct pieces of state are involved, and clearing one does not clear
// the other:
//
//   - The shared stop signal. Left armed, the resumed run re-stops itself at
//     its very first dispatch checkpoint.
//   - The progress screen's own latched confirm/stop state. Two of the three
//     restart paths reuse the existing ProgressScreen instance rather than
//     building a fresh one, so a latched stop carries forward. That leaves the
//     resumed run showing a notice asserting a stop that was just cancelled,
//     and makes the stop key inert -- the confirmation gate is only entered
//     when no stop is already latched, so the whole stop affordance is silently
//     lost on the resumed run.
//
// RED: the exec-override retry path and the stop-recovery screen path do
// neither today, and no path clears the reused screen's latch. The done-screen
// continue path already disarms the signal (see
// TestResume_RunStopped_ResetsStopSignalBeforeRestart in resume_test.go) and
// already rebuilds the progress screen, so its rows in the tables below are
// regression guards rather than new failures.
//
// Live verification of a real stop followed by a real resume is out of scope
// here: it is hand-performed user acceptance, and the coverage below is its
// proxy, not its substitute.

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newRestartModel returns a rootModel wired to the given stop signal, so a test
// can observe whether a restart path disarmed it.
func newRestartModel(stopSignal *session.StopSignal) *rootModel {
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	return newRootModel(context.Background(), sess, Options{
		Theme:      tuicommon.DefaultTheme(),
		StopSignal: stopSignal,
	})
}

// armGracefulStop puts m on the progress screen and confirms a graceful stop
// there, arming both the shared signal and the screen's own latch -- the state
// a run that the user stopped leaves behind.
func armGracefulStop(t *testing.T, m *rootModel) {
	t.Helper()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	triggerGracefulStop(m)

	if !m.stopSignal.Requested() {
		t.Fatal("precondition: stopSignal.Requested() = false after a confirmed stop; " +
			"the fixture must arm the shared signal before a restart path is asserted")
	}
	if !m.progressScreen.GracefulStop() {
		t.Fatal("precondition: progressScreen.GracefulStop() = false after a confirmed stop; " +
			"the fixture must latch the screen's own stop state before a restart path is asserted")
	}
}

// restartViaDoneScreenContinue drives the done-screen continue path: a graceful
// stop reaches the done screen, and 'c' resumes the run.
func restartViaDoneScreenContinue(t *testing.T, m *rootModel) {
	t.Helper()
	sendRunStoppedDone(m)
	if m.screen != screenDone {
		t.Fatalf("precondition: screen = %v after a RunStopped outcome, want screenDone", m.screen)
	}
	pressContinue(m)
	if m.screen != screenProgress {
		t.Fatalf("precondition: screen = %v after continue, want screenProgress; "+
			"the restart did not happen, so nothing about the resumed run can be asserted", m.screen)
	}
}

// restartViaExecOverrideRetry drives the executable-override retry path: a
// harness launch failure reaches the override screen, and confirming a path
// rebuilds the session and restarts the run.
func restartViaExecOverrideRetry(t *testing.T, m *rootModel) {
	t.Helper()
	m.Update(runErrorMsg{err: &domain.HarnessLaunchError{
		Harness:    "claude-code",
		Executable: "/usr/bin/claude",
		Err:        errors.New("exec: no such file or directory"),
	}})
	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v after a harness launch failure, want screenExecOverride", m.screen)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/opt/claude")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenProgress {
		t.Fatalf("precondition: screen = %v after confirming an override path, want screenProgress; "+
			"the restart did not happen, so nothing about the resumed run can be asserted", m.screen)
	}
}

// restartViaStopRecoveryRetry drives the stop-recovery screen's Retry choice: a
// consultant-issued stop reaches the recovery screen, and Retry restarts the run.
func restartViaStopRecoveryRetry(t *testing.T, m *rootModel) {
	t.Helper()
	enterStopRecoveryScreen(t, m)
	// The cursor opens on Retry.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenProgress {
		t.Fatalf("precondition: screen = %v after choosing Retry, want screenProgress; "+
			"the restart did not happen, so nothing about the resumed run can be asserted", m.screen)
	}
}

// restartViaStopRecoveryManualDispatch drives the stop-recovery screen's other
// choice, which restarts the run the same way with manual dispatch selected.
func restartViaStopRecoveryManualDispatch(t *testing.T, m *rootModel) {
	t.Helper()
	enterStopRecoveryScreen(t, m)
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenProgress {
		t.Fatalf("precondition: screen = %v after choosing Manual dispatch, want screenProgress; "+
			"the restart did not happen, so nothing about the resumed run can be asserted", m.screen)
	}
}

func enterStopRecoveryScreen(t *testing.T, m *rootModel) {
	t.Helper()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:     domain.RunStoppedByConsultant,
		StopReason: "the consultant ended the run",
	}})
	if m.screen != screenStop {
		t.Fatalf("precondition: screen = %v after a consultant stop, want screenStop", m.screen)
	}
}

// restartPaths enumerates every way a session is rebuilt or restarted after a
// terminal outcome. Each must leave the resumed run with a clean stop state.
var restartPaths = []struct {
	name    string
	restart func(t *testing.T, m *rootModel)
}{
	{"done-screen continue", restartViaDoneScreenContinue},
	{"exec-override retry", restartViaExecOverrideRetry},
	{"stop-recovery retry", restartViaStopRecoveryRetry},
	{"stop-recovery manual dispatch", restartViaStopRecoveryManualDispatch},
}

// ---------------------------------------------------------------------------
// The shared stop signal is disarmed on every restart path
// ---------------------------------------------------------------------------

// TestRestart_DisarmsStopSignal asserts that every restart path leaves the
// shared stop signal unarmed. An armed signal is observed by the resumed
// session at its first dispatch checkpoint, which stops the run the user just
// asked to continue.
func TestRestart_DisarmsStopSignal(t *testing.T) {
	for _, path := range restartPaths {
		t.Run(path.name, func(t *testing.T) {
			stopSignal := session.NewStopSignal()
			m := newRestartModel(stopSignal)
			armGracefulStop(t, m)

			path.restart(t, m)

			if stopSignal.Requested() {
				t.Error("stopSignal.Requested() = true after the restart; want false -- " +
					"the resumed run would stop again at its first dispatch checkpoint")
			}
		})
	}
}

// TestRestart_ExecOverrideRetry_DisarmsSignalBeforeRebuildingSession asserts
// the ordering the requirement states: the signal is disarmed before the
// resumed session exists, not merely at some point during the restart. This is
// observable on the exec-override path because it rebuilds the session through
// the injected factory.
func TestRestart_ExecOverrideRetry_DisarmsSignalBeforeRebuildingSession(t *testing.T) {
	stopSignal := session.NewStopSignal()
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}

	armedAtBuild := true
	factoryCalled := false
	m := newRootModel(context.Background(), sess, Options{
		Theme:      tuicommon.DefaultTheme(),
		StopSignal: stopSignal,
		SessionFactory: func(string, bool, string, screens.ConfigSelection) session.Session {
			factoryCalled = true
			armedAtBuild = stopSignal.Requested()
			return sess
		},
	})
	armGracefulStop(t, m)

	restartViaExecOverrideRetry(t, m)

	if !factoryCalled {
		t.Fatal("precondition: the session factory was not called on the retry path; " +
			"the ordering the test exists to check never occurred")
	}
	if armedAtBuild {
		t.Error("stopSignal was still armed when the resumed session was constructed; " +
			"the disarm must precede the rebuild so the new session never observes the prior stop")
	}
}

// TestRestart_ExecOverrideRetry_WithoutProgressScreen_DisarmsSignal asserts the
// disarm is unconditional rather than a side effect of touching the progress
// screen. The retry path itself allows a nil progress screen -- it constructs
// one when absent -- so clearing the screen's state must not be a precondition
// for clearing the signal, and must not panic.
func TestRestart_ExecOverrideRetry_WithoutProgressScreen_DisarmsSignal(t *testing.T) {
	stopSignal := session.NewStopSignal()
	m := newRestartModel(stopSignal)
	stopSignal.Request()

	if m.progressScreen != nil {
		t.Fatal("precondition: progressScreen is non-nil on a fresh model; " +
			"this test covers the restart path taken before any progress screen exists")
	}

	restartViaExecOverrideRetry(t, m)

	if stopSignal.Requested() {
		t.Error("stopSignal.Requested() = true after a restart with no progress screen; " +
			"want false -- the disarm is unconditional")
	}
}

// ---------------------------------------------------------------------------
// The progress screen carries no stop state into the resumed run
// ---------------------------------------------------------------------------

// TestRestart_ProgressScreenShowsNoCarriedOverStopState asserts that the
// resumed run's progress screen makes no claim about a stop. Both the
// persistent notice and the confirmation acknowledgement assert that the run is
// stopping, which contradicts a run the user has just chosen to continue.
func TestRestart_ProgressScreenShowsNoCarriedOverStopState(t *testing.T) {
	for _, path := range restartPaths {
		t.Run(path.name, func(t *testing.T) {
			m := newRestartModel(session.NewStopSignal())
			armGracefulStop(t, m)

			if !containsStr(m.progressScreen.View(), "Stopping after current step completes") {
				t.Fatal("precondition: the stop notice is absent before the restart; " +
					"the fixture must render it so its removal is what the test observes")
			}

			path.restart(t, m)

			view := m.progressScreen.View()
			if containsStr(view, "Stopping after current step completes") {
				t.Errorf("the resumed run's progress screen still shows the stop notice:\n%s", view)
			}
			if containsStr(view, "Stop confirmed") {
				t.Errorf("the resumed run's progress screen still shows the stop acknowledgement "+
					"from the run that ended:\n%s", view)
			}
		})
	}
}

// TestRestart_StopKeyReEntersConfirmationGate asserts that the stop key works
// again on the resumed run. The gate is entered only when no stop is already
// latched, so a carried-over latch makes the key inert -- the run becomes
// unstoppable, silently, which is the failure this work exists to prevent.
func TestRestart_StopKeyReEntersConfirmationGate(t *testing.T) {
	for _, path := range restartPaths {
		t.Run(path.name, func(t *testing.T) {
			m := newRestartModel(session.NewStopSignal())
			armGracefulStop(t, m)

			path.restart(t, m)

			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

			if !m.progressScreen.ConfirmPending() {
				t.Error("ConfirmPending() = false after pressing 's' on the resumed run; want true -- " +
					"the stop key must be live again, not inert behind the previous run's latch")
			}
		})
	}
}

// TestRestart_ResumedRunStopCanBeCancelled asserts that a stop opened on the
// resumed run and then cancelled leaves the run going. On a path that carried
// the previous latch forward this fails: the screen reports a stop the user
// never confirmed, and the shared signal is armed behind it.
//
// The affirmative counterpart -- confirming a second stop and observing an
// armed signal -- is deliberately not asserted here. Carried-over state
// satisfies it vacuously, so it would pass against every defect this stage
// fixes.
func TestRestart_ResumedRunStopCanBeCancelled(t *testing.T) {
	for _, path := range restartPaths {
		t.Run(path.name, func(t *testing.T) {
			stopSignal := session.NewStopSignal()
			m := newRestartModel(stopSignal)
			armGracefulStop(t, m)

			path.restart(t, m)
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

			if m.progressScreen.GracefulStop() {
				t.Error("progressScreen.GracefulStop() = true after the resumed run's stop was " +
					"cancelled; want false -- no stop was ever confirmed on this run")
			}
			if stopSignal.Requested() {
				t.Error("stopSignal.Requested() = true after the resumed run's stop was cancelled; " +
					"want false -- a cancelled stop must leave the run going")
			}
		})
	}
}
