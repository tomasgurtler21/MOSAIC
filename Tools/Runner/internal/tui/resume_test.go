package tui

// resume_test.go verifies the app-level resume handler that re-enters the
// dispatch loop after a graceful stop. The handler is triggered when
// DoneScreen.Continue() is true, which can only happen when the done screen
// was constructed for a domain.RunStopped outcome.
//
// RED phase: DoneScreen has no Continue() method yet, and rootModel has no
// resume path in updateDone. Every test below fails until I3.1 (DoneScreen
// continue action) and I3.3 (app.go resume handler) are implemented.
//
// The core invariants under test:
//   - Triggering continue transitions the model from screenDone to screenProgress.
//   - Session factory is called (or skipped when nil) with the existing
//     run-identity fields, mirroring updateExecOverride's nil-check pattern.
//   - StopSignal.Reset() is called before the session is rebuilt, so the
//     prior confirmed stop does not re-arm immediately on the resumed run.
//   - m.ctx is reused unchanged -- the same pointer, not a freshly derived
//     context -- because Stage 2 stopped cancelling ctx on graceful stop,
//     making the existing ctx valid for reuse.
//   - A fresh ProgressScreen is constructed unconditionally (the old screen
//     still holds the prior run's row history and stop notice).
//   - For every non-RunStopped terminal status, the 'q' / enter / esc exit
//     path is unaffected; the continue path does not activate.

import (
	"context"
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

// newResumeModel returns a rootModel whose stub session will return the given
// outcome when started. The model is positioned at screenProgress so that
// receiving a runDoneMsg naturally routes to screenDone.
func newResumeModel(outcome domain.RunOutcome) *rootModel {
	sess := &stubNavSession{outcome: outcome}
	return newRootModel(context.Background(), sess, Options{
		Theme: tuicommon.DefaultTheme(),
	})
}

// newResumeModelWithFactory returns a rootModel whose stub session returns
// the given outcome and whose SessionFactory is the supplied function.
func newResumeModelWithFactory(
	outcome domain.RunOutcome,
	factory func(string, bool, string, screens.ConfigSelection) session.Session,
) *rootModel {
	sess := &stubNavSession{outcome: outcome}
	return newRootModel(context.Background(), sess, Options{
		Theme:          tuicommon.DefaultTheme(),
		SessionFactory: factory,
	})
}

// newResumeModelWithStopSignal returns a rootModel wired to the given
// StopSignal so tests can observe Reset() calls and check Requested().
func newResumeModelWithStopSignal(outcome domain.RunOutcome, stopSignal *session.StopSignal) *rootModel {
	sess := &stubNavSession{outcome: outcome}
	return newRootModel(context.Background(), sess, Options{
		Theme:      tuicommon.DefaultTheme(),
		StopSignal: stopSignal,
	})
}

// triggerGracefulStop arms a confirmed graceful stop on m's progress screen
// via the 's' then 'y' key sequence (matching the Stage-2 confirmation state
// machine), so that the StopSignal is armed and m.ctx is not cancelled.
func triggerGracefulStop(m *rootModel) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
}

// sendRunStoppedDone delivers a runDoneMsg with RunStopped to the model,
// which transitions it from screenProgress to screenDone.
func sendRunStoppedDone(m *rootModel) {
	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:  domain.RunStopped,
		Message: "run stopped: graceful stop confirmed",
	}})
}

// pressContinue presses 'c' on the done screen to trigger the continue action.
func pressContinue(m *rootModel) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
}

// ---------------------------------------------------------------------------
// T3.2a -- Transition to screenProgress on continue
// ---------------------------------------------------------------------------

// TestResume_RunStopped_ContinueTransitionsToProgressScreen asserts that
// pressing 'c' on a RunStopped done screen transitions the model back to
// screenProgress, re-entering the dispatch loop.
func TestResume_RunStopped_ContinueTransitionsToProgressScreen(t *testing.T) {
	m := newResumeModel(domain.RunOutcome{Status: domain.RunStopped})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	sendRunStoppedDone(m)

	if m.screen != screenDone {
		t.Fatalf("precondition: screen = %v after runDoneMsg(RunStopped), want screenDone", m.screen)
	}

	pressContinue(m)

	if m.screen != screenProgress {
		t.Errorf("screen = %v after 'c' on RunStopped done screen, want screenProgress (%v); "+
			"the resume handler must re-enter the progress screen",
			m.screen, screenProgress)
	}
}

// TestResume_RunStopped_ContinueConstructsNewProgressScreen asserts that the
// resume handler always constructs a new ProgressScreen, discarding the old
// one that still carries the prior run's rows and stop notice.
func TestResume_RunStopped_ContinueConstructsNewProgressScreen(t *testing.T) {
	m := newResumeModel(domain.RunOutcome{Status: domain.RunStopped})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	oldProgressScreen := m.progressScreen

	sendRunStoppedDone(m)
	pressContinue(m)

	if m.progressScreen == nil {
		t.Fatal("progressScreen = nil after resume; must be constructed")
	}
	if m.progressScreen == oldProgressScreen {
		t.Error("progressScreen is the same instance after resume; " +
			"the resume handler must construct a new ProgressScreen, not reuse the old one " +
			"(which still holds prior run row history and the stop notice)")
	}
}

// ---------------------------------------------------------------------------
// T3.2b -- Session factory called during resume
// ---------------------------------------------------------------------------

// TestResume_RunStopped_CallsSessionFactory asserts that the session factory is
// invoked during resume so the new run session is wired with the correct
// run-identity (runFolder, isNewRun, orchFile, config) and a fresh
// StopRequested hook via the shared StopSignal.
func TestResume_RunStopped_CallsSessionFactory(t *testing.T) {
	factoryCalled := false
	returnSess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}

	m := newResumeModelWithFactory(
		domain.RunOutcome{Status: domain.RunStopped},
		func(runFolder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
			factoryCalled = true
			return returnSess
		},
	)
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	sendRunStoppedDone(m)
	pressContinue(m)

	if !factoryCalled {
		t.Error("sessionFactory was not called during resume; " +
			"the resume handler must rebuild the session via m.sessionFactory")
	}
}

// TestResume_RunStopped_FactoryReceivesExistingRunIdentity asserts that the
// session factory is called with the model's current run-identity fields
// (runFolder, isNewRun, orchFile, config), not zero values.
func TestResume_RunStopped_FactoryReceivesExistingRunIdentity(t *testing.T) {
	const wantFolder = "/runs/Orchestration-20260830T000000Z-test"
	const wantOrchFile = "/runs/Orchestration-20260830T000000Z-test/orchestrator.md"

	var capturedFolder string
	var capturedOrchFile string
	var capturedIsNewRun bool
	returnSess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}

	m := newResumeModelWithFactory(
		domain.RunOutcome{Status: domain.RunStopped},
		func(runFolder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
			capturedFolder = runFolder
			capturedOrchFile = orchFile
			capturedIsNewRun = isNewRun
			return returnSess
		},
	)
	m.selections.runFolder = wantFolder
	m.selections.orchestratorFile = wantOrchFile
	m.selections.isNewRun = false
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	sendRunStoppedDone(m)
	pressContinue(m)

	if capturedFolder != wantFolder {
		t.Errorf("sessionFactory received runFolder = %q, want %q; "+
			"the resume handler must pass the existing run-folder, not a zero value",
			capturedFolder, wantFolder)
	}
	if capturedOrchFile != wantOrchFile {
		t.Errorf("sessionFactory received orchFile = %q, want %q",
			capturedOrchFile, wantOrchFile)
	}
	if capturedIsNewRun {
		t.Error("sessionFactory received isNewRun = true for a resumed run; want false")
	}
}

// TestResume_RunStopped_NilSessionFactory_DoesNotPanic asserts that the resume
// handler does not panic when no SessionFactory is set (the nil-factory path
// reuses m.sess directly, mirroring updateExecOverride's nil-check pattern).
func TestResume_RunStopped_NilSessionFactory_DoesNotPanic(t *testing.T) {
	m := newResumeModel(domain.RunOutcome{Status: domain.RunStopped})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	// Explicitly ensure no factory is set.
	m.sessionFactory = nil

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("resume with nil sessionFactory panicked: %v", r)
		}
	}()

	sendRunStoppedDone(m)
	pressContinue(m)

	if m.screen != screenProgress {
		t.Errorf("screen = %v after resume with nil factory, want screenProgress", m.screen)
	}
}

// ---------------------------------------------------------------------------
// T3.2c -- ctx reuse
// ---------------------------------------------------------------------------

// TestResume_RunStopped_ReusesExistingCtx asserts that m.ctx is the same
// context instance after resume as it was before the graceful stop. The resume
// handler must NOT construct a new context -- it relies on Stage 2's removal
// of the stop-time ctxCancel() call to keep the existing ctx valid and reusable.
func TestResume_RunStopped_ReusesExistingCtx(t *testing.T) {
	stopSignal := session.NewStopSignal()
	m := newResumeModelWithStopSignal(domain.RunOutcome{Status: domain.RunStopped}, stopSignal)
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	// Capture the ctx before the stop sequence.
	ctxBefore := m.ctx

	// Arm the stop signal the same way the production path does.
	triggerGracefulStop(m)

	sendRunStoppedDone(m)
	pressContinue(m)

	// m.ctx must be the same object (same pointer) -- not a new derived context.
	if m.ctx != ctxBefore {
		t.Error("m.ctx changed across the graceful-stop/resume cycle; " +
			"the resume handler must reuse the existing ctx unchanged, " +
			"not construct a new one (AC3.3)")
	}
	if m.ctx.Err() != nil {
		t.Errorf("m.ctx.Err() = %v after resume; ctx must be uncancelled and reusable "+
			"(Stage 2 must not have cancelled it on graceful stop)", m.ctx.Err())
	}
}

// ---------------------------------------------------------------------------
// T3.2d -- StopSignal.Reset() called before resume
// ---------------------------------------------------------------------------

// TestResume_RunStopped_ResetsStopSignalBeforeRestart asserts that
// StopSignal.Reset() is called as part of the resume path, so the prior
// confirmed stop does not immediately re-arm on the new run's very first
// dispatch-loop boundary check.
func TestResume_RunStopped_ResetsStopSignalBeforeRestart(t *testing.T) {
	stopSignal := session.NewStopSignal()
	m := newResumeModelWithStopSignal(domain.RunOutcome{Status: domain.RunStopped}, stopSignal)
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	// Arm the stop signal (simulating a confirmed graceful stop).
	triggerGracefulStop(m)

	if !stopSignal.Requested() {
		t.Fatal("precondition: stopSignal.Requested() = false after confirmed stop; " +
			"test setup must arm the signal before asserting Reset")
	}

	sendRunStoppedDone(m)
	pressContinue(m)

	// The resume handler must have called Reset() so Requested() is false again.
	if stopSignal.Requested() {
		t.Error("stopSignal.Requested() = true after resume; " +
			"the resume handler must call Reset() before restarting so the prior stop " +
			"does not immediately re-trigger the new run's dispatch loop")
	}
}

// ---------------------------------------------------------------------------
// T3.2e -- Non-RunStopped statuses are unaffected
// ---------------------------------------------------------------------------

// TestResume_RunCompleted_QKeyQuits asserts that the done screen for RunCompleted
// still exits (tea.Quit) on 'q', with no resume path triggered.
func TestResume_RunCompleted_QKeyQuits(t *testing.T) {
	m := newResumeModel(domain.RunOutcome{Status: domain.RunCompleted})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted}})

	if m.screen != screenDone {
		t.Fatalf("precondition: screen = %v after RunCompleted runDoneMsg, want screenDone", m.screen)
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if cmd == nil {
		t.Error("cmd = nil after 'q' on RunCompleted done screen; want tea.Quit (non-nil)")
	}
	if m.screen == screenProgress {
		t.Error("screen transitioned to screenProgress after 'q' on RunCompleted done screen; " +
			"only 'c' on RunStopped must trigger resume")
	}
}

// TestResume_RunFailed_QKeyQuits asserts that the done screen for RunFailed
// exits on 'q' and the resume path is not taken.
func TestResume_RunFailed_QKeyQuits(t *testing.T) {
	m := newResumeModel(domain.RunOutcome{Status: domain.RunFailed})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunFailed, Message: "fatal error"}})

	if m.screen != screenDone {
		t.Fatalf("precondition: screen = %v, want screenDone", m.screen)
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if cmd == nil {
		t.Error("cmd = nil after 'q' on RunFailed done screen; want tea.Quit")
	}
	if m.screen == screenProgress {
		t.Error("screen transitioned to screenProgress after 'q' on RunFailed done screen; " +
			"resume must only activate for RunStopped + 'c'")
	}
}

// TestResume_RunStopped_EnterKeyQuits_NotResume asserts that Enter on a
// RunStopped done screen still exits (Done() path), not resumes. The continue
// action requires the explicit 'c' key; Enter must not accidentally resume.
func TestResume_RunStopped_EnterKeyQuits_NotResume(t *testing.T) {
	m := newResumeModel(domain.RunOutcome{Status: domain.RunStopped})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	sendRunStoppedDone(m)

	if m.screen != screenDone {
		t.Fatalf("precondition: screen = %v, want screenDone", m.screen)
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Error("cmd = nil after Enter on RunStopped done screen; want tea.Quit "+
			"(Enter is the exit key, not the continue key)")
	}
	if m.screen == screenProgress {
		t.Error("screen transitioned to screenProgress after Enter on RunStopped done screen; " +
			"Enter must exit, not resume -- only 'c' should trigger the continue path")
	}
}
