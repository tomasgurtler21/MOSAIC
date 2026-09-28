package tui

// exec_override_config_test.go verifies the ConfigSelection.ExecutablePath
// field and the session-factory contract for it: an empty path when no
// override is set, and the confirmed override path once one is.
//
// T6.1 (config override field): the field exists and compiles (regression
//   guards are GREEN), but the factory usage test is RED because the routing
//   to screenExecOverride is not yet wired, so the precondition fails.

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
// T6.1 — ConfigSelection.ExecutablePath field
// ---------------------------------------------------------------------------

// TestConfigSelection_ExecutablePathFieldExists verifies that ConfigSelection
// carries an ExecutablePath field. Compilation regression guard.
func TestConfigSelection_ExecutablePathFieldExists(t *testing.T) {
	cfg := runconfig.ConfigSelection{
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
	var cfg runconfig.ConfigSelection
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
		SessionFactory: func(runFolder string, isNewRun bool, orchFile string, cfg runconfig.ConfigSelection) session.Session {
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
		SessionFactory: func(runFolder string, isNewRun bool, orchFile string, cfg runconfig.ConfigSelection) session.Session {
			capturedPaths = append(capturedPaths, cfg.ExecutablePath)
			return successSess
		},
	})

	// Jump to the config screen (the model starts at screenSetupHarness).
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
