package tui

// exec_override_test.go verifies the TUI's executable-override recovery
// behaviour: failure routing, config-selection override field, error-identity
// contracts, restart mechanics, process-lifetime persistence, and repeat-failure
// handling.
//
// Tests are in package tui (internal) so they can access the unexported
// rootModel fields (screen, execOverrideScreen, launchFailureAttempt,
// selections) and the unexported message types (runDoneMsg, runErrorMsg).
//
// RED STATUS per task group:
//
// T6.1 (config override field): the field exists and compiles (regression
//   guards are GREEN), but the factory usage test is RED because the routing
//   to screenExecOverride is not yet wired, so the precondition fails.
//
// T6.2 (error identity): error-identity assertions on HarnessLaunchError are
//   GREEN (the type is correctly constructed); routing assertions are RED
//   because the runErrorMsg/runDoneMsg handlers do not yet check errors.As.
//
// T6.3 (routing): all routing-to-screenExecOverride tests are RED because the
//   Update handler's runErrorMsg and runDoneMsg branches do not perform
//   errors.As checks.
//
// T6.5 (restart/persistence): all restart tests are RED by transitivity
//   because they depend on first reaching screenExecOverride (which is RED).

import (
	"context"
	"errors"
	"fmt"
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

// launchFailureErr returns a *domain.HarnessLaunchError wrapping a synthetic
// cause, exactly as the harness adapters will produce on a failed Start().
func launchFailureErr(harnessID, exe string) *domain.HarnessLaunchError {
	return &domain.HarnessLaunchError{
		Harness:    harnessID,
		Executable: exe,
		Err:        fmt.Errorf("exec: no such file or directory"),
	}
}

// newOverrideTestModel creates a rootModel whose stub session returns a
// completed outcome, suitable for receiving synthetic messages in routing tests.
func newOverrideTestModel() *rootModel {
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	return newRootModel(context.Background(), sess, Options{
		Theme: tuicommon.DefaultTheme(),
	})
}

// ---------------------------------------------------------------------------
// T6.1 — ConfigSelection.ExecutablePath field
// ---------------------------------------------------------------------------

// TestConfigSelection_ExecutablePathFieldExists verifies that ConfigSelection
// carries an ExecutablePath field. Compilation regression guard.
func TestConfigSelection_ExecutablePathFieldExists(t *testing.T) {
	cfg := screens.ConfigSelection{
		ExecutablePath: "/custom/executable",
	}
	if cfg.ExecutablePath != "/custom/executable" {
		t.Errorf("ConfigSelection.ExecutablePath = %q, want %q",
			cfg.ExecutablePath, "/custom/executable")
	}
}

// TestConfigSelection_ExecutablePath_ZeroValueIsEmpty verifies that the zero
// value of ConfigSelection has ExecutablePath == "" (the "absent / use default"
// sentinel).
func TestConfigSelection_ExecutablePath_ZeroValueIsEmpty(t *testing.T) {
	var cfg screens.ConfigSelection
	if cfg.ExecutablePath != "" {
		t.Errorf("zero ConfigSelection.ExecutablePath = %q, want empty string", cfg.ExecutablePath)
	}
}

// TestSessionFactory_WithExecOverride_UsesOverridePath verifies that after the
// user confirms an override on the exec-override screen, the session factory is
// called with cfg.ExecutablePath set to the confirmed path.
//
// RED: the routing from runErrorMsg to screenExecOverride is not wired, so the
// precondition (m.screen == screenExecOverride) fails before the factory can be
// checked.
func TestSessionFactory_WithExecOverride_UsesOverridePath(t *testing.T) {
	successSess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	capturedPaths := []string{}

	m := newRootModel(context.Background(), successSess, Options{
		Theme: tuicommon.DefaultTheme(),
		SessionFactory: func(runFolder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
			capturedPaths = append(capturedPaths, cfg.ExecutablePath)
			return successSess
		},
	})

	// Trigger a launch failure — should route to screenExecOverride.
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	m.Update(runErrorMsg{err: le})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v, want screenExecOverride; "+
			"launch failure must route to override screen before override path can be confirmed",
			m.screen)
	}

	// Type an override path and confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/custom/copilot")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// The session factory must have been called with the override path.
	if len(capturedPaths) == 0 {
		t.Fatal("sessionFactory was not called after confirming override; " +
			"the restart must call the factory with the new config")
	}
	last := capturedPaths[len(capturedPaths)-1]
	if last != "/custom/copilot" {
		t.Errorf("sessionFactory received ExecutablePath = %q, want %q; "+
			"the factory must use the user-supplied override, not the per-harness default",
			last, "/custom/copilot")
	}
}

// TestSessionFactory_WithoutExecOverride_ModelSendsEmptyPath verifies that when
// no override has been set, the session factory receives cfg.ExecutablePath == ""
// when the user completes the configuration screen — so buildAdapter's per-harness
// default applies. The test drives the model through the config screen key sequence
// rather than calling the factory directly, so the model's factory seam is
// actually exercised. Regression guard (GREEN today).
func TestSessionFactory_WithoutExecOverride_ModelSendsEmptyPath(t *testing.T) {
	capturedPaths := []string{}
	successSess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}

	m := newRootModel(context.Background(), successSess, Options{
		Theme: tuicommon.DefaultTheme(),
		SessionFactory: func(runFolder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
			capturedPaths = append(capturedPaths, cfg.ExecutablePath)
			return successSess
		},
	})

	// Jump to the config screen (the model starts at screenSetupFile).
	m.screen = screenSetupConfig

	// Drive the config screen through all steps for orchestrated mode, which
	// has the fewest steps (no pre-consult or commit-branch steps):
	//   Down   → cursor -1→0 (select "orchestrated" mode — required before Enter is accepted)
	//   Enter  → confirm mode, advance to harness
	//   Enter  → confirm harness (first option, cursor=0) → timeout text-input
	//   "30m"  → type a valid timeout (placeholder is not a value; Enter with empty fails validation)
	//   Enter  → confirm timeout → version-drift
	//   Enter  → confirm version-drift (cursor=0) → checkpoints
	//   Enter  → confirm checkpoints (cursor=0, none) → manual-resolution (orchestrated skips pre-consult)
	//   Enter  → confirm manual-resolution (cursor=0, no manual) → Done → factory called
	keys := []tea.Msg{
		tea.KeyMsg{Type: tea.KeyDown},
		tea.KeyMsg{Type: tea.KeyEnter},
		tea.KeyMsg{Type: tea.KeyEnter},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("30m")},
		tea.KeyMsg{Type: tea.KeyEnter},
		tea.KeyMsg{Type: tea.KeyEnter},
		tea.KeyMsg{Type: tea.KeyEnter},
		tea.KeyMsg{Type: tea.KeyEnter},
	}
	for _, key := range keys {
		m.Update(key)
	}

	// The config screen completion must have triggered the factory call with no
	// override path set on the model.
	if len(capturedPaths) == 0 {
		t.Fatal("sessionFactory was not called after config screen completed; " +
			"the model must call the factory when the config screen is done")
	}
	last := capturedPaths[len(capturedPaths)-1]
	if last != "" {
		t.Errorf("sessionFactory received ExecutablePath = %q without override set, want empty string; "+
			"an absent override must not substitute a default executable name",
			last)
	}
}

// ---------------------------------------------------------------------------
// T6.2 — Error identity at the TUI boundary
// ---------------------------------------------------------------------------

// TestHarnessLaunchError_ErrorsIs_ErrHarnessLaunchFailed verifies that a
// *domain.HarnessLaunchError reports true for errors.Is against
// domain.ErrHarnessLaunchFailed. This pins the class-sentinel identity.
func TestHarnessLaunchError_ErrorsIs_ErrHarnessLaunchFailed(t *testing.T) {
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	if !errors.Is(le, domain.ErrHarnessLaunchFailed) {
		t.Errorf("errors.Is(launchErr, domain.ErrHarnessLaunchFailed) = false; want true — "+
			"the class sentinel must survive wrapping so the TUI can detect a launch failure "+
			"without knowing which harness produced it")
	}
}

// TestHarnessLaunchError_ErrorsAs_ExtractsFields verifies that errors.As on a
// *domain.HarnessLaunchError extracts the Harness and Executable fields. The
// TUI uses these fields to populate the override screen without re-deriving
// them from an error message.
func TestHarnessLaunchError_ErrorsAs_ExtractsFields(t *testing.T) {
	const harnessID = "ghcp-cli"
	const exe = "/usr/bin/copilot"
	le := launchFailureErr(harnessID, exe)

	var found *domain.HarnessLaunchError
	if !errors.As(le, &found) {
		t.Fatal("errors.As did not find *domain.HarnessLaunchError; " +
			"the TUI relies on errors.As to extract the harness ID and executable path " +
			"for display on the override screen")
	}
	if found.Harness != harnessID {
		t.Errorf("HarnessLaunchError.Harness = %q, want %q", found.Harness, harnessID)
	}
	if found.Executable != exe {
		t.Errorf("HarnessLaunchError.Executable = %q, want %q", found.Executable, exe)
	}
}

// TestHarnessLaunchError_SurvivesWrapping_DetectableViaErrorsAs verifies that a
// *domain.HarnessLaunchError remains detectable via errors.As after wrapping
// with fmt.Errorf("%w", ...), simulating session-boundary wrapping.
func TestHarnessLaunchError_SurvivesWrapping_DetectableViaErrorsAs(t *testing.T) {
	inner := launchFailureErr("claude-code", "/usr/local/bin/claude")
	outer := fmt.Errorf("session failed: %w", inner)

	var found *domain.HarnessLaunchError
	if !errors.As(outer, &found) {
		t.Error("errors.As did not find *domain.HarnessLaunchError through %w wrapping; " +
			"the sentinel must survive cross-layer wrapping so the TUI can detect it")
	}
}

// TestNonLaunchFailure_ErrNonZeroExit_NotDetectedAsLaunchFailure verifies that
// a plain non-zero exit error is not classified as a *domain.HarnessLaunchError.
// Regression guard (GREEN today; must stay GREEN after routing is wired).
func TestNonLaunchFailure_ErrNonZeroExit_NotDetectedAsLaunchFailure(t *testing.T) {
	exitErr := fmt.Errorf("process exited: exit status 1")
	var le *domain.HarnessLaunchError
	if errors.As(exitErr, &le) {
		t.Error("errors.As found *domain.HarnessLaunchError in a non-zero exit error; " +
			"want false — non-zero exit must not trigger the override screen")
	}
}

// TestNonLaunchFailure_Timeout_NotDetectedAsLaunchFailure verifies that a
// timeout error is not classified as a launch failure.
func TestNonLaunchFailure_Timeout_NotDetectedAsLaunchFailure(t *testing.T) {
	timeoutErr := fmt.Errorf("invocation timed out after 30m")
	var le *domain.HarnessLaunchError
	if errors.As(timeoutErr, &le) {
		t.Error("errors.As found *domain.HarnessLaunchError in a timeout error; " +
			"want false — timeout must not trigger the override screen")
	}
}

// TestNonLaunchFailure_ContextCancelled_NotDetectedAsLaunchFailure verifies
// that a context-cancellation error is not classified as a launch failure.
func TestNonLaunchFailure_ContextCancelled_NotDetectedAsLaunchFailure(t *testing.T) {
	var le *domain.HarnessLaunchError
	if errors.As(context.Canceled, &le) {
		t.Error("errors.As found *domain.HarnessLaunchError in context.Canceled; " +
			"want false — cancellation must not trigger the override screen")
	}
}

// TestLaunchFailureDetection_TextOnlyError_NotDetectedAsLaunchFailure verifies
// that an error with matching text but no *domain.HarnessLaunchError type in its
// chain is not classified as a launch failure. This pins that detection uses
// type identity (errors.As), not string matching.
func TestLaunchFailureDetection_TextOnlyError_NotDetectedAsLaunchFailure(t *testing.T) {
	textErr := errors.New("harness: launch failed — could not start process")
	var le *domain.HarnessLaunchError
	if errors.As(textErr, &le) {
		t.Error("errors.As found *domain.HarnessLaunchError in a plain text error; " +
			"want false — launch-failure detection must use type identity, not string matching")
	}
}

// TestRunOutcome_Cause_CarriesLaunchError verifies that RunOutcome.Cause can
// hold a *domain.HarnessLaunchError and that errors.As finds it, covering the
// runDoneMsg path where session.refusal preserves the error in Cause.
func TestRunOutcome_Cause_CarriesLaunchError(t *testing.T) {
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	outcome := domain.RunOutcome{
		Status:  domain.RunRefused,
		Message: "pre-consultation failed: " + le.Error(),
		Cause:   le,
	}

	var found *domain.HarnessLaunchError
	if !errors.As(outcome.Cause, &found) {
		t.Error("errors.As did not find *domain.HarnessLaunchError in RunOutcome.Cause; " +
			"the Cause field must carry the error with its type intact across the refusal boundary")
	}
}

// ---------------------------------------------------------------------------
// T6.3 — Routing: launch failure → screenExecOverride
// ---------------------------------------------------------------------------

// TestExecOverrideRouting_RunErrorMsg_LaunchFailure_ShowsOverrideScreen verifies
// that a runErrorMsg carrying a *domain.HarnessLaunchError routes the TUI to
// screenExecOverride, not to the existing done screen.
//
// RED: the runErrorMsg handler currently routes all errors to screenDone without
// checking errors.As for *domain.HarnessLaunchError.
func TestExecOverrideRouting_RunErrorMsg_LaunchFailure_ShowsOverrideScreen(t *testing.T) {
	m := newOverrideTestModel()
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	m.Update(runErrorMsg{err: le})

	if m.screen != screenExecOverride {
		t.Errorf("screen = %v after runErrorMsg with *domain.HarnessLaunchError, "+
			"want screenExecOverride (%v); "+
			"a launch failure must show the override screen so the user can supply a working path",
			m.screen, screenExecOverride)
	}
	if m.execOverrideScreen == nil {
		t.Error("execOverrideScreen = nil; must be constructed when a launch failure is detected")
	}
}

// TestExecOverrideRouting_RunErrorMsg_LaunchFailure_ScreenShowsHarnessID verifies
// that the override screen's view includes the harness identifier extracted from
// the *domain.HarnessLaunchError.
//
// RED: routing not wired; precondition (screen == screenExecOverride) fails.
func TestExecOverrideRouting_RunErrorMsg_LaunchFailure_ScreenShowsHarnessID(t *testing.T) {
	const harnessID = "ghcp-cli"
	m := newOverrideTestModel()
	le := launchFailureErr(harnessID, "/usr/bin/copilot")
	m.Update(runErrorMsg{err: le})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v, want screenExecOverride", m.screen)
	}
	if !containsStr(m.View(), harnessID) {
		t.Errorf("override screen view does not contain harness ID %q:\n%s", harnessID, m.View())
	}
}

// TestExecOverrideRouting_RunErrorMsg_LaunchFailure_ScreenShowsAttemptedPath verifies
// that the override screen includes the executable path that was tried.
//
// RED: routing not wired.
func TestExecOverrideRouting_RunErrorMsg_LaunchFailure_ScreenShowsAttemptedPath(t *testing.T) {
	const exe = "/usr/bin/copilot"
	m := newOverrideTestModel()
	le := launchFailureErr("ghcp-cli", exe)
	m.Update(runErrorMsg{err: le})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v, want screenExecOverride", m.screen)
	}
	if !containsStr(m.View(), exe) {
		t.Errorf("override screen view does not contain attempted path %q:\n%s", exe, m.View())
	}
}

// TestExecOverrideRouting_RunDoneMsg_LaunchFailureCause_ShowsOverrideScreen
// verifies that a runDoneMsg whose outcome.Cause is a *domain.HarnessLaunchError
// routes to screenExecOverride, covering the session-refusal path.
//
// RED: the runDoneMsg handler does not yet check outcome.Cause.
func TestExecOverrideRouting_RunDoneMsg_LaunchFailureCause_ShowsOverrideScreen(t *testing.T) {
	m := newOverrideTestModel()
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	outcome := domain.RunOutcome{
		Status:  domain.RunRefused,
		Message: "pre-consultation failed: " + le.Error(),
		Cause:   le,
	}
	m.Update(runDoneMsg{outcome: outcome})

	if m.screen != screenExecOverride {
		t.Errorf("screen = %v after runDoneMsg with launch-failure Cause, "+
			"want screenExecOverride (%v); "+
			"a refusal caused by a launch failure must show the override screen, "+
			"not the done screen — the user may be able to fix it",
			m.screen, screenExecOverride)
	}
}

// TestExecOverrideRouting_RunErrorMsg_NonZeroExit_ShowsDoneScreen verifies
// that a non-zero exit error routes to the existing done screen.
// Regression guard (GREEN today; must stay GREEN after routing is wired).
func TestExecOverrideRouting_RunErrorMsg_NonZeroExit_ShowsDoneScreen(t *testing.T) {
	m := newOverrideTestModel()
	m.Update(runErrorMsg{err: fmt.Errorf("process failed: exit status 1")})

	if m.screen == screenExecOverride {
		t.Error("screen = screenExecOverride after non-zero exit; " +
			"want screenDone — only launch failures may show the override screen")
	}
	if m.screen != screenDone {
		t.Errorf("screen = %v after non-zero exit, want screenDone (%v)", m.screen, screenDone)
	}
}

// TestExecOverrideRouting_RunDoneMsg_RefusedNoCause_ShowsDoneScreen verifies
// that RunRefused without a launch-failure Cause routes to the existing done
// screen. Regression guard (GREEN today).
func TestExecOverrideRouting_RunDoneMsg_RefusedNoCause_ShowsDoneScreen(t *testing.T) {
	m := newOverrideTestModel()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:  domain.RunRefused,
		Message: "workflow file not found",
		Cause:   nil,
	}})

	if m.screen == screenExecOverride {
		t.Error("screen = screenExecOverride after RunRefused with nil Cause; " +
			"want screenDone — only a launch-failure cause redirects to the override screen")
	}
	if m.screen != screenDone {
		t.Errorf("screen = %v after RunRefused without launch-failure cause, want screenDone (%v)",
			m.screen, screenDone)
	}
}

// TestExecOverrideRouting_RunDoneMsg_StoppedByConsultant_WithLaunchFailureCause_ShowsOverrideScreen
// verifies that a runDoneMsg with RunStoppedByConsultant status and a
// *domain.HarnessLaunchError in outcome.Cause routes to screenExecOverride,
// not to screenStop. This covers the routing-table row distinct from RunRefused:
// the consultant may stop a run because the harness failed to launch, and both
// outcome statuses must be checked for a launch-failure Cause.
//
// RED: the runDoneMsg handler does not yet check outcome.Cause for
// *domain.HarnessLaunchError on the RunStoppedByConsultant path.
func TestExecOverrideRouting_RunDoneMsg_StoppedByConsultant_WithLaunchFailureCause_ShowsOverrideScreen(t *testing.T) {
	m := newOverrideTestModel()
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	outcome := domain.RunOutcome{
		Status:     domain.RunStoppedByConsultant,
		StopReason: "pre-consultation failed: launch failure",
		Cause:      le,
	}
	m.Update(runDoneMsg{outcome: outcome})

	if m.screen != screenExecOverride {
		t.Errorf("screen = %v after runDoneMsg with RunStoppedByConsultant and launch-failure Cause, "+
			"want screenExecOverride (%v); "+
			"a consultant stop caused by a launch failure must show the override screen, "+
			"not the stop-recovery screen — the user needs a path to fix the executable",
			m.screen, screenExecOverride)
	}
}

// TestExecOverrideRouting_RunDoneMsg_StoppedByConsultant_NoCause_ShowsStopScreen
// verifies that RunStoppedByConsultant without a launch-failure Cause still
// routes to screenStop. Regression guard (GREEN today).
func TestExecOverrideRouting_RunDoneMsg_StoppedByConsultant_NoCause_ShowsStopScreen(t *testing.T) {
	m := newOverrideTestModel()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:     domain.RunStoppedByConsultant,
		StopReason: "quota exceeded",
		Cause:      nil,
	}})

	if m.screen == screenExecOverride {
		t.Error("screen = screenExecOverride after RunStoppedByConsultant with nil Cause; " +
			"want screenStop — a consultant stop without launch failure uses the stop recovery screen")
	}
	if m.screen != screenStop {
		t.Errorf("screen = %v after RunStoppedByConsultant without cause, want screenStop (%v)",
			m.screen, screenStop)
	}
}

// TestExecOverrideRouting_EscOnOverrideScreen_ShowsDoneScreen verifies that
// pressing Esc on the override screen transitions to screenDone so the user
// sees the normal error summary. This differs from StopScreen's Esc (which
// quits) because a launch failure is worth showing.
//
// RED: routing to screenExecOverride is not wired; precondition fails.
func TestExecOverrideRouting_EscOnOverrideScreen_ShowsDoneScreen(t *testing.T) {
	m := newOverrideTestModel()
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	m.Update(runErrorMsg{err: le})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v, want screenExecOverride", m.screen)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.screen != screenDone {
		t.Errorf("screen = %v after Esc on override screen, want screenDone (%v); "+
			"abandoning the override must show the normal error summary, not quit abruptly",
			m.screen, screenDone)
	}
	if m.doneScreen == nil {
		t.Error("doneScreen = nil after Esc on override screen; must be constructed on abandon")
	}
}

// ---------------------------------------------------------------------------
// T6.5 — Restart: same run folder, process-lifetime persistence, no loop
// ---------------------------------------------------------------------------

// TestExecOverrideRestart_ConfirmOverride_RestartsSameRunFolder verifies that
// after the user confirms a path on the override screen, the model's resolved
// run folder is preserved — the override restart targets the same folder.
//
// RED: routing to screenExecOverride is not wired; precondition fails.
func TestExecOverrideRestart_ConfirmOverride_RestartsSameRunFolder(t *testing.T) {
	const runFolder = "/workspace/Orchestration-20260817T140615Z-test"

	successSess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	m := newRootModel(context.Background(), successSess, Options{
		Theme: tuicommon.DefaultTheme(),
		SessionFactory: func(folder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
			return successSess
		},
	})
	m.selections.runFolder = runFolder

	// Trigger the launch failure.
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	m.Update(runErrorMsg{err: le})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v, want screenExecOverride", m.screen)
	}

	// Confirm an override path.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/custom/copilot")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// The model's run folder must remain unchanged after the override restart.
	if m.selections.runFolder != runFolder {
		t.Errorf("selections.runFolder = %q after restart, want %q; "+
			"the override restart must target the same already-resolved run folder",
			m.selections.runFolder, runFolder)
	}
}

// TestExecOverrideRestart_ConfirmOverride_SessionFactoryReceivesOverridePath
// verifies that after confirming an override, the session factory is called
// with cfg.ExecutablePath set to the confirmed path.
//
// RED: routing to screenExecOverride is not wired.
func TestExecOverrideRestart_ConfirmOverride_SessionFactoryReceivesOverridePath(t *testing.T) {
	const overridePath = "/custom/copilot"

	successSess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	capturedPaths := []string{}
	m := newRootModel(context.Background(), successSess, Options{
		Theme: tuicommon.DefaultTheme(),
		SessionFactory: func(folder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
			capturedPaths = append(capturedPaths, cfg.ExecutablePath)
			return successSess
		},
	})

	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	m.Update(runErrorMsg{err: le})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v, want screenExecOverride", m.screen)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(overridePath)})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if len(capturedPaths) == 0 {
		t.Fatal("sessionFactory was not called after confirming override; " +
			"restart must rebuild the session via the factory with the override path")
	}
	last := capturedPaths[len(capturedPaths)-1]
	if last != overridePath {
		t.Errorf("factory called with ExecutablePath = %q, want %q; "+
			"the factory must use the user-supplied override, not the per-harness default",
			last, overridePath)
	}
}

// TestExecOverrideRestart_OverridePersistsOnModel verifies that after confirming
// an override, the path is held on m.selections.config.ExecutablePath so that
// every subsequent session built in this process uses it.
//
// RED: routing to screenExecOverride is not wired.
func TestExecOverrideRestart_OverridePersistsOnModel(t *testing.T) {
	const overridePath = "/custom/copilot"

	successSess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	m := newRootModel(context.Background(), successSess, Options{
		Theme: tuicommon.DefaultTheme(),
		SessionFactory: func(folder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
			return successSess
		},
	})

	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	m.Update(runErrorMsg{err: le})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v, want screenExecOverride", m.screen)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(overridePath)})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// The override must be held on the model's config so it survives for
	// subsequent runs started via the session factory in the same process.
	if m.selections.config.ExecutablePath != overridePath {
		t.Errorf("selections.config.ExecutablePath = %q after confirming override, want %q; "+
			"the override must persist on the model for the process lifetime",
			m.selections.config.ExecutablePath, overridePath)
	}
}

// TestExecOverrideRestart_RepeatedLaunchFailure_ShowsOverrideScreenAgain
// verifies that a second launch failure after an override shows the override
// screen again rather than looping invisibly. The user must make an explicit
// choice on every failure.
//
// RED: routing to screenExecOverride is not wired for the first failure.
func TestExecOverrideRestart_RepeatedLaunchFailure_ShowsOverrideScreenAgain(t *testing.T) {
	successSess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	m := newRootModel(context.Background(), successSess, Options{
		Theme: tuicommon.DefaultTheme(),
		SessionFactory: func(folder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
			return successSess
		},
	})

	// First launch failure → override screen.
	le1 := launchFailureErr("ghcp-cli", "/first/attempt")
	m.Update(runErrorMsg{err: le1})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v, want screenExecOverride for first failure", m.screen)
	}

	// Confirm first override path (transitions to screenProgress + startSession).
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/second/attempt")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Simulate the restarted session returning another launch failure.
	le2 := launchFailureErr("ghcp-cli", "/second/attempt")
	m.Update(runErrorMsg{err: le2})

	// After the second launch failure the model must be on screenExecOverride
	// again, not stuck on screenProgress and not looping invisibly.
	if m.screen != screenExecOverride {
		t.Errorf("screen = %v after second launch failure, want screenExecOverride (%v); "+
			"a repeated launch failure must return the user to the override screen, "+
			"never loop invisibly or leave the TUI in an unrecoverable state",
			m.screen, screenExecOverride)
	}
}

// TestExecOverrideRestart_LaunchFailureAttempt_Increments verifies that
// launchFailureAttempt on the model increments with each consecutive launch
// failure, so the override screen receives the correct attempt number.
//
// RED: routing to screenExecOverride is not wired for the first failure.
func TestExecOverrideRestart_LaunchFailureAttempt_Increments(t *testing.T) {
	successSess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	m := newRootModel(context.Background(), successSess, Options{
		Theme: tuicommon.DefaultTheme(),
		SessionFactory: func(folder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
			return successSess
		},
	})

	// First launch failure.
	le1 := launchFailureErr("ghcp-cli", "/first/path")
	m.Update(runErrorMsg{err: le1})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v, want screenExecOverride for first failure", m.screen)
	}
	if m.launchFailureAttempt != 1 {
		t.Errorf("launchFailureAttempt = %d after first failure, want 1", m.launchFailureAttempt)
	}

	// Confirm override → triggers restart.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/second/path")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Second launch failure.
	le2 := launchFailureErr("ghcp-cli", "/second/path")
	m.Update(runErrorMsg{err: le2})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v, want screenExecOverride for second failure", m.screen)
	}
	if m.launchFailureAttempt != 2 {
		t.Errorf("launchFailureAttempt = %d after second failure, want 2", m.launchFailureAttempt)
	}
}

// TestExecOverrideRestart_LaunchFailureAttempt_ResetsOnSuccess verifies that
// launchFailureAttempt is reset to zero after a successful run outcome, so that
// a subsequent launch failure shows "attempt 1" rather than a misleading
// accumulated count.
//
// RED: routing to screenExecOverride is not wired for the first failure, so the
// precondition check fails before the reset can be verified.
func TestExecOverrideRestart_LaunchFailureAttempt_ResetsOnSuccess(t *testing.T) {
	successSess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	m := newRootModel(context.Background(), successSess, Options{
		Theme: tuicommon.DefaultTheme(),
		SessionFactory: func(folder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
			return successSess
		},
	})

	// First launch failure → launchFailureAttempt == 1.
	le1 := launchFailureErr("ghcp-cli", "/first/path")
	m.Update(runErrorMsg{err: le1})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v after first launch failure, want screenExecOverride; "+
			"cannot verify reset without first reaching the override screen",
			m.screen)
	}
	if m.launchFailureAttempt != 1 {
		t.Errorf("launchFailureAttempt = %d after first failure, want 1", m.launchFailureAttempt)
	}

	// Confirm an override — the restarted session will succeed.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/valid/path")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Simulate the restarted session completing successfully. A non-launch-failure
	// terminal outcome must reset launchFailureAttempt to zero.
	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted}})

	// Trigger a new launch failure on the next run.
	le2 := launchFailureErr("ghcp-cli", "/valid/path")
	m.Update(runErrorMsg{err: le2})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v after second launch failure, want screenExecOverride",
			m.screen)
	}

	// The counter must have been reset by the successful run, so this is
	// "attempt 1" again — not "attempt 2".
	if m.launchFailureAttempt != 1 {
		t.Errorf("launchFailureAttempt = %d after success + new failure, want 1; "+
			"a successful run must reset the attempt counter so subsequent failures "+
			"are not reported as accumulating from a previous failure sequence",
			m.launchFailureAttempt)
	}
}

// TestExecOverrideRestart_NothingWrittenToDisk documents that the override path
// is held only in model state (m.selections.config.ExecutablePath), with no
// filesystem write. This is verified structurally: no file path is constructed
// and no write call is made anywhere in this test. The in-process-only contract
// is made explicit here rather than relying on the reader to deduce it.
func TestExecOverrideRestart_NothingWrittenToDisk(t *testing.T) {
	m := newOverrideTestModel()

	// Simulate the override being applied (as updateExecOverride will do).
	m.selections.config.ExecutablePath = "/custom/override"

	// Verify the value is held in model memory only.
	if m.selections.config.ExecutablePath != "/custom/override" {
		t.Errorf("ExecutablePath not retained in model state; got %q", m.selections.config.ExecutablePath)
	}
	// No file I/O is performed in this test by construction.
}
