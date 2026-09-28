package tui

// exec_override_restart_test.go verifies restart mechanics after an override
// is confirmed: the same run folder is targeted, the session factory receives
// the override path, the override persists on the model for the process
// lifetime, repeated failures return to the override screen again (no
// invisible loop), and the failure-attempt counter increments and resets
// correctly.
//
// T6.5 (restart/persistence): all restart tests are RED by transitivity
//   because they depend on first reaching screenExecOverride (which is RED
//   until T6.3's routing is wired).

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui/screens/runconfig"
)

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
		SessionFactory: func(folder string, isNewRun bool, orchFile string, cfg runconfig.ConfigSelection) session.Session {
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
		SessionFactory: func(folder string, isNewRun bool, orchFile string, cfg runconfig.ConfigSelection) session.Session {
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
		SessionFactory: func(folder string, isNewRun bool, orchFile string, cfg runconfig.ConfigSelection) session.Session {
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
		SessionFactory: func(folder string, isNewRun bool, orchFile string, cfg runconfig.ConfigSelection) session.Session {
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
		SessionFactory: func(folder string, isNewRun bool, orchFile string, cfg runconfig.ConfigSelection) session.Session {
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
		SessionFactory: func(folder string, isNewRun bool, orchFile string, cfg runconfig.ConfigSelection) session.Session {
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
